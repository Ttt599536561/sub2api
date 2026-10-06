//go:build unit

package service

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestReviewInflightMatchesContextPricingRules(t *testing.T) {
	for _, tc := range []struct {
		name     string
		enabled  bool
		interval bool
	}{
		{"catalog ladder enabled", true, false},
		{"catalog ladder disabled", false, false},
		{"channel interval enabled", true, true},
		{"channel interval disabled", false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			pricing := ChannelModelPricing{
				Platform: PlatformGemini, Models: []string{"gemini-2.5-pro"}, BillingMode: BillingModeToken,
				InputPrice: testPtrFloat64(1e-6), OutputPrice: testPtrFloat64(2e-6),
			}
			if tc.interval {
				pricing.Intervals = []PricingInterval{{MinTokens: 200000, InputPrice: testPtrFloat64(5e-6), OutputPrice: testPtrFloat64(10e-6)}}
			}
			billing, resolver := newTokenCostTestEnv(t, PlatformGemini, []ChannelModelPricing{pricing}, geminiLadderCatalogStub(t))
			group := &Group{ID: 100, Platform: PlatformGemini, RateMultiplier: 1, LongContextPricingEnabled: tc.enabled}
			key := &APIKey{User: &User{ID: 7}, GroupID: &group.ID, Group: group}
			tokens := UsageTokens{InputTokens: 300000, OutputTokens: 1000}
			charged, err := billing.CalculateTokenCostForRequest(TokenCostRequest{
				Ctx: context.Background(), Model: "gemini-2.5-pro", Group: group, Tokens: tokens, RateMultiplier: 1, Resolver: resolver,
			})
			require.NoError(t, err)
			deps := inflightEstimateDeps{billing: billing, resolver: resolver}
			estimate, priced := deps.estimate(context.Background(), key, InflightEstimateRequest{Model: "gemini-2.5-pro", BodyBytes: 4 * tokens.InputTokens, MaxTokens: tokens.OutputTokens})
			require.True(t, priced)
			require.InDelta(t, charged.ActualCost, estimate, 1e-12, "identical token counts must honor the billing group context-pricing switch")
		})
	}
}

func TestReviewInflightMatchesDeepSeekFrozenPricing(t *testing.T) {
	for _, contextKind := range []string{"gateway", "openai"} {
		for _, hour := range []int{2, 12} {
			t.Run(contextKind+time.Date(2026, 10, 5, hour, 0, 0, 0, time.UTC).Format("15"), func(t *testing.T) {
				at := time.Date(2026, 10, 5, hour, 0, 0, 0, time.UTC)
				ctx := context.WithValue(context.Background(), gatewayTokenRequestPricingAtCtxKey{}, at)
				if contextKind == "openai" {
					ctx = context.WithValue(context.Background(), openAIPricingAtCtxKey{}, at)
				}
				billing, resolver := newTokenCostTestEnv(t, PlatformDeepseek, nil, nil)
				group := &Group{ID: 100, Platform: PlatformDeepseek, RateMultiplier: 1}
				key := &APIKey{User: &User{ID: 7}, GroupID: &group.ID, Group: group}
				charged, err := billing.CalculateTokenCostForRequest(TokenCostRequest{
					Ctx: ctx, Model: "deepseek-v4-flash", Group: group, Tokens: UsageTokens{InputTokens: 1000, OutputTokens: 1000}, RateMultiplier: 1, Resolver: resolver, PricingAt: at,
				})
				require.NoError(t, err)
				deps := inflightEstimateDeps{billing: billing, resolver: resolver}
				estimate, priced := deps.estimate(ctx, key, InflightEstimateRequest{Model: "deepseek-v4-flash", BodyBytes: 4000, MaxTokens: 1000})
				require.True(t, priced)
				require.InDelta(t, charged.ActualCost, estimate, 1e-12, "reservation must use the same frozen official peak/off-peak rule as settlement")
			})
		}
	}
}

func TestReviewInflightRecognizesExplicitFreeModel(t *testing.T) {
	for _, mode := range []BillingMode{BillingModeToken, BillingModePerRequest} {
		t.Run(string(mode), func(t *testing.T) {
			gateway := newInflightEstimateGateway(t, nil)
			group := &Group{ID: 100, Platform: PlatformOpenAI, RateMultiplier: 1, ModelPricing: []ChannelModelPricing{{
				Models: []string{"free-model"}, BillingMode: mode,
				InputPrice: testPtrFloat64(0), OutputPrice: testPtrFloat64(0), PerRequestPrice: testPtrFloat64(0),
			}}}
			key := &APIKey{User: &User{ID: 7}, GroupID: &group.ID, Group: group}
			charged, err := gateway.billingService.CalculateTokenCostForRequest(TokenCostRequest{
				Ctx: context.Background(), Model: "free-model", Group: group, Tokens: UsageTokens{InputTokens: 1000, OutputTokens: 1000}, RateMultiplier: 1, Resolver: gateway.resolver,
			})
			require.NoError(t, err)
			require.Zero(t, charged.ActualCost)
			deps := gateway.inflightEstimateDeps()
			deps.accountMappedModels = func(context.Context, *APIKey, string) []string {
				t.Fatal("a priced free model must not fall back to a paid account mapping")
				return nil
			}
			estimate, priced := deps.estimate(context.Background(), key, InflightEstimateRequest{Model: "free-model", BodyBytes: 4000, MaxTokens: 1000})
			require.True(t, priced, "explicit free pricing must remain admissible with fail_closed_on_unpriced")
			require.Zero(t, estimate)
		})
	}
}
