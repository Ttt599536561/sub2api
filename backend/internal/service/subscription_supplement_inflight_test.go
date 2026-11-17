//go:build unit

package service

import (
	"context"
	"errors"
	"math"
	"sync"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

type supplementScopeCache struct {
	BillingCache
	mu      sync.Mutex
	amounts map[string]map[string]float64
	err     error
	renews  int
}

type supplementDurableRepo struct {
	*subscriptionAdmissionRepo
	mu    sync.Mutex
	holds map[string]float64
	err   error
}

func (r *supplementDurableRepo) ReserveSubscriptionSupplement(_ context.Context, h *SubscriptionSupplementReservation) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.err != nil {
		return r.err
	}
	if r.holds == nil {
		r.holds = map[string]float64{}
	}
	var pending float64
	for _, amount := range r.holds {
		pending += amount
	}
	if pending+h.AmountUSD > r.sub.AvailableQuota(r.sub.Group) {
		return ErrMonthlyLimitExceeded
	}
	r.holds[h.ID] = h.AmountUSD
	return nil
}
func (r *supplementDurableRepo) RenewSubscriptionSupplementReservation(_ context.Context, id string, _ time.Duration) (bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	_, ok := r.holds[id]
	return ok, nil
}
func (r *supplementDurableRepo) ReleaseSubscriptionSupplementReservation(_ context.Context, id string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.holds, id)
	return nil
}
func (r *supplementDurableRepo) TransferSubscriptionSupplementReservation(context.Context, string, string, time.Duration) error {
	return nil
}

func TestSubscriptionSupplementDurableAuthoritySurvivesLostRedisHold(t *testing.T) {
	svc, cache, sub := supplementInflightFixture()
	repo := &supplementDurableRepo{subscriptionAdmissionRepo: &subscriptionAdmissionRepo{sub: sub}}
	svc.subRepo = repo
	first, err := svc.ReserveInflight(context.Background(), &User{ID: 1}, sub.Group, sub, 4)
	require.NoError(t, err)
	require.NotNil(t, first)
	defer first.HandlerDone()
	cache.mu.Lock()
	cache.amounts = nil
	cache.mu.Unlock()
	second, err := svc.ReserveInflight(context.Background(), &User{ID: 1}, sub.Group, sub, 4)
	defer second.HandlerDone()
	require.ErrorIs(t, err, ErrMonthlyLimitExceeded, "durable hold survives loss of every Redis key")
	require.NotEmpty(t, first.SupplementHoldID())
	ctx := WithInflightReservation(context.Background(), first)
	require.Equal(t, first.SupplementHoldID(), SubscriptionSupplementHoldIDFromContext(ctx))
	cache.err = errors.New("redis offline")
	first.HandlerDone()
	second, err = svc.ReserveInflight(context.Background(), &User{ID: 1}, sub.Group, sub, 4)
	defer second.HandlerDone()
	require.NoError(t, err, "Redis is auxiliary when durable authority is available")
}

func (c *supplementScopeCache) ReserveInflightScope(_ context.Context, scope, id string, amount float64, _ time.Duration) (float64, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.err != nil {
		return 0, c.err
	}
	if c.amounts == nil {
		c.amounts = map[string]map[string]float64{}
	}
	if c.amounts[scope] == nil {
		c.amounts[scope] = map[string]float64{}
	}
	var total float64
	for _, v := range c.amounts[scope] {
		total += v
	}
	c.amounts[scope][id] = amount
	return total, nil
}
func (c *supplementScopeCache) ReleaseInflightScope(_ context.Context, scope, id string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.amounts[scope], id)
	return nil
}
func (c *supplementScopeCache) RenewInflightScope(_ context.Context, scope, id string, _ time.Duration) (bool, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.renews++
	_, ok := c.amounts[scope][id]
	return ok, nil
}

func TestSubscriptionSupplementInflightRenewsUntilAsyncSettlementCompletes(t *testing.T) {
	svc, cache, sub := supplementInflightFixture()
	svc.cfg.Billing.InflightReservation.TTLSeconds = 1
	res, err := svc.ReserveInflight(context.Background(), &User{ID: 1}, sub.Group, sub, 1)
	require.NoError(t, err)
	require.NotNil(t, res)
	settled := res.Acquire()
	defer settled()
	res.HandlerDone()
	require.Eventually(t, func() bool { cache.mu.Lock(); defer cache.mu.Unlock(); return cache.renews > 0 }, 600*time.Millisecond, 20*time.Millisecond, "handler completion must not stop a credit hold's renewal before its queued SQL finishes")
	settled()
	select {
	case <-res.renewDone:
	case <-time.After(time.Second):
		t.Fatal("settlement must stop renewal")
	}
}

func supplementInflightFixture() (*BillingCacheService, *supplementScopeCache, *UserSubscription) {
	now := time.Now()
	limit := 10.0
	g := &Group{ID: 2, Status: StatusActive, SubscriptionType: SubscriptionTypeSubscription, MonthlyLimitUSD: &limit}
	sub := &UserSubscription{ID: 3, UserID: 1, GroupID: 2, StartsAt: now.Add(-time.Hour), ExpiresAt: now.Add(time.Hour), Status: SubscriptionStatusActive, Group: g, MonthlyUsageUSD: 10, SupplementGrantedUSD: 5}
	cache := &supplementScopeCache{}
	svc := &BillingCacheService{cfg: &config.Config{}, cache: cache, subRepo: &supplementDurableRepo{subscriptionAdmissionRepo: &subscriptionAdmissionRepo{sub: sub}}}
	return svc, cache, sub
}

func TestSubscriptionSupplementInflightIncludesOtherRequestsWhenBalanceFeatureDisabled(t *testing.T) {
	svc, _, sub := supplementInflightFixture()
	user := &User{ID: 1}
	first, err := svc.ReserveInflight(context.Background(), user, sub.Group, sub, 4)
	require.NoError(t, err)
	require.NotNil(t, first)
	defer first.HandlerDone()
	second, err := svc.ReserveInflight(context.Background(), user, sub.Group, sub, 2)
	defer second.HandlerDone()
	require.ErrorIs(t, err, ErrMonthlyLimitExceeded)
	finished := first.Acquire()
	first.HandlerDone()
	second, err = svc.ReserveInflight(context.Background(), user, sub.Group, sub, 2)
	defer second.HandlerDone()
	require.ErrorIs(t, err, ErrMonthlyLimitExceeded, "async settlement still owns the credit hold")
	finished()
	second, err = svc.ReserveInflight(context.Background(), user, sub.Group, sub, 2)
	defer second.HandlerDone()
	require.NoError(t, err)
}

func TestSubscriptionSupplementInflightFailsClosedAndRejectsNewTerm(t *testing.T) {
	svc, _, sub := supplementInflightFixture()
	user := &User{ID: 1}
	repo, ok := svc.subRepo.(*supplementDurableRepo)
	require.True(t, ok)
	repo.err = ErrBillingServiceUnavailable
	res, err := svc.ReserveInflight(context.Background(), user, sub.Group, sub, 1)
	defer res.HandlerDone()
	require.ErrorIs(t, err, ErrBillingServiceUnavailable)
	repo.err = nil
	snapshot := *sub
	sub.StartsAt = sub.StartsAt.Add(time.Second)
	res, err = svc.ReserveInflight(context.Background(), user, sub.Group, &snapshot, 1)
	defer res.HandlerDone()
	require.ErrorIs(t, err, ErrSubscriptionInvalid)
}

func TestSubscriptionSupplementFixedChargeSharesTokenReservation(t *testing.T) {
	svc, _, sub := supplementInflightFixture()
	user := &User{ID: 1}
	first, err := svc.ReserveInflight(context.Background(), user, sub.Group, sub, 4)
	require.NoError(t, err)
	defer first.HandlerDone()
	second, err := svc.ReserveFixedCharge(context.Background(), user, nil, sub.Group, sub, 2)
	defer second.HandlerDone()
	require.ErrorIs(t, err, ErrMonthlyLimitExceeded)
}

func TestSubscriptionSupplementFixedChargeRejectsInvalidAmounts(t *testing.T) {
	svc, _, sub := supplementInflightFixture()
	for _, amount := range []float64{-1, math.NaN(), math.Inf(1)} {
		res, err := svc.ReserveFixedCharge(context.Background(), &User{ID: 1}, nil, sub.Group, sub, amount)
		res.HandlerDone()
		require.ErrorIs(t, err, ErrSubscriptionInvalid)
	}
}
