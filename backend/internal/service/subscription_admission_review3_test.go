//go:build unit

package service

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

func TestReview3DeletedGroupDoesNotTripBillingCircuitBreaker(t *testing.T) {
	for _, groupType := range []string{SubscriptionTypeStandard, SubscriptionTypeSubscription} {
		t.Run(groupType, func(t *testing.T) {
			group := &Group{ID: 2, Status: StatusActive, SubscriptionType: groupType}
			svc := &SubscriptionService{
				groupRepo:   &subscriptionAdmissionGroupRepo{err: fmt.Errorf("load group: %w", ErrGroupNotFound)},
				userSubRepo: &subscriptionAdmissionRepo{},
			}
			breaker := newBillingCircuitBreaker(config.CircuitBreakerConfig{Enabled: true, FailureThreshold: 1})
			billing := &BillingCacheService{cfg: &config.Config{}, subscriptionService: svc, circuitBreaker: breaker}
			user := &User{ID: 1}
			key := &APIKey{UserID: user.ID, User: user, GroupID: &group.ID, Group: group}
			var subscription *UserSubscription
			if group.IsSubscriptionType() {
				subscription = &UserSubscription{ID: 3, UserID: user.ID, GroupID: group.ID}
			}

			err := billing.CheckBillingEligibility(context.Background(), user, key, group, subscription, "")

			require.ErrorIs(t, err, ErrSubscriptionInvalid, "a group deleted after authentication is a business rejection")
			require.Equal(t, billingCircuitClosed, breaker.state)
			require.True(t, breaker.Allow(), "a deleted group must not block billing for unrelated users")
		})
	}
}

func TestReview3AdmissionGroupDatabaseFailureStillOpensBillingCircuit(t *testing.T) {
	group := &Group{ID: 2, Status: StatusActive, SubscriptionType: SubscriptionTypeSubscription}
	svc := &SubscriptionService{
		groupRepo:   &subscriptionAdmissionGroupRepo{err: errors.New("database unavailable")},
		userSubRepo: &subscriptionAdmissionRepo{},
	}
	breaker := newBillingCircuitBreaker(config.CircuitBreakerConfig{Enabled: true, FailureThreshold: 1})
	billing := &BillingCacheService{cfg: &config.Config{}, subscriptionService: svc, circuitBreaker: breaker}
	user := &User{ID: 1}
	key := &APIKey{UserID: user.ID, User: user, GroupID: &group.ID, Group: group}
	err := billing.CheckBillingEligibility(context.Background(), user, key, group, &UserSubscription{ID: 3}, "")
	require.ErrorIs(t, err, ErrBillingServiceUnavailable)
	require.Equal(t, billingCircuitOpen, breaker.state)
	require.False(t, breaker.Allow())
}

func TestReview3MonthlyLimitPreventsAdmissionAutoReset(t *testing.T) {
	now := time.Now()
	sub := resetTestSubscription(now)
	sub.DailyWindowStart, sub.WeeklyWindowStart, sub.MonthlyWindowStart = &now, &now, &now
	monthlyLimit := 500.0
	sub.Group.MonthlyLimitUSD = &monthlyLimit
	sub.DailyUsageUSD, sub.MonthlyUsageUSD = *sub.Group.DailyLimitUSD, monthlyLimit
	sub.AutoDailyResetEnabled = true
	repo := &subscriptionAdmissionRepo{sub: sub}
	resetRepo := &admissionDailyResetRepo{subRepo: repo, group: sub.Group, now: now}
	svc := &SubscriptionService{groupRepo: &subscriptionAdmissionGroupRepo{group: sub.Group}, userSubRepo: repo, dailyResetRepo: resetRepo, now: func() time.Time { return now }}

	_, _, err := svc.GetSubscriptionForAdmission(context.Background(), sub.UserID, sub.GroupID)

	require.ErrorIs(t, err, ErrMonthlyLimitExceeded)
	require.Zero(t, resetRepo.applies, "a daily reset cannot relieve an exhausted monthly quota")
	require.Equal(t, now.Add(10*24*time.Hour), sub.ExpiresAt, "reject without charging subscription validity")
}

func TestReview3ScannerNaturalMaintenanceDoesNotLeaveCachedAdmissionDenied(t *testing.T) {
	now := time.Now()
	sub := resetTestSubscription(now)
	sub.StartsAt = now.Add(-100 * 24 * time.Hour)
	oldWindow := now.Add(-35 * 24 * time.Hour)
	sub.DailyWindowStart, sub.WeeklyWindowStart, sub.MonthlyWindowStart = &oldWindow, &oldWindow, &oldWindow
	sub.DailyUsageUSD, sub.WeeklyUsageUSD, sub.MonthlyUsageUSD = 100, 100, 100
	sub.AutoDailyResetEnabled = true
	subRepo := &subscriptionAdmissionRepo{sub: sub}
	cache := &subscriptionAdmissionCacheStub{data: &SubscriptionCacheData{
		Status: SubscriptionStatusActive, ExpiresAt: sub.ExpiresAt, DailyUsage: 100, WeeklyUsage: 100, MonthlyUsage: 100,
	}}
	cfg := &config.Config{SubscriptionCache: config.SubscriptionCacheConfig{L1Size: 100, L1TTLSeconds: 300}}
	billing := &BillingCacheService{cfg: cfg, cache: cache}
	svc := NewSubscriptionService(&subscriptionAdmissionGroupRepo{group: sub.Group}, subRepo, billing, nil, cfg)
	t.Cleanup(svc.Stop)
	stale, err := svc.GetActiveSubscription(context.Background(), sub.UserID, sub.GroupID)
	require.NoError(t, err)
	svc.subCacheL1.Wait()
	_, cached := svc.subCacheL1.Get(subCacheKey(sub.UserID, sub.GroupID))
	require.True(t, cached, "the test must retain an exhausted L1 snapshot across scanner maintenance")
	var paidResets int
	svc.dailyResetRepo = &dailyResetScannerRepo{
		list: func(context.Context, int64, int) ([]SubscriptionDailyResetCandidate, error) {
			return []SubscriptionDailyResetCandidate{{ID: sub.ID}}, nil
		},
		state: func(context.Context, int64, int64) (*SubscriptionDailyResetState, error) {
			subRepo.sub.MaintainUsageWindowsAt(now)
			fresh := *subRepo.sub
			return &SubscriptionDailyResetState{Subscription: &fresh, ServerTime: now}, nil
		},
		apply: func(context.Context, *SubscriptionDailyResetCommand) (*SubscriptionDailyResetResult, error) {
			paidResets++
			return nil, errors.New("natural maintenance must not charge validity")
		},
	}
	var cursor int64
	svc.scanAutoDailyResets(context.Background(), &cursor)
	require.Zero(t, paidResets)
	require.Equal(t, now.Add(10*24*time.Hour), subRepo.sub.ExpiresAt)
	require.Equal(t, 100.0, stale.DailyUsageUSD)

	key := &APIKey{User: sub.User, UserID: sub.UserID, GroupID: &sub.GroupID, Group: sub.Group}
	err = billing.CheckBillingEligibility(context.Background(), sub.User, key, sub.Group, stale, "")

	require.NoError(t, err, "authoritative admission must see the scanner's natural resets despite both stale caches")
	require.Zero(t, stale.DailyUsageUSD)
	require.Zero(t, stale.WeeklyUsageUSD)
	require.Zero(t, stale.MonthlyUsageUSD)
	require.Equal(t, subRepo.sub.DailyResetVersion, stale.DailyResetVersion)
	require.Zero(t, cache.reads)
}
