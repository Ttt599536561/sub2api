package service

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

// Every database snapshot races a committed balance update and its cache
// invalidation. The fourth snapshot models the balance at the final direct read.
type welfareContendedBalanceRepo struct {
	UserRepository
	cache           *welfareFencedCache
	calls           atomic.Int64
	otherUserCalls  atomic.Int64
	initialBalance  float64
	fallbackBalance float64
	fallbackErr     error
}

func (r *welfareContendedBalanceRepo) GetByID(_ context.Context, id int64) (*User, error) {
	if id != 7 {
		r.otherUserCalls.Add(1)
		return &User{ID: id, Balance: 20}, nil
	}
	call := r.calls.Add(1)
	r.cache.generation.Add(1)
	if call%4 == 0 {
		return &User{ID: id, Balance: r.fallbackBalance}, r.fallbackErr
	}
	return &User{ID: id, Balance: r.initialBalance}, nil
}

func TestWelfareBalanceServiceContentionDoesNotOpenCircuit(t *testing.T) {
	const requests = 32
	cache := &welfareFencedCache{refilled: make(chan welfareBalanceRefill, 4*requests)}
	repo := &welfareContendedBalanceRepo{cache: cache, initialBalance: 20, fallbackBalance: 20}
	cfg := &config.Config{}
	cfg.Billing.CircuitBreaker.Enabled = true // Use the production default threshold of five failures.
	svc := NewBillingCacheService(cache, repo, nil, nil, nil, nil, cfg, nil)
	t.Cleanup(svc.Stop)

	start := make(chan struct{})
	results := make(chan error, requests)
	var requestsDone sync.WaitGroup
	for i := 0; i < requests; i++ {
		requestsDone.Add(1)
		go func() {
			defer requestsDone.Done()
			<-start
			results <- svc.CheckBillingEligibility(context.Background(), &User{ID: 7}, nil, nil, nil, "")
		}()
	}
	close(start)
	requestsDone.Wait()
	close(results)
	var rejected int
	for err := range results {
		if err != nil {
			rejected++
		}
	}

	// One busy account must not cause a global billing outage for another user.
	require.NoError(t, svc.CheckBillingEligibility(context.Background(), &User{ID: 8}, nil, nil, nil, ""))
	require.Zero(t, rejected, "healthy positive balances must remain eligible during concurrent balance updates")
	require.EqualValues(t, 1, repo.otherUserCalls.Load())
	require.LessOrEqual(t, repo.calls.Load(), int64(4*requests), "contention must not cause an unbounded database retry loop")
	require.Equal(t, billingCircuitClosed, svc.circuitBreaker.state)
	require.Zero(t, svc.circuitBreaker.failures)
}

func TestWelfareBalanceServiceContentionFallbackEnforcesBalance(t *testing.T) {
	for _, tc := range []struct {
		name                    string
		initial, final, reserve float64
		wantErr                 error
	}{
		{name: "fresh_positive_balance", initial: 0, final: 20},
		{name: "zero_balance", initial: 20, final: 0, wantErr: ErrInsufficientBalance},
		{name: "negative_balance", initial: 20, final: -1, wantErr: ErrInsufficientBalance},
		{name: "below_minimum_reserve", initial: 20, final: 0.005, reserve: 0.01, wantErr: ErrInsufficientBalance},
		{name: "at_minimum_reserve", initial: 0, final: 0.01, reserve: 0.01},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cache := &welfareFencedCache{refilled: make(chan welfareBalanceRefill, 16)}
			repo := &welfareContendedBalanceRepo{cache: cache, initialBalance: tc.initial, fallbackBalance: tc.final}
			cfg := &config.Config{}
			cfg.Billing.CircuitBreaker.Enabled = true
			cfg.Billing.MinimumBalanceReserve = tc.reserve
			svc := NewBillingCacheService(cache, repo, nil, nil, nil, nil, cfg, nil)
			t.Cleanup(svc.Stop)

			err := svc.CheckBillingEligibility(context.Background(), &User{ID: 7}, nil, nil, nil, "")
			if tc.wantErr != nil {
				require.ErrorIs(t, err, tc.wantErr)
			} else {
				require.NoError(t, err)
			}
			require.EqualValues(t, 4, repo.calls.Load(), "retry three invalidated snapshots, then start one fresh database read")
			require.Zero(t, svc.circuitBreaker.failures, "a successful database read, including an exhausted balance, is not an infrastructure failure")

			svc.Stop()
			close(cache.refilled)
			var refills int
			for refill := range cache.refilled {
				refills++
				require.False(t, refill.stored, "generation CAS must still reject every invalidated snapshot")
			}
			require.Equal(t, 3, refills, "the final direct snapshot must not be queued for cache refill")
			require.Zero(t, atomic.LoadInt64(&cache.balanceUpdates), "the legacy cache setter must not bypass generation fencing")
		})
	}
}

func TestWelfareBalanceServiceContentionFallbackDatabaseFailureStillOpensCircuit(t *testing.T) {
	databaseErr := errors.New("database unavailable on final balance read")
	cache := &welfareFencedCache{refilled: make(chan welfareBalanceRefill, 32)}
	repo := &welfareContendedBalanceRepo{cache: cache, initialBalance: 20, fallbackErr: databaseErr}
	cfg := &config.Config{}
	cfg.Billing.CircuitBreaker.Enabled = true
	svc := NewBillingCacheService(cache, repo, nil, nil, nil, nil, cfg, nil)
	t.Cleanup(svc.Stop)

	for request := 1; request <= 5; request++ {
		err := svc.CheckBillingEligibility(context.Background(), &User{ID: 7}, nil, nil, nil, "")
		require.ErrorIs(t, err, ErrBillingServiceUnavailable)
		require.ErrorIs(t, err, databaseErr, "the final database error must retain its cause for existing failure handling")
		require.Equal(t, int64(4*request), repo.calls.Load())
	}
	require.Equal(t, billingCircuitOpen, svc.circuitBreaker.state)
	require.ErrorIs(t, svc.CheckBillingEligibility(context.Background(), &User{ID: 8}, nil, nil, nil, ""), ErrBillingServiceUnavailable)
	require.Zero(t, repo.otherUserCalls.Load(), "real database failures should still trip the existing circuit breaker")
}
