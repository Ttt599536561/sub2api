package service

import (
	"context"
	"errors"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

type cancellationBalanceRepo struct {
	UserRepository
	read func(context.Context, int64) (*User, error)
}

func (r *cancellationBalanceRepo) GetByID(ctx context.Context, id int64) (*User, error) {
	return r.read(ctx, id)
}

type cancellationGenerationCache struct{ billingCacheWorkerStub }

func (c *cancellationGenerationCache) UserBalanceGeneration(ctx context.Context, _ int64) (int64, error) {
	return 0, ctx.Err()
}

func (c *cancellationGenerationCache) SetUserBalanceIfGeneration(context.Context, int64, float64, int64) (bool, error) {
	return true, nil
}

type cancellationAdmissionService struct {
	read func(context.Context) error
}

func (s *cancellationAdmissionService) GetSubscriptionForAdmission(ctx context.Context, _, _ int64) (*UserSubscription, *Group, error) {
	return nil, nil, s.read(ctx)
}

func (s *cancellationAdmissionService) TriggerAutoDailyReset(context.Context, int64) error {
	return nil
}

func newCancellationBillingService(read func(context.Context) error, subscription bool) *BillingCacheService {
	svc := &BillingCacheService{
		cfg: &config.Config{},
		circuitBreaker: newBillingCircuitBreaker(config.CircuitBreakerConfig{
			Enabled: true, FailureThreshold: 5, HalfOpenRequests: 2,
		}),
		userRepo: &cancellationBalanceRepo{read: func(ctx context.Context, id int64) (*User, error) {
			return &User{ID: id, Balance: 20}, read(ctx)
		}},
	}
	if subscription {
		svc.subscriptionService = &cancellationAdmissionService{read: func(ctx context.Context) error {
			if err := read(ctx); err != nil {
				return ErrBillingServiceUnavailable.WithCause(err)
			}
			return ErrSubscriptionExpired // A healthy database can still reject admission.
		}}
	}
	return svc
}

func checkCancellationBillingEligibility(svc *BillingCacheService, ctx context.Context) error {
	return svc.CheckBillingEligibility(ctx, &User{ID: 7}, nil, &Group{ID: 2}, nil, "")
}

func expireCancellationBreaker(breaker *billingCircuitBreaker) {
	breaker.mu.Lock()
	defer breaker.mu.Unlock()
	breaker.openedAt = time.Now().Add(-2 * breaker.resetTimeout)
}

func TestBillingCallerCancellationAfterFencedReadDoesNotOpenCircuit(t *testing.T) {
	var cancelCaller context.CancelFunc
	var healthyReads, canceledReads atomic.Int64
	repo := &cancellationBalanceRepo{read: func(ctx context.Context, id int64) (*User, error) {
		if err := ctx.Err(); err != nil {
			canceledReads.Add(1)
			return nil, err
		}
		healthyReads.Add(1)
		if cancelCaller != nil {
			cancelCaller()
		}
		return &User{ID: id, Balance: 20}, nil
	}}
	cfg := &config.Config{}
	cfg.Billing.CircuitBreaker.Enabled = true
	svc := NewBillingCacheService(&cancellationGenerationCache{}, repo, nil, nil, nil, nil, cfg, nil)
	t.Cleanup(svc.Stop)

	for i := 0; i < 5; i++ {
		ctx, cancel := context.WithCancel(context.Background())
		cancelCaller = cancel
		err := checkCancellationBillingEligibility(svc, ctx)
		cancel()
		require.ErrorIs(t, err, context.Canceled)
	}
	require.EqualValues(t, 5, healthyReads.Load(), "each detached database read must succeed before caller cancellation")
	require.EqualValues(t, 5, canceledReads.Load(), "the generation-check fallback must observe the caller cancellation")
	cancelCaller = nil
	require.NoError(t, checkCancellationBillingEligibility(svc, context.Background()), "client disconnects must not deny unrelated healthy requests")
}

func TestBillingCallerCancellationPreservesFailureStreak(t *testing.T) {
	for _, subscription := range []bool{false, true} {
		for _, deadline := range []bool{false, true} {
			t.Run(fmt.Sprintf("subscription=%t/deadline=%t", subscription, deadline), func(t *testing.T) {
				svc := newCancellationBillingService(func(ctx context.Context) error { return fmt.Errorf("read interrupted: %w", ctx.Err()) }, subscription)
				breaker := svc.circuitBreaker
				breaker.OnFailure(errors.New("previous database failure"))
				var ctx context.Context
				var cancel context.CancelFunc
				if deadline {
					ctx, cancel = context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
				} else {
					ctx, cancel = context.WithCancel(context.Background())
					cancel()
				}
				defer cancel()
				require.ErrorIs(t, checkCancellationBillingEligibility(svc, ctx), ctx.Err())
				require.Equal(t, billingCircuitClosed, breaker.state)
				require.Equal(t, 1, breaker.failures, "cancellation must neither add failures nor erase the preceding database failure")
			})
		}
	}
}

func TestBillingCallerCancellationReleasesHalfOpenProbe(t *testing.T) {
	for _, subscription := range []bool{false, true} {
		t.Run(fmt.Sprintf("subscription=%t", subscription), func(t *testing.T) {
			svc := newCancellationBillingService(func(ctx context.Context) error { return ctx.Err() }, subscription)
			breaker := svc.circuitBreaker
			breaker.failureThreshold = 1
			breaker.OnFailure(errors.New("previous database outage"))
			expireCancellationBreaker(breaker)
			for i := 0; i < 5; i++ {
				ctx, cancel := context.WithCancel(context.Background())
				cancel()
				require.ErrorIs(t, checkCancellationBillingEligibility(svc, ctx), context.Canceled)
				require.Equal(t, billingCircuitHalfOpen, breaker.state, "cancellation must not reopen or close the recovering circuit")
				require.Equal(t, 1, breaker.failures)
			}
			require.True(t, breaker.Allow(), "canceled probes must release capacity for a new health check")
			require.True(t, breaker.Allow())
			require.False(t, breaker.Allow(), "releasing canceled probes must not grow the configured capacity")
		})
	}
}

func TestBillingLateCallerCancellationDoesNotReleaseAnotherProbe(t *testing.T) {
	for _, startHalfOpen := range []bool{false, true} {
		t.Run(fmt.Sprintf("started_half_open=%t", startHalfOpen), func(t *testing.T) {
			started, release := make(chan struct{}), make(chan struct{})
			svc := newCancellationBillingService(func(ctx context.Context) error {
				close(started)
				<-release
				return ctx.Err()
			}, false)
			breaker := svc.circuitBreaker
			breaker.failureThreshold, breaker.halfOpenRequests = 1, 1
			if startHalfOpen {
				breaker.OnFailure(errors.New("earlier database outage"))
				expireCancellationBreaker(breaker)
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			result := make(chan error, 1)
			go func() { result <- checkCancellationBillingEligibility(svc, ctx) }()
			<-started
			breaker.OnFailure(errors.New("new database outage"))
			expireCancellationBreaker(breaker)
			require.True(t, breaker.Allow(), "reserve the new recovery cycle's only probe")
			cancel()
			close(release)
			require.ErrorIs(t, <-result, context.Canceled)
			require.Equal(t, billingCircuitHalfOpen, breaker.state)
			require.False(t, breaker.Allow(), "an older canceled request must not return the current probe's capacity")
		})
	}
}

func TestBillingDatabaseErrorsStillOpenCircuit(t *testing.T) {
	for _, subscription := range []bool{false, true} {
		for _, databaseErr := range []error{errors.New("database unavailable"), context.DeadlineExceeded, context.Canceled} {
			t.Run(fmt.Sprintf("subscription=%t/%v", subscription, databaseErr), func(t *testing.T) {
				svc := newCancellationBillingService(func(context.Context) error { return databaseErr }, subscription)
				for i := 0; i < 5; i++ {
					require.ErrorIs(t, checkCancellationBillingEligibility(svc, context.Background()), databaseErr)
				}
				require.Equal(t, billingCircuitOpen, svc.circuitBreaker.state, "errors from the dependency must still trip the breaker while the caller context is healthy")
				require.False(t, svc.circuitBreaker.Allow())
				expireCancellationBreaker(svc.circuitBreaker)
				require.ErrorIs(t, checkCancellationBillingEligibility(svc, context.Background()), databaseErr)
				require.Equal(t, billingCircuitOpen, svc.circuitBreaker.state, "a failed half-open database probe must reopen the breaker")
				require.False(t, svc.circuitBreaker.Allow())
			})
		}
	}
}

func TestBillingDatabaseErrorWithCanceledCallerStillCounts(t *testing.T) {
	databaseErr := errors.New("database connection lost")
	svc := newCancellationBillingService(func(context.Context) error { return databaseErr }, false)
	svc.circuitBreaker.failureThreshold = 1
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	require.ErrorIs(t, checkCancellationBillingEligibility(svc, ctx), databaseErr)
	require.Equal(t, billingCircuitOpen, svc.circuitBreaker.state, "caller cancellation must not hide a separate database failure")
}
