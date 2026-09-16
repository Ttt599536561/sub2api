//go:build integration

package repository

import (
	"context"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestPostmergeBulkRenewalPreservesResetStateWithPostgresTransactions(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	client := testEntClient(t)
	repo := NewUserSubscriptionRepository(client)
	originals := make([]*service.UserSubscription, 0, 3)
	for _, tc := range []struct {
		status  string
		expired bool
	}{
		{status: service.SubscriptionStatusActive},
		{status: service.SubscriptionStatusActive, expired: true},
		{status: service.SubscriptionStatusSuspended, expired: true},
	} {
		resetRepo, sub, now := newDailyResetFixture(t)
		_, err := resetRepo.Apply(ctx, dailyResetCommand(sub, now))
		require.NoError(t, err, "start with a real previously charged reset event")
		expires := now.Add(30 * 24 * time.Hour)
		if tc.expired {
			expires = now.Add(-time.Hour)
		}
		_, err = client.UserSubscription.UpdateOneID(sub.ID).
			SetStatus(tc.status).SetExpiresAt(expires).SetAutoDailyResetEnabled(true).
			SetPreserveCalendarDailyReset(true).SetDailyUsageUsd(40).Save(ctx)
		require.NoError(t, err)
		original, err := repo.GetByID(ctx, sub.ID)
		require.NoError(t, err)
		originals = append(originals, original)
	}
	active, expired, suspended := originals[0], originals[1], originals[2]
	svc := service.NewSubscriptionService(nil, repo, nil, client, nil)
	t.Cleanup(svc.Stop)
	before := time.Now()
	result, err := svc.BulkSubscriptionAction(ctx, &service.BulkSubscriptionActionInput{
		SubscriptionIDs: []int64{active.ID, expired.ID, active.ID, suspended.ID, 9223372036854775807},
		Action:          "extend", Days: 3,
	})
	after := time.Now()
	require.NoError(t, err)
	require.Equal(t, 3, result.SuccessCount, "batch results: %+v", result.Results)
	require.Equal(t, 1, result.FailedCount)
	require.Len(t, result.Results, 4, "duplicate IDs must not extend or advance the version twice")
	for _, original := range originals {
		fresh, err := repo.GetByID(ctx, original.ID)
		require.NoError(t, err)
		require.Equal(t, original.DailyResetVersion+1, fresh.DailyResetVersion)
		if original.ID == active.ID {
			require.True(t, fresh.ExpiresAt.Equal(original.ExpiresAt.AddDate(0, 0, 3)))
			require.True(t, fresh.AutoDailyResetEnabled)
			require.True(t, fresh.PreserveCalendarDailyReset)
			require.Equal(t, original.DailyUsageUSD, fresh.DailyUsageUSD)
			require.Equal(t, original.WeeklyUsageUSD, fresh.WeeklyUsageUSD)
			require.Equal(t, original.MonthlyUsageUSD, fresh.MonthlyUsageUSD)
			require.True(t, fresh.StartsAt.Equal(original.StartsAt))
		} else {
			require.False(t, fresh.StartsAt.Before(before.Add(-time.Microsecond)))
			require.False(t, fresh.StartsAt.After(after.Add(time.Microsecond)))
			require.True(t, fresh.ExpiresAt.Equal(fresh.StartsAt.AddDate(0, 0, 3)))
			require.False(t, fresh.AutoDailyResetEnabled)
			require.False(t, fresh.PreserveCalendarDailyReset)
			require.Zero(t, fresh.DailyUsageUSD)
			require.Zero(t, fresh.WeeklyUsageUSD)
			require.Zero(t, fresh.MonthlyUsageUSD)
		}
		require.Equal(t, original.Status, fresh.Status, "extension must preserve a suspension")
		var events int
		require.NoError(t, integrationDB.QueryRowContext(ctx,
			"SELECT count(*) FROM subscription_daily_reset_events WHERE subscription_id = $1", fresh.ID).Scan(&events))
		require.Equal(t, 1, events, "renewal must retain prior paid reset audit/count events")
	}
}
