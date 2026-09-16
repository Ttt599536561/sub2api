//go:build integration

package repository

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestSubscriptionDailyResetRepository_PostMergeManualAndAutomaticShareOneRound(t *testing.T) {
	repo, sub, now := newDailyResetFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	_, err := integrationDB.ExecContext(ctx, `UPDATE user_subscriptions
		SET auto_daily_reset_enabled = true, daily_usage_usd = 100 WHERE id = $1`, sub.ID)
	require.NoError(t, err)
	sub.AutoDailyResetEnabled, sub.DailyUsageUSD = true, 100

	const workers = 12
	type outcome struct {
		result *service.SubscriptionDailyResetResult
		err    error
	}
	results := make(chan outcome, workers)
	start := make(chan struct{})
	var wg sync.WaitGroup
	for i := range workers {
		command := dailyResetCommand(sub, now)
		if i%2 == 0 {
			command.Source = "automatic"
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			result, err := repo.Apply(ctx, command)
			results <- outcome{result, err}
		}()
	}
	close(start)
	wg.Wait()
	close(results)
	winners := 0
	for got := range results {
		if got.err == nil {
			winners++
			require.NotNil(t, got.result.Event)
			require.False(t, got.result.Replayed)
		} else {
			require.ErrorIs(t, got.err, service.ErrResetStateChanged)
		}
	}
	require.Equal(t, 1, winners, "manual and background operations must compete for the same reset version")
	state, err := repo.GetState(ctx, sub.UserID, sub.ID)
	require.NoError(t, err)
	require.Equal(t, 1, state.TodayCount)
	require.Equal(t, sub.ExpiresAt.Add(-24*time.Hour), state.Subscription.ExpiresAt)
	require.Zero(t, state.Subscription.DailyUsageUSD)
	require.Equal(t, sub.WeeklyUsageUSD, state.Subscription.WeeklyUsageUSD)
	require.Equal(t, sub.MonthlyUsageUSD, state.Subscription.MonthlyUsageUSD)
	require.True(t, state.Subscription.AutoDailyResetEnabled)
}

func TestSubscriptionDailyResetRepository_PostMergeRejectsAnotherUsersSubscription(t *testing.T) {
	repo, sub, now := newDailyResetFixture(t)
	_, other, _ := newDailyResetFixture(t)
	ctx := context.Background()
	command := dailyResetCommand(sub, now)
	command.UserID = other.UserID
	_, err := repo.Apply(ctx, command)
	require.ErrorIs(t, err, service.ErrSubscriptionNotFound)
	_, err = repo.GetState(ctx, other.UserID, sub.ID)
	require.ErrorIs(t, err, service.ErrSubscriptionNotFound)
	_, err = repo.SetAutomatic(ctx, other.UserID, sub.ID, sub.DailyResetVersion, true)
	require.ErrorIs(t, err, service.ErrSubscriptionNotFound)
	state, err := repo.GetState(ctx, sub.UserID, sub.ID)
	require.NoError(t, err)
	require.Equal(t, sub.ExpiresAt, state.Subscription.ExpiresAt)
	require.Equal(t, sub.DailyUsageUSD, state.Subscription.DailyUsageUSD)
	require.Zero(t, state.TodayCount)
	require.False(t, state.Subscription.AutoDailyResetEnabled)

	// A successful owner's operation must remain invisible to a different,
	// otherwise valid user; having an operation ID does not grant ownership.
	command.UserID = sub.UserID
	_, err = repo.Apply(ctx, command)
	require.NoError(t, err)
	_, err = repo.GetOperation(ctx, other.UserID, sub.ID, command.OperationID)
	require.ErrorIs(t, err, service.ErrResetOperationNotFound)
}

func TestSubscriptionDailyResetRepository_PostMergeRevokedPermissionAllowsDisabling(t *testing.T) {
	repo, sub, now := newDailyResetFixture(t)
	ctx := context.Background()
	on, err := repo.SetAutomatic(ctx, sub.UserID, sub.ID, sub.DailyResetVersion, true)
	require.NoError(t, err)
	_, err = integrationDB.ExecContext(ctx, "UPDATE groups SET allow_subscription_day_reset = false WHERE id = $1", sub.GroupID)
	require.NoError(t, err)
	command := dailyResetCommand(on.Subscription, now)
	_, err = repo.Apply(ctx, command)
	require.ErrorIs(t, err, service.ErrResetNotAllowed)
	command.Source = "automatic"
	_, err = repo.Apply(ctx, command)
	require.ErrorIs(t, err, service.ErrResetNotAllowed)

	off, err := repo.SetAutomatic(ctx, sub.UserID, sub.ID, on.Subscription.DailyResetVersion, false)
	require.NoError(t, err)
	require.False(t, off.Subscription.AutoDailyResetEnabled)
	require.Equal(t, sub.ExpiresAt, off.Subscription.ExpiresAt)
	require.Equal(t, sub.DailyUsageUSD, off.Subscription.DailyUsageUSD)
	require.Equal(t, sub.WeeklyUsageUSD, off.Subscription.WeeklyUsageUSD)
	require.Equal(t, sub.MonthlyUsageUSD, off.Subscription.MonthlyUsageUSD)
	require.Zero(t, off.TodayCount)
}
