package service

import (
	"math"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/timezone"
	"github.com/stretchr/testify/require"
)

func resetTestSubscription(now time.Time) *UserSubscription {
	limit := 100.0
	start := timezone.StartOfDay(now)
	return &UserSubscription{ID: 1, UserID: 2, GroupID: 3, Status: SubscriptionStatusActive,
		StartsAt: now.Add(-48 * time.Hour), ExpiresAt: now.Add(10 * 24 * time.Hour), DailyWindowStart: &start,
		DailyUsageUSD: 60, Group: &Group{ID: 3, Status: StatusActive, SubscriptionType: SubscriptionTypeSubscription,
			AllowSubscriptionDayReset: true, DailyLimitUSD: &limit}, User: &User{ID: 2, Status: StatusActive}}
}

func TestDailyResetEligibility(t *testing.T) {
	now := time.Date(2026, 9, 6, 12, 0, 0, 0, time.UTC)
	for _, tt := range []struct {
		name   string
		change func(*UserSubscription)
		count  int
		auto   bool
		want   error
	}{
		{name: "partial manual"},
		{name: "partial automatic", auto: true, change: func(s *UserSubscription) { s.AutoDailyResetEnabled = true }, want: ErrResetNoUsage},
		{name: "automatic off", auto: true, change: func(s *UserSubscription) { s.DailyUsageUSD = 100 }, want: ErrResetNotAllowed},
		{name: "automatic exhausted", auto: true, change: func(s *UserSubscription) { s.AutoDailyResetEnabled = true; s.DailyUsageUSD = 100 }},
		{name: "empty", change: func(s *UserSubscription) { s.DailyUsageUSD = 0 }, want: ErrResetNoUsage},
		{name: "exact day", change: func(s *UserSubscription) { s.ExpiresAt = now.Add(24 * time.Hour) }, want: ErrResetInsufficientValidity},
		{name: "day plus microsecond", change: func(s *UserSubscription) { s.ExpiresAt = now.Add(24*time.Hour + time.Microsecond) }},
		{name: "count full", count: 100, want: ErrResetDailyCountLimit},
		{name: "last count", count: 99},
		{name: "weekly full", change: func(s *UserSubscription) { l := 700.0; s.Group.WeeklyLimitUSD = &l; s.WeeklyUsageUSD = l }, want: ErrResetWeeklyLimit},
		{name: "monthly full", change: func(s *UserSubscription) { l := 3000.0; s.Group.MonthlyLimitUSD = &l; s.MonthlyUsageUSD = l }, want: ErrResetMonthlyLimit},
		{name: "permission off", change: func(s *UserSubscription) { s.Group.AllowSubscriptionDayReset = false }, want: ErrResetNotAllowed},
		{name: "user disabled", change: func(s *UserSubscription) { s.User.Status = "disabled" }, want: ErrResetNotAllowed},
		{name: "user deleted", change: func(s *UserSubscription) { s.User.DeletedAt = &now }, want: ErrResetNotAllowed},
		{name: "group deleted", change: func(s *UserSubscription) { s.Group.Status = "deleted" }, want: ErrResetNotAllowed},
		{name: "not started", change: func(s *UserSubscription) { s.StartsAt = now.Add(time.Hour) }, want: ErrResetNotAllowed},
		{name: "NaN usage", change: func(s *UserSubscription) { s.DailyUsageUSD = math.NaN() }, want: ErrResetInvalidData},
		{name: "infinite limit", change: func(s *UserSubscription) { l := math.Inf(1); s.Group.DailyLimitUSD = &l }, want: ErrResetInvalidData},
		{name: "negative usage", change: func(s *UserSubscription) { s.DailyUsageUSD = -1 }, want: ErrResetInvalidData},
	} {
		t.Run(tt.name, func(t *testing.T) {
			sub := resetTestSubscription(now)
			if tt.change != nil {
				tt.change(sub)
			}
			err := ValidateDailyReset(sub, now, tt.count, tt.auto)
			if tt.want == nil {
				require.NoError(t, err)
			} else {
				require.ErrorIs(t, err, tt.want)
			}
		})
	}
}

func TestDailyResetPreservesCalendarAfterShortening(t *testing.T) {
	now := time.Date(2026, 9, 6, 12, 0, 0, 0, time.UTC)
	sub := resetTestSubscription(now)
	sub.StartsAt = now.Add(-time.Hour)
	sub.ExpiresAt = now.Add(time.Hour)
	sub.PreserveCalendarDailyReset = true
	require.False(t, sub.HasOneTimeDailyQuota())
	next := timezone.StartOfDay(now).AddDate(0, 0, 1)
	require.True(t, sub.MaintainUsageWindowsAt(next))
	require.Zero(t, sub.DailyUsageUSD)
	require.Equal(t, next, *sub.DailyWindowStart)
	require.Equal(t, int64(1), sub.DailyResetVersion)
	sub.PreserveCalendarDailyReset = false
	require.True(t, sub.HasOneTimeDailyQuota())
}
