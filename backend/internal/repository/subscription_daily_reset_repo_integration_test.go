//go:build integration

package repository

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/timezone"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func newDailyResetFixture(t *testing.T) (service.SubscriptionDailyResetRepository, *service.UserSubscription, time.Time) {
	t.Helper()
	ctx := context.Background()
	client := testEntClient(t)
	user, err := client.User.Create().SetEmail("daily-reset-" + uuid.NewString() + "@example.com").SetPasswordHash("hash").SetStatus("active").Save(ctx)
	require.NoError(t, err)
	group, err := client.Group.Create().SetName("daily-reset-" + uuid.NewString()).SetStatus("active").
		SetSubscriptionType("subscription").SetAllowSubscriptionDayReset(true).SetDailyLimitUsd(100).Save(ctx)
	require.NoError(t, err)
	var now time.Time
	require.NoError(t, integrationDB.QueryRowContext(ctx, "SELECT clock_timestamp()").Scan(&now))
	sub, err := client.UserSubscription.Create().SetUserID(user.ID).SetGroupID(group.ID).
		SetStartsAt(now.Add(-time.Hour)).SetExpiresAt(now.Add(365 * 24 * time.Hour)).SetStatus("active").
		SetDailyWindowStart(timezone.StartOfDay(now)).SetWeeklyWindowStart(now).SetMonthlyWindowStart(now).
		SetDailyUsageUsd(60).SetWeeklyUsageUsd(300).SetMonthlyUsageUsd(800).Save(ctx)
	require.NoError(t, err)
	t.Cleanup(func() {
		_, _ = integrationDB.Exec("DELETE FROM subscription_daily_reset_events WHERE subscription_id = $1", sub.ID)
		_, _ = integrationDB.Exec("DELETE FROM user_subscriptions WHERE id = $1", sub.ID)
		_, _ = integrationDB.Exec("DELETE FROM groups WHERE id = $1", group.ID)
		_, _ = integrationDB.Exec("DELETE FROM users WHERE id = $1", user.ID)
	})
	return NewSubscriptionDailyResetRepository(client, integrationDB), userSubscriptionEntityToService(sub), now
}

func dailyResetCommand(sub *service.UserSubscription, now time.Time) *service.SubscriptionDailyResetCommand {
	return &service.SubscriptionDailyResetCommand{UserID: sub.UserID, SubscriptionID: sub.ID,
		ExpectedVersion: sub.DailyResetVersion, OperationID: uuid.NewString(), ObservedDate: resetDate(now), Source: "manual", RequestFingerprint: uuid.NewString()}
}

func TestSubscriptionDailyResetRepository_ConcurrentIdempotencyAndBillingOrder(t *testing.T) {
	repo, sub, now := newDailyResetFixture(t)
	cmd := dailyResetCommand(sub, now)
	const workers = 20
	results := make([]*service.SubscriptionDailyResetResult, workers)
	errs := make([]error, workers)
	start := make(chan struct{})
	var wg sync.WaitGroup
	for i := range workers {
		wg.Add(1)
		go func() { defer wg.Done(); <-start; results[i], errs[i] = repo.Apply(context.Background(), cmd) }()
	}
	close(start)
	wg.Wait()
	applied := 0
	for i := range workers {
		require.NoError(t, errs[i])
		if !results[i].Replayed {
			applied++
		}
	}
	require.Equal(t, 1, applied)
	state, err := repo.GetState(context.Background(), sub.UserID, sub.ID)
	require.NoError(t, err)
	require.Equal(t, 1, state.TodayCount)
	require.Equal(t, sub.ExpiresAt.Add(-24*time.Hour), state.Subscription.ExpiresAt)
	require.Zero(t, state.Subscription.DailyUsageUSD)
	require.Equal(t, float64(300), state.Subscription.WeeklyUsageUSD)
	require.Equal(t, float64(800), state.Subscription.MonthlyUsageUSD)
	require.True(t, state.Subscription.PreserveCalendarDailyReset)
	require.NoError(t, NewUserSubscriptionRepository(testEntClient(t)).IncrementUsage(context.Background(), sub.ID, 0.8))
	replayed, err := repo.Apply(context.Background(), cmd)
	require.NoError(t, err)
	require.True(t, replayed.Replayed)
	require.InDelta(t, 0.8, replayed.State.Subscription.DailyUsageUSD, 1e-10)
	conflict := *cmd
	conflict.ExpectedVersion++
	_, err = repo.Apply(context.Background(), &conflict)
	require.ErrorIs(t, err, service.ErrResetOperationConflict)
	_, err = repo.GetOperation(context.Background(), sub.UserID+99999, sub.ID, cmd.OperationID)
	require.ErrorIs(t, err, service.ErrResetOperationNotFound)
}

func TestSubscriptionDailyResetRepository_DifferentOperationsShareOneRound(t *testing.T) {
	repo, sub, now := newDailyResetFixture(t)
	first, second := dailyResetCommand(sub, now), dailyResetCommand(sub, now)
	start := make(chan struct{})
	results := make(chan error, 2)
	for _, cmd := range []*service.SubscriptionDailyResetCommand{first, second} {
		go func() { <-start; _, err := repo.Apply(context.Background(), cmd); results <- err }()
	}
	close(start)
	one, two := <-results, <-results
	if one == nil {
		require.ErrorIs(t, two, service.ErrResetStateChanged)
	} else {
		require.NoError(t, two)
		require.ErrorIs(t, one, service.ErrResetStateChanged)
	}
}

func TestSubscriptionDailyResetRepository_CountLimitAndDatabaseConstraint(t *testing.T) {
	repo, sub, now := newDailyResetFixture(t)
	_, err := integrationDB.Exec(`INSERT INTO subscription_daily_reset_events
		(subscription_id,user_id,group_id,source,operation_id,request_fingerprint,before_version,after_version,timezone,count_date,day_sequence,before_daily_usage_usd,daily_limit_usd,before_expires_at,after_expires_at,decided_at)
		SELECT $1,$2,$3,'manual','seed-'||n,'seed',n*2,n*2+1,'UTC',$4::date,n,100,100,$5::timestamptz,$5::timestamptz-INTERVAL '86400 seconds',$6::timestamptz
		FROM generate_series(1,99) n`, sub.ID, sub.UserID, sub.GroupID, resetDate(now), sub.ExpiresAt, now)
	require.NoError(t, err)
	cmd := dailyResetCommand(sub, now)
	result, err := repo.Apply(context.Background(), cmd)
	require.NoError(t, err)
	require.Equal(t, 100, result.State.TodayCount)
	_, err = integrationDB.Exec("UPDATE user_subscriptions SET daily_usage_usd = 100 WHERE id = $1", sub.ID)
	require.NoError(t, err)
	next := dailyResetCommand(result.State.Subscription, now)
	_, err = repo.Apply(context.Background(), next)
	require.ErrorIs(t, err, service.ErrResetDailyCountLimit)
	_, err = integrationDB.Exec(`UPDATE subscription_daily_reset_events SET day_sequence = 101 WHERE subscription_id = $1 AND operation_id = $2`, sub.ID, cmd.OperationID)
	require.Error(t, err, "database constraint must prevent a 101st sequence")
}

func TestSubscriptionDailyResetRepository_NaturalPeriodicMaintenanceInvalidatesOldRound(t *testing.T) {
	repo, sub, now := newDailyResetFixture(t)
	_, err := integrationDB.Exec("UPDATE groups SET weekly_limit_usd = 300 WHERE id = $1", sub.GroupID)
	require.NoError(t, err)
	_, err = integrationDB.Exec("UPDATE user_subscriptions SET starts_at = $2, weekly_window_start = $2 WHERE id = $1", sub.ID, now.Add(-8*24*time.Hour))
	require.NoError(t, err)
	result, err := repo.Apply(context.Background(), dailyResetCommand(sub, now))
	require.ErrorIs(t, err, service.ErrResetStateChanged)
	require.Zero(t, result.State.TodayCount)
	require.Zero(t, result.State.Subscription.WeeklyUsageUSD)
	require.Equal(t, sub.ExpiresAt, result.State.Subscription.ExpiresAt)
	result, err = repo.Apply(context.Background(), dailyResetCommand(result.State.Subscription, now))
	require.NoError(t, err)
	require.Equal(t, 1, result.State.TodayCount)
	require.Equal(t, float64(800), result.State.Subscription.MonthlyUsageUSD)
}

func TestSubscriptionDailyResetRepository_PreferenceUsesCompareAndSet(t *testing.T) {
	repo, sub, _ := newDailyResetFixture(t)
	ctx := context.Background()
	on, err := repo.SetAutomatic(ctx, sub.UserID, sub.ID, 0, true)
	require.NoError(t, err)
	off, err := repo.SetAutomatic(ctx, sub.UserID, sub.ID, on.Subscription.DailyResetVersion, false)
	require.NoError(t, err)
	stale, err := repo.SetAutomatic(ctx, sub.UserID, sub.ID, 0, true)
	require.ErrorIs(t, err, service.ErrResetStateChanged)
	require.False(t, stale.Subscription.AutoDailyResetEnabled)
	require.Equal(t, off.Subscription.DailyResetVersion, stale.Subscription.DailyResetVersion)
	candidates, err := repo.ListAutomaticCandidates(ctx, sub.ID-1, 100)
	require.NoError(t, err)
	for _, candidate := range candidates {
		require.NotEqual(t, sub.ID, candidate.ID, fmt.Sprintf("disabled subscription %d must not be scanned", sub.ID))
	}
}

func TestSubscriptionDailyResetRepository_UserStatusUpdateSerializesWithReset(t *testing.T) {
	repo, sub, now := newDailyResetFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	tx, err := integrationDB.BeginTx(ctx, nil)
	require.NoError(t, err)
	defer tx.Rollback()
	_, err = tx.ExecContext(ctx, "UPDATE users SET status = 'disabled' WHERE id = $1", sub.UserID)
	require.NoError(t, err)
	var blockerPID int
	require.NoError(t, tx.QueryRowContext(ctx, "SELECT pg_backend_pid()").Scan(&blockerPID))
	result := make(chan error, 1)
	go func() { _, err := repo.Apply(ctx, dailyResetCommand(sub, now)); result <- err }()
	require.Eventually(t, func() bool {
		var blocked bool
		err := integrationDB.QueryRowContext(ctx, `SELECT EXISTS (
			SELECT 1 FROM pg_stat_activity WHERE $1 = ANY(pg_blocking_pids(pid)))`, blockerPID).Scan(&blocked)
		return err == nil && blocked
	}, 5*time.Second, 10*time.Millisecond, "reset must wait for the pending user status update")
	require.NoError(t, tx.Commit())
	require.ErrorIs(t, <-result, service.ErrResetNotAllowed)
	var expiresAt time.Time
	var usage float64
	require.NoError(t, integrationDB.QueryRowContext(ctx, "SELECT expires_at, daily_usage_usd FROM user_subscriptions WHERE id = $1", sub.ID).Scan(&expiresAt, &usage))
	require.Equal(t, sub.ExpiresAt, expiresAt)
	require.Equal(t, sub.DailyUsageUSD, usage)
}

func TestSubscriptionDailyResetRepository_ConcurrentBillingConservesUsage(t *testing.T) {
	repo, sub, now := newDailyResetFixture(t)
	ctx := context.Background()
	usageRepo := NewUserSubscriptionRepository(testEntClient(t))
	const workers = 20
	start := make(chan struct{})
	errs := make(chan error, workers)
	for range workers {
		go func() { <-start; errs <- usageRepo.IncrementUsage(ctx, sub.ID, 0.8) }()
	}
	close(start)
	result, err := repo.Apply(ctx, dailyResetCommand(sub, now))
	require.NoError(t, err)
	for range workers {
		require.NoError(t, <-errs)
	}
	state, err := repo.GetState(ctx, sub.UserID, sub.ID)
	require.NoError(t, err)
	require.InDelta(t, sub.DailyUsageUSD+workers*0.8, result.Event.BeforeDailyUsageUSD+state.Subscription.DailyUsageUSD, 1e-9)
	require.InDelta(t, sub.WeeklyUsageUSD+workers*0.8, state.Subscription.WeeklyUsageUSD, 1e-9)
	require.InDelta(t, sub.MonthlyUsageUSD+workers*0.8, state.Subscription.MonthlyUsageUSD, 1e-9)
	require.Equal(t, 1, state.TodayCount)
}

func TestSubscriptionDailyResetRepository_EventConstraintRollsBackDeduction(t *testing.T) {
	repo, sub, now := newDailyResetFixture(t)
	ctx := context.Background()
	_, err := integrationDB.ExecContext(ctx, `INSERT INTO subscription_daily_reset_events
		(subscription_id,user_id,group_id,source,operation_id,request_fingerprint,before_version,after_version,timezone,count_date,day_sequence,before_daily_usage_usd,daily_limit_usd,before_expires_at,after_expires_at,decided_at)
		VALUES ($1,$2,$3,'manual','existing-version','existing',0,1,'UTC',$4::date,1,60,100,$5::timestamptz,$5::timestamptz-INTERVAL '86400 seconds',$6)`,
		sub.ID, sub.UserID, sub.GroupID, resetDate(now), sub.ExpiresAt, now)
	require.NoError(t, err)
	_, err = repo.Apply(ctx, dailyResetCommand(sub, now))
	require.ErrorContains(t, err, "duplicate key", "a conflicting event must abort the entire reset")
	state, err := repo.GetState(ctx, sub.UserID, sub.ID)
	require.NoError(t, err)
	require.Equal(t, sub.ExpiresAt, state.Subscription.ExpiresAt)
	require.Equal(t, sub.DailyUsageUSD, state.Subscription.DailyUsageUSD)
	require.Equal(t, sub.WeeklyUsageUSD, state.Subscription.WeeklyUsageUSD)
	require.Equal(t, sub.MonthlyUsageUSD, state.Subscription.MonthlyUsageUSD)
	require.Equal(t, sub.DailyResetVersion, state.Subscription.DailyResetVersion)
	require.False(t, state.Subscription.PreserveCalendarDailyReset)
	require.Equal(t, 1, state.TodayCount)
}

func TestSubscriptionDailyResetRepository_InvalidAndExhaustedStatesDoNotCharge(t *testing.T) {
	tests := []struct {
		name, mutation string
		want           error
	}{
		{"unused", "UPDATE user_subscriptions SET daily_usage_usd = 0 WHERE id = $1", service.ErrResetNoUsage},
		{"negative usage", "UPDATE user_subscriptions SET daily_usage_usd = -1 WHERE id = $1", service.ErrResetInvalidData},
		{"nan usage", "UPDATE user_subscriptions SET daily_usage_usd = 'NaN'::numeric WHERE id = $1", service.ErrResetInvalidData},
		{"nan limit", "UPDATE groups SET daily_limit_usd = 'NaN'::numeric WHERE id = $1", service.ErrResetInvalidData},
		{"weekly exhausted", "UPDATE groups SET weekly_limit_usd = 300 WHERE id = $1", service.ErrResetWeeklyLimit},
		{"monthly exhausted", "UPDATE groups SET monthly_limit_usd = 800 WHERE id = $1", service.ErrResetMonthlyLimit},
		{"last day", "UPDATE user_subscriptions SET expires_at = clock_timestamp() + INTERVAL '86400 seconds' WHERE id = $1", service.ErrResetInsufficientValidity},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo, sub, now := newDailyResetFixture(t)
			id := sub.ID
			if tt.name == "nan limit" || tt.name == "weekly exhausted" || tt.name == "monthly exhausted" {
				id = sub.GroupID
			}
			_, err := integrationDB.Exec(tt.mutation, id)
			require.NoError(t, err)
			var before time.Time
			require.NoError(t, integrationDB.QueryRow("SELECT expires_at FROM user_subscriptions WHERE id = $1", sub.ID).Scan(&before))
			_, err = repo.Apply(context.Background(), dailyResetCommand(sub, now))
			require.ErrorIs(t, err, tt.want)
			var after time.Time
			var version, count int64
			require.NoError(t, integrationDB.QueryRow("SELECT expires_at, daily_reset_version FROM user_subscriptions WHERE id = $1", sub.ID).Scan(&after, &version))
			require.NoError(t, integrationDB.QueryRow("SELECT COUNT(*) FROM subscription_daily_reset_events WHERE subscription_id = $1", sub.ID).Scan(&count))
			require.Equal(t, before, after)
			require.Zero(t, version)
			require.Zero(t, count)
		})
	}
}

func TestSubscriptionDailyResetRepository_ShortenedValidityPreservesCalendarRefresh(t *testing.T) {
	repo, sub, now := newDailyResetFixture(t)
	ctx := context.Background()
	expiresAt := now.Add(25 * time.Hour)
	_, err := integrationDB.ExecContext(ctx, "UPDATE user_subscriptions SET expires_at = $2 WHERE id = $1", sub.ID, expiresAt)
	require.NoError(t, err)
	paid, err := repo.Apply(ctx, dailyResetCommand(sub, now))
	require.NoError(t, err)
	require.True(t, paid.State.Subscription.PreserveCalendarDailyReset)
	require.False(t, paid.State.Subscription.HasOneTimeDailyQuota())
	_, err = integrationDB.ExecContext(ctx, "UPDATE user_subscriptions SET daily_usage_usd = 1, daily_window_start = $2 WHERE id = $1", sub.ID, timezone.StartOfDay(now).AddDate(0, 0, -1))
	require.NoError(t, err)
	_, err = integrationDB.ExecContext(ctx, "UPDATE groups SET allow_subscription_day_reset = FALSE WHERE id = $1", sub.GroupID)
	require.NoError(t, err)
	state, err := repo.GetState(ctx, sub.UserID, sub.ID)
	require.NoError(t, err)
	require.Zero(t, state.Subscription.DailyUsageUSD)
	require.Equal(t, expiresAt.Add(-24*time.Hour), state.Subscription.ExpiresAt)
	require.Equal(t, 1, state.TodayCount)
	require.EqualValues(t, 2, state.Subscription.DailyResetVersion)
}

func TestSubscriptionDailyResetRepository_AutomaticRechecksPreferenceAndLimit(t *testing.T) {
	repo, sub, now := newDailyResetFixture(t)
	ctx := context.Background()
	cmd := dailyResetCommand(sub, now)
	cmd.Source = "automatic"
	_, err := repo.Apply(ctx, cmd)
	require.ErrorIs(t, err, service.ErrResetNotAllowed)
	enabled, err := repo.SetAutomatic(ctx, sub.UserID, sub.ID, 0, true)
	require.NoError(t, err)
	cmd.ExpectedVersion = enabled.Subscription.DailyResetVersion
	_, err = repo.Apply(ctx, cmd)
	require.ErrorIs(t, err, service.ErrResetNoUsage)
	_, err = integrationDB.ExecContext(ctx, "UPDATE user_subscriptions SET daily_usage_usd = 100 WHERE id = $1", sub.ID)
	require.NoError(t, err)
	paid, err := repo.Apply(ctx, cmd)
	require.NoError(t, err)
	require.True(t, paid.State.Subscription.AutoDailyResetEnabled)
	require.Equal(t, float64(100), paid.Event.BeforeDailyUsageUSD)
	stale := dailyResetCommand(paid.State.Subscription, now)
	stale.Source = "automatic"
	disabled, err := repo.SetAutomatic(ctx, sub.UserID, sub.ID, paid.State.Subscription.DailyResetVersion, false)
	require.NoError(t, err)
	_, err = repo.Apply(ctx, stale)
	require.ErrorIs(t, err, service.ErrResetStateChanged)
	stale.ExpectedVersion = disabled.Subscription.DailyResetVersion
	_, err = repo.Apply(ctx, stale)
	require.ErrorIs(t, err, service.ErrResetNotAllowed)
}
