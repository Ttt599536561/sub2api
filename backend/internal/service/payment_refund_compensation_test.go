//go:build unit

package service

import (
	"context"
	"errors"
	"testing"
	"time"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/ent/schema/mixins"
	"github.com/Wei-Shaw/sub2api/internal/payment"
	"github.com/dgraph-io/ristretto"
	"github.com/stretchr/testify/require"
)

// SQLite cannot SELECT FOR UPDATE; this adapter keeps real Ent transactions and
// writes while exposing the same locked snapshot and version changes as the
// production subscription repository. PostgreSQL lock coverage lives in the
// repository integration suite.
type refundCompensationSubscriptionRepository struct {
	refundRevocationSubscriptionRepository
	refreshError error
	wrote        bool
	lockReads    int
}

func (r *refundCompensationSubscriptionRepository) snapshot(ctx context.Context, id int64) (*UserSubscription, error) {
	sub, err := r.txClient(ctx).UserSubscription.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	return &UserSubscription{ID: sub.ID, UserID: sub.UserID, GroupID: sub.GroupID,
		StartsAt: sub.StartsAt, ExpiresAt: sub.ExpiresAt, Status: sub.Status,
		DailyWindowStart: sub.DailyWindowStart, WeeklyWindowStart: sub.WeeklyWindowStart, MonthlyWindowStart: sub.MonthlyWindowStart,
		DailyUsageUSD: sub.DailyUsageUsd, WeeklyUsageUSD: sub.WeeklyUsageUsd, MonthlyUsageUSD: sub.MonthlyUsageUsd,
		AutoDailyResetEnabled: sub.AutoDailyResetEnabled, PreserveCalendarDailyReset: sub.PreserveCalendarDailyReset,
		DailyResetVersion: sub.DailyResetVersion, DeletedAt: sub.DeletedAt}, nil
}
func (r *refundCompensationSubscriptionRepository) GetByID(ctx context.Context, id int64) (*UserSubscription, error) {
	if r.wrote && r.refreshError != nil {
		return nil, r.refreshError
	}
	return r.snapshot(ctx, id)
}
func (r *refundCompensationSubscriptionRepository) GetByIDForUpdate(ctx context.Context, id int64) (*UserSubscription, error) {
	r.lockReads++
	return r.snapshot(ctx, id)
}
func (r *refundCompensationSubscriptionRepository) ExtendExpiry(ctx context.Context, id int64, expiry time.Time) error {
	_, err := r.txClient(ctx).UserSubscription.UpdateOneID(id).SetExpiresAt(expiry).AddDailyResetVersion(1).Save(ctx)
	if err == nil {
		r.wrote = true
	}
	return err
}
func (r *refundCompensationSubscriptionRepository) Update(ctx context.Context, sub *UserSubscription) error {
	_, err := r.txClient(ctx).UserSubscription.UpdateOneID(sub.ID).SetStartsAt(sub.StartsAt).SetExpiresAt(sub.ExpiresAt).
		SetStatus(sub.Status).SetNillableDailyWindowStart(sub.DailyWindowStart).SetNillableWeeklyWindowStart(sub.WeeklyWindowStart).
		SetNillableMonthlyWindowStart(sub.MonthlyWindowStart).SetDailyUsageUsd(sub.DailyUsageUSD).SetWeeklyUsageUsd(sub.WeeklyUsageUSD).
		SetMonthlyUsageUsd(sub.MonthlyUsageUSD).SetAutoDailyResetEnabled(sub.AutoDailyResetEnabled).
		SetPreserveCalendarDailyReset(sub.PreserveCalendarDailyReset).AddDailyResetVersion(1).Save(ctx)
	if err == nil {
		r.wrote = true
	}
	return err
}
func (r *refundCompensationSubscriptionRepository) UpdateStatus(ctx context.Context, id int64, status string) error {
	_, err := r.txClient(ctx).UserSubscription.UpdateOneID(id).SetStatus(status).AddDailyResetVersion(1).Save(ctx)
	return err
}

func refundCompensationFixture(t *testing.T) (*PaymentService, *dbent.PaymentOrder, *dbent.UserSubscription, *refundRevocationProvider, *refundCompensationSubscriptionRepository, *time.Time) {
	t.Helper()
	svc, order, original, provider := refundRevocationFixture(t)
	now := time.Now().UTC().Truncate(time.Microsecond)
	original, err := svc.entClient.UserSubscription.UpdateOneID(original.ID).
		SetExpiresAt(now.AddDate(0, 0, 7).Add(time.Second)).SetMonthlyUsageUsd(11).Save(context.Background())
	require.NoError(t, err)
	repo := &refundCompensationSubscriptionRepository{refundRevocationSubscriptionRepository: refundRevocationSubscriptionRepository{
		refundWelfareSubscriptionRepository{client: svc.entClient, subID: original.ID}}}
	svc.subscriptionSvc.userSubRepo = repo
	svc.subscriptionSvc.now = func() time.Time { return now }
	return svc, order, original, provider, repo, &now
}
func assertRefundCompensationTerm(t *testing.T, expected, actual *dbent.UserSubscription, expiry time.Time) {
	t.Helper()
	require.True(t, expiry.Equal(actual.ExpiresAt), "compensation must add only deducted days to the current expiry")
	require.True(t, expected.StartsAt.Equal(actual.StartsAt), "compensation must preserve the current term")
	require.Equal(t, expected.Status, actual.Status)
	require.Equal(t, expected.DailyUsageUsd, actual.DailyUsageUsd)
	require.Equal(t, expected.WeeklyUsageUsd, actual.WeeklyUsageUsd)
	require.Equal(t, expected.MonthlyUsageUsd, actual.MonthlyUsageUsd)
	require.Equal(t, expected.AutoDailyResetEnabled, actual.AutoDailyResetEnabled)
	require.Equal(t, expected.PreserveCalendarDailyReset, actual.PreserveCalendarDailyReset)
	require.True(t, expected.DailyWindowStart.Equal(*actual.DailyWindowStart))
	require.Equal(t, expected.WeeklyWindowStart, actual.WeeklyWindowStart)
	require.Equal(t, expected.MonthlyWindowStart, actual.MonthlyWindowStart)
}

func TestRefundShorteningCompensationAcrossExpiryPreservesTerm(t *testing.T) {
	for _, status := range []string{payment.ProviderStatusFailed, payment.ProviderStatusPending} {
		t.Run(status, func(t *testing.T) {
			ctx := context.Background()
			svc, order, original, provider, repo, now := refundCompensationFixture(t)
			provider.refundStatus = status
			provider.beforeReturn = func() { *now = now.Add(2 * time.Second) }
			plan, early, err := svc.PrepareRefund(ctx, order.ID, 25, "cross expiry", false, true)
			require.NoError(t, err)
			require.Nil(t, early)
			result, err := svc.ExecuteRefund(ctx, plan)
			require.NoError(t, err)
			require.False(t, result.Success)
			actual, err := svc.entClient.UserSubscription.Get(ctx, original.ID)
			require.NoError(t, err)
			assertRefundCompensationTerm(t, original, actual, original.ExpiresAt)
			require.GreaterOrEqual(t, repo.lockReads, 2, "both deduction and compensation need a locked current snapshot")
			beforeRepeat := actual.DailyResetVersion
			require.True(t, svc.RollbackRefund(ctx, plan, nil))
			actual, err = svc.entClient.UserSubscription.Get(ctx, original.ID)
			require.NoError(t, err)
			require.Equal(t, beforeRepeat, actual.DailyResetVersion, "the same completed compensation must be a no-op")
			if status == payment.ProviderStatusPending {
				detail, err := svc.latestRefundPendingDetail(ctx, order.ID)
				require.NoError(t, err)
				require.True(t, detail.DeductionRollbackOK)
				require.True(t, detail.shouldDeduct())
			}
		})
	}
}

func TestRefundShorteningCompensationPreservesConcurrentChanges(t *testing.T) {
	for _, status := range []string{payment.ProviderStatusFailed, payment.ProviderStatusPending} {
		for _, change := range []string{"paid_reset", "active_renewal", "expired_renewal", "suspension"} {
			t.Run(status+"/"+change, func(t *testing.T) {
				ctx := context.Background()
				svc, order, original, provider, _, now := refundCompensationFixture(t)
				provider.refundStatus = status
				var concurrent *dbent.UserSubscription
				provider.beforeReturn = func() {
					current, err := svc.entClient.UserSubscription.Get(ctx, original.ID)
					require.NoError(t, err)
					switch change {
					case "paid_reset":
						// A paid reset occurred while the provider was in flight.
						// Its consumed day and updated preference must survive.
						_, err = svc.entClient.UserSubscription.UpdateOneID(original.ID).
							SetExpiresAt(current.ExpiresAt.Add(-24 * time.Hour)).SetDailyUsageUsd(0).
							SetAutoDailyResetEnabled(false).AddDailyResetVersion(1).Save(ctx)
						require.NoError(t, err)
						*now = now.Add(2 * time.Second)
					case "active_renewal":
						_, err = svc.subscriptionSvc.ExtendSubscription(ctx, original.ID, 2)
						require.NoError(t, err)
						*now = now.Add(2 * time.Second)
					case "expired_renewal":
						*now = now.Add(2 * time.Second)
						_, err = svc.subscriptionSvc.ExtendSubscription(ctx, original.ID, 2)
						require.NoError(t, err)
					case "suspension":
						_, err = svc.entClient.UserSubscription.UpdateOneID(original.ID).
							SetStatus(SubscriptionStatusSuspended).SetWeeklyUsageUsd(19).AddDailyResetVersion(1).Save(ctx)
						require.NoError(t, err)
						*now = now.Add(2 * time.Second)
					}
					concurrent, err = svc.entClient.UserSubscription.Get(ctx, original.ID)
					require.NoError(t, err)
				}
				plan, early, err := svc.PrepareRefund(ctx, order.ID, 25, "concurrent change", false, true)
				require.NoError(t, err)
				require.Nil(t, early)
				_, err = svc.ExecuteRefund(ctx, plan)
				require.NoError(t, err)
				actual, err := svc.entClient.UserSubscription.Get(ctx, original.ID)
				require.NoError(t, err)
				assertRefundCompensationTerm(t, concurrent, actual, concurrent.ExpiresAt.AddDate(0, 0, 7))
				require.Greater(t, actual.DailyResetVersion, concurrent.DailyResetVersion)
			})
		}
	}
}

func TestRefundShorteningCompensationRefreshFailureRollsBack(t *testing.T) {
	for _, status := range []string{payment.ProviderStatusFailed, payment.ProviderStatusPending} {
		t.Run(status, func(t *testing.T) {
			ctx := context.Background()
			svc, order, original, provider, repo, now := refundCompensationFixture(t)
			provider.refundStatus = status
			provider.beforeReturn = func() {
				*now = now.Add(2 * time.Second)
				repo.refreshError = errors.New("compensation refresh unavailable")
			}
			plan, early, err := svc.PrepareRefund(ctx, order.ID, 25, "refresh failure", false, true)
			require.NoError(t, err)
			require.Nil(t, early)
			_, _ = svc.ExecuteRefund(ctx, plan)
			actual, err := svc.entClient.UserSubscription.Get(ctx, original.ID)
			require.NoError(t, err)
			assertRefundCompensationTerm(t, original, actual, original.ExpiresAt.AddDate(0, 0, -7))
			require.True(t, svc.hasAuditLog(ctx, order.ID, "REFUND_ROLLBACK_FAILED"))
			require.Equal(t, 7, plan.SubDaysToDeduct, "failed compensation remains available for reconciliation")
		})
	}
}

func TestRefundShorteningCompensationDoesNotUndoConcurrentDeletion(t *testing.T) {
	ctx := context.Background()
	svc, order, original, provider, _, now := refundCompensationFixture(t)
	provider.refundStatus = payment.ProviderStatusFailed
	provider.beforeReturn = func() {
		*now = now.Add(2 * time.Second)
		require.NoError(t, svc.subscriptionSvc.RevokeSubscription(ctx, original.ID))
	}
	plan, early, err := svc.PrepareRefund(ctx, order.ID, 25, "deleted meanwhile", false, true)
	require.NoError(t, err)
	require.Nil(t, early)
	_, err = svc.ExecuteRefund(ctx, plan)
	require.Error(t, err)
	actual, err := svc.entClient.UserSubscription.Get(mixins.SkipSoftDelete(ctx), original.ID)
	require.NoError(t, err)
	require.NotNil(t, actual.DeletedAt)
	require.True(t, original.ExpiresAt.AddDate(0, 0, -7).Equal(actual.ExpiresAt))
	require.True(t, svc.hasAuditLog(ctx, order.ID, "REFUND_ROLLBACK_FAILED"))
}

func TestRefundShorteningCompensationCapsExpiryAndInvalidatesCache(t *testing.T) {
	ctx := context.Background()
	svc, order, original, provider, _, _ := refundCompensationFixture(t)
	cache, err := ristretto.NewCache(&ristretto.Config{NumCounters: 1000, MaxCost: 100, BufferItems: 64})
	require.NoError(t, err)
	t.Cleanup(cache.Close)
	svc.subscriptionSvc.subCacheL1 = cache
	key := subCacheKey(original.UserID, original.GroupID)
	provider.refundStatus = payment.ProviderStatusFailed
	provider.beforeReturn = func() {
		_, err := svc.entClient.UserSubscription.UpdateOneID(original.ID).SetExpiresAt(MaxExpiresAt.AddDate(0, 0, -3)).Save(ctx)
		require.NoError(t, err)
		require.True(t, cache.SetWithTTL(key, &UserSubscription{ID: original.ID, ExpiresAt: original.ExpiresAt}, 1, time.Minute))
		cache.Wait()
	}
	plan, early, err := svc.PrepareRefund(ctx, order.ID, 25, "cap and cache", false, true)
	require.NoError(t, err)
	require.Nil(t, early)
	_, err = svc.ExecuteRefund(ctx, plan)
	require.NoError(t, err)
	actual, err := svc.entClient.UserSubscription.Get(ctx, original.ID)
	require.NoError(t, err)
	assertRefundCompensationTerm(t, original, actual, MaxExpiresAt)
	cache.Wait()
	_, cached := cache.Get(key)
	require.False(t, cached, "committed compensation must synchronously evict old expiry")
}

func TestRefundShorteningCompensationPreservesElapsedTerm(t *testing.T) {
	for _, elapsedOriginal := range []bool{false, true} {
		name := "reactivate restored validity"
		if elapsedOriginal {
			name = "original validity also elapsed"
		}
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			svc, order, original, provider, _, now := refundCompensationFixture(t)
			provider.refundStatus = payment.ProviderStatusFailed
			provider.beforeReturn = func() {
				*now = now.Add(2 * time.Second)
				if elapsedOriginal {
					*now = original.ExpiresAt.Add(time.Second)
				}
				_, err := svc.entClient.UserSubscription.UpdateOneID(original.ID).SetStatus(SubscriptionStatusExpired).Save(ctx)
				require.NoError(t, err)
			}
			plan, early, err := svc.PrepareRefund(ctx, order.ID, 25, "expiry status", false, true)
			require.NoError(t, err)
			require.Nil(t, early)
			_, err = svc.ExecuteRefund(ctx, plan)
			require.NoError(t, err)
			actual, err := svc.entClient.UserSubscription.Get(ctx, original.ID)
			require.NoError(t, err)
			expected := *original
			if elapsedOriginal {
				expected.Status = SubscriptionStatusExpired
			}
			assertRefundCompensationTerm(t, &expected, actual, original.ExpiresAt)
		})
	}
}

func TestRefundShorteningCompensationUsesCallerTransaction(t *testing.T) {
	ctx := context.Background()
	svc, _, original, _, _, _ := refundCompensationFixture(t)
	tx, err := svc.entClient.Tx(ctx)
	require.NoError(t, err)
	defer tx.Rollback()
	txCtx := dbent.NewTxContext(ctx, tx)
	restored, err := svc.subscriptionSvc.restoreSubscriptionDaysAfterRefund(txCtx, original.ID, 7)
	require.NoError(t, err)
	require.True(t, original.ExpiresAt.AddDate(0, 0, 7).Equal(restored.ExpiresAt))
	require.NoError(t, tx.Rollback())
	actual, err := svc.entClient.UserSubscription.Get(ctx, original.ID)
	require.NoError(t, err)
	assertRefundCompensationTerm(t, original, actual, original.ExpiresAt)
	require.Equal(t, original.DailyResetVersion, actual.DailyResetVersion)
}

type refundCompensationCache struct {
	billingCacheWorkerStub
	invalidateError error
	publishError    error
	invalidations   int
	publications    int
	afterCommit     func()
}

func (c *refundCompensationCache) InvalidateSubscriptionCache(context.Context, int64, int64) error {
	c.invalidations++
	if c.afterCommit != nil {
		c.afterCommit()
	}
	return c.invalidateError
}
func (c *refundCompensationCache) PublishSubscriptionCacheInvalidation(context.Context, string) error {
	c.publications++
	return c.publishError
}
func (*refundCompensationCache) SubscribeSubscriptionCacheInvalidation(context.Context, func(string)) error {
	return nil
}

func TestRefundShorteningCompensationCacheFailureDoesNotRepeatDays(t *testing.T) {
	for _, failure := range []string{"invalidate", "publish"} {
		t.Run(failure, func(t *testing.T) {
			ctx := context.Background()
			svc, order, original, provider, _, now := refundCompensationFixture(t)
			provider.refundStatus = payment.ProviderStatusFailed
			cache := &refundCompensationCache{afterCommit: func() {
				persisted, err := svc.entClient.UserSubscription.Get(ctx, original.ID)
				require.NoError(t, err)
				require.True(t, original.ExpiresAt.Equal(persisted.ExpiresAt), "cache eviction must read the committed compensation")
			}}
			if failure == "invalidate" {
				cache.invalidateError = errors.New("cache invalidation unavailable")
			} else {
				cache.publishError = errors.New("cache publication unavailable")
			}
			provider.beforeReturn = func() {
				*now = now.Add(2 * time.Second)
				svc.subscriptionSvc.billingCacheService = &BillingCacheService{cache: cache}
			}
			plan, early, err := svc.PrepareRefund(ctx, order.ID, 25, "cache failure", false, true)
			require.NoError(t, err)
			require.Nil(t, early)
			_, err = svc.ExecuteRefund(ctx, plan)
			require.NoError(t, err)
			require.Zero(t, plan.SubDaysToDeduct, "committed compensation is complete even if caches fail")
			require.Equal(t, 1, cache.invalidations)
			if failure == "publish" {
				require.Equal(t, 1, cache.publications)
			}
			require.True(t, svc.RollbackRefund(ctx, plan, nil))
			actual, err := svc.entClient.UserSubscription.Get(ctx, original.ID)
			require.NoError(t, err)
			assertRefundCompensationTerm(t, original, actual, original.ExpiresAt)
			require.False(t, svc.hasAuditLog(ctx, order.ID, "REFUND_ROLLBACK_FAILED"))
		})
	}
}
