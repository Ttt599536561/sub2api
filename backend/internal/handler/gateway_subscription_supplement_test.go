package handler

import (
	"context"
	"encoding/json"
	"math"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestReserveInflightBalance_SupplementDoesNotDependOnBalanceSwitch(t *testing.T) {
	billing := service.NewBillingCacheService(nil, nil, nil, nil, nil, nil, &config.Config{}, nil)
	t.Cleanup(billing.Stop)
	key := &service.APIKey{User: &service.User{ID: 1}, Group: &service.Group{SubscriptionType: service.SubscriptionTypeSubscription}}
	var subscription service.UserSubscription
	require.NoError(t, json.Unmarshal([]byte(`{"ID":9,"SupplementGrantedUSD":20}`), &subscription))
	estimator := &countingEstimator{priced: true}
	_, done, err := reserveInflightBalanceCtx(context.Background(), billing, estimator, key, &subscription, tokenInflightEstimate("model", nil))
	require.NoError(t, err)
	done()
	require.Equal(t, 1, estimator.calls, "one-time subscription credit must pass through pricing even when balance reservations are disabled")
}

func TestReserveInflightBalance_SupplementRequiresPricingAndDependencies(t *testing.T) {
	billing := service.NewBillingCacheService(nil, nil, nil, nil, nil, nil, &config.Config{}, nil)
	t.Cleanup(billing.Stop)
	key := &service.APIKey{User: &service.User{ID: 1}, Group: &service.Group{SubscriptionType: service.SubscriptionTypeSubscription}}
	subscription := &service.UserSubscription{ID: 9, SupplementGrantedUSD: 20}
	for _, tc := range []struct {
		name      string
		billing   *service.BillingCacheService
		estimator inflightReservationEstimator
		key       *service.APIKey
	}{
		{"no billing", nil, &countingEstimator{priced: true}, key},
		{"no estimator", billing, nil, key},
		{"no key", billing, &countingEstimator{priced: true}, nil},
		{"no user", billing, &countingEstimator{priced: true}, &service.APIKey{}},
		{"no group", billing, &countingEstimator{priced: true}, &service.APIKey{User: key.User}},
		{"unknown zero price", billing, &countingEstimator{}, key},
		{"unknown positive price", billing, &countingEstimator{cost: 1}, key},
		{"nan price", billing, &countingEstimator{cost: math.NaN(), priced: true}, key},
		{"infinite price", billing, &countingEstimator{cost: math.Inf(1), priced: true}, key},
		{"negative price", billing, &countingEstimator{cost: -1, priced: true}, key},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, done, err := reserveInflightBalanceCtx(context.Background(), tc.billing, tc.estimator, tc.key, subscription, tokenInflightEstimate("model", nil))
			require.ErrorIs(t, err, service.ErrBillingServiceUnavailable)
			done()
		})
	}
}
