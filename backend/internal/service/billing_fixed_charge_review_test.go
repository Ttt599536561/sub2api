//go:build unit

package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

// Keep the generation contract visible: an authoritative rejection recheck must
// not repopulate Redis with a database snapshot that lacks a generation fence.
type fixedChargeReviewFencedCache struct {
	fixedChargeCacheStub
	refills int
}

func (c *fixedChargeReviewFencedCache) UserBalanceGeneration(context.Context, int64) (int64, error) {
	return 7, nil
}

func (c *fixedChargeReviewFencedCache) SetUserBalanceIfGeneration(context.Context, int64, float64, int64) (bool, error) {
	c.refills++
	return true, nil
}

func TestFixedChargeReviewRechecksUnderstatedPositiveBalance(t *testing.T) {
	for _, tc := range []struct {
		name           string
		actual         float64
		dbErr, wantErr error
	}{
		{name: "redeemed balance", actual: 10},
		{name: "actual balance insufficient", actual: 3, wantErr: ErrInsufficientBalance},
		{name: "authoritative read failed", dbErr: errors.New("database unavailable"), wantErr: ErrBillingServiceUnavailable},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cache := &fixedChargeReviewFencedCache{fixedChargeCacheStub: fixedChargeCacheStub{balance: 4}}
			repo := &welfareAuthoritativeBalanceRepo{balance: tc.actual, err: tc.dbErr}
			cfg := &config.Config{}
			cfg.Billing.InflightReservation.Enabled = true
			svc := NewBillingCacheService(cache, repo, nil, nil, nil, nil, cfg, nil)
			t.Cleanup(svc.Stop)
			user := &User{ID: 1}
			first, err := svc.ReserveFixedCharge(context.Background(), user, nil, nil, nil, 5)
			defer first.Release()
			require.ErrorIs(t, err, tc.wantErr)
			if tc.wantErr == nil {
				require.NotNil(t, first)
				second, err := svc.ReserveFixedCharge(context.Background(), user, nil, nil, nil, 5)
				defer second.Release()
				require.NoError(t, err, "fresh balance must still be checked atomically against existing reservations")
				rejected, err := svc.ReserveFixedCharge(context.Background(), user, nil, nil, nil, 1)
				defer rejected.Release()
				require.ErrorIs(t, err, ErrInsufficientBalance, "authoritative recovery must not discard the other reserved charges")
			}
			require.Positive(t, repo.calls.Load())
			svc.Stop()
			require.Zero(t, cache.refills, "a direct rejection recheck must not refill Redis")
			require.Zero(t, cache.balanceUpdates, "the legacy setter must not bypass the generation fence")
			require.Equal(t, 4.0, cache.balance)
		})
	}
}

func fixedChargeReviewSubscription(t *testing.T, oldLimit, newLimit *float64, usage float64) (*BillingCacheService, *fixedChargeCacheStub, *subscriptionAdmissionGroupRepo, *subscriptionAdmissionRepo, *APIKey, *UserSubscription) {
	t.Helper()
	now := time.Now()
	group := &Group{ID: 2, Status: StatusActive, SubscriptionType: SubscriptionTypeSubscription, DailyLimitUSD: newLimit}
	oldGroup := *group
	oldGroup.DailyLimitUSD = oldLimit
	user := &User{ID: 1, Status: StatusActive}
	current := &UserSubscription{
		ID: 3, UserID: user.ID, GroupID: group.ID, User: user, Group: group,
		Status: SubscriptionStatusActive, StartsAt: now.Add(-48 * time.Hour), ExpiresAt: now.Add(10 * 24 * time.Hour),
		DailyWindowStart: &now, WeeklyWindowStart: &now, MonthlyWindowStart: &now, DailyUsageUSD: usage,
	}
	snapshot := *current
	snapshot.Group = &oldGroup
	groupRepo := &subscriptionAdmissionGroupRepo{group: group}
	subRepo := &subscriptionAdmissionRepo{sub: current}
	cache := &fixedChargeCacheStub{balance: 1000}
	cfg := &config.Config{}
	cfg.Billing.InflightReservation.Enabled = true
	billing := NewBillingCacheService(cache, nil, subRepo, nil, nil, nil, cfg, nil)
	t.Cleanup(billing.Stop)
	billing.SetSubscriptionService(&SubscriptionService{groupRepo: groupRepo, userSubRepo: subRepo, now: func() time.Time { return now }})
	key := &APIKey{ID: 4, User: user, UserID: user.ID, GroupID: &group.ID, Group: &oldGroup}
	require.NoError(t, billing.CheckBillingEligibility(context.Background(), user, key, key.Group, &snapshot, ""))
	return billing, cache, groupRepo, subRepo, key, &snapshot
}

func TestFixedChargeReviewUsesAuthoritativeGroupLimit(t *testing.T) {
	for _, tc := range []struct {
		name               string
		oldLimit, newLimit float64
		wantErr            error
	}{
		{name: "lowered limit", oldLimit: 100, newLimit: 10, wantErr: ErrDailyLimitExceeded},
		{name: "raised limit", oldLimit: 10, newLimit: 100},
	} {
		t.Run(tc.name, func(t *testing.T) {
			svc, _, _, _, key, sub := fixedChargeReviewSubscription(t, &tc.oldLimit, &tc.newLimit, 9)
			snapshot := *sub
			res, err := svc.ReserveFixedCharge(context.Background(), key.User, key, key.Group, sub, 5)
			defer res.Release()
			require.ErrorIs(t, err, tc.wantErr)
			require.Equal(t, snapshot, *sub, "fixed-charge admission must not mutate the caller's billing snapshot")
		})
	}
}

func TestFixedChargeReviewNewLimitRegistersSubscriptionScope(t *testing.T) {
	limit := 10.0
	svc, _, _, _, key, sub := fixedChargeReviewSubscription(t, nil, &limit, 0)
	first, err := svc.ReserveFixedCharge(context.Background(), key.User, key, key.Group, sub, 6)
	defer first.Release()
	require.NoError(t, err)
	second, err := svc.ReserveFixedCharge(context.Background(), key.User, key, key.Group, sub, 6)
	defer second.Release()
	require.ErrorIs(t, err, ErrDailyLimitExceeded, "a newly configured limit must include the first unsettled charge")
}

func TestFixedChargeReviewRejectsChangedSubscriptionIdentity(t *testing.T) {
	for _, name := range []string{"replacement", "billing mode", "group unavailable"} {
		t.Run(name, func(t *testing.T) {
			limit := 100.0
			svc, cache, groupRepo, subRepo, key, sub := fixedChargeReviewSubscription(t, &limit, &limit, 0)
			wantErr := ErrSubscriptionInvalid
			switch name {
			case "replacement":
				subRepo.sub.ID++
			case "billing mode":
				groupRepo.group.SubscriptionType = SubscriptionTypeStandard
			case "group unavailable":
				groupRepo.err = errors.New("database unavailable")
				wantErr = ErrBillingServiceUnavailable
			}
			res, err := svc.ReserveFixedCharge(context.Background(), key.User, key, key.Group, sub, 5)
			defer res.Release()
			require.ErrorIs(t, err, wantErr)
			require.Empty(t, cache.reserved, "changed ownership or mode must reject before reserving")
		})
	}
}

func TestFixedChargeReviewDoesNotRepeatAdmissionReset(t *testing.T) {
	limit := 10.0
	svc, _, groupRepo, subRepo, key, sub := fixedChargeReviewSubscription(t, &limit, &limit, 0)
	now := time.Now()
	groupRepo.group.AllowSubscriptionDayReset = true
	subRepo.sub.AutoDailyResetEnabled = true
	subRepo.sub.DailyUsageUSD = limit
	resetRepo := &admissionDailyResetRepo{subRepo: subRepo, group: groupRepo.group, now: now}
	svc.SetSubscriptionService(&SubscriptionService{
		groupRepo: groupRepo, userSubRepo: subRepo, dailyResetRepo: resetRepo, now: func() time.Time { return now },
	})
	require.NoError(t, svc.CheckBillingEligibility(context.Background(), key.User, key, key.Group, sub, ""))
	require.Equal(t, 1, resetRepo.applies)
	res, err := svc.ReserveFixedCharge(context.Background(), key.User, key, key.Group, sub, 5)
	defer res.Release()
	require.NoError(t, err)
	require.Equal(t, 1, resetRepo.applies, "rechecking the reset subscription must not deduct another day")
}
