//go:build unit

package service

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/ctxkey"
	"github.com/Wei-Shaw/sub2api/internal/pkg/timezone"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestGroupUltrafastImageReservationRequiresKnownSafeSnapshot(t *testing.T) {
	imageAccount := Account{ID: 9911, Platform: PlatformOpenAI, Type: AccountTypeAPIKey}
	imageMapped := Account{ID: 9912, Platform: PlatformOpenAI, Type: AccountTypeAPIKey,
		Credentials: map[string]any{"model_mapping": map[string]any{"gpt-image-2": "gpt-image-1"}}}
	rawChat := Account{Platform: PlatformOpenAI, Type: AccountTypeAPIKey, Extra: map[string]any{"openai_responses_supported": false}}
	passthrough := Account{Platform: PlatformOpenAI, Type: AccountTypeAPIKey, Extra: map[string]any{"openai_passthrough": true}}
	textMapped := Account{ID: 9913, Platform: PlatformOpenAI, Type: AccountTypeAPIKey,
		Credentials: map[string]any{"model_mapping": map[string]any{"gpt-image-*": "gpt-5.4"}}}
	for _, tc := range []struct {
		name                           string
		accounts                       []Account
		listErr                        error
		noSnapshot, plainText, compact bool
		want                           float64
	}{
		{name: "identity_image", accounts: []Account{imageAccount}, want: 0.15},
		{name: "image_to_image", accounts: []Account{imageMapped}, want: 0.15},
		{name: "all_images", accounts: []Account{imageAccount, imageMapped}, want: 0.15},
		{name: "image_to_text_wildcard", accounts: []Account{textMapped}, want: 1.2},
		{name: "raw_chat_skips_image_bridge", accounts: []Account{imageAccount, rawChat}, want: 1.2},
		{name: "passthrough_skips_image_bridge", accounts: []Account{passthrough}, want: 1.2},
		{name: "mixed_candidates", accounts: []Account{imageAccount, textMapped}, want: 1.2},
		{name: "empty_snapshot", want: 1.2},
		{name: "snapshot_error", listErr: errors.New("snapshot unavailable"), want: 1.2},
		{name: "missing_snapshot", noSnapshot: true, want: 1.2},
		{name: "compact_is_text", accounts: []Account{imageAccount}, compact: true, want: 1.2},
		{name: "priced_text_does_not_read_snapshot", accounts: []Account{imageAccount}, plainText: true, want: 1.2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			model := "gpt-image-2"
			if tc.plainText {
				model = "gpt-5.4"
			}
			price := 0.3
			group := &Group{ID: 9910, Platform: PlatformOpenAI, RateMultiplier: 0.5, ForceOpenAIUltrafast: true,
				ModelPricing: []ChannelModelPricing{{Models: []string{model, "gpt-image-1"}, BillingMode: BillingModePerRequest, PerRequestPrice: &price}}}
			key := &APIKey{GroupID: &group.ID, Group: group, User: &User{ID: 9914}}
			billing := NewBillingService(&config.Config{}, nil)
			deps := inflightEstimateDeps{billing: billing, resolver: NewModelPricingResolver(nil, billing)}
			reads := 0
			if !tc.noSnapshot {
				deps.imageRouteModels = inflightImageRouteModelsFromSnapshot(func(context.Context, *int64, string, bool) ([]Account, error) {
					reads++
					return tc.accounts, tc.listErr
				}, func(context.Context, *APIKey, string) (string, bool, bool) { return PlatformOpenAI, false, true })
			}
			body := []byte(`{"model":"` + model + `","input":"draw"}`)
			estimate, priced := deps.estimate(context.Background(), key, InflightEstimateRequest{Model: model, BodyBytes: len(body), MaxTokens: 100,
				ImageOnlyRequest: imageOnlyRequestForTest(model, body), OpenAICompactPath: tc.compact})
			require.True(t, priced)
			require.InDelta(t, tc.want, estimate, 1e-12)
			if tc.plainText || tc.compact || tc.noSnapshot {
				require.Zero(t, reads)
			} else {
				require.Equal(t, 1, reads)
			}
		})
	}
}

func TestGroupUltrafastImageReservationStrippedOrRemapped(t *testing.T) {
	for _, tc := range []struct {
		name, model, body, target string
		strip, sparkDriver        bool
	}{
		{name: "spark_strips_forced_tool", model: "gpt-5.3-codex-spark", body: `{"model":"gpt-5.3-codex-spark","input":"hello","tools":[{"type":"image_generation"}],"tool_choice":{"type":"image_generation"}}`},
		{name: "account_policy_strips_forced_tool", model: "gpt-5.4", body: `{"model":"gpt-5.4","input":"hello","tools":[{"type":"image_generation"}],"tool_choice":{"type":"image_generation"}}`, strip: true},
		{name: "image_alias_mapped_to_text", model: "gpt-image-2", body: `{"model":"gpt-image-2","input":"hello"}`, target: "gpt-5.4"},
		{name: "image_driver_spark_strips_tool", model: "gpt-image-2", body: `{"model":"gpt-image-2","input":"hello","tools":[{"type":"image_generation"}],"tool_choice":{"type":"image_generation"}}`, sparkDriver: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if tc.sparkDriver {
				t.Setenv("SUB2API_IMAGES_MAIN_MODEL", "gpt-5.3-codex-spark")
			}
			logs := &openAIRecordUsageLogRepoStub{inserted: true}
			users := &openAIRecordUsageUserRepoStub{}
			svc := newOpenAIRecordUsageServiceForTest(logs, users, &openAIRecordUsageSubRepoStub{}, nil)
			svc.resolver = NewModelPricingResolver(nil, svc.billingService)
			price := 0.3
			group := &Group{ID: 9991, Platform: PlatformOpenAI, Hydrated: true, Status: StatusActive, RateMultiplier: 0.5, ForceOpenAIUltrafast: true, AllowImageGeneration: true, ModelPricing: []ChannelModelPricing{{Models: []string{tc.model, "gpt-5.4"}, BillingMode: BillingModePerRequest, PerRequestPrice: &price}}}
			user := &User{ID: 9992}
			key := &APIKey{ID: 9993, GroupID: &group.ID, Group: group, User: user}
			account := &Account{ID: 9994, Platform: PlatformOpenAI, Type: AccountTypeAPIKey, Credentials: map[string]any{"api_key": "sk-test", "base_url": "https://example.com"}, Extra: map[string]any{"openai_responses_supported": true}}
			if tc.strip {
				account.Extra[featureKeyCodexImageGenerationExplicitToolPolicy] = codexImageGenerationExplicitToolPolicyStrip
			}
			if tc.target != "" {
				account.Credentials["model_mapping"] = map[string]any{tc.model: tc.target}
			}
			upstream := &httpUpstreamRecorder{resp: &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(strings.NewReader(`{"id":"text_response","output":[{"type":"message","content":[{"type":"output_text","text":"hello"}]}],"usage":{"input_tokens":1,"output_tokens":2}}`))}}
			svc.httpUpstream = upstream
			snap := &inflightSnapshotCacheStub{byBucket: map[string][]Account{inflightBucketKey(group.ID, PlatformOpenAI): {*account}}}
			svc.schedulerSnapshot = NewSchedulerSnapshotService(snap, nil, &inflightCountingAccountRepo{}, nil, svc.cfg)
			ctx := context.WithValue(context.Background(), ctxkey.Group, group)
			c := newOpenAICompactFallbackTestContext(t, "/v1/responses")
			c.Set("api_key", key)
			c.Request = c.Request.WithContext(ctx)
			if tc.strip {
				c.Request.Header.Set("User-Agent", "codex_cli_rs/0.98.0")
			}
			result, err := svc.Forward(ctx, c, account, []byte(tc.body))
			require.NoError(t, err)
			require.Zero(t, result.ImageCount)
			require.False(t, gjson.GetBytes(upstream.lastBody, `tools.#(type=="image_generation")`).Exists())
			require.NoError(t, svc.RecordUsage(ctx, &OpenAIRecordUsageInput{APIKey: key, User: user, Account: account, Result: result, OriginalModel: tc.model}))
			estimate, priced := svc.EstimateInflightReservation(ctx, key, InflightEstimateRequest{Model: tc.model, BodyBytes: len(tc.body), MaxTokens: 100, ImageOnlyRequest: imageOnlyRequestForTest(tc.model, []byte(tc.body))})
			require.True(t, priced)
			t.Logf("estimate=%v settled=%v upstream=%s", estimate, users.lastAmount, string(upstream.lastBody))
			require.InDelta(t, users.lastAmount, estimate, 1e-12)
		})
	}
}

func TestGroupUltrafastImageReservationChoiceAndStrip(t *testing.T) {
	for _, tc := range []struct {
		name, body                      string
		strip, image, independent, peak bool
		imageRate, want                 float64
	}{
		{name: "image_model_explicit_none", body: `{"model":"gpt-image-2","input":"hello","tool_choice":"none"}`},
		{name: "image_model_explicit_auto", body: `{"model":"gpt-image-2","input":"hello","tool_choice":"auto"}`},
		{name: "image_model_duplicate_choice", body: `{"model":"gpt-image-2","input":"hello","tool_choice":{"type":"image_generation"},"tool_choice":"none"}`},
		{name: "image_model_duplicate_choice_type", body: `{"model":"gpt-image-2","input":"hello","tools":[{"type":"function","name":"lookup","parameters":{"type":"object"}}],"tool_choice":{"type":"image_generation","type":"function","name":"lookup"}}`},
		{name: "image_model_strip_policy_reinjects", body: `{"model":"gpt-image-2","input":"draw","tools":[{"type":"image_generation"}],"tool_choice":{"type":"image_generation"}}`, strip: true, image: true},
		{name: "independent_image_rate", body: `{"model":"gpt-image-2","input":"draw"}`, image: true, independent: true, imageRate: 1, want: 0.3},
		{name: "image_does_not_use_text_peak", body: `{"model":"gpt-image-2","input":"draw"}`, image: true, peak: true, want: 0.15},
		{name: "independent_image_rate_with_text_peak", body: `{"model":"gpt-image-2","input":"draw"}`, image: true, independent: true, imageRate: 1, peak: true, want: 0.3},
	} {
		t.Run(tc.name, func(t *testing.T) {
			logs := &openAIRecordUsageLogRepoStub{inserted: true}
			users := &openAIRecordUsageUserRepoStub{}
			svc := newOpenAIRecordUsageServiceForTest(logs, users, &openAIRecordUsageSubRepoStub{}, nil)
			svc.resolver = NewModelPricingResolver(nil, svc.billingService)
			price := 0.3
			group := &Group{ID: 9991, Platform: PlatformOpenAI, Hydrated: true, Status: StatusActive, RateMultiplier: 0.5, ForceOpenAIUltrafast: true, AllowImageGeneration: true, ModelPricing: []ChannelModelPricing{{Models: []string{"gpt-image-2"}, BillingMode: BillingModePerRequest, PerRequestPrice: &price}}}
			group.ImageRateIndependent, group.ImageRateMultiplier = tc.independent, tc.imageRate
			if tc.peak {
				group.SubscriptionType = "subscription"
				group.PeakRateEnabled, group.PeakStart, group.PeakEnd, group.PeakRateMultiplier = true, "00:00", "23:59", 3
				if group.PeakMultiplierAt(timezone.Now()) != 3 {
					t.Skip("peak windows cannot include the last minute of the day")
				}
			}
			user := &User{ID: 9992}
			key := &APIKey{ID: 9993, GroupID: &group.ID, Group: group, User: user}
			account := &Account{ID: 9994, Platform: PlatformOpenAI, Type: AccountTypeAPIKey, Credentials: map[string]any{"api_key": "sk-test", "base_url": "https://example.com"}, Extra: map[string]any{"openai_responses_supported": true}}
			if tc.strip {
				account.Extra[featureKeyCodexImageGenerationExplicitToolPolicy] = codexImageGenerationExplicitToolPolicyStrip
			}
			respBody := `{"id":"text_response","output":[{"type":"message","content":[{"type":"output_text","text":"hello"}]}],"usage":{"input_tokens":1,"output_tokens":2}}`
			if tc.name == "image_model_duplicate_choice_type" {
				respBody = `{"id":"function_response","output":[{"type":"function_call","name":"lookup","arguments":"{}","call_id":"call1"}],"usage":{"input_tokens":1,"output_tokens":2}}`
			}
			if tc.image {
				respBody = `{"id":"image_response","output":[{"type":"image_generation_call","result":"image-bytes"}],"usage":{"input_tokens":1,"output_tokens":2}}`
			}
			upstream := &httpUpstreamRecorder{resp: &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(strings.NewReader(respBody))}}
			svc.httpUpstream = upstream
			snap := &inflightSnapshotCacheStub{byBucket: map[string][]Account{inflightBucketKey(group.ID, PlatformOpenAI): {*account}}}
			svc.schedulerSnapshot = NewSchedulerSnapshotService(snap, nil, &inflightCountingAccountRepo{}, nil, svc.cfg)
			ctx := context.WithValue(context.Background(), ctxkey.Group, group)
			c := newOpenAICompactFallbackTestContext(t, "/v1/responses")
			c.Set("api_key", key)
			c.Request = c.Request.WithContext(ctx)
			if tc.strip {
				c.Request.Header.Set("User-Agent", "codex_cli_rs/0.98.0")
			}
			result, err := svc.Forward(ctx, c, account, []byte(tc.body))
			require.NoError(t, err)
			require.True(t, gjson.GetBytes(upstream.lastBody, `tools.#(type=="image_generation")`).Exists())
			require.NoError(t, svc.RecordUsage(ctx, &OpenAIRecordUsageInput{APIKey: key, User: user, Account: account, Result: result, OriginalModel: "gpt-image-2"}))
			if tc.want > 0 {
				require.InDelta(t, tc.want, users.lastAmount, 1e-12, "flat image settlement uses the independent image rate without the token peak factor")
			}
			estimate, priced := svc.EstimateInflightReservation(ctx, key, InflightEstimateRequest{Model: "gpt-image-2", BodyBytes: len(tc.body), MaxTokens: 100, ImageOnlyRequest: imageOnlyRequestForTest("gpt-image-2", []byte(tc.body))})
			require.True(t, priced)
			t.Logf("estimate=%v settled=%v upstream=%s", estimate, users.lastAmount, string(upstream.lastBody))
			require.InDelta(t, users.lastAmount, estimate, 1e-12)
		})
	}
}

func TestGroupUltrafastImageReservationCandidatePrices(t *testing.T) {
	const requestModel, mapped, unknown = "gpt-image-2", "gpt-image-mapped", "gpt-image-unpriced"
	for _, tc := range []struct {
		name        string
		models      []string
		mappedPrice float64
		want        float64
	}{
		{"account_order_does_not_change_maximum", []string{mapped, requestModel, mapped}, .9, .45},
		{"retained_model_can_be_more_expensive", []string{mapped, requestModel}, .1, .15},
		{"sole_less_expensive_route", []string{mapped}, .1, .05},
		{"unknown_candidate_restores_conservative_price", []string{mapped, unknown}, .9, 1.2},
		{"unknown_candidate_preserves_known_expensive_floor", []string{mapped, unknown}, 3, 1.5},
		{"all_candidates_free", []string{mapped}, 0, 0},
		{"unknown_route_costs_at_least_the_default_image_price", []string{unknown}, 0, defaultImageGenerationPrice},
	} {
		t.Run(tc.name, func(t *testing.T) {
			group := &Group{ID: 9966, Platform: PlatformOpenAI, RateMultiplier: .5, ForceOpenAIUltrafast: true, ModelPricing: []ChannelModelPricing{
				{Models: []string{requestModel}, BillingMode: BillingModePerRequest, PerRequestPrice: testPtrFloat64(.3)},
				{Models: []string{mapped}, BillingMode: BillingModeImage, PerRequestPrice: &tc.mappedPrice},
			}}
			if tc.name == "unknown_route_costs_at_least_the_default_image_price" {
				group.ModelPricing[0].PerRequestPrice = testPtrFloat64(.01)
			}
			key := &APIKey{GroupID: &group.ID, Group: group, User: &User{ID: 9967}}
			var accounts []Account
			for i, model := range tc.models {
				accounts = append(accounts, Account{ID: int64(i + 1), Platform: PlatformOpenAI, Type: AccountTypeAPIKey, Credentials: map[string]any{"model_mapping": map[string]any{requestModel: model}}})
			}
			svc := newInflightEstimateGateway(t, nil)
			snap := &inflightSnapshotCacheStub{byBucket: map[string][]Account{inflightBucketKey(group.ID, PlatformOpenAI): accounts}}
			accountRepo := attachInflightSnapshot(svc, snap)
			estimate, priced := svc.EstimateInflightReservation(context.Background(), key, InflightEstimateRequest{Model: requestModel, BodyBytes: 400, MaxTokens: 100, ImageOnlyRequest: true})
			require.True(t, priced)
			require.InDelta(t, tc.want, estimate, 1e-12)
			require.EqualValues(t, 1, snap.reads.Load())
			require.Zero(t, accountRepo.dbCalls.Load())
		})
	}
}

func TestGroupUltrafastImageReservationPricePrecedence(t *testing.T) {
	const model = "gpt-image-2"
	for _, tc := range []struct {
		name                        string
		groupRule, allSizes, effort bool
		oneK                        float64
		want                        float64
	}{
		{"group_model_wins_over_group_size", true, false, false, 4, .15},
		{"group_size_wins_over_channel_model", false, false, false, 4, 2},
		{"other_sizes_use_channel_effort_ceiling", false, false, true, .1, .45},
		{"all_sizes_override_channel_effort", false, true, true, .1, .05},
		{"group_model_effort_ceiling", true, false, true, 4, .45},
	} {
		t.Run(tc.name, func(t *testing.T) {
			group := &Group{ID: 9970, Platform: PlatformOpenAI, RateMultiplier: .5, ForceOpenAIUltrafast: true, ImagePrice1K: &tc.oneK}
			if tc.allSizes {
				group.ImagePrice2K, group.ImagePrice4K = &tc.oneK, &tc.oneK
			}
			rule := ChannelModelPricing{Platform: PlatformOpenAI, Models: []string{model}, BillingMode: BillingModePerRequest, PerRequestPrice: testPtrFloat64(.3)}
			if tc.effort {
				rule.ReasoningEffortMultipliers = map[string]float64{"high": 3}
			}
			channel := Channel{ID: 9971, Status: StatusActive, GroupIDs: []int64{group.ID}, ModelPricing: []ChannelModelPricing{rule}}
			if tc.groupRule {
				group.ModelPricing = []ChannelModelPricing{rule}
			}
			cs := newTestChannelService(makeStandardRepo(channel, map[int64]string{group.ID: PlatformOpenAI}))
			svc := newInflightEstimateGateway(t, cs)
			snap := &inflightSnapshotCacheStub{byBucket: map[string][]Account{inflightBucketKey(group.ID, PlatformOpenAI): {{ID: 9972, Platform: PlatformOpenAI, Type: AccountTypeAPIKey}}}}
			attachInflightSnapshot(svc, snap)
			key := &APIKey{GroupID: &group.ID, Group: group, User: &User{ID: 9973}}
			estimate, priced := svc.EstimateInflightReservation(context.Background(), key, InflightEstimateRequest{Model: model, ImageOnlyRequest: true})
			require.True(t, priced)
			require.InDelta(t, tc.want, estimate, 1e-12)
		})
	}
}
