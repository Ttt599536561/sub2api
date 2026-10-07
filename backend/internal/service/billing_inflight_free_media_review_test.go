//go:build unit

package service

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestReviewInflightExplicitFreeMediaRemainsPriced(t *testing.T) {
	zero := 0.0
	for _, tc := range []struct {
		name    string
		request InflightEstimateRequest
	}{
		{"tts", InflightEstimateRequest{Model: "tts", Kind: InflightEstimateAudio, AudioMode: "tts", AudioUnits: 0.001}},
		{"stt", InflightEstimateRequest{Model: "stt", Kind: InflightEstimateAudio, AudioMode: "stt", AudioUnits: 0.01}},
		{"realtime", InflightEstimateRequest{Model: "realtime", Kind: InflightEstimateAudio, AudioMode: "realtime", AudioUnits: 1}},
		{"search", InflightEstimateRequest{Model: "grok-web-search", Kind: InflightEstimatePerRequest, SearchCalls: 1}},
		{"video", InflightEstimateRequest{Model: "grok-imagine-video", Kind: InflightEstimateVideo, VideoResolution: "480p", VideoDurationSeconds: 5}},
		{"image", InflightEstimateRequest{Model: "grok-imagine-image", Kind: InflightEstimateImage, Units: 1}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			gateway := newInflightEstimateGateway(t, nil)
			group := &Group{
				ID: 100, Platform: PlatformGrok, RateMultiplier: 1,
				AudioTTSPricePerMillionChars: &zero, AudioSTTPricePerHour: &zero,
				AudioRealtimePricePerMin: &zero, SearchPricePer1k: &zero,
				VideoPrice480P: &zero, VideoPrice720P: &zero, VideoPrice1080P: &zero,
				ImagePrice1K: &zero, ImagePrice2K: &zero, ImagePrice4K: &zero,
			}
			key := &APIKey{User: &User{ID: 7}, GroupID: &group.ID, Group: group}
			var actual *CostBreakdown
			switch tc.request.Kind {
			case InflightEstimateAudio:
				actual = gateway.billingService.CalculateAudioCost(tc.request.AudioMode, tc.request.AudioUnits, groupAudioPriceConfigFromAPIKey(key), 1)
			case InflightEstimatePerRequest:
				actual = gateway.billingService.CalculateSearchCost(tc.request.SearchCalls, groupSearchPricePer1kFromAPIKey(key), 1)
			case InflightEstimateVideo:
				actual = gateway.billingService.CalculateVideoCost(tc.request.Model, tc.request.VideoResolution, 1, tc.request.VideoDurationSeconds, videoPriceConfigFromAPIKey(key), 1)
			case InflightEstimateImage:
				actual = gateway.billingService.CalculateImageCost(tc.request.Model, "2K", 1, imagePriceConfigFromAPIKey(key), 1)
			}
			require.NotNil(t, actual)
			require.Zero(t, actual.ActualCost, "precondition: explicitly configured free settlement")
			estimate, priced := gateway.EstimateInflightReservation(context.Background(), key, tc.request)
			require.True(t, priced, "explicit zero pricing must not trigger fail_closed_on_unpriced")
			require.Zero(t, estimate, "explicitly free media must not consume balance reservations")
		})
	}
}

func TestReviewInflightMediaZeroDoesNotHideOtherPricing(t *testing.T) {
	zero := 0.0
	for _, tc := range []struct {
		name         string
		group        Group
		request      InflightEstimateRequest
		wantPriced   bool
		wantPositive bool
	}{
		{"missing audio override uses default", Group{}, InflightEstimateRequest{Model: "tts", Kind: InflightEstimateAudio, AudioMode: "tts", AudioUnits: 1}, true, true},
		{"different audio mode uses default", Group{AudioSTTPricePerHour: &zero}, InflightEstimateRequest{Model: "tts", Kind: InflightEstimateAudio, AudioMode: "tts", AudioUnits: 1}, true, true},
		{"missing search override uses default", Group{}, InflightEstimateRequest{Model: "grok-web-search", Kind: InflightEstimatePerRequest, SearchCalls: 1}, true, true},
		{"missing image override uses default", Group{}, InflightEstimateRequest{Model: "grok-imagine-image", Kind: InflightEstimateImage}, true, true},
		{"other image tiers retain default", Group{ImagePrice1K: &zero, ImagePrice2K: &zero}, InflightEstimateRequest{Model: "grok-imagine-image", Kind: InflightEstimateImage}, true, true},
		{"missing video override uses default", Group{}, InflightEstimateRequest{Model: "grok-imagine-video", Kind: InflightEstimateVideo, VideoResolution: "480p"}, true, true},
		{"different video resolution uses default", Group{VideoPrice480P: &zero}, InflightEstimateRequest{Model: "grok-imagine-video", Kind: InflightEstimateVideo, VideoResolution: "720p"}, true, true},
		{"free images do not hide token rule", Group{ImagePrice1K: &zero, ImagePrice2K: &zero, ImagePrice4K: &zero, ModelPricing: []ChannelModelPricing{{Models: []string{"image-token-rule"}, BillingMode: BillingModeToken, InputPrice: testPtrFloat64(1e-6), OutputPrice: testPtrFloat64(2e-6)}}}, InflightEstimateRequest{Model: "image-token-rule", Kind: InflightEstimateImage, BodyBytes: 4000, MaxTokens: 1000}, true, true},
		{"free search does not price unknown token", Group{SearchPricePer1k: &zero}, InflightEstimateRequest{Model: "unknown-review-token", Kind: InflightEstimateToken, SearchCalls: 1}, false, false},
		{"free audio does not price unknown mode", Group{AudioTTSPricePerMillionChars: &zero}, InflightEstimateRequest{Model: "unknown-review-audio", Kind: InflightEstimateAudio, AudioMode: "unknown", AudioUnits: 1}, false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			gateway := newInflightEstimateGateway(t, nil)
			group := tc.group
			group.ID, group.Platform, group.RateMultiplier = 100, PlatformGrok, 1
			key := &APIKey{User: &User{ID: 7}, GroupID: &group.ID, Group: &group}
			estimate, priced := gateway.EstimateInflightReservation(context.Background(), key, tc.request)
			require.Equal(t, tc.wantPriced, priced)
			if tc.wantPositive {
				require.Greater(t, estimate, 0.0)
			} else {
				require.Zero(t, estimate)
			}
		})
	}
}

// Exercise the actual RecordUsage dispatch and the atomic billing command, not
// just the per-image calculator: catalog token prices and nonzero upstream token
// usage must still honor an explicitly free flat media price.
func TestReviewInflightFreeMediaMatchesRecordUsageWithCatalogTokens(t *testing.T) {
	for _, tc := range []struct {
		name, model, platform string
		gateway, video        bool
	}{
		{"gateway gpt image", "gpt-image-2", PlatformOpenAI, true, false},
		{"gateway gemini image", "gemini-3-pro-image-preview", PlatformGemini, true, false},
		{"openai gpt image", "gpt-image-2", PlatformOpenAI, false, false},
		{"openai gemini image", "gemini-3-pro-image-preview", PlatformGemini, false, false},
		{"openai grok video", "grok-imagine-video", PlatformGrok, false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			zero := 0.0
			group := &Group{
				ID: 100, Platform: tc.platform, RateMultiplier: 1,
				ImagePrice1K: &zero, ImagePrice2K: &zero, ImagePrice4K: &zero,
				VideoPrice480P: &zero, VideoPrice720P: &zero, VideoPrice1080P: &zero,
			}
			user := &User{ID: 7, Balance: 10}
			key := &APIKey{ID: 8, User: user, GroupID: &group.ID, Group: group}
			account := &Account{ID: 9, Platform: tc.platform}
			catalog := newStubPricingServiceFromMap(map[string]*LiteLLMModelPricing{
				tc.model: {InputCostPerToken: 5e-6, OutputCostPerToken: 10e-6, OutputCostPerImageToken: 30e-6},
			})
			usageRepo := &openAIRecordUsageLogRepoStub{inserted: true}
			billingRepo := &openAIRecordUsageBillingRepoStub{result: &UsageBillingApplyResult{Applied: true}}
			request := InflightEstimateRequest{Model: tc.model, Kind: InflightEstimateImage, BodyBytes: 4000, MaxTokens: 1000, Units: 1}
			var estimate float64
			var priced bool
			var resolved *ResolvedPricing
			if tc.gateway {
				svc := newGatewayRecordUsageServiceWithBillingRepoForTest(usageRepo, billingRepo, &openAIRecordUsageUserRepoStub{}, &openAIRecordUsageSubRepoStub{})
				svc.billingService = NewBillingService(svc.cfg, catalog)
				svc.resolver = NewModelPricingResolver(nil, svc.billingService)
				resolved = svc.resolver.Resolve(ctx, PricingInput{Model: tc.model, GroupID: key.GroupID, Group: group})
				estimate, priced = svc.EstimateInflightReservation(ctx, key, request)
				require.NoError(t, svc.RecordUsage(ctx, &RecordUsageInput{
					Result: &ForwardResult{
						RequestID: "free-media-review", Model: tc.model, ImageCount: 1, ImageSize: "2K",
						Usage: ClaudeUsage{InputTokens: 1000, OutputTokens: 1000, ImageOutputTokens: 100},
					}, APIKey: key, User: user, Account: account,
				}))
			} else {
				svc := newOpenAIRecordUsageServiceWithBillingRepoForTest(usageRepo, billingRepo, &openAIRecordUsageUserRepoStub{}, &openAIRecordUsageSubRepoStub{}, nil)
				svc.billingService = NewBillingService(svc.cfg, catalog)
				svc.resolver = NewModelPricingResolver(nil, svc.billingService)
				resolved = svc.resolver.Resolve(ctx, PricingInput{Model: tc.model, GroupID: key.GroupID, Group: group})
				result := &OpenAIForwardResult{
					RequestID: "free-media-review", Model: tc.model, ImageCount: 1, ImageSize: "2K",
					Usage: OpenAIUsage{InputTokens: 1000, OutputTokens: 1000, ImageOutputTokens: 100},
				}
				if tc.video {
					request.Kind, request.VideoResolution, request.VideoDurationSeconds = InflightEstimateVideo, "720p", 5
					result.ImageCount, result.ImageSize = 0, ""
					result.VideoCount, result.VideoResolution, result.VideoDurationSeconds = 1, "720p", 5
				}
				estimate, priced = svc.EstimateInflightReservation(ctx, key, request)
				require.NoError(t, svc.RecordUsage(ctx, &OpenAIRecordUsageInput{Result: result, APIKey: key, User: user, Account: account}))
			}
			require.Equal(t, PricingSourceLiteLLM, resolved.Source)
			require.Equal(t, BillingModeToken, resolved.Mode)
			require.NotNil(t, resolved.BasePricing)
			require.Greater(t, resolved.BasePricing.InputPricePerToken, 0.0, "precondition: catalog has paid token pricing")
			require.True(t, priced)
			require.Zero(t, estimate)
			require.NotNil(t, billingRepo.lastCmd)
			require.Zero(t, billingRepo.lastCmd.BalanceCost)
			require.NotNil(t, usageRepo.lastLog)
			require.Zero(t, usageRepo.lastLog.ActualCost)
			require.NotNil(t, usageRepo.lastLog.BillingMode)
			wantMode := BillingModeImage
			if tc.video {
				wantMode = BillingModeVideo
			}
			require.Equal(t, string(wantMode), *usageRepo.lastLog.BillingMode)
		})
	}
}
