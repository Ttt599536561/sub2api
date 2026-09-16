//go:build integration

package repository

import (
	"context"
	"errors"
	"testing"
	"time"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/google/uuid"
	"github.com/lib/pq"
	"github.com/stretchr/testify/require"
)

func TestSubscriptionDailyResetRepository_UserDeletionAndBillingLockOrder(t *testing.T) {
	repo, sub, now := newDailyResetFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	client := testEntClient(t)
	key, err := client.APIKey.Create().SetUserID(sub.UserID).SetGroupID(sub.GroupID).
		SetKey(uuid.NewString()).SetName("reset-lock-order").SetQuota(100).Save(ctx)
	require.NoError(t, err)
	t.Cleanup(func() {
		_, _ = integrationDB.Exec("DELETE FROM api_keys WHERE id = $1", key.ID)
	})

	// Pause billing after its subscription update, before its API-key update.
	billingTx, err := integrationDB.BeginTx(ctx, nil)
	require.NoError(t, err)
	defer billingTx.Rollback()
	var billingPID int
	require.NoError(t, billingTx.QueryRowContext(ctx, "SELECT pg_backend_pid()").Scan(&billingPID))
	require.NoError(t, incrementUsageBillingSubscription(ctx, billingTx, sub.ID, 1))

	resetResult := make(chan error, 1)
	resetDone := make(chan struct{})
	go func() {
		_, err := repo.Apply(ctx, dailyResetCommand(sub, now))
		resetResult <- err
		close(resetDone)
	}()
	require.Eventually(t, func() bool {
		select {
		case <-resetDone:
			return true
		default:
		}
		var blocked bool
		err := integrationDB.QueryRowContext(ctx, `SELECT EXISTS (
			SELECT 1 FROM pg_stat_activity WHERE $1 = ANY(pg_blocking_pids(pid)))`, billingPID).Scan(&blocked)
		return err == nil && blocked
	}, 5*time.Second, 10*time.Millisecond, "reset should wait or release its locks after bounded contention retries")

	// Admin deletion tombstones keys before deleting the user in one transaction.
	deletionTx, err := client.Tx(ctx)
	require.NoError(t, err)
	defer deletionTx.Rollback()
	deletionCtx := dbent.NewTxContext(ctx, deletionTx)
	require.NoError(t, NewAPIKeyRepository(client, integrationDB).DeleteWithAudit(deletionCtx, key.ID))
	billingResult := make(chan error, 1)
	go func() {
		_, err := incrementUsageBillingAPIKeyQuota(ctx, billingTx, key.ID, 1)
		if err != nil {
			_ = billingTx.Rollback()
		} else {
			err = billingTx.Commit()
		}
		billingResult <- err
	}()
	require.Eventually(t, func() bool {
		var blocked bool
		err := integrationDB.QueryRowContext(ctx, "SELECT cardinality(pg_blocking_pids($1)) > 0", billingPID).Scan(&blocked)
		return err == nil && blocked
	}, 5*time.Second, 10*time.Millisecond, "billing should wait for the deleted key")

	deletionErr := NewUserRepository(client, integrationDB).Delete(deletionCtx, sub.UserID)
	if deletionErr != nil {
		_ = deletionTx.Rollback()
	} else {
		deletionErr = deletionTx.Commit()
	}
	for operation, operationErr := range map[string]error{
		"deletion": deletionErr,
		"billing":  <-billingResult,
		"reset":    <-resetResult,
	} {
		t.Logf("%s result: %v", operation, operationErr)
		var pgErr *pq.Error
		if errors.As(operationErr, &pgErr) {
			require.NotEqual(t, pq.ErrorCode("40P01"), pgErr.Code,
				"%s formed a lock cycle: %s", operation, pgErr.Detail)
		}
	}
}
