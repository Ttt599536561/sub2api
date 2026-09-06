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

type subscriptionAdmissionCacheStub struct {
	billingCacheWorkerStub
	data  *SubscriptionCacheData
	reads int
}

func (s *subscriptionAdmissionCacheStub) GetSubscriptionCache(context.Context, int64, int64) (*SubscriptionCacheData, error) {
	s.reads++
	return s.data, nil
}

type subscriptionAdmissionGroupRepo struct {
	GroupRepository
	group *Group
	err   error
	reads int
}

func (r *subscriptionAdmissionGroupRepo) GetByID(context.Context, int64) (*Group, error) {
	r.reads++
	if r.err != nil {
		return nil, r.err
	}
	group := *r.group
	return &group, nil
}

type subscriptionAdmissionRepo struct {
	UserSubscriptionRepository
	sub   *UserSubscription
	err   error
	reads int
}

func (r *subscriptionAdmissionRepo) GetByID(context.Context, int64) (*UserSubscription, error) {
	r.reads++
	if r.err != nil {
		return nil, r.err
	}
	sub := *r.sub
	return &sub, nil
}

func (r *subscriptionAdmissionRepo) GetActiveByUserIDAndGroupID(ctx context.Context, _, _ int64) (*UserSubscription, error) {
	return r.GetByID(ctx, 0)
}

func (r *subscriptionAdmissionRepo) GetByUserIDAndGroupID(ctx context.Context, _, _ int64) (*UserSubscription, error) {
	return r.GetByID(ctx, 0)
}

func TestBillingEligibilityUsesLatestSubscriptionAdmission(t *testing.T) {
	for _, test := range []struct {
		name     string
		expired  bool
		dbError  bool
		groupErr bool
		usage    float64
		wantErr  error
	}{
		{name: "expired after entering queue", expired: true, wantErr: ErrSubscriptionExpired},
		{name: "latest group daily limit", usage: 60, wantErr: ErrDailyLimitExceeded},
		{name: "subscription database unavailable", dbError: true, wantErr: ErrBillingServiceUnavailable},
		{name: "group database unavailable", groupErr: true, wantErr: ErrBillingServiceUnavailable},
	} {
		t.Run(test.name, func(t *testing.T) {
			now := time.Now()
			window := now
			limit, oldLimit := 50.0, 100.0
			group := &Group{ID: 2, Status: StatusActive, SubscriptionType: SubscriptionTypeSubscription, DailyLimitUSD: &limit}
			staleGroup := *group
			staleGroup.DailyLimitUSD = &oldLimit
			fresh := &UserSubscription{
				ID: 3, UserID: 1, GroupID: group.ID, Status: SubscriptionStatusActive,
				StartsAt: now.Add(-48 * time.Hour), ExpiresAt: now.Add(10 * 24 * time.Hour),
				DailyWindowStart: &window, WeeklyWindowStart: &window, MonthlyWindowStart: &window,
				DailyUsageUSD: test.usage,
			}
			stale := *fresh
			stale.DailyUsageUSD = 0
			if test.expired {
				fresh.ExpiresAt = now.Add(-time.Second)
			}
			subRepo := &subscriptionAdmissionRepo{sub: fresh}
			groupRepo := &subscriptionAdmissionGroupRepo{group: group}
			if test.dbError {
				subRepo.err = errors.New("database unavailable")
			}
			if test.groupErr {
				groupRepo.err = errors.New("database unavailable")
			}
			cache := &subscriptionAdmissionCacheStub{data: &SubscriptionCacheData{
				Status: SubscriptionStatusActive, ExpiresAt: stale.ExpiresAt,
			}}
			cfg := &config.Config{}
			billing := NewBillingCacheService(cache, nil, subRepo, nil, nil, nil, cfg, nil)
			t.Cleanup(billing.Stop)
			subscriptionService := NewSubscriptionService(groupRepo, subRepo, billing, nil, cfg)
			t.Cleanup(subscriptionService.Stop)
			user := &User{ID: 1, Status: StatusActive}
			key := &APIKey{User: user, UserID: user.ID, GroupID: &group.ID, Group: &staleGroup}

			err := billing.CheckBillingEligibility(context.Background(), user, key, &staleGroup, &stale, "")

			require.ErrorIs(t, err, test.wantErr)
			require.Zero(t, cache.reads, "cached subscription usage must not decide admission")
			require.Positive(t, groupRepo.reads, "reload group permission and limits before admission")
		})
	}
}

func TestRevalidateSubscriptionAfterAccountWait(t *testing.T) {
	now := time.Now()
	sub := resetTestSubscription(now)
	sub.WeeklyWindowStart, sub.MonthlyWindowStart = &now, &now
	stale := *sub
	groupRepo := &subscriptionAdmissionGroupRepo{group: sub.Group}
	subRepo := &subscriptionAdmissionRepo{sub: sub}
	svc := &SubscriptionService{groupRepo: groupRepo, userSubRepo: subRepo, now: func() time.Time { return now }}
	billing := &BillingCacheService{subscriptionService: svc}

	sub.ExpiresAt = now.Add(-time.Microsecond)
	require.ErrorIs(t, billing.RevalidateSubscription(context.Background(), &stale), ErrSubscriptionExpired)
	sub.ExpiresAt = now.Add(time.Hour)
	sub.DailyUsageUSD = *sub.Group.DailyLimitUSD
	require.ErrorIs(t, billing.RevalidateSubscription(context.Background(), &stale), ErrDailyLimitExceeded)
	sub.DailyUsageUSD = 0
	require.NoError(t, billing.RevalidateSubscription(context.Background(), &stale))
	require.Equal(t, now.Add(10*24*time.Hour), stale.ExpiresAt, "keep the in-flight billing snapshot immutable")
	sub.StartsAt = now.Add(time.Hour)
	require.ErrorIs(t, billing.RevalidateSubscription(context.Background(), &stale), ErrSubscriptionInvalid)
}

type admissionDailyResetRepo struct {
	SubscriptionDailyResetRepository
	subRepo        *subscriptionAdmissionRepo
	group          *Group
	now            time.Time
	stillExhausted bool
	applies        int
}

func (r *admissionDailyResetRepo) GetState(ctx context.Context, _, id int64) (*SubscriptionDailyResetState, error) {
	sub, err := r.subRepo.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}
	sub.Group = r.group
	return &SubscriptionDailyResetState{Subscription: sub, ServerTime: r.now}, nil
}

func (r *admissionDailyResetRepo) Apply(ctx context.Context, cmd *SubscriptionDailyResetCommand) (*SubscriptionDailyResetResult, error) {
	r.applies++
	r.subRepo.sub.DailyResetVersion++
	r.subRepo.sub.ExpiresAt = r.subRepo.sub.ExpiresAt.Add(-24 * time.Hour)
	if !r.stillExhausted {
		r.subRepo.sub.DailyUsageUSD = 0
	}
	state, err := r.GetState(ctx, cmd.UserID, cmd.SubscriptionID)
	if err != nil {
		return nil, err
	}
	return &SubscriptionDailyResetResult{State: state, Event: &SubscriptionDailyResetEvent{OperationID: cmd.OperationID}}, nil
}

func TestSubscriptionAdmissionRechecksAutoResetAtMostOnce(t *testing.T) {
	for _, test := range []struct {
		name            string
		stillExhausted  bool
		weeklyExhausted bool
		wantErr         error
		wantResets      int
	}{
		{name: "exact daily limit resets and admits", wantResets: 1},
		{name: "new consumption exhausts reset quota", stillExhausted: true, wantResets: 1, wantErr: ErrDailyLimitExceeded},
		{name: "weekly exhausted blocks automatic reset", weeklyExhausted: true, wantErr: ErrWeeklyLimitExceeded},
	} {
		t.Run(test.name, func(t *testing.T) {
			now := time.Now()
			limit, weeklyLimit := 100.0, 500.0
			group := &Group{ID: 2, Status: StatusActive, SubscriptionType: SubscriptionTypeSubscription, AllowSubscriptionDayReset: true, DailyLimitUSD: &limit, WeeklyLimitUSD: &weeklyLimit}
			sub := &UserSubscription{ID: 3, UserID: 1, GroupID: 2, Status: SubscriptionStatusActive, StartsAt: now.Add(-48 * time.Hour), ExpiresAt: now.Add(10 * 24 * time.Hour), DailyWindowStart: &now, WeeklyWindowStart: &now, MonthlyWindowStart: &now, DailyUsageUSD: limit, AutoDailyResetEnabled: true, Group: group}
			sub.User = &User{ID: sub.UserID, Status: StatusActive}
			if test.weeklyExhausted {
				sub.WeeklyUsageUSD = weeklyLimit
			}
			subRepo := &subscriptionAdmissionRepo{sub: sub}
			resetRepo := &admissionDailyResetRepo{subRepo: subRepo, group: group, now: now, stillExhausted: test.stillExhausted}
			svc := &SubscriptionService{groupRepo: &subscriptionAdmissionGroupRepo{group: group}, userSubRepo: subRepo, dailyResetRepo: resetRepo, now: func() time.Time { return now }}
			fresh, _, err := svc.GetSubscriptionForAdmission(context.Background(), 1, 2)
			if test.wantErr == nil {
				require.NoError(t, err)
				require.Zero(t, fresh.DailyUsageUSD)
			} else {
				require.ErrorIs(t, err, test.wantErr)
			}
			require.Equal(t, test.wantResets, resetRepo.applies)
		})
	}
}
