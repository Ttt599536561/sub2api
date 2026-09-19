package service

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

type welfareBalanceRefill struct {
	balance float64
	stored  bool
}

type welfareFencedCache struct {
	billingCacheWorkerStub
	generation        atomic.Int64
	generationFailure atomic.Bool
	refilled          chan welfareBalanceRefill
}

func (c *welfareFencedCache) UserBalanceGeneration(context.Context, int64) (int64, error) {
	if c.generationFailure.Load() {
		return 0, errors.New("generation unavailable")
	}
	return c.generation.Load(), nil
}
func (c *welfareFencedCache) InvalidateUserBalance(context.Context, int64) error {
	c.generation.Add(1)
	return nil
}
func (c *welfareFencedCache) SetUserBalanceIfGeneration(_ context.Context, _ int64, balance float64, generation int64) (bool, error) {
	ok := generation == c.generation.Load()
	c.refilled <- welfareBalanceRefill{balance: balance, stored: ok}
	return ok, nil
}

type welfareInflightBalanceRepo struct {
	UserRepository
	started, release chan struct{}
	calls            atomic.Int64
}

func (r *welfareInflightBalanceRepo) GetByID(ctx context.Context, id int64) (*User, error) {
	if r.calls.Add(1) == 1 {
		close(r.started)
		select {
		case <-r.release:
			return &User{ID: id, Balance: 0}, nil
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	return &User{ID: id, Balance: 20}, nil
}

type welfareBalanceRead struct {
	balance float64
	err     error
}

func welfareReadBalance(svc *BillingCacheService) <-chan welfareBalanceRead {
	done := make(chan welfareBalanceRead, 1)
	go func() {
		balance, err := svc.GetUserBalance(context.Background(), 7)
		done <- welfareBalanceRead{balance: balance, err: err}
	}()
	return done
}

func requireWelfareBalance(t *testing.T, done <-chan welfareBalanceRead, want float64) {
	t.Helper()
	select {
	case result := <-done:
		require.NoError(t, result.err)
		require.Equal(t, want, result.balance)
	case <-time.After(2 * time.Second):
		t.Fatal("balance read did not complete")
	}
}

func TestWelfareBalanceServiceCarriesGenerationFromBeforeDBRead(t *testing.T) {
	cache := &welfareFencedCache{refilled: make(chan welfareBalanceRefill, 16)}
	repo := &welfareInflightBalanceRepo{started: make(chan struct{}), release: make(chan struct{})}
	svc := NewBillingCacheService(cache, repo, nil, nil, nil, nil, &config.Config{}, nil)
	t.Cleanup(svc.Stop)
	var release sync.Once
	defer release.Do(func() { close(repo.release) })
	done := welfareReadBalance(svc)
	select {
	case <-repo.started:
	case <-time.After(time.Second):
		t.Fatal("database read not started")
	}
	// Another instance commits the credit and invalidates shared Redis. The
	// reader must observe that generation without any local invalidation call.
	require.NoError(t, cache.InvalidateUserBalance(context.Background(), 7))
	release.Do(func() { close(repo.release) })
	requireWelfareBalance(t, done, 20)
	svc.Stop()
	close(cache.refilled)
	var currentStored bool
	for refill := range cache.refilled {
		if refill.stored {
			require.Equal(t, 20.0, refill.balance, "a pre-credit snapshot must never repopulate the cache")
			currentStored = true
		}
	}
	require.True(t, currentStored, "the fresh snapshot should still populate the cache")
	require.Zero(t, atomic.LoadInt64(&cache.balanceUpdates), "unsafe legacy setter must not run on the production cache")
}

func TestWelfareBalanceServicePostCreditReadDoesNotJoinOldFlight(t *testing.T) {
	cache := &welfareFencedCache{refilled: make(chan welfareBalanceRefill, 16)}
	repo := &welfareInflightBalanceRepo{started: make(chan struct{}), release: make(chan struct{})}
	svc := NewBillingCacheService(cache, repo, nil, nil, nil, nil, &config.Config{}, nil)
	t.Cleanup(svc.Stop)
	var release sync.Once
	defer release.Do(func() { close(repo.release) })
	oldRead := welfareReadBalance(svc)
	select {
	case <-repo.started:
	case <-time.After(time.Second):
		t.Fatal("database read not started")
	}
	require.NoError(t, cache.InvalidateUserBalance(context.Background(), 7))
	// This request begins after the remote credit. It must get the new balance
	// while the pre-credit read is still blocked, not wait for or reuse that read.
	requireWelfareBalance(t, welfareReadBalance(svc), 20)
	release.Do(func() { close(repo.release) })
	requireWelfareBalance(t, oldRead, 20)
}

type welfareChangingBalanceRepo struct {
	UserRepository
	cache *welfareFencedCache
	calls atomic.Int64
}

func (r *welfareChangingBalanceRepo) GetByID(context.Context, int64) (*User, error) {
	r.calls.Add(1)
	r.cache.generation.Add(1)
	return &User{Balance: 0}, nil
}

func TestWelfareBalanceServiceBoundsRetriesDuringContinuousChanges(t *testing.T) {
	cache := &welfareFencedCache{refilled: make(chan welfareBalanceRefill, 16)}
	repo := &welfareChangingBalanceRepo{cache: cache}
	svc := NewBillingCacheService(cache, repo, nil, nil, nil, nil, &config.Config{}, nil)
	t.Cleanup(svc.Stop)
	balance, err := svc.GetUserBalance(context.Background(), 7)
	require.NoError(t, err, "continuous changes should fall back to a fresh database read")
	require.Zero(t, balance)
	require.EqualValues(t, 4, repo.calls.Load(), "continuous debits must stop after three retries and one direct database read")
}

type welfareGenerationFailureRepo struct {
	UserRepository
	cache *welfareFencedCache
	calls atomic.Int64
}

func (r *welfareGenerationFailureRepo) GetByID(context.Context, int64) (*User, error) {
	if r.calls.Add(1) == 1 {
		r.cache.generationFailure.Store(true)
		return &User{Balance: 0}, nil
	}
	return &User{Balance: 20}, nil
}

func TestWelfareBalanceServiceGenerationFailureUsesFreshUnsharedRead(t *testing.T) {
	cache := &welfareFencedCache{refilled: make(chan welfareBalanceRefill, 16)}
	repo := &welfareGenerationFailureRepo{cache: cache}
	svc := NewBillingCacheService(cache, repo, nil, nil, nil, nil, &config.Config{}, nil)
	t.Cleanup(svc.Stop)
	balance, err := svc.GetUserBalance(context.Background(), 7)
	require.NoError(t, err)
	require.Equal(t, 20.0, balance, "if Redis fails after the shared read, fall back to a new database snapshot")
	require.EqualValues(t, 2, repo.calls.Load())
}

// This adapter models the Redis balance operations, with a barrier before the
// queued deduction's atomic generation increment and subtraction.
type welfareDelayedDeductionCache struct {
	billingCacheWorkerStub
	mu               sync.Mutex
	balance          float64
	exists           bool
	generation       int64
	deductionStarted chan struct{}
	deductionRelease chan struct{}
	deductionDone    chan struct{}
	refilled         chan float64
}

func (c *welfareDelayedDeductionCache) GetUserBalance(context.Context, int64) (float64, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.exists {
		return 0, errors.New("cache miss")
	}
	return c.balance, nil
}

func (c *welfareDelayedDeductionCache) UserBalanceGeneration(context.Context, int64) (int64, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.generation, nil
}

func (c *welfareDelayedDeductionCache) SetUserBalanceIfGeneration(_ context.Context, _ int64, balance float64, generation int64) (bool, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if generation != c.generation {
		return false, nil
	}
	c.balance, c.exists = balance, true
	if c.refilled != nil {
		c.refilled <- balance
	}
	return true, nil
}

func (c *welfareDelayedDeductionCache) InvalidateUserBalance(context.Context, int64) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.generation++
	c.exists = false
	return nil
}

func (c *welfareDelayedDeductionCache) DeductUserBalance(ctx context.Context, _ int64, amount float64) error {
	close(c.deductionStarted)
	select {
	case <-c.deductionRelease:
	case <-ctx.Done():
		return ctx.Err()
	}
	c.mu.Lock()
	c.generation++
	if c.exists {
		c.balance -= amount
	}
	c.mu.Unlock()
	close(c.deductionDone)
	return nil
}

type welfareAuthoritativeBalanceRepo struct {
	UserRepository
	balance float64
	err     error
	calls   atomic.Int64
}

func (r *welfareAuthoritativeBalanceRepo) GetByID(_ context.Context, id int64) (*User, error) {
	r.calls.Add(1)
	return &User{ID: id, Balance: r.balance}, r.err
}

func TestWelfareBalanceServiceDelayedDeductionCannotRejectRedeemedBalance(t *testing.T) {
	cache := &welfareDelayedDeductionCache{
		balance: 100, exists: true, refilled: make(chan float64, 16),
		deductionStarted: make(chan struct{}), deductionRelease: make(chan struct{}), deductionDone: make(chan struct{}),
	}
	repo := &welfareAuthoritativeBalanceRepo{balance: 10} // SQL has committed the $90 debit.
	svc := NewBillingCacheService(cache, repo, nil, nil, nil, nil, &config.Config{}, nil)
	t.Cleanup(svc.Stop)
	var release sync.Once
	defer release.Do(func() { close(cache.deductionRelease) })
	svc.QueueDeductBalance(7, 90)
	select {
	case <-cache.deductionStarted:
	case <-time.After(time.Second):
		t.Fatal("queued deduction did not start")
	}
	repo.balance = 30 // Redemption commits $20 after the debit.
	require.NoError(t, svc.InvalidateUserBalance(context.Background(), 7))
	require.NoError(t, svc.InvalidateUserBalance(context.Background(), 7)) // Outbox also completes.
	balance, err := svc.GetUserBalance(context.Background(), 7)
	require.NoError(t, err)
	require.Equal(t, 30.0, balance)
	select {
	case balance := <-cache.refilled:
		require.Equal(t, 30.0, balance)
	case <-time.After(time.Second):
		t.Fatal("post-redemption balance was not cached")
	}
	release.Do(func() { close(cache.deductionRelease) })
	select {
	case <-cache.deductionDone:
	case <-time.After(time.Second):
		t.Fatal("delayed deduction did not complete")
	}
	balance, err = cache.GetUserBalance(context.Background(), 7)
	require.NoError(t, err)
	require.Equal(t, -60.0, balance, "the old task subtracts an already-settled debit from the new cache snapshot")
	require.NoError(t, svc.checkBalanceEligibility(context.Background(), 7), "actual positive account balance must still permit API admission")
	svc.Stop()
	balance, err = cache.GetUserBalance(context.Background(), 7)
	require.NoError(t, err)
	require.Equal(t, 30.0, balance, "the authoritative balance should repair the cache using generation CAS")
}

func TestWelfareBalanceServiceLowCacheUsesAuthoritativeEligibility(t *testing.T) {
	databaseErr := errors.New("database unavailable")
	for _, tc := range []struct {
		name                    string
		cached, actual, reserve float64
		databaseErr, wantErr    error
	}{
		{name: "negative_cache", cached: -60, actual: 30},
		{name: "below_reserve", cached: 0.005, actual: 0.02, reserve: 0.01},
		{name: "still_exhausted", cached: -1, actual: 0, wantErr: ErrInsufficientBalance},
		{name: "still_below_reserve", cached: 0.004, actual: 0.005, reserve: 0.01, wantErr: ErrInsufficientBalance},
		{name: "database_error", cached: 0, databaseErr: databaseErr, wantErr: ErrBillingServiceUnavailable},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cache := &welfareDelayedDeductionCache{balance: tc.cached, exists: true}
			repo := &welfareAuthoritativeBalanceRepo{balance: tc.actual, err: tc.databaseErr}
			cfg := &config.Config{}
			cfg.Billing.MinimumBalanceReserve = tc.reserve
			svc := NewBillingCacheService(cache, repo, nil, nil, nil, nil, cfg, nil)
			t.Cleanup(svc.Stop)
			err := svc.checkBalanceEligibility(context.Background(), 7)
			if tc.wantErr != nil {
				require.ErrorIs(t, err, tc.wantErr)
			} else {
				require.NoError(t, err)
			}
			require.EqualValues(t, 1, repo.calls.Load(), "cached rejection needs one authoritative read")
		})
	}
}

func TestWelfareBalanceServiceLowCacheBypassesInflightRead(t *testing.T) {
	cache := &welfareDelayedDeductionCache{}
	repo := &welfareInflightBalanceRepo{started: make(chan struct{}), release: make(chan struct{})}
	svc := NewBillingCacheService(cache, repo, nil, nil, nil, nil, &config.Config{}, nil)
	t.Cleanup(svc.Stop)
	var release sync.Once
	defer release.Do(func() { close(repo.release) })
	oldRead := welfareReadBalance(svc)
	select {
	case <-repo.started:
	case <-time.After(time.Second):
		t.Fatal("database read did not start")
	}
	// Another worker leaves a low cache while a pre-credit read is in flight.
	// Even before outbox invalidation arrives, rechecking this hit needs a new
	// database snapshot rather than joining that old flight of the same generation.
	stored, err := cache.SetUserBalanceIfGeneration(context.Background(), 7, 0, 0)
	require.NoError(t, err)
	require.True(t, stored)
	requireWelfareBalance(t, welfareReadBalance(svc), 20)
	require.NoError(t, cache.InvalidateUserBalance(context.Background(), 7))
	release.Do(func() { close(repo.release) })
	requireWelfareBalance(t, oldRead, 20)
}
