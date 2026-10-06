//go:build unit

package service

import (
	"context"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

type reviewAuthoritativeInflightCache struct {
	*memInflightCache
	requestIDs []string
	balances   []float64
}

func (c *reviewAuthoritativeInflightCache) ReserveInflightBalance(ctx context.Context, userID int64, id string, amount, balance float64, ttl time.Duration) (bool, float64, error) {
	c.requestIDs = append(c.requestIDs, id)
	c.balances = append(c.balances, balance)
	return c.memInflightCache.ReserveInflightBalance(ctx, userID, id, amount, balance, ttl)
}

type reviewAuthoritativeInflightRepo struct {
	UserRepository
	read func(context.Context) (*User, error)
}

func (r *reviewAuthoritativeInflightRepo) GetByID(ctx context.Context, _ int64) (*User, error) {
	return r.read(ctx)
}

func newReviewAuthoritativeInflightService(t *testing.T, repo UserRepository) (*BillingCacheService, *reviewAuthoritativeInflightCache) {
	t.Helper()
	cache := &reviewAuthoritativeInflightCache{memInflightCache: newMemInflightCache(20)}
	allowed, _, err := cache.memInflightCache.ReserveInflightBalance(context.Background(), 7, "other-request", 15, 20, time.Minute)
	require.NoError(t, err)
	require.True(t, allowed)
	cfg := &config.Config{}
	cfg.Billing.InflightReservation.Enabled = true
	svc := NewBillingCacheService(cache, repo, nil, nil, nil, nil, cfg, nil)
	t.Cleanup(svc.Stop)
	return svc, cache
}

func TestReviewAuthoritativeInflightRetryRetainsConcurrentReservations(t *testing.T) {
	var cache *reviewAuthoritativeInflightCache
	repo := &reviewAuthoritativeInflightRepo{read: func(ctx context.Context) (*User, error) {
		// Another request reserves after the initial rejection and before the
		// authoritative balance read finishes. The retry must include it.
		allowed, _, err := cache.memInflightCache.ReserveInflightBalance(ctx, 7, "concurrent-request", 10, 100, time.Minute)
		require.NoError(t, err)
		require.True(t, allowed)
		return &User{ID: 7, Balance: 30}, nil
	}}
	svc, c := newReviewAuthoritativeInflightService(t, repo)
	cache = c
	reservation, err := svc.ReserveInflight(context.Background(), &User{ID: 7}, nil, nil, 10)
	require.ErrorIs(t, err, ErrInsufficientBalance)
	require.Nil(t, reservation)
	require.Equal(t, []float64{20, 30}, cache.balances)
	require.Len(t, cache.requestIDs, 2)
	require.Equal(t, cache.requestIDs[0], cache.requestIDs[1])
	require.Equal(t, 2, cache.count(), "only the two other requests may remain")
}

func TestReviewAuthoritativeInflightRetryRegistersAndReleasesOnce(t *testing.T) {
	repo := &reviewAuthoritativeInflightRepo{read: func(context.Context) (*User, error) {
		return &User{ID: 7, Balance: 100}, nil
	}}
	svc, cache := newReviewAuthoritativeInflightService(t, repo)
	reservation, err := svc.ReserveInflight(context.Background(), &User{ID: 7}, nil, nil, 10)
	require.NoError(t, err)
	require.NotNil(t, reservation)
	require.Len(t, cache.requestIDs, 2)
	require.Equal(t, cache.requestIDs[0], cache.requestIDs[1])
	require.Equal(t, 2, cache.count())
	billingDone := reservation.Acquire()
	reservation.HandlerDone()
	require.Equal(t, 2, cache.count(), "the retry reservation still belongs to its billing task")
	billingDone()
	billingDone()
	reservation.HandlerDone()
	require.Equal(t, 1, cache.count())
	require.EqualValues(t, 1, cache.releaseCt.Load())
	balance, err := cache.GetUserBalance(context.Background(), 7)
	require.NoError(t, err)
	require.Equal(t, 20.0, balance, "a retry must not overwrite or debit the cache")
}

func TestReviewAuthoritativeInflightCanceledReadRetainsRejection(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	repo := &reviewAuthoritativeInflightRepo{read: func(ctx context.Context) (*User, error) {
		cancel()
		return nil, ctx.Err()
	}}
	svc, cache := newReviewAuthoritativeInflightService(t, repo)
	reservation, err := svc.ReserveInflight(ctx, &User{ID: 7}, nil, nil, 10)
	require.ErrorIs(t, err, ErrInsufficientBalance)
	require.Nil(t, reservation)
	require.Len(t, cache.requestIDs, 1, "a canceled DB read must not retry the reservation")
	require.Equal(t, 1, cache.count())
}

func TestReviewAuthoritativeInflightNilRepoRetainsCacheOnlyRejection(t *testing.T) {
	svc, cache := newReviewAuthoritativeInflightService(t, nil)
	reservation, err := svc.ReserveInflight(context.Background(), &User{ID: 7}, nil, nil, 10)
	require.ErrorIs(t, err, ErrInsufficientBalance)
	require.Nil(t, reservation)
	require.Len(t, cache.requestIDs, 1)
	require.Equal(t, 1, cache.count())
}
