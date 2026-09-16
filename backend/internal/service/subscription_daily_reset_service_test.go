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

func (r *dailyResetServiceRepo) GetState(context.Context, int64, int64) (*SubscriptionDailyResetState, error) {
	return r.state, nil
}

type dailyResetListingRepo struct {
	UserSubscriptionRepository
	listed UserSubscription
}

func (r *dailyResetListingRepo) ListActiveByUserID(context.Context, int64) ([]UserSubscription, error) {
	return []UserSubscription{r.listed}, nil
}

func TestDailyResetActiveListFiltersRefreshedInactiveSubscriptions(t *testing.T) {
	now := time.Date(2026, 9, 6, 12, 0, 0, 0, time.UTC)
	for _, tt := range []struct {
		name   string
		status string
		expiry time.Time
		want   int
	}{
		{"still active", SubscriptionStatusActive, now.Add(time.Hour), 1},
		{"expired while loading", SubscriptionStatusActive, now, 0},
		{"suspended while loading", SubscriptionStatusSuspended, now.Add(time.Hour), 0},
		{"revoked while loading", SubscriptionStatusRevoked, now.Add(time.Hour), 0},
	} {
		t.Run(tt.name, func(t *testing.T) {
			sub := resetTestSubscription(now)
			listed := *sub
			sub.Status, sub.ExpiresAt = tt.status, tt.expiry
			svc := &SubscriptionService{
				userSubRepo:    &dailyResetListingRepo{listed: listed},
				dailyResetRepo: &dailyResetServiceRepo{state: &SubscriptionDailyResetState{Subscription: sub, ServerTime: now}},
			}
			got, err := svc.ListActiveUserSubscriptions(context.Background(), sub.UserID)
			require.NoError(t, err)
			require.Len(t, got, tt.want)
			if tt.want > 0 {
				require.Equal(t, tt.expiry, got[0].ExpiresAt)
			}
		})
	}
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

func TestDailyResetServiceResponseNormalizesExpiryAtServerTime(t *testing.T) {
	now := time.Date(2026, 9, 6, 12, 0, 0, 0, time.UTC)
	for _, tt := range []struct {
		name   string
		expiry time.Time
		status string
		want   string
	}{
		{"expired", now.Add(-time.Second), SubscriptionStatusActive, SubscriptionStatusExpired},
		{"exact expiry", now, SubscriptionStatusActive, SubscriptionStatusExpired},
		{"still active", now.Add(time.Second), SubscriptionStatusActive, SubscriptionStatusActive},
		{"suspended", now.Add(-time.Second), SubscriptionStatusSuspended, SubscriptionStatusSuspended},
	} {
		t.Run(tt.name, func(t *testing.T) {
			sub := resetTestSubscription(now)
			sub.ExpiresAt, sub.Status = tt.expiry, tt.status
			sub.AutoDailyResetEnabled = true
			repo := &dailyResetServiceRepo{state: &SubscriptionDailyResetState{Subscription: sub, ServerTime: now}}
			svc := &SubscriptionService{dailyResetRepo: repo}
			out, err := svc.SetAutoDailyReset(context.Background(), sub.UserID, sub.ID, 0, false)
			require.NoError(t, err)
			require.True(t, out.PreferenceSaved)
			require.Equal(t, tt.want, out.Subscription.Status)
			require.Equal(t, now, out.Subscription.DailyResetState.ServerTime)
			require.Equal(t, tt.status, sub.Status, "response normalization must not mutate the repository snapshot")
		})
	}
}
