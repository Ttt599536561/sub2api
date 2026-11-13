//go:build unit

package service

import (
	"errors"
	"testing"

	"github.com/dgraph-io/ristretto"
	"github.com/stretchr/testify/require"
)

func TestWelfareSubscriptionCacheInvalidatorEvictsAndPropagatesFailure(t *testing.T) {
	for _, fail := range []string{"none", "invalidate", "publish"} {
		t.Run(fail, func(t *testing.T) {
			cache, err := ristretto.NewCache(&ristretto.Config{NumCounters: 1000, MaxCost: 100, BufferItems: 64})
			require.NoError(t, err)
			t.Cleanup(cache.Close)
			key := subCacheKey(11, 13)
			require.True(t, cache.Set(key, &UserSubscription{ID: 99}, 1))
			cache.Wait()
			backend := &refundCompensationCache{}
			failure := errors.New("cache unavailable")
			if fail == "invalidate" {
				backend.invalidateError = failure
			}
			if fail == "publish" {
				backend.publishError = failure
			}
			svc := &SubscriptionService{subCacheL1: cache, billingCacheService: &BillingCacheService{cache: backend}}
			invalidator, ok := any(svc).(interface{ InvalidateWelfareSubscriptionCaches(int64, int64) error })
			require.True(t, ok, "subscription service must expose committed welfare cache invalidation")
			err = invalidator.InvalidateWelfareSubscriptionCaches(11, 13)
			if fail == "none" {
				require.NoError(t, err)
			} else {
				require.ErrorIs(t, err, failure)
			}
			_, exists := cache.Get(key)
			require.False(t, exists)
			require.Equal(t, 1, backend.invalidations)
			if fail != "invalidate" {
				require.Equal(t, 1, backend.publications)
			}
		})
	}
}
