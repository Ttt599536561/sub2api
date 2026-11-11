//go:build unit

package service

import (
	"context"
	"errors"
	"regexp"
	"strings"
	"testing"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	"github.com/DATA-DOG/go-sqlmock"
	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/dgraph-io/ristretto"
	"github.com/stretchr/testify/require"
)

type redeemSubscriptionCommitRepo struct {
	userSubRepoNoop
	t      *testing.T
	locked *bool
	cache  *ristretto.Cache
	sub    *UserSubscription
}

func (r *redeemSubscriptionCommitRepo) GetByUserIDAndGroupID(context.Context, int64, int64) (*UserSubscription, error) {
	return nil, ErrSubscriptionNotFound
}
func (r *redeemSubscriptionCommitRepo) Create(ctx context.Context, sub *UserSubscription) error {
	require.NotContains(r.t, sub.Notes, "MONTH-TEST", "redeem plaintext must not enter subscription notes")
	require.Contains(r.t, sub.Notes, "#1")
	if !*r.locked {
		return errors.New("subscription creation occurred before durable user lock")
	}
	sub.ID = 7
	r.sub = sub
	tx := dbent.TxFromContext(ctx)
	require.NotNil(r.t, tx)
	tx.OnCommit(func(next dbent.Committer) dbent.Committer {
		return dbent.CommitFunc(func(ctx context.Context, tx *dbent.Tx) error {
			r.cache.Wait()
			_, exists := r.cache.Get(subCacheKey(sub.UserID, sub.GroupID))
			require.True(r.t, exists, "subscription L1 must remain intact until the outer transaction commits")
			return next.Commit(ctx, tx)
		})
	})
	return nil
}
func (r *redeemSubscriptionCommitRepo) GetByID(context.Context, int64) (*UserSubscription, error) {
	copy := *r.sub
	return &copy, nil
}

func TestRedeemSubscriptionLocksUserAndDefersCacheUntilCommit(t *testing.T) {
	for _, commitFail := range []bool{false, true} {
		t.Run(map[bool]string{false: "commit", true: "rollback"}[commitFail], func(t *testing.T) {
			locked := false
			db, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherFunc(func(expected, actual string) error {
				matched, e := regexp.MatchString(expected, actual)
				if e != nil {
					return e
				}
				if !matched {
					return errors.New("unexpected SQL")
				}
				if strings.Contains(actual, "FOR NO KEY UPDATE") {
					locked = true
				}
				return nil
			})))
			require.NoError(t, err)
			defer func() { _ = db.Close() }()
			client := dbent.NewClient(dbent.Driver(entsql.OpenDB(dialect.Postgres, db)))
			mock.ExpectBegin()
			mock.ExpectQuery(`SELECT .*"users".*FOR NO KEY UPDATE`).WithArgs(int64(11)).WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(int64(11)))
			mock.ExpectQuery(`SELECT EXISTS\(SELECT 1 FROM qq_invite_code_pool p JOIN qq_invite_rewards r`).
				WithArgs(int64(1)).WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(false))
			commit := mock.ExpectCommit()
			if commitFail {
				commit.WillReturnError(errors.New("commit unavailable"))
			}
			cache, err := ristretto.NewCache(&ristretto.Config{NumCounters: 1000, MaxCost: 100, BufferItems: 64})
			require.NoError(t, err)
			t.Cleanup(cache.Close)
			key := subCacheKey(11, 13)
			require.True(t, cache.Set(key, &UserSubscription{ID: 99}, 1))
			cache.Wait()
			repo := &redeemSubscriptionCommitRepo{t: t, locked: &locked, cache: cache}
			subSvc := &SubscriptionService{groupRepo: &subscriptionGroupRepoStub{group: &Group{ID: 13, SubscriptionType: SubscriptionTypeSubscription}}, userSubRepo: repo, subCacheL1: cache}
			groupID := int64(13)
			codeRepo := &redeemRejectRepo{code: RedeemCode{ID: 1, Code: "MONTH-TEST", Type: RedeemTypeSubscription, Status: StatusUnused, GroupID: &groupID, ValidityDays: 30}}
			auth := &mockAuthCacheInvalidator{}
			svc := NewRedeemService(codeRepo, &mockUserRepo{getByIDUser: &User{ID: 11}}, subSvc, nil, nil, client, auth, nil)
			_, err = svc.Redeem(context.Background(), 11, "MONTH-TEST")
			if commitFail {
				require.ErrorContains(t, err, "commit transaction")
			} else {
				require.NoError(t, err)
			}
			require.NoError(t, mock.ExpectationsWereMet())
			cache.Wait()
			_, exists := cache.Get(key)
			require.Equal(t, commitFail, exists, "only committed subscriptions invalidate L1")
			if commitFail {
				require.Empty(t, auth.invalidatedUserIDs)
			} else {
				require.Equal(t, []int64{11}, auth.invalidatedUserIDs)
			}
		})
	}
}

func TestRedeemSubscriptionProvenanceFailureRollsBackBeforeAssignment(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()
	client := dbent.NewClient(dbent.Driver(entsql.OpenDB(dialect.Postgres, db)))
	mock.ExpectBegin()
	mock.ExpectQuery(`SELECT .*"users".*FOR NO KEY UPDATE`).WithArgs(int64(11)).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(int64(11)))
	lookupErr := errors.New("welfare provenance unavailable")
	mock.ExpectQuery(`SELECT EXISTS\(SELECT 1 FROM qq_invite_code_pool p JOIN qq_invite_rewards r`).
		WithArgs(int64(1)).WillReturnError(lookupErr)
	mock.ExpectRollback()
	groupID := int64(13)
	codeRepo := &redeemRejectRepo{code: RedeemCode{ID: 1, Code: "PROVENANCE-FAIL", Type: RedeemTypeSubscription,
		Status: StatusUnused, GroupID: &groupID, ValidityDays: 30}}
	auth := &mockAuthCacheInvalidator{}
	// A missing subscription service makes any accidental fail-open assignment
	// observable; provenance failure must roll back before granting any term.
	svc := NewRedeemService(codeRepo, &mockUserRepo{getByIDUser: &User{ID: 11}}, nil, nil, nil, client, auth, nil)
	got, err := svc.Redeem(context.Background(), 11, codeRepo.code.Code)
	require.Nil(t, got)
	require.ErrorIs(t, err, lookupErr)
	require.ErrorContains(t, err, "verify welfare subscription provenance")
	require.Empty(t, auth.invalidatedUserIDs)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestRedeemSubscriptionPostCommitInvalidationIsSynchronous(t *testing.T) {
	cache, err := ristretto.NewCache(&ristretto.Config{NumCounters: 1000, MaxCost: 100, BufferItems: 64})
	require.NoError(t, err)
	t.Cleanup(cache.Close)
	key := subCacheKey(11, 13)
	require.True(t, cache.Set(key, &UserSubscription{ID: 99}, 1))
	cache.Wait()
	backend := &refundCompensationCache{}
	billing := &BillingCacheService{cache: backend}
	subs := &SubscriptionService{subCacheL1: cache, billingCacheService: billing}
	svc := &RedeemService{subscriptionService: subs, billingCacheService: billing}
	groupID := int64(13)
	svc.invalidateRedeemCaches(context.Background(), 11, &RedeemCode{Type: RedeemTypeSubscription, GroupID: &groupID})
	_, exists := cache.Get(key)
	require.False(t, exists)
	require.Equal(t, 1, backend.invalidations)
	require.Equal(t, 1, backend.publications)
}

func TestRedeemSubscriptionCommittedAuthInvalidationSurvivesCancellation(t *testing.T) {
	auth := &ctxRecordingAuthCacheInvalidator{}
	svc := &RedeemService{authCacheInvalidator: auth}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	groupID := int64(13)
	svc.invalidateRedeemCaches(ctx, 11, &RedeemCode{Type: RedeemTypeSubscription, GroupID: &groupID})
	require.Equal(t, []error{nil}, auth.ctxErrs)
}
