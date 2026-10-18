//go:build unit

package service

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/ctxkey"
	"github.com/stretchr/testify/require"
)

func TestInflightEstimate_GroupUltrafastMatchesSettlementAndAdmission(t *testing.T) {
	for _, model := range []string{"gpt-6.1-sol", "gpt-6-astra"} {
		for _, enabled := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/enabled=%t", model, enabled), func(t *testing.T) {
				ctx := context.Background()
				usageRepo := &openAIRecordUsageLogRepoStub{inserted: true}
				userRepo := &openAIRecordUsageUserRepoStub{}
				svc := newOpenAIRecordUsageServiceForTest(usageRepo, userRepo, &openAIRecordUsageSubRepoStub{}, nil)
				svc.cfg.Billing.InflightReservation = config.InflightReservationConfig{
					Enabled: true, DefaultMaxTokens: 100, MaxInputTokens: 200000, MaxOutputTokens: 128000,
				}
				svc.resolver = NewModelPricingResolver(nil, svc.billingService)
				inputPrice, outputPrice := 0.001, 0.002
				group := &Group{
					ID: 880, Platform: PlatformOpenAI, Hydrated: true, Status: StatusActive,
					RateMultiplier: 0.5, ForceOpenAIUltrafast: enabled, FreeOpenAIFast: true,
					ModelPricing: []ChannelModelPricing{{Models: []string{model}, BillingMode: BillingModeToken, InputPrice: &inputPrice, OutputPrice: &outputPrice}},
				}
				user := &User{ID: 2880}
				key := &APIKey{ID: 1880, GroupID: &group.ID, Group: group, User: user}
				estimate, priced := svc.EstimateInflightReservation(ctx, key, InflightEstimateRequest{
					Model: model, BodyBytes: 400, MaxTokens: 100, Kind: InflightEstimateToken,
				})
				require.True(t, priced)
				t.Run("quotation", func(t *testing.T) {
					tier := ""
					want := 0.15
					if enabled {
						tier = OpenAIFastTierUltrafast
						want *= 8
					}
					err := svc.RecordUsage(ctx, &OpenAIRecordUsageInput{
						APIKey: key, User: user, Account: &Account{ID: 3880, Platform: PlatformOpenAI, Type: AccountTypeAPIKey},
						Result: &OpenAIForwardResult{
							RequestID: "resp_reservation_" + model, Model: model, UpstreamModel: model, ServiceTier: &tier,
							Usage: OpenAIUsage{InputTokens: 100, OutputTokens: 100},
						},
					})
					require.NoError(t, err)
					require.NotNil(t, usageRepo.lastLog)
					require.InDelta(t, want, usageRepo.lastLog.ActualCost, 1e-12)
					require.InDelta(t, want, userRepo.lastAmount, 1e-12)
					require.InDelta(t, want, estimate, 1e-12, "reservation must use the same forced tier and effective prices as settlement")
				})
				t.Run("concurrent_admission", func(t *testing.T) {
					cache := newMemInflightCache(1.50)
					reservationSvc := NewBillingCacheService(cache, nil, nil, nil, nil, nil, svc.cfg, nil)
					t.Cleanup(reservationSvc.Stop)
					first, err := reservationSvc.ReserveInflight(ctx, user, group, nil, estimate)
					require.NoError(t, err)
					require.NotNil(t, first)
					defer first.HandlerDone()
					second, err := reservationSvc.ReserveInflight(ctx, user, group, nil, estimate)
					if second != nil {
						defer second.HandlerDone()
					}
					if enabled {
						require.ErrorIs(t, err, ErrInsufficientBalance)
						require.Nil(t, second)
					} else {
						require.NoError(t, err)
						require.NotNil(t, second)
					}
				})
			})
		}
	}
}

func TestInflightEstimate_GroupUltrafastUsesActualModelForRequestedPricing(t *testing.T) {
	for _, tc := range []struct {
		name          string
		requestModel  string
		channelModel  string
		accountModels []string
		want          float64
	}{
		{name: "channel_alias_to_target", requestModel: "client-model", channelModel: "gpt-6-astra", want: 1.20},
		{name: "target_mapped_to_non_target", requestModel: "gpt-6-astra", channelModel: "gpt-5.4", want: 0.15},
		{name: "priced_alias_account_mapping_to_target", requestModel: "client-model", accountModels: []string{"gpt-6.1-sol"}, want: 1.20},
		{name: "non_target_unchanged", requestModel: "gpt-5.4", want: 0.15},
	} {
		t.Run(tc.name, func(t *testing.T) {
			inputPrice, outputPrice := 0.001, 0.002
			group := &Group{
				ID: 881, Platform: PlatformOpenAI, Status: StatusActive, Hydrated: true, RateMultiplier: 0.5, ForceOpenAIUltrafast: true,
				ModelPricing: []ChannelModelPricing{{Models: []string{tc.requestModel}, BillingMode: BillingModeToken, InputPrice: &inputPrice, OutputPrice: &outputPrice}},
			}
			billing := NewBillingService(&config.Config{}, nil)
			deps := inflightEstimateDeps{cfg: &config.Config{}, billing: billing, resolver: NewModelPricingResolver(nil, billing)}
			if tc.channelModel != "" {
				deps.resolveMapping = func(context.Context, int64, string) ChannelMappingResult {
					return ChannelMappingResult{MappedModel: tc.channelModel, BillingModelSource: BillingModelSourceRequested}
				}
			}
			if tc.accountModels != nil {
				deps.accountMappedModels = func(context.Context, *APIKey, string) []string { return tc.accountModels }
			}
			key := &APIKey{GroupID: &group.ID, Group: group, User: &User{ID: 2881}}
			estimate, priced := deps.estimate(context.Background(), key, InflightEstimateRequest{Model: tc.requestModel, BodyBytes: 400, MaxTokens: 100})
			require.True(t, priced)
			require.InDelta(t, tc.want, estimate, 1e-12)
		})
	}
}

func TestInflightEstimate_GroupUltrafastWithoutResolver(t *testing.T) {
	for _, model := range []string{"gpt-6.1-sol", "gpt-6-astra"} {
		t.Run(model, func(t *testing.T) {
			group := &Group{ID: 882, Platform: PlatformOpenAI, RateMultiplier: 1, ForceOpenAIUltrafast: true}
			key := &APIKey{GroupID: &group.ID, Group: group, User: &User{ID: 2882}}
			billing := NewBillingService(&config.Config{}, nil)
			override := 8.0
			want, err := billing.CalculateCostUnified(CostInput{
				Model: model, Group: group, GroupID: &group.ID, Tokens: UsageTokens{InputTokens: 100, OutputTokens: 100},
				RateMultiplier: 1, ServiceTier: OpenAIFastTierUltrafast, UltrafastMultiplierOverride: &override,
			})
			require.NoError(t, err)
			estimate, priced := (inflightEstimateDeps{billing: billing}).estimate(context.Background(), key, InflightEstimateRequest{Model: model, BodyBytes: 400, MaxTokens: 100})
			require.True(t, priced)
			require.InDelta(t, want.ActualCost, estimate, 1e-12)
		})
	}
}

func TestInflightEstimate_GroupUltrafastUsesCompleteAccountCandidates(t *testing.T) {
	for _, tc := range []struct {
		name, requestModel, channelModel                          string
		normalModel, compactModel, compactKey, globalCompactModel string
		finalModel                                                string
		mixed, passthrough, unsupported                           bool
		legacyCompact, nativeCompact, retryCompact                bool
		wantUltrafast                                             bool
	}{
		{name: "all_accounts_map_target_out", requestModel: "gpt-6-astra", finalModel: "gpt-5.4"},
		{name: "channel_then_all_accounts_map_target_out", requestModel: "client-model", channelModel: "gpt-6-astra", finalModel: "gpt-5.4"},
		{name: "mixed_accounts_retain_target_candidate", requestModel: "gpt-6-astra", mixed: true, finalModel: "gpt-6-astra", wantUltrafast: true},
		{name: "passthrough_ignores_regular_mapping", requestModel: "gpt-6-astra", passthrough: true, finalModel: "gpt-6-astra", wantUltrafast: true},
		{name: "unsupported_identity_is_not_a_candidate", requestModel: "gpt-6-astra", unsupported: true, finalModel: "gpt-5.4"},
		{name: "legacy_compact_specific_target", requestModel: "gpt-6-astra", compactModel: "gpt-6.1-sol", legacyCompact: true, finalModel: "gpt-6.1-sol", wantUltrafast: true},
		{name: "legacy_compact_configured_target", requestModel: "gpt-6-astra", globalCompactModel: "gpt-6.1-sol", legacyCompact: true, finalModel: "gpt-6.1-sol", wantUltrafast: true},
		{name: "legacy_compact_maps_out_of_target", requestModel: "gpt-6-astra", normalModel: "gpt-6-astra", compactModel: "gpt-5.4", legacyCompact: true, finalModel: "gpt-5.4"},
		{name: "native_compact_fallback_target", requestModel: "gpt-6-astra", compactModel: "gpt-6.1-sol", nativeCompact: true, retryCompact: true, finalModel: "gpt-6.1-sol", wantUltrafast: true},
		{name: "native_compact_keeps_initial_target", requestModel: "gpt-6-astra", normalModel: "gpt-6-astra", compactModel: "gpt-5.4", nativeCompact: true, finalModel: "gpt-6-astra", wantUltrafast: true},
		{name: "legacy_compact_chained_mapping_to_target", requestModel: "gpt-6-astra", normalModel: "gpt-5.4", compactKey: "gpt-5.4", compactModel: "gpt-6.1-sol", legacyCompact: true, finalModel: "gpt-6.1-sol", wantUltrafast: true},
		{name: "legacy_compact_chained_mapping_out_of_target", requestModel: "gpt-6-astra", normalModel: "gpt-6.1-sol", compactKey: "gpt-6.1-sol", compactModel: "gpt-5.4", legacyCompact: true, finalModel: "gpt-5.4"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			upstreamInput := tc.channelModel
			if upstreamInput == "" {
				upstreamInput = tc.requestModel
			}
			normalModel := tc.normalModel
			if normalModel == "" {
				normalModel = "gpt-5.4"
			}
			inputPrice, outputPrice := 0.001, 0.002
			group := &Group{
				ID: 883, Platform: PlatformOpenAI, Status: StatusActive, Hydrated: true, RateMultiplier: 0.5, ForceOpenAIUltrafast: true,
				ModelPricing: []ChannelModelPricing{{Models: []string{tc.requestModel}, BillingMode: BillingModeToken, InputPrice: &inputPrice, OutputPrice: &outputPrice}},
			}
			accounts := []Account{{
				ID: 883, Name: "mapped-out", Platform: PlatformOpenAI, Type: AccountTypeAPIKey, Status: StatusActive, Schedulable: true, Concurrency: 1,
				Credentials: map[string]any{"api_key": "sk-test", "model_mapping": map[string]any{upstreamInput: normalModel}},
				Extra:       map[string]any{"openai_responses_supported": true},
			}}
			if tc.compactModel != "" {
				compactKey := tc.compactKey
				if compactKey == "" {
					compactKey = upstreamInput
				}
				accounts[0].Credentials["compact_model_mapping"] = map[string]any{compactKey: tc.compactModel}
			}
			if tc.passthrough {
				accounts[0].Extra["openai_passthrough"] = true
			}
			if tc.mixed || tc.unsupported {
				accounts = append(accounts, Account{
					ID: 884, Name: "target-retained", Platform: PlatformOpenAI, Type: AccountTypeAPIKey, Status: StatusActive, Schedulable: true, Concurrency: 1,
					Credentials: map[string]any{"api_key": "sk-test"}, Extra: map[string]any{"openai_responses_supported": true},
				})
			}
			if tc.unsupported {
				accounts[1].Credentials["model_mapping"] = map[string]any{"other-model": "gpt-6-astra"}
			}
			channel := Channel{
				ID: 883, Status: StatusActive, GroupIDs: []int64{group.ID}, BillingModelSource: BillingModelSourceRequested,
				ModelMapping: map[string]map[string]string{PlatformOpenAI: {tc.requestModel: upstreamInput}},
			}
			cs := newTestChannelService(makeStandardRepo(channel, map[int64]string{group.ID: PlatformOpenAI}))
			estimator := newInflightEstimateGateway(t, cs)
			estimator.cfg.Gateway.OpenAICompactModel = tc.globalCompactModel
			snap := &inflightSnapshotCacheStub{byBucket: map[string][]Account{inflightBucketKey(group.ID, PlatformOpenAI): accounts}}
			accountRepo := attachInflightSnapshot(estimator, snap)
			user := &User{ID: 2883}
			key := &APIKey{ID: 1883, GroupID: &group.ID, Group: group, User: user}
			estimate, priced := estimator.EstimateInflightReservation(ctx, key, InflightEstimateRequest{
				Model: tc.requestModel, BodyBytes: 400, MaxTokens: 100,
				OpenAICompactPath: tc.legacyCompact, OpenAICompactionFallback: tc.nativeCompact,
			})
			require.True(t, priced)
			want := 0.15
			if tc.wantUltrafast {
				want *= 8
			}
			require.InDelta(t, want, estimate, 1e-12, "reservation must reflect complete route-aware model candidates")
			require.Zero(t, accountRepo.dbCalls.Load(), "candidate resolution must use the scheduler snapshot")

			account := &accounts[0]
			if tc.mixed {
				account = &accounts[1]
			}
			usageRepo := &openAIRecordUsageLogRepoStub{inserted: true}
			userRepo := &openAIRecordUsageUserRepoStub{}
			forwarder := newOpenAIRecordUsageServiceForTest(usageRepo, userRepo, &openAIRecordUsageSubRepoStub{}, nil)
			forwarder.cfg.Gateway.OpenAICompactModel = tc.globalCompactModel
			forwarder.resolver = NewModelPricingResolver(nil, forwarder.billingService)
			response := &http.Response{
				StatusCode: http.StatusOK, Header: http.Header{"Content-Type": []string{"application/json"}},
				Body: io.NopCloser(strings.NewReader(`{"id":"resp_account_candidates","model":"` + tc.finalModel + `","status":"completed","output":[],"usage":{"input_tokens":100,"output_tokens":100}}`)),
			}
			upstream := &httpUpstreamRecorder{resp: response}
			if tc.retryCompact {
				upstream.responses = []*http.Response{
					{StatusCode: http.StatusBadRequest, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(strings.NewReader(`{"error":{"code":"context_length_exceeded","message":"context window exceeded"}}`))}, response,
				}
			}
			forwarder.httpUpstream = upstream
			body := []byte(`{"model":"` + upstreamInput + `","input":"hello","stream":false}`)
			if tc.nativeCompact {
				body = []byte(`{"model":"` + upstreamInput + `","input":[{"type":"message","role":"user","content":"hello"},{"type":"compaction_trigger"}],"stream":false}`)
			}
			path := "/v1/responses"
			if tc.legacyCompact {
				path += "/compact"
			}
			c := newOpenAICompactFallbackTestContext(t, path)
			if tc.nativeCompact {
				MarkOpenAINativeCompactionV2(c)
			}
			groupCtx := context.WithValue(ctx, ctxkey.Group, group)
			c.Request = c.Request.WithContext(groupCtx)
			result, err := forwarder.Forward(groupCtx, c, account, body)
			require.NoError(t, err)
			actualModel := result.UpstreamModel
			if actualModel == "" {
				actualModel = result.Model
			}
			require.Equal(t, tc.finalModel, actualModel)
			if tc.wantUltrafast {
				require.NotNil(t, result.ServiceTier)
				require.Equal(t, OpenAIFastTierUltrafast, *result.ServiceTier)
			} else {
				require.Nil(t, result.ServiceTier)
			}
			require.NoError(t, forwarder.RecordUsage(ctx, &OpenAIRecordUsageInput{
				APIKey: key, User: user, Account: account, Result: result,
				OriginalModel: tc.requestModel, ChannelMappedModel: upstreamInput, BillingModelSource: BillingModelSourceRequested,
			}))
			require.InDelta(t, want, usageRepo.lastLog.ActualCost, 1e-12)
			require.InDelta(t, want, userRepo.lastAmount, 1e-12)
			cache := newMemInflightCache(1.50)
			reservationSvc := NewBillingCacheService(cache, nil, nil, nil, nil, nil, estimator.cfg, nil)
			t.Cleanup(reservationSvc.Stop)
			first, err := reservationSvc.ReserveInflight(ctx, user, group, nil, estimate)
			require.NoError(t, err)
			require.NotNil(t, first)
			defer first.HandlerDone()
			second, err := reservationSvc.ReserveInflight(ctx, user, group, nil, estimate)
			if second != nil {
				defer second.HandlerDone()
			}
			if tc.wantUltrafast {
				require.ErrorIs(t, err, ErrInsufficientBalance)
			} else {
				require.NoError(t, err)
				require.NotNil(t, second)
			}
		})
	}
}
