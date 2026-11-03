package service

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestOpenAIGatewayServiceRecordUsage_GroupUltrafastAllResponseTiers(t *testing.T) {
	for _, observed := range []string{"", "default", "flex", "priority", "ultrafast"} {
		t.Run(observed, func(t *testing.T) {
			logs := &openAIRecordUsageLogRepoStub{inserted: true}
			users := &openAIRecordUsageUserRepoStub{}
			svc := newOpenAIRecordUsageServiceForTest(logs, users, &openAIRecordUsageSubRepoStub{}, nil)
			svc.resolver = NewModelPricingResolver(nil, svc.billingService)
			in, out, read, write := 0.001, 0.002, 0.0004, 0.003
			group := &Group{ID: 884, Platform: PlatformOpenAI, Hydrated: true, RateMultiplier: 0.5,
				ForceOpenAIUltrafast: true, FreeOpenAIFast: true,
				ModelPricing: []ChannelModelPricing{{Models: []string{"custom-reasoner"}, BillingMode: BillingModeToken,
					InputPrice: &in, OutputPrice: &out, CacheReadPrice: &read, CacheWritePrice: &write}}}
			// Forward paths may already have resolved the response tier before settlement.
			tier := observed
			if tier == "" {
				tier = "ultrafast"
			}
			result := &OpenAIForwardResult{RequestID: "all_models_" + observed, Model: "client-alias", UpstreamModel: "custom-reasoner",
				ServiceTier: &tier, UpstreamResponseServiceTier: observed,
				Usage: OpenAIUsage{InputTokens: 110, OutputTokens: 50, CacheReadInputTokens: 25, CacheCreationInputTokens: 10}}
			err := svc.RecordUsage(context.Background(), &OpenAIRecordUsageInput{Result: result,
				APIKey: &APIKey{ID: 1884, GroupID: &group.ID, Group: group}, User: &User{ID: 2884},
				Account: &Account{ID: 3884, Platform: PlatformOpenAI, Type: AccountTypeAPIKey}})
			require.NoError(t, err)
			require.NotNil(t, logs.lastLog)
			// (75 input + 50 output + 25 cache-read + 10 cache-write) at configured prices.
			require.InDelta(t, 1.72, logs.lastLog.TotalCost, 1e-12)
			require.InDelta(t, 0.86, users.lastAmount, 1e-12)
			require.Equal(t, "ultrafast", *logs.lastLog.ServiceTier)
			require.Equal(t, observed, result.UpstreamResponseServiceTier)
		})
	}
}

func TestCalculateOpenAIRecordUsageCost_GroupUltrafastTextPerRequestAndMedia(t *testing.T) {
	for _, tc := range []struct {
		name     string
		result   OpenAIForwardResult
		mode     BillingMode
		wantTier bool
		want     float64
	}{
		{name: "text_per_request", result: OpenAIForwardResult{Model: "custom-text"}, mode: BillingModePerRequest, want: 8, wantTier: true},
		{name: "image_per_request", result: OpenAIForwardResult{Model: "custom-text", ImageCount: 2, ImageEndpoint: true}, mode: BillingModeImage, want: 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			svc := newOpenAIRecordUsageServiceForTest(&openAIRecordUsageLogRepoStub{}, &openAIRecordUsageUserRepoStub{}, &openAIRecordUsageSubRepoStub{}, nil)
			svc.resolver = NewModelPricingResolver(nil, svc.billingService)
			price := 2.0
			group := &Group{ID: 885, Platform: PlatformOpenAI, ForceOpenAIUltrafast: true,
				ModelPricing: []ChannelModelPricing{{Models: []string{"custom-text"}, BillingMode: tc.mode, PerRequestPrice: &price}}}
			if tc.wantTier {
				tier := "ultrafast"
				tc.result.ServiceTier = &tier
			}
			key := &APIKey{GroupID: &group.ID, Group: group}
			cost, err := svc.calculateOpenAIRecordUsageCost(context.Background(), &tc.result, key, []string{"custom-text"},
				0.5, 0.5, 0.5, 0.5, UsageTokens{InputTokens: 100}, "default", nil, false, time.Now())
			require.NoError(t, err)
			require.InDelta(t, tc.want, cost.ActualCost, 1e-12)
		})
	}
}

func TestCalculateOpenAIRecordUsageCost_GroupUltrafastPreservesMediaAndEmbeddings(t *testing.T) {
	for _, tc := range []struct {
		name   string
		result OpenAIForwardResult
		tier   string
	}{
		{name: "embedding", result: OpenAIForwardResult{Model: "custom-model"}},
		{name: "image_tokens", result: OpenAIForwardResult{Model: "custom-model", ImageEndpoint: true}, tier: "default"},
		{name: "audio", result: OpenAIForwardResult{Model: "custom-model", AudioUsage: &AudioUsage{Mode: "tts", DurationOrUnits: 2}}, tier: "default"},
		{name: "video", result: OpenAIForwardResult{Model: "grok-imagine-video-1.5", VideoCount: 1, VideoDurationSeconds: 1}},
		{name: "web_search", result: OpenAIForwardResult{Model: "custom-model", WebSearchCalls: 2}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			svc := newOpenAIRecordUsageServiceForTest(&openAIRecordUsageLogRepoStub{}, &openAIRecordUsageUserRepoStub{}, &openAIRecordUsageSubRepoStub{}, nil)
			svc.resolver = NewModelPricingResolver(nil, svc.billingService)
			in, out := 0.001, 0.002
			group := &Group{ID: 886, Platform: PlatformOpenAI, ModelPricing: []ChannelModelPricing{{Models: []string{tc.result.Model}, BillingMode: BillingModeToken, InputPrice: &in, OutputPrice: &out}}}
			key := &APIKey{GroupID: &group.ID, Group: group}
			calculate := func() *CostBreakdown {
				cost, err := svc.calculateOpenAIRecordUsageCost(context.Background(), &tc.result, key, []string{tc.result.Model}, 0.5, 0.5, 0.5, 0.5, UsageTokens{InputTokens: 100, OutputTokens: 50}, tc.tier, nil, false, time.Now())
				require.NoError(t, err)
				return cost
			}
			standard := calculate()
			group.ForceOpenAIUltrafast = true
			forced := calculate()
			require.Equal(t, standard, forced)
		})
	}
}

func TestCalculateOpenAIRecordUsageCost_GroupUltrafastDoesNotMultiplySearchSurcharge(t *testing.T) {
	for _, tc := range []struct {
		mode BillingMode
		want float64
	}{
		{BillingModeToken, 0.81}, {BillingModePerRequest, 8.01},
	} {
		t.Run(string(tc.mode), func(t *testing.T) {
			svc := newOpenAIRecordUsageServiceForTest(&openAIRecordUsageLogRepoStub{}, &openAIRecordUsageUserRepoStub{}, &openAIRecordUsageSubRepoStub{}, nil)
			svc.resolver = NewModelPricingResolver(nil, svc.billingService)
			in, out, requestPrice, searchPrice := 0.001, 0.002, 2.0, 10.0
			group := &Group{ID: 887, Platform: PlatformOpenAI, ForceOpenAIUltrafast: true, SearchPricePer1k: &searchPrice,
				ModelPricing: []ChannelModelPricing{{Models: []string{"custom-text"}, BillingMode: tc.mode, InputPrice: &in, OutputPrice: &out, PerRequestPrice: &requestPrice}}}
			key := &APIKey{GroupID: &group.ID, Group: group}
			result := &OpenAIForwardResult{Model: "custom-text", SearchCount: 2}
			cost, err := svc.calculateOpenAIRecordUsageCost(context.Background(), result, key, []string{"custom-text"}, 0.5, 0.5, 0.5, 0.5,
				UsageTokens{InputTokens: 100, OutputTokens: 50}, "ultrafast", nil, false, time.Now())
			require.NoError(t, err)
			require.InDelta(t, tc.want, cost.ActualCost, 1e-12, "text is 8x, two searches remain 10/1000 * 2 * 0.5")
		})
	}
}
