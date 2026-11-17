//go:build unit

package service

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type supplementLegacyRepo struct {
	UserSubscriptionRepository
	increments int
}

func (r *supplementLegacyRepo) IncrementUsage(context.Context, int64, float64) error {
	r.increments++
	return nil
}

func TestSubscriptionSupplementCannotUseNonAtomicBillingFallback(t *testing.T) {
	repo := &supplementLegacyRepo{}
	p := &postUsageBillingParams{Cost: &CostBreakdown{ActualCost: 1, TotalCost: 1}, User: &User{ID: 1}, APIKey: &APIKey{ID: 2}, Account: &Account{ID: 3}, IsSubscriptionBill: true, Subscription: &UserSubscription{ID: 4, EntitlementKind: SubscriptionEntitlementWelfare, SupplementGrantedUSD: 2}}
	_, err := applyUsageBilling(context.Background(), "supplement-fallback", nil, p, &billingDeps{userSubRepo: repo}, nil)
	require.ErrorIs(t, err, ErrAtomicUsageBillingRequired)
	require.Zero(t, repo.increments)
}

func TestSubscriptionSupplementProgressReportsSeparateCredit(t *testing.T) {
	sub := &UserSubscription{ID: 3, ExpiresAt: time.Now().Add(time.Hour), SupplementGrantedUSD: 5, SupplementUsedUSD: 2}
	progress := (&SubscriptionService{}).calculateProgress(sub, &Group{Name: "monthly"})
	encoded, err := json.Marshal(progress)
	require.NoError(t, err)
	var decoded map[string]any
	require.NoError(t, json.Unmarshal(encoded, &decoded))
	require.Equal(t, 3.0, decoded["supplement_remaining_usd"])
}

func TestSubscriptionSupplementZeroCostRetainsTermForHoldSettlement(t *testing.T) {
	start := time.Now().Add(-time.Hour)
	p := &postUsageBillingParams{Cost: &CostBreakdown{}, User: &User{ID: 1}, APIKey: &APIKey{ID: 2}, Account: &Account{ID: 3}, IsSubscriptionBill: true, Subscription: &UserSubscription{ID: 4, StartsAt: start}}
	cmd := buildUsageBillingCommand("free-completion", nil, p)
	require.NotNil(t, cmd.SubscriptionTermStartsAt, "a free completion must still settle the originally funded term's hold")
	require.Equal(t, start, *cmd.SubscriptionTermStartsAt)
	require.Equal(t, int64(4), *cmd.SubscriptionID)
	require.Zero(t, cmd.SubscriptionCost)
}

type supplementAdmissionRepo struct {
	*subscriptionAdmissionRepo
	granted, used float64
	loadErr       error
}

func (r *supplementAdmissionRepo) LoadSubscriptionSupplement(_ context.Context, sub *UserSubscription) error {
	if r.loadErr != nil {
		return r.loadErr
	}
	sub.SupplementGrantedUSD = r.granted
	sub.SupplementUsedUSD = r.used
	return nil
}

func TestSubscriptionSupplementAdmissionReadsCurrentCreditAndFailsClosed(t *testing.T) {
	now := time.Now()
	sub := resetTestSubscription(now)
	sub.DailyWindowStart = &now
	sub.WeeklyWindowStart = &now
	sub.MonthlyWindowStart = &now
	sub.DailyUsageUSD = *sub.Group.DailyLimitUSD
	sub.AutoDailyResetEnabled = false
	repo := &supplementAdmissionRepo{subscriptionAdmissionRepo: &subscriptionAdmissionRepo{sub: sub}, granted: 2}
	svc := &SubscriptionService{groupRepo: &subscriptionAdmissionGroupRepo{group: sub.Group}, userSubRepo: repo, now: func() time.Time { return now }}
	fresh, _, err := svc.GetSubscriptionForAdmission(context.Background(), sub.UserID, sub.GroupID)
	require.NoError(t, err)
	require.Equal(t, 2.0, fresh.SupplementRemainingUSD())
	repo.used = 2
	_, _, err = svc.GetSubscriptionForAdmission(context.Background(), sub.UserID, sub.GroupID)
	require.ErrorIs(t, err, ErrDailyLimitExceeded)
	repo.loadErr = errors.New("credit store offline")
	_, _, err = svc.GetSubscriptionForAdmission(context.Background(), sub.UserID, sub.GroupID)
	require.ErrorIs(t, err, ErrBillingServiceUnavailable)
}

func TestSubscriptionSupplementRevalidationRejectsRenewedTerm(t *testing.T) {
	now := time.Now()
	fresh := resetTestSubscription(now)
	fresh.DailyWindowStart = &now
	fresh.WeeklyWindowStart = &now
	fresh.MonthlyWindowStart = &now
	snapshot := *fresh
	fresh.StartsAt = fresh.StartsAt.Add(time.Second)
	subs := &SubscriptionService{groupRepo: &subscriptionAdmissionGroupRepo{group: fresh.Group}, userSubRepo: &subscriptionAdmissionRepo{sub: fresh}, now: func() time.Time { return now }}
	billing := &BillingCacheService{subscriptionService: subs}
	require.ErrorIs(t, billing.RevalidateSubscription(context.Background(), &snapshot), ErrSubscriptionInvalid)
}

func TestSubscriptionSupplementCoversExhaustedWindowsOnce(t *testing.T) {
	now := time.Now()
	limit := 10.0
	g := &Group{ID: 2, Status: StatusActive, SubscriptionType: SubscriptionTypeSubscription, DailyLimitUSD: &limit, WeeklyLimitUSD: &limit, MonthlyLimitUSD: &limit}
	sub := &UserSubscription{ID: 3, UserID: 1, GroupID: 2, StartsAt: now.Add(-time.Hour), ExpiresAt: now.Add(time.Hour), Status: SubscriptionStatusActive, DailyUsageUSD: 11, WeeklyUsageUSD: 11, MonthlyUsageUSD: 11, SupplementGrantedUSD: 5, SupplementUsedUSD: 1}
	require.NoError(t, checkAuthoritativeSubscriptionLimits(sub, g, now))
	daily, weekly, monthly := sub.CheckAllLimits(g, 4)
	require.True(t, daily && weekly && monthly, "old base overage must not consume newly granted supplement")
	daily, weekly, monthly = sub.CheckAllLimits(g, 4.01)
	require.False(t, daily || weekly || monthly)
	sub.SupplementUsedUSD = 5
	require.ErrorIs(t, checkAuthoritativeSubscriptionLimits(sub, g, now), ErrWeeklyLimitExceeded)
}

func TestSubscriptionSupplementPreservesWelfareZeroAndLifecycle(t *testing.T) {
	now := time.Now()
	zero := 0.0
	g := &Group{ID: 2, Status: StatusActive, SubscriptionType: SubscriptionTypeSubscription}
	sub := &UserSubscription{ID: 3, UserID: 1, GroupID: 2, StartsAt: now.Add(-time.Hour), ExpiresAt: now.Add(time.Hour), Status: SubscriptionStatusActive, EntitlementKind: SubscriptionEntitlementWelfare, WelfareMonthlyLimitUSD: &zero, SupplementGrantedUSD: 2}
	require.NoError(t, checkAuthoritativeSubscriptionLimits(sub, g, now))
	require.True(t, sub.CheckMonthlyLimit(g, 2))
	require.False(t, sub.CheckMonthlyLimit(g, 2.01))
	sub.Status = SubscriptionStatusSuspended
	require.ErrorIs(t, checkAuthoritativeSubscriptionLimits(sub, g, now), ErrSubscriptionInvalid)
	sub.Status = SubscriptionStatusActive
	sub.ExpiresAt = now.Add(-time.Second)
	require.ErrorIs(t, checkAuthoritativeSubscriptionLimits(sub, g, now), ErrSubscriptionExpired)
}

func TestSubscriptionSupplementBaseCapacityAndDebt(t *testing.T) {
	daily, weekly, monthly := 10.0, 20.0, 50.0
	g := &Group{DailyLimitUSD: &daily, WeeklyLimitUSD: &weekly, MonthlyLimitUSD: &monthly}
	sub := &UserSubscription{DailyUsageUSD: 8, WeeklyUsageUSD: 19, MonthlyUsageUSD: 40, SupplementGrantedUSD: 10, SupplementUsedUSD: 3}
	require.Equal(t, 1.0, sub.BaseQuotaRemaining(g))
	require.Equal(t, 8.0, sub.AvailableQuota(g))
	sub.SupplementUsedUSD = 12
	require.Zero(t, sub.SupplementRemainingUSD())
	require.Equal(t, 1.0, sub.AvailableQuota(g), "newly reset base windows remain usable; supplementary debt is retained")
}
