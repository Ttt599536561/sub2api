//go:build integration

package repository

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/google/uuid"
	"github.com/lib/pq"
	"github.com/stretchr/testify/require"
)

type adminDeletionPausedKeyRepository struct {
	service.APIKeyRepository
	keyLocked       chan struct{}
	allowDeleteUser chan struct{}
	signalOnce      sync.Once
}

func (r *adminDeletionPausedKeyRepository) DeleteWithAudit(ctx context.Context, id int64) error {
	if err := r.APIKeyRepository.DeleteWithAudit(ctx, id); err != nil {
		return err
	}
	r.signalOnce.Do(func() { close(r.keyLocked) })
	select {
	case <-r.allowDeleteUser:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
func adminDeletionTestService(client *dbent.Client, userRepo service.UserRepository, keyRepo service.APIKeyRepository) service.AdminService {
	return service.NewAdminService(nil, userRepo, nil, nil, nil, keyRepo,
		nil, nil, nil, nil, nil, nil, nil, client, nil, nil, nil, nil, nil, nil, nil, nil, nil)
}

func TestAdminDeleteUserSerializesWithBalanceBilling(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	client := testEntClient(t)
	user := mustCreateUser(t, client, &service.User{Email: "delete-billing-" + uuid.NewString() + "@test.invalid", Balance: 100})
	key := mustCreateApiKey(t, client, &service.APIKey{UserID: user.ID, Key: "sk-delete-billing-" + uuid.NewString(), Quota: 100})
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cleanupCancel()
		_, err := integrationDB.ExecContext(cleanupCtx, "DELETE FROM api_keys WHERE id=$1", key.ID)
		require.NoError(t, err)
		_, err = integrationDB.ExecContext(cleanupCtx, "DELETE FROM users WHERE id=$1", user.ID)
		require.NoError(t, err)
	})
	billingTx, err := integrationDB.BeginTx(ctx, nil)
	require.NoError(t, err)
	defer billingTx.Rollback()
	var billingPID int
	require.NoError(t, billingTx.QueryRowContext(ctx, "SELECT pg_backend_pid()").Scan(&billingPID))
	balance, sufficient, err := deductUsageBillingBalance(ctx, billingTx, user.ID, 1)
	require.NoError(t, err)
	require.True(t, sufficient)
	require.Equal(t, 99.0, balance)
	keyRepo := &adminDeletionPausedKeyRepository{APIKeyRepository: NewAPIKeyRepository(client, integrationDB), keyLocked: make(chan struct{}), allowDeleteUser: make(chan struct{})}
	var releaseOnce sync.Once
	releaseDeletion := func() { releaseOnce.Do(func() { close(keyRepo.allowDeleteUser) }) }
	svc := adminDeletionTestService(client, NewUserRepository(client, integrationDB), keyRepo)
	deletionResult := make(chan error, 1)
	go func() { deletionResult <- svc.DeleteUser(ctx, user.ID) }()
	var deletionReceived, billingReceived, billingStarted bool
	billingResult := make(chan error, 1)
	defer func() {
		releaseDeletion()
		cancel()
		_ = billingTx.Rollback()
		if !deletionReceived {
			select {
			case err := <-deletionResult:
				t.Errorf("deletion ended during test cleanup: %v", err)
			case <-time.After(time.Second):
				t.Error("deletion goroutine did not exit after cancellation")
			}
		}
		if billingStarted && !billingReceived {
			select {
			case err := <-billingResult:
				t.Errorf("billing ended during test cleanup: %v", err)
			case <-time.After(time.Second):
				t.Error("billing goroutine did not exit after cancellation")
			}
		}
	}()
	// Old code pauses after holding the key; fixed code waits on the user before
	// touching any key. Observe either real state, not the SQL spelling.
	keyFirst := false
	observed := false
	for !observed && ctx.Err() == nil {
		select {
		case <-keyRepo.keyLocked:
			keyFirst = true
			observed = true
		default:
		}
		if observed {
			break
		}
		var userBlocked bool
		err = integrationDB.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE $1=ANY(pg_blocking_pids(pid)))`, billingPID).Scan(&userBlocked)
		require.NoError(t, err)
		if userBlocked {
			observed = true
			break
		}
		select {
		case <-ctx.Done():
		case <-time.After(10 * time.Millisecond):
		}
	}
	require.True(t, observed, "deletion must either hold its key or wait on the billing user lock")
	billingStarted = true
	go func() {
		_, err := incrementUsageBillingAPIKeyQuota(ctx, billingTx, key.ID, 1)
		if err == nil {
			err = billingTx.Commit()
		} else if rollbackErr := billingTx.Rollback(); rollbackErr != nil && !errors.Is(rollbackErr, sql.ErrTxDone) {
			err = errors.Join(err, rollbackErr)
		}
		billingResult <- err
	}()
	if keyFirst {
		// Let billing wait on the held key before deletion requests its user
		// lock. This deterministically exposes the previous two-row lock cycle.
		require.Eventually(t, func() bool {
			var blocked bool
			queryErr := integrationDB.QueryRowContext(ctx, "SELECT cardinality(pg_blocking_pids($1))>0", billingPID).Scan(&blocked)
			if queryErr != nil {
				err = queryErr
				return false
			}
			return blocked
		}, 3*time.Second, 10*time.Millisecond, "billing must wait on the deletion's key lock")
		require.NoError(t, err)
		releaseDeletion()
	}
	select {
	case err = <-billingResult:
		billingReceived = true
	case <-ctx.Done():
		err = ctx.Err()
	}
	billingErr := err
	if !keyFirst {
		releaseDeletion()
	}
	select {
	case err = <-deletionResult:
		deletionReceived = true
	case <-ctx.Done():
		err = ctx.Err()
	}
	deletionErr := err
	for operation, operationErr := range map[string]error{"billing": billingErr, "deletion": deletionErr} {
		var pgErr *pq.Error
		if errors.As(operationErr, &pgErr) {
			t.Logf("%s PostgreSQL error %s: %s", operation, pgErr.Code, pgErr.Detail)
		}
		require.NoError(t, operationErr, "%s must finish without deadlock/cancellation", operation)
	}
	var storedBalance, quotaUsed float64
	var userDeleted, keyDeleted bool
	var storedKey string
	require.NoError(t, integrationDB.QueryRowContext(ctx, "SELECT balance,deleted_at IS NOT NULL FROM users WHERE id=$1", user.ID).Scan(&storedBalance, &userDeleted))
	require.NoError(t, integrationDB.QueryRowContext(ctx, "SELECT quota_used,deleted_at IS NOT NULL,key FROM api_keys WHERE id=$1", key.ID).Scan(&quotaUsed, &keyDeleted, &storedKey))
	require.Equal(t, 99.0, storedBalance)
	require.Equal(t, 1.0, quotaUsed)
	require.True(t, userDeleted)
	require.True(t, keyDeleted)
	require.True(t, strings.HasPrefix(storedKey, "__deleted__"))
}

// The initial authorization read can become stale while deletion waits for its
// transaction lock. A concurrent promotion must be checked in that locked row.
type adminDeletionPromoteAfterReadRepository struct {
	service.UserRepository
	db *sql.DB
}

func (r *adminDeletionPromoteAfterReadRepository) GetByID(ctx context.Context, id int64) (*service.User, error) {
	user, err := r.UserRepository.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}
	_, err = r.db.ExecContext(ctx, "UPDATE users SET role=$2 WHERE id=$1", id, service.RoleAdmin)
	return user, err
}
func TestAdminDeleteUserRechecksPromotedRoleInTransaction(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	client := testEntClient(t)
	user := mustCreateUser(t, client, &service.User{Email: "delete-promoted-" + uuid.NewString() + "@test.invalid"})
	key := mustCreateApiKey(t, client, &service.APIKey{UserID: user.ID, Key: "sk-promoted-" + uuid.NewString()})
	t.Cleanup(func() {
		_, err := integrationDB.Exec("DELETE FROM api_keys WHERE id=$1", key.ID)
		require.NoError(t, err)
		_, err = integrationDB.Exec("DELETE FROM users WHERE id=$1", user.ID)
		require.NoError(t, err)
	})
	svc := adminDeletionTestService(client, &adminDeletionPromoteAfterReadRepository{UserRepository: NewUserRepository(client, integrationDB), db: integrationDB}, NewAPIKeyRepository(client, integrationDB))
	err := svc.DeleteUser(ctx, user.ID)
	require.ErrorContains(t, err, "cannot delete admin user")
	var role string
	var userDeleted, keyDeleted bool
	require.NoError(t, integrationDB.QueryRowContext(ctx, "SELECT role,deleted_at IS NOT NULL FROM users WHERE id=$1", user.ID).Scan(&role, &userDeleted))
	require.NoError(t, integrationDB.QueryRowContext(ctx, "SELECT deleted_at IS NOT NULL FROM api_keys WHERE id=$1", key.ID).Scan(&keyDeleted))
	require.Equal(t, service.RoleAdmin, role)
	require.False(t, userDeleted)
	require.False(t, keyDeleted)
}

type adminDeletionFailAfterKeyRepository struct {
	service.APIKeyRepository
	failure error
}

func (r *adminDeletionFailAfterKeyRepository) DeleteWithAudit(ctx context.Context, id int64) error {
	if err := r.APIKeyRepository.DeleteWithAudit(ctx, id); err != nil {
		return err
	}
	return r.failure
}

func TestAdminDeleteUserKeyFailureRollsBackTransaction(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	client := testEntClient(t)
	user := mustCreateUser(t, client, &service.User{Email: "delete-rollback-" + uuid.NewString() + "@test.invalid"})
	key := mustCreateApiKey(t, client, &service.APIKey{UserID: user.ID, Key: "sk-rollback-" + uuid.NewString()})
	t.Cleanup(func() {
		_, err := integrationDB.Exec("DELETE FROM api_keys WHERE id=$1", key.ID)
		require.NoError(t, err)
		_, err = integrationDB.Exec("DELETE FROM users WHERE id=$1", user.ID)
		require.NoError(t, err)
	})
	failure := errors.New("key deletion interrupted after tombstone")
	svc := adminDeletionTestService(client, NewUserRepository(client, integrationDB), &adminDeletionFailAfterKeyRepository{
		APIKeyRepository: NewAPIKeyRepository(client, integrationDB), failure: failure,
	})
	require.ErrorIs(t, svc.DeleteUser(ctx, user.ID), failure)
	var userDeleted, keyDeleted bool
	var storedKey string
	require.NoError(t, integrationDB.QueryRowContext(ctx, "SELECT deleted_at IS NOT NULL FROM users WHERE id=$1", user.ID).Scan(&userDeleted))
	require.NoError(t, integrationDB.QueryRowContext(ctx, "SELECT deleted_at IS NOT NULL,key FROM api_keys WHERE id=$1", key.ID).Scan(&keyDeleted, &storedKey))
	require.False(t, userDeleted)
	require.False(t, keyDeleted)
	require.Equal(t, key.Key, storedKey)
}

func TestAdminDeleteUserCanceledUserLockLeavesNoDeletion(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	client := testEntClient(t)
	user := mustCreateUser(t, client, &service.User{Email: "delete-cancel-" + uuid.NewString() + "@test.invalid"})
	key := mustCreateApiKey(t, client, &service.APIKey{UserID: user.ID, Key: "sk-cancel-" + uuid.NewString()})
	t.Cleanup(func() {
		_, err := integrationDB.Exec("DELETE FROM api_keys WHERE id=$1", key.ID)
		require.NoError(t, err)
		_, err = integrationDB.Exec("DELETE FROM users WHERE id=$1", user.ID)
		require.NoError(t, err)
	})
	holder, err := integrationDB.BeginTx(ctx, nil)
	require.NoError(t, err)
	defer holder.Rollback()
	var lockedID int64
	require.NoError(t, holder.QueryRowContext(ctx, "SELECT id FROM users WHERE id=$1 FOR UPDATE", user.ID).Scan(&lockedID))
	svc := adminDeletionTestService(client, NewUserRepository(client, integrationDB), NewAPIKeyRepository(client, integrationDB))
	canceledCtx, cancelDeletion := context.WithTimeout(ctx, 200*time.Millisecond)
	defer cancelDeletion()
	err = svc.DeleteUser(canceledCtx, user.ID)
	require.Error(t, err)
	require.ErrorIs(t, canceledCtx.Err(), context.DeadlineExceeded)
	var pgErr *pq.Error
	require.True(t, errors.Is(err, context.DeadlineExceeded) || (errors.As(err, &pgErr) && pgErr.Code == "57014"),
		"the blocked deletion must report its canceled query, not hide another database error: %v", err)
	require.NoError(t, holder.Rollback())
	var userDeleted, keyDeleted bool
	require.NoError(t, integrationDB.QueryRowContext(ctx, "SELECT deleted_at IS NOT NULL FROM users WHERE id=$1", user.ID).Scan(&userDeleted))
	require.NoError(t, integrationDB.QueryRowContext(ctx, "SELECT deleted_at IS NOT NULL FROM api_keys WHERE id=$1", key.ID).Scan(&keyDeleted))
	require.False(t, userDeleted)
	require.False(t, keyDeleted)
	// Cancellation must release this attempt's transaction; a normal retry can
	// subsequently take both locks and finish the unchanged deletion path.
	require.NoError(t, svc.DeleteUser(ctx, user.ID))
}
