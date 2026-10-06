//go:build unit

package service

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestReviewInflightChannelTimePricingMatchesTokenBilling(t *testing.T) {
	for _, multiplier := range []float64{10, 0.1} {
		for _, contextKind := range []string{"gateway", "openai"} {
			t.Run(fmt.Sprintf("%s/%gx", contextKind, multiplier), func(t *testing.T) {
				at := time.Date(2026, 10, 5, 10, 0, 0, 0, time.UTC)
				timePricing := &ChannelTimePricing{Timezone: "UTC", Periods: []ChannelTimePricingPeriod{{StartTime: "09:00", EndTime: "11:00", Multiplier: multiplier}}}
				billing, resolver := newTokenCostTestEnv(t, PlatformOpenAI, []ChannelModelPricing{{
					Platform: PlatformOpenAI, Models: []string{"timed-model"}, BillingMode: BillingModeToken,
					InputPrice: testPtrFloat64(1e-6), OutputPrice: testPtrFloat64(2e-6), TimePricing: timePricing,
				}}, nil)
				groupID := int64(100)
				group := &Group{ID: groupID, Platform: PlatformOpenAI, RateMultiplier: 2}
				apiKey := &APIKey{User: &User{ID: 7}, GroupID: &groupID, Group: group}
				ctx := context.WithValue(context.Background(), gatewayTokenRequestPricingAtCtxKey{}, at)
				if contextKind == "openai" {
					ctx = context.WithValue(context.Background(), openAIPricingAtCtxKey{}, at)
				}
				charged, err := billing.CalculateTokenCostForRequest(TokenCostRequest{
					Ctx: ctx, Model: "timed-model", Group: group, Tokens: UsageTokens{InputTokens: 1000, OutputTokens: 1000},
					RateMultiplier: 2, PricingAt: at, Resolver: resolver,
				})
				require.NoError(t, err)
				require.InDelta(t, 0.006*multiplier, charged.ActualCost, 1e-12)
				deps := inflightEstimateDeps{billing: billing, resolver: resolver}
				estimate, priced := deps.estimate(ctx, apiKey, InflightEstimateRequest{Model: "timed-model", BodyBytes: 4000, MaxTokens: 1000})
				require.True(t, priced)
				require.InDelta(t, charged.ActualCost, estimate, 1e-12, "identical estimated token counts must retain configured channel time multiplier")
			})
		}
	}
}
