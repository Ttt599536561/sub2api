//go:build unit

package service

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestPaidMonthlyQuotaAdvancesOnlyLegacyWindows(t *testing.T) {
	start := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	legacyExpiry := start.Add(90 * 24 * time.Hour)
	limit, oldCap := 2240.0, 1120.0
	sub := UserSubscription{
		StartsAt: start, ExpiresAt: start.Add(120 * 24 * time.Hour),
		MonthlyWindowStart: &start, MonthlyUsageUSD: 1440,
		PaidMonthlyLimitUSD: &limit, PaidMonthlyLegacyExpiresAt: &legacyExpiry, PaidMonthlyLegacyLimitUSD: &oldCap,
	}

	nextStart, added, ok := sub.MonthlyWindowAdvanceAt(start.Add(95 * 24 * time.Hour))
	require.True(t, ok)
	require.Equal(t, start.Add(60*24*time.Hour), nextStart)
	require.Equal(t, 2240.0, added)
	sub.ApplyMonthlyWindowAdvance(nextStart, added)
	require.Equal(t, 4480.0, *sub.PaidMonthlyLimitUSD)
	require.Equal(t, 1440.0, sub.MonthlyUsageUSD)

	_, _, ok = sub.MonthlyWindowAdvanceAt(start.Add(105 * 24 * time.Hour))
	require.False(t, ok, "the renewal must not add another allowance at the original expiry")
	require.Equal(t, sub.ExpiresAt, *sub.MonthlyResetTime())
}

func TestPaidMonthlyQuotaIsAbsoluteAndLeavesOtherCaps(t *testing.T) {
	daily, weekly, monthly, paid := 20.0, 60.0, 100.0, 2240.0
	group := &Group{DailyLimitUSD: &daily, WeeklyLimitUSD: &weekly, MonthlyLimitUSD: &monthly}
	sub := &UserSubscription{PaidMonthlyLimitUSD: &paid, MonthlyUsageUSD: 1120}

	effective := sub.EffectiveGroup(group)
	require.Equal(t, 2240.0, *effective.MonthlyLimitUSD)
	require.Equal(t, 2240.0, *sub.EffectiveGroup(effective).MonthlyLimitUSD)
	require.Equal(t, 100.0, *group.MonthlyLimitUSD)
	require.Equal(t, 20.0, *effective.DailyLimitUSD)
	require.Equal(t, 60.0, *effective.WeeklyLimitUSD)
	require.True(t, sub.CheckMonthlyLimit(group, 1120))
	require.False(t, sub.CheckMonthlyLimit(group, 1120.01))

	paid = 0
	sub.MonthlyUsageUSD = 0
	require.False(t, sub.CheckMonthlyLimit(group, 0), "an exhausted paid cap cannot be treated as unlimited")
	group.DailyLimitUSD, group.WeeklyLimitUSD = nil, nil
	require.Zero(t, sub.BaseQuotaRemaining(group))
}

func TestPaidMonthlyQuotaAdmissionAndDailyResetUsePaidCap(t *testing.T) {
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	daily, groupMonthly, paid := 10.0, 50.0, 200.0
	group := &Group{
		Status: StatusActive, SubscriptionType: SubscriptionTypeSubscription, AllowSubscriptionDayReset: true,
		DailyLimitUSD: &daily, MonthlyLimitUSD: &groupMonthly,
	}
	sub := &UserSubscription{
		Status: SubscriptionStatusActive, StartsAt: now.Add(-time.Hour), ExpiresAt: now.Add(168 * time.Hour),
		Group: group, User: &User{Status: StatusActive}, PaidMonthlyLimitUSD: &paid, MonthlyUsageUSD: 80, DailyUsageUSD: 9,
	}
	require.NoError(t, checkAuthoritativeSubscriptionLimits(sub, group, now))
	require.NoError(t, ValidateDailyReset(sub, now, 0, false))

	sub.MonthlyUsageUSD = paid
	require.ErrorIs(t, checkAuthoritativeSubscriptionLimits(sub, group, now), ErrMonthlyLimitExceeded)
	require.ErrorIs(t, ValidateDailyReset(sub, now, 0, false), ErrResetMonthlyLimit)
}

func TestPaidMonthlyQuotaLockedMaintenancePreservesUsedQuota(t *testing.T) {
	start := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	legacyExpiry := start.Add(60 * 24 * time.Hour)
	limit, oldCap := 100.0, 50.0
	sub := &UserSubscription{
		StartsAt: start, ExpiresAt: start.Add(90 * 24 * time.Hour),
		MonthlyWindowStart: &start, MonthlyUsageUSD: 70,
		PaidMonthlyLimitUSD: &limit, PaidMonthlyLegacyExpiresAt: &legacyExpiry, PaidMonthlyLegacyLimitUSD: &oldCap,
	}
	firstLegacyWindow := start.Add(30 * 24 * time.Hour)
	require.Equal(t, firstLegacyWindow, *sub.MonthlyResetTime())
	require.True(t, sub.MaintainUsageWindowsAt(firstLegacyWindow))
	require.Equal(t, firstLegacyWindow, *sub.MonthlyWindowStart)
	require.Equal(t, 150.0, *sub.PaidMonthlyLimitUSD)
	require.Equal(t, 70.0, sub.MonthlyUsageUSD)
	require.False(t, sub.MaintainUsageWindowsAt(legacyExpiry))
	require.EqualValues(t, 1, sub.DailyResetVersion)
}
