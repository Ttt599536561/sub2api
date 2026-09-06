package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type dailyResetServiceRepo struct {
	SubscriptionDailyResetRepository
	state    *SubscriptionDailyResetState
	applyErr error
	command  *SubscriptionDailyResetCommand
}

func (r *dailyResetServiceRepo) SetAutomatic(_ context.Context, _, _, version int64, enabled bool) (*SubscriptionDailyResetState, error) {
	if version != r.state.Subscription.DailyResetVersion {
		return nil, ErrResetStateChanged
	}
	r.state.Subscription.AutoDailyResetEnabled = enabled
	r.state.Subscription.DailyResetVersion++
	return r.state, nil
}

func (r *dailyResetServiceRepo) Apply(_ context.Context, command *SubscriptionDailyResetCommand) (*SubscriptionDailyResetResult, error) {
	r.command = command
	if r.applyErr != nil {
		return nil, r.applyErr
	}
	r.state.Subscription.DailyUsageUSD = 0
	r.state.Subscription.ExpiresAt = r.state.Subscription.ExpiresAt.Add(-24 * time.Hour)
	r.state.Subscription.DailyResetVersion++
	r.state.TodayCount++
	return &SubscriptionDailyResetResult{State: r.state, Event: &SubscriptionDailyResetEvent{OperationID: command.OperationID}}, nil
}

func TestDailyResetServiceEnableKeepsSavedPreferenceOnCheckFailure(t *testing.T) {
	now := time.Date(2026, 9, 6, 12, 0, 0, 0, time.UTC)
	sub := resetTestSubscription(now)
	sub.DailyUsageUSD = *sub.Group.DailyLimitUSD
	repo := &dailyResetServiceRepo{state: &SubscriptionDailyResetState{Subscription: sub, ServerTime: now}, applyErr: errors.New("database unavailable")}
	svc := &SubscriptionService{dailyResetRepo: repo}
	out, err := svc.SetAutoDailyReset(context.Background(), sub.UserID, sub.ID, 0, true)
	require.NoError(t, err)
	require.True(t, out.PreferenceSaved)
	require.True(t, out.Subscription.AutoDailyResetEnabled)
	require.False(t, out.ResetPerformed)
	require.Equal(t, "RESET_CHECK_FAILED", out.CheckError)
	require.Equal(t, now.Add(10*24*time.Hour), out.Subscription.ExpiresAt)
	require.Equal(t, int64(1), repo.command.ExpectedVersion)
	require.Equal(t, "auto:1:2026-09-06", repo.command.OperationID)

	_, err = svc.SetAutoDailyReset(context.Background(), sub.UserID, sub.ID, 0, false)
	require.ErrorIs(t, err, ErrResetStateChanged)
	require.True(t, sub.AutoDailyResetEnabled)
}

func TestDailyResetServiceEnableChecksImmediately(t *testing.T) {
	for _, exhausted := range []bool{false, true} {
		t.Run(map[bool]string{false: "partial usage", true: "exhausted"}[exhausted], func(t *testing.T) {
			now := time.Date(2026, 9, 6, 12, 0, 0, 0, time.UTC)
			sub := resetTestSubscription(now)
			if exhausted {
				sub.DailyUsageUSD = *sub.Group.DailyLimitUSD
			}
			repo := &dailyResetServiceRepo{state: &SubscriptionDailyResetState{Subscription: sub, ServerTime: now}}
			svc := &SubscriptionService{dailyResetRepo: repo}
			out, err := svc.SetAutoDailyReset(context.Background(), sub.UserID, sub.ID, 0, true)
			require.NoError(t, err)
			require.True(t, out.PreferenceSaved)
			require.Equal(t, exhausted, out.ResetPerformed)
			if exhausted {
				require.Zero(t, out.Subscription.DailyUsageUSD)
				require.Equal(t, 1, out.Subscription.DailyResetState.DailyResetCount)
				require.Equal(t, now.Add(9*24*time.Hour), out.Subscription.ExpiresAt)
			} else {
				require.Nil(t, repo.command)
			}
		})
	}
}

func TestDailyResetServiceDisableNeverAttemptsReset(t *testing.T) {
	now := time.Date(2026, 9, 6, 12, 0, 0, 0, time.UTC)
	sub := resetTestSubscription(now)
	sub.AutoDailyResetEnabled = true
	sub.DailyUsageUSD = *sub.Group.DailyLimitUSD
	sub.ExpiresAt = now.Add(-time.Hour)
	sub.Group.AllowSubscriptionDayReset = false
	repo := &dailyResetServiceRepo{state: &SubscriptionDailyResetState{Subscription: sub, ServerTime: now}}
	svc := &SubscriptionService{dailyResetRepo: repo}
	out, err := svc.SetAutoDailyReset(context.Background(), sub.UserID, sub.ID, 0, false)
	require.NoError(t, err)
	require.True(t, out.PreferenceSaved)
	require.False(t, out.Subscription.AutoDailyResetEnabled)
	require.Nil(t, repo.command)
}
