//go:build unit

package service

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestWelfareZeroMonthlyQuotaBlocksAdmissionAndPreservesStandardZero(t *testing.T) {
	now := time.Date(2026, 11, 13, 12, 0, 0, 0, time.UTC)
	zero := 0.0
	group := &Group{MonthlyLimitUSD: &zero}
	for _, kind := range []string{SubscriptionEntitlementStandard, SubscriptionEntitlementWelfare} {
		t.Run(kind, func(t *testing.T) {
			sub := &UserSubscription{EntitlementKind: kind, WelfareMonthlyLimitUSD: &zero,
				Status: SubscriptionStatusActive, StartsAt: now.Add(-time.Hour), ExpiresAt: now.Add(time.Hour)}
			if kind == SubscriptionEntitlementWelfare {
				require.False(t, sub.CheckMonthlyLimit(group, 0))
				require.False(t, sub.CheckMonthlyLimit(group, 0.01))
				require.ErrorIs(t, checkAuthoritativeSubscriptionLimits(sub, group, now), ErrMonthlyLimitExceeded)
			} else {
				require.True(t, sub.CheckMonthlyLimit(group, 100))
				require.NoError(t, checkAuthoritativeSubscriptionLimits(sub, group, now))
			}
		})
	}
}

func TestWelfareMonthlyQuotaNewGrantAfterZeroRemainsFinite(t *testing.T) {
	zero, grant := 0.0, 30.0
	combined, err := addWelfareQuota(&zero, &grant)
	require.NoError(t, err)
	require.NotNil(t, combined)
	require.Equal(t, 30.0, *combined)
	sub := &UserSubscription{EntitlementKind: SubscriptionEntitlementWelfare,
		WelfareMonthlyLimitUSD: combined, MonthlyUsageUSD: 20}
	require.True(t, sub.CheckMonthlyLimit(&Group{}, 10))
	require.False(t, sub.CheckMonthlyLimit(&Group{}, 10.01))
}

func TestExtendedWelfareTermKeepsTotalMonthlyUsageAcrossMaintenance(t *testing.T) {
	start := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	now := start.Add(45 * 24 * time.Hour)
	limit := 30.0
	original := UserSubscription{ID: 42, EntitlementKind: SubscriptionEntitlementWelfare,
		Status: SubscriptionStatusActive, StartsAt: start, ExpiresAt: start.Add(90 * 24 * time.Hour),
		MonthlyWindowStart: &start, MonthlyUsageUSD: 35, WelfareMonthlyLimitUSD: &limit}
	require.False(t, original.NeedsMonthlyResetAt(now))
	require.False(t, original.canAutomaticallyResetMonthlyAt(now))
	require.Equal(t, original.ExpiresAt, *original.MonthlyResetTime())

	locked := original
	require.False(t, locked.MaintainUsageWindowsAt(now))
	require.Equal(t, 35.0, locked.MonthlyUsageUSD)
	require.Equal(t, start, *locked.MonthlyWindowStart)

	normalized := []UserSubscription{original}
	normalizeExpiredWindowsAt(normalized, now)
	require.Equal(t, 35.0, normalized[0].MonthlyUsageUSD)
	require.Equal(t, start, *normalized[0].MonthlyWindowStart)

	repo := &monthlyResetUserSubRepo{}
	svc := NewSubscriptionService(groupRepoNoop{}, repo, nil, nil, nil)
	svc.now = func() time.Time { return now }
	maintained := original
	needsMaintenance, err := svc.ValidateAndCheckLimits(&maintained, &Group{})
	require.False(t, needsMaintenance)
	require.ErrorIs(t, err, ErrMonthlyLimitExceeded)
	require.NoError(t, svc.CheckAndResetWindows(context.Background(), &maintained))
	require.False(t, repo.resetCalled)
	require.Equal(t, 35.0, maintained.MonthlyUsageUSD)

	standard := original
	standard.EntitlementKind = SubscriptionEntitlementStandard
	require.True(t, standard.NeedsMonthlyResetAt(now))
	require.True(t, standard.MaintainUsageWindowsAt(now))
	require.Zero(t, standard.MonthlyUsageUSD)
}

func TestWelfareZeroMonthlyProgressShowsExhaustedFiniteTotal(t *testing.T) {
	start := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	zero := 0.0
	for _, used := range []float64{0, 7} {
		sub := &UserSubscription{EntitlementKind: SubscriptionEntitlementWelfare,
			WelfareMonthlyLimitUSD: &zero, MonthlyUsageUSD: used,
			StartsAt: start, ExpiresAt: start.Add(90 * 24 * time.Hour), MonthlyWindowStart: &start}
		progress := (&SubscriptionService{}).calculateProgress(sub, &Group{})
		require.NotNil(t, progress.Monthly)
		require.Zero(t, progress.Monthly.LimitUSD)
		require.Equal(t, used, progress.Monthly.UsedUSD)
		require.Zero(t, progress.Monthly.RemainingUSD)
		require.Equal(t, 100.0, progress.Monthly.Percentage)
		require.Equal(t, sub.ExpiresAt, progress.Monthly.ResetsAt)
		_, err := json.Marshal(progress)
		require.NoError(t, err)
	}
}
