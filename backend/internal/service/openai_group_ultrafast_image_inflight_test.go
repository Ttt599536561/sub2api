//go:build unit

package service

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/ctxkey"
	"github.com/stretchr/testify/require"
)

func TestGroupUltrafastImageReservationMatchesSettlementAndAdmission(t *testing.T) {
	logs := &openAIRecordUsageLogRepoStub{inserted: true}
	users := &openAIRecordUsageUserRepoStub{}
	svc := newOpenAIRecordUsageServiceForTest(logs, users, &openAIRecordUsageSubRepoStub{}, nil)
	svc.resolver = NewModelPricingResolver(nil, svc.billingService)
	price := 0.3
	group := &Group{ID: 9891, Platform: PlatformOpenAI, Hydrated: true, Status: StatusActive, RateMultiplier: 0.5,
		ForceOpenAIUltrafast: true, AllowImageGeneration: true,
		ModelPricing: []ChannelModelPricing{{Models: []string{"gpt-image-2"}, BillingMode: BillingModePerRequest, PerRequestPrice: &price}}}
	user := &User{ID: 9892}
	key := &APIKey{ID: 9893, GroupID: &group.ID, Group: group, User: user}
	account := &Account{ID: 9894, Platform: PlatformOpenAI, Type: AccountTypeAPIKey,
		Credentials: map[string]any{"api_key": "sk-test", "base_url": "https://example.com"}, Extra: map[string]any{"openai_responses_supported": true}}
	snap := &inflightSnapshotCacheStub{byBucket: map[string][]Account{inflightBucketKey(group.ID, PlatformOpenAI): {*account}}}
	svc.schedulerSnapshot = NewSchedulerSnapshotService(snap, nil, &inflightCountingAccountRepo{}, nil, svc.cfg)
	body := []byte(`{"model":"gpt-image-2","stream":false,"input":"draw","max_output_tokens":100,"tool_choice":{"type":"image_generation"}}`)
	svc.httpUpstream = &httpUpstreamRecorder{resp: &http.Response{StatusCode: http.StatusOK,
		Header: http.Header{"Content-Type": []string{"application/json"}},
		Body:   io.NopCloser(strings.NewReader(`{"id":"image_reservation","output":[{"id":"ig_1","type":"image_generation_call","result":"final-image"}],"usage":{"input_tokens":1,"output_tokens":2}}`))}}
	ctx := context.WithValue(context.Background(), ctxkey.Group, group)
	c := newOpenAICompactFallbackTestContext(t, "/v1/responses")
	c.Request = c.Request.WithContext(ctx)
	result, err := svc.Forward(ctx, c, account, body)
	require.NoError(t, err)
	require.Equal(t, 1, result.ImageCount)
	require.False(t, result.ImageEndpoint, "Responses keeps its existing endpoint billing semantics")
	require.NoError(t, svc.RecordUsage(ctx, &OpenAIRecordUsageInput{APIKey: key, User: user, Account: account, Result: result, OriginalModel: "gpt-image-2"}))
	estimate, priced := svc.EstimateInflightReservation(ctx, key, InflightEstimateRequest{Model: "gpt-image-2", BodyBytes: len(body), MaxTokens: 100, ImageOnlyRequest: imageOnlyRequestForTest("gpt-image-2", body)})
	require.True(t, priced)
	require.InDelta(t, 0.15, users.lastAmount, 1e-12)
	require.InDelta(t, users.lastAmount, estimate, 1e-12, "flat image fees do not acquire the text Ultrafast multiplier")
	cfg := &config.Config{}
	cfg.Billing.InflightReservation = config.InflightReservationConfig{Enabled: true, TTLSeconds: 60}
	reservations := NewBillingCacheService(newMemInflightCache(0.5), nil, nil, nil, nil, nil, cfg, nil)
	t.Cleanup(reservations.Stop)
	for range 2 {
		reservation, err := reservations.ReserveInflight(ctx, user, group, nil, estimate)
		require.NoError(t, err, "balance 0.5 covers two image requests costing 0.15 each")
		require.NotNil(t, reservation)
		defer reservation.HandlerDone()
	}
}

func TestGroupUltrafastImageReservationPricingBoundaries(t *testing.T) {
	for _, tc := range []struct {
		name, requested, pricedModel, mapped, body string
		mode                                       BillingMode
		accountFallback                            bool
		want                                       float64
	}{
		{name: "forced_native_image", requested: "text-driver", pricedModel: "text-driver", mode: BillingModePerRequest,
			body: `{"input":"draw","tools":[{"type":"image_generation"}],"tool_choice":{"type":"image_generation"}}`, want: 1.2},
		{name: "optional_native_tool_is_text", requested: "text-driver", pricedModel: "text-driver", mode: BillingModePerRequest,
			body: `{"tools":[{"type":"image_generation"}],"tool_choice":"auto"}`, want: 1.2},
		{name: "custom_function_is_text", requested: "text-driver", pricedModel: "text-driver", mode: BillingModePerRequest,
			body: `{"tools":[{"type":"function","name":"image_generation"}],"tool_choice":{"type":"function","name":"image_generation"}}`, want: 1.2},
		{name: "image_input_is_text", requested: "text-driver", pricedModel: "text-driver", mode: BillingModePerRequest,
			body: `{"input":[{"type":"input_image","image_url":"data:image/png;base64,abc"}]}`, want: 1.2},
		{name: "channel_mapping_with_requested_price_is_ambiguous", requested: "public-image", pricedModel: "public-image", mapped: "gpt-image-2", mode: BillingModePerRequest, want: 1.2},
		{name: "account_mapping_price_fallback_is_ambiguous", requested: "unpriced-image-alias", pricedModel: "gpt-image-2", accountFallback: true, mode: BillingModePerRequest, want: 1.2},
		{name: "explicit_image_model", requested: "gpt-image-2", pricedModel: "gpt-image-2", mode: BillingModePerRequest,
			body: `{"model":"gpt-image-2","input":"draw"}`, want: 0.15},
		{name: "image_model_disables_tool", requested: "gpt-image-2", pricedModel: "gpt-image-2", mode: BillingModePerRequest,
			body: `{"model":"gpt-image-2","input":"hello","tool_choice":"none"}`, want: 1.2},
		{name: "image_model_channel_maps_to_text", requested: "gpt-image-2", pricedModel: "gpt-image-2", mapped: "text-driver", mode: BillingModePerRequest,
			body: `{"model":"gpt-image-2","input":"hello"}`, want: 1.2},
		{name: "token_pricing_keeps_text_policy", requested: "text-driver", pricedModel: "text-driver", mode: BillingModeToken,
			body: `{"tools":[{"type":"image_generation"}],"tool_choice":{"type":"image_generation"}}`, want: 1.2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			in, out, price := 0.001, 0.002, 0.3
			group := &Group{ID: 9895, Platform: PlatformOpenAI, RateMultiplier: 0.5, ForceOpenAIUltrafast: true,
				ModelPricing: []ChannelModelPricing{{Models: []string{tc.pricedModel}, BillingMode: tc.mode, InputPrice: &in, OutputPrice: &out, PerRequestPrice: &price}}}
			billing := NewBillingService(&config.Config{}, nil)
			deps := inflightEstimateDeps{billing: billing, resolver: NewModelPricingResolver(nil, billing)}
			deps.imageRouteModels = func(_ context.Context, _ *APIKey, model string) []string { return []string{model} }
			if tc.mapped != "" {
				deps.resolveMapping = func(context.Context, int64, string) ChannelMappingResult {
					return ChannelMappingResult{MappedModel: tc.mapped, BillingModelSource: BillingModelSourceRequested}
				}
			}
			if tc.accountFallback {
				deps.accountMappedModels = func(context.Context, *APIKey, string) []string { return []string{"gpt-image-2"} }
			}
			estimate, priced := deps.estimate(context.Background(), &APIKey{GroupID: &group.ID, Group: group, User: &User{ID: 9896}},
				InflightEstimateRequest{Model: tc.requested, BodyBytes: 400, MaxTokens: 100, ImageOnlyRequest: imageOnlyRequestForTest(tc.requested, []byte(tc.body))})
			require.True(t, priced)
			require.InDelta(t, tc.want, estimate, 1e-12)
		})
	}
}

func TestGroupUltrafastImageReservationMappedSettlement(t *testing.T) {
	const requested, channelModel, accountModel, toolModel = "gpt-image-2", "gpt-image-channel", "gpt-image-1", "gpt-image-explicit"
	for _, tc := range []struct {
		name, source, mapped string
		explicitTool         bool
		want                 float64
	}{
		{"explicit_tool_price", "", "", true, .8},
		{"fixed_requested_with_explicit_tool", BillingModelSourceRequested, channelModel, true, .15},
		{"default_uses_account", "", "", false, .45},
		{"upstream_uses_account", BillingModelSourceUpstream, channelModel, false, .45},
		{"response_model_uses_account_for_images", BillingModelSourceResponse, channelModel, false, .45},
		{"requested_uses_public_price", BillingModelSourceRequested, channelModel, false, .15},
		{"channel_mapped_uses_channel_target", BillingModelSourceChannelMapped, channelModel, false, .25},
		{"identity_channel_falls_through_to_account", BillingModelSourceChannelMapped, "", false, .45},
	} {
		t.Run(tc.name, func(t *testing.T) {
			users := &openAIRecordUsageUserRepoStub{}
			svc := newOpenAIRecordUsageServiceForTest(&openAIRecordUsageLogRepoStub{inserted: true}, users, &openAIRecordUsageSubRepoStub{}, nil)
			group := &Group{ID: 9961, Platform: PlatformOpenAI, Hydrated: true, Status: StatusActive, RateMultiplier: .5, ForceOpenAIUltrafast: true, AllowImageGeneration: true,
				ModelPricing: []ChannelModelPricing{
					{Models: []string{requested}, BillingMode: BillingModePerRequest, PerRequestPrice: testPtrFloat64(.3)},
					{Models: []string{channelModel}, BillingMode: BillingModeImage, PerRequestPrice: testPtrFloat64(.5)},
					{Models: []string{toolModel}, BillingMode: BillingModeImage, PerRequestPrice: testPtrFloat64(1.6)},
					{Models: []string{accountModel}, BillingMode: BillingModePerRequest, PerRequestPrice: testPtrFloat64(.9)},
				}}
			ctx := context.WithValue(context.Background(), ctxkey.Group, group)
			mapping := ChannelMappingResult{MappedModel: requested}
			if tc.source != "" {
				channel := Channel{ID: 9965, Status: StatusActive, GroupIDs: []int64{group.ID}, BillingModelSource: tc.source}
				if tc.mapped != "" {
					channel.ModelMapping = map[string]map[string]string{PlatformOpenAI: {requested: tc.mapped}}
				}
				svc.channelService = newTestChannelService(makeStandardRepo(channel, map[int64]string{group.ID: PlatformOpenAI}))
				mapping = svc.channelService.ResolveChannelMapping(ctx, group.ID, requested)
				require.Equal(t, tc.source, mapping.BillingModelSource)
			}
			svc.resolver = NewModelPricingResolver(svc.channelService, svc.billingService)
			user := &User{ID: 9962}
			key := &APIKey{ID: 9963, GroupID: &group.ID, Group: group, User: user}
			account := &Account{ID: 9964, Platform: PlatformOpenAI, Type: AccountTypeAPIKey,
				Credentials: map[string]any{"api_key": "sk-test", "base_url": "https://example.com", "model_mapping": map[string]any{mapping.MappedModel: accountModel}},
				Extra:       map[string]any{"openai_responses_supported": true}}
			snap := &inflightSnapshotCacheStub{byBucket: map[string][]Account{inflightBucketKey(group.ID, PlatformOpenAI): {*account}}}
			svc.schedulerSnapshot = NewSchedulerSnapshotService(snap, nil, &inflightCountingAccountRepo{}, nil, svc.cfg)
			body := []byte(`{"model":"gpt-image-2","input":"draw"}`)
			expectedBillingModel := accountModel
			if tc.explicitTool {
				body = []byte(`{"model":"gpt-image-2","input":"draw","tools":[{"type":"image_generation","model":"gpt-image-explicit"}]}`)
				expectedBillingModel = toolModel
			}
			imageOnly, explicitTool := OpenAIResponsesImageOnlyEstimate(requested, body)
			estimate, priced := svc.EstimateInflightReservation(ctx, key, InflightEstimateRequest{Model: requested, BodyBytes: len(body), MaxTokens: 100, ImageOnlyRequest: imageOnly, ImageToolModel: explicitTool})
			require.True(t, priced)
			c := newOpenAICompactFallbackTestContext(t, "/v1/responses")
			c.Set("api_key", key)
			c.Request = c.Request.WithContext(ctx)
			svc.httpUpstream = &httpUpstreamRecorder{resp: &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": []string{"application/json"}},
				Body: io.NopCloser(strings.NewReader(`{"id":"mapped_image","output":[{"type":"image_generation_call","result":"image-bytes"}],"usage":{"input_tokens":1,"output_tokens":2}}`))}}
			result, err := svc.Forward(ctx, c, account, svc.ReplaceModelInBody(body, mapping.MappedModel))
			require.NoError(t, err)
			require.Equal(t, 1, result.ImageCount)
			require.Equal(t, expectedBillingModel, result.BillingModel)
			require.NoError(t, svc.RecordUsage(ctx, &OpenAIRecordUsageInput{APIKey: key, User: user, Account: account, Result: result, ChannelUsageFields: mapping.ToUsageFields(requested, result.UpstreamModel)}))
			require.InDelta(t, tc.want, users.lastAmount, 1e-12)
			require.InDelta(t, users.lastAmount, estimate, 1e-12)
		})
	}
}

func imageOnlyRequestForTest(model string, body []byte) bool {
	imageOnly, _ := OpenAIResponsesImageOnlyEstimate(model, body)
	return imageOnly
}
