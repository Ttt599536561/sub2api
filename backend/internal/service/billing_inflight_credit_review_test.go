//go:build unit

package service

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

type reviewInflightCreditCache struct {
	*welfareDelayedDeductionCache
	reservations *memInflightCache
}

func (c *reviewInflightCreditCache) ReserveInflightBalance(ctx context.Context, userID int64, requestID string, amount, balance float64, ttl time.Duration) (bool, float64, error) {
	return c.reservations.ReserveInflightBalance(ctx, userID, requestID, amount, balance, ttl)
}

func (c *reviewInflightCreditCache) ReleaseInflightBalance(ctx context.Context, userID int64, requestID string) error {
	return c.reservations.ReleaseInflightBalance(ctx, userID, requestID)
}

func TestInflightReservationCreditAfterDelayedDebit(t *testing.T) {
	ctx := context.Background()
	cache := &reviewInflightCreditCache{
		welfareDelayedDeductionCache: &welfareDelayedDeductionCache{
			balance: 100, exists: true, refilled: make(chan float64, 16),
			deductionStarted: make(chan struct{}), deductionRelease: make(chan struct{}), deductionDone: make(chan struct{}),
		},
		reservations: newMemInflightCache(0),
	}
	repo := &welfareAuthoritativeBalanceRepo{balance: 10}
	cfg := &config.Config{}
	cfg.Billing.InflightReservation.Enabled = true
	svc := NewBillingCacheService(cache, repo, nil, nil, nil, nil, cfg, nil)
	t.Cleanup(svc.Stop)
	var release sync.Once
	defer release.Do(func() { close(cache.deductionRelease) })
	svc.QueueDeductBalance(7, 90) // The SQL debit is committed; its cache write is delayed.
	select {
	case <-cache.deductionStarted:
	case <-time.After(time.Second):
		t.Fatal("cache deduction did not start")
	}
	repo.balance = 110 // A welfare redemption credits $100 after the debit.
	require.NoError(t, svc.InvalidateUserBalance(ctx, 7))
	balance, err := svc.GetUserBalance(ctx, 7)
	require.NoError(t, err)
	require.Equal(t, 110.0, balance)
	select {
	case <-cache.refilled:
	case <-time.After(time.Second):
		t.Fatal("post-credit balance was not cached")
	}
	release.Do(func() { close(cache.deductionRelease) })
	select {
	case <-cache.deductionDone:
	case <-time.After(time.Second):
		t.Fatal("cache deduction did not finish")
	}
	balance, err = cache.GetUserBalance(ctx, 7)
	require.NoError(t, err)
	require.Equal(t, 20.0, balance, "stale deduction leaves a positive but understated cache")
	first, err := svc.ReserveInflight(ctx, &User{ID: 7}, nil, nil, 15)
	require.NoError(t, err)
	require.NotNil(t, first)
	defer first.HandlerDone()
	second, err := svc.ReserveInflight(ctx, &User{ID: 7}, nil, nil, 10)
	require.NoError(t, err, "actual $110 covers both reservations despite the stale positive $20 cache")
	require.NotNil(t, second)
	defer second.HandlerDone()
	require.Equal(t, 2, cache.reservations.count())
}

func TestInflightReservationAuthoritativeRetryKeepsProtection(t *testing.T) {
	for _, tc := range []struct {
		name   string
		actual float64
		err    error
	}{
		{name: "still insufficient", actual: 20},
		{name: "credit smaller than combined estimates", actual: 24},
		{name: "database unavailable", err: errors.New("database unavailable")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cache := &reviewInflightCreditCache{welfareDelayedDeductionCache: &welfareDelayedDeductionCache{balance: 20, exists: true}, reservations: newMemInflightCache(0)}
			repo := &welfareAuthoritativeBalanceRepo{balance: tc.actual, err: tc.err}
			cfg := &config.Config{}
			cfg.Billing.InflightReservation.Enabled = true
			svc := NewBillingCacheService(cache, repo, nil, nil, nil, nil, cfg, nil)
			t.Cleanup(svc.Stop)
			first, err := svc.ReserveInflight(context.Background(), &User{ID: 7}, nil, nil, 15)
			require.NoError(t, err)
			defer first.HandlerDone()
			second, err := svc.ReserveInflight(context.Background(), &User{ID: 7}, nil, nil, 10)
			require.ErrorIs(t, err, ErrInsufficientBalance)
			require.Nil(t, second)
			require.Equal(t, 1, cache.reservations.count(), "a rejected retry must not create another reservation")
		})
	}
}
