//go:build unit

package service

import (
	"context"
	"fmt"
	"github.com/stretchr/testify/require"
	"net/http"
	"testing"

	"github.com/tidwall/gjson"
)

func TestRequestModelContextUltrafastReservation(t *testing.T) {
	for _, tc := range []struct {
		name, requested, mapped, mode string
		passthrough, native           bool
		want                          float64
	}{
		{"messages-replacement", "gpt-5.5", "gpt-6.1-sol", "", true, true, 1.2},
		{"chat-maps-target-out", "gpt-6-astra", "gpt-5.4", "", true, true, .15},
		{"native-ws-replacement", "gpt-5.5", "gpt-6.1-sol", OpenAIWSIngressModeCtxPool, true, false, 1.2},
		{"passthrough-ws-keeps-target", "gpt-6-astra", "gpt-5.4", OpenAIWSIngressModePassthrough, false, false, 1.2},
		{"http-passthrough-keeps-target", "gpt-6-astra", "gpt-5.4", "", true, false, 1.2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			inputPrice, outputPrice := .001, .002
			group := &Group{ID: 8813, Platform: PlatformOpenAI, Hydrated: true, Status: StatusActive, RateMultiplier: .5, ForceOpenAIUltrafast: true, ModelPricing: []ChannelModelPricing{{Models: []string{tc.requested}, BillingMode: BillingModeToken, InputPrice: &inputPrice, OutputPrice: &outputPrice}}}
			account := Account{ID: 8813, Platform: PlatformOpenAI, Type: AccountTypeOAuth, Status: StatusActive, Schedulable: true, Concurrency: 1, Credentials: map[string]any{"model_mapping": map[string]any{tc.requested: tc.mapped}}, Extra: map[string]any{"openai_passthrough": tc.passthrough, "openai_oauth_responses_websockets_v2_mode": tc.mode}}
			estimator := newInflightEstimateGateway(t, nil)
			estimator.cfg.Gateway.OpenAIWS.ModeRouterV2Enabled = true
			repo := attachInflightSnapshot(estimator, &inflightSnapshotCacheStub{byBucket: map[string][]Account{inflightBucketKey(group.ID, PlatformOpenAI): {account}}})
			ctx := context.Background()
			if tc.native {
				ctx = WithOpenAINativeModelMapping(ctx)
			}
			if tc.mode != "" {
				ctx = (&OpenAIGatewayService{cfg: estimator.cfg}).WithOpenAIWSModelRouting(ctx, []byte(`{"type":"response.create"}`))
			}
			estimate, priced := estimator.EstimateInflightReservation(ctx, &APIKey{ID: 8813, GroupID: &group.ID, Group: group, User: &User{ID: 8813}}, InflightEstimateRequest{Model: tc.requested, BodyBytes: 400, MaxTokens: 100})
			require.True(t, priced)
			require.InDelta(t, tc.want, estimate, 1e-12)
			require.Zero(t, repo.dbCalls.Load())
		})
	}
}

func TestRequestModelContextSelectionHonorsActualModelCooldown(t *testing.T) {
	for _, advanced := range []bool{false, true} {
		for _, tc := range []struct {
			name, mode          string
			native, passthrough bool
			actual              string
		}{
			{"messages", "", true, true, "gpt-6.1-sol"},
			{"native-ws", OpenAIWSIngressModeCtxPool, false, true, "gpt-6.1-sol"},
			{"passthrough-ws", OpenAIWSIngressModePassthrough, false, false, "public-alias"},
		} {
			t.Run(fmt.Sprintf("%s/advanced=%t", tc.name, advanced), func(t *testing.T) {
				defer resetOpenAIAdvancedSchedulerSettingCacheForTest()
				account := Account{ID: 8814, Platform: PlatformOpenAI, Type: AccountTypeAPIKey, Status: StatusActive, Schedulable: true, Concurrency: 1, Credentials: map[string]any{"model_mapping": map[string]any{"public-alias": "gpt-6.1-sol"}}, Extra: map[string]any{"openai_passthrough": tc.passthrough, "openai_apikey_responses_websockets_v2_mode": tc.mode}}
				cfg := newSchedulerTestOpenAIWSV2Config()
				cfg.Gateway.OpenAIWS.ModeRouterV2Enabled = true
				svc := &OpenAIGatewayService{cfg: cfg, accountRepo: schedulerTestOpenAIAccountRepo{accounts: []Account{account}}, cache: &schedulerTestGatewayCache{}, concurrencyService: NewConcurrencyService(schedulerTestConcurrencyCache{}), rateLimitService: newOpenAIAdvancedSchedulerRateLimitService(fmt.Sprint(advanced))}
				ctx := context.Background()
				transport := OpenAIUpstreamTransportHTTPSSE
				if tc.native {
					ctx = WithOpenAINativeModelMapping(ctx)
				}
				if tc.mode != "" {
					ctx = svc.WithOpenAIWSModelRouting(ctx, nil)
					transport = OpenAIUpstreamTransportResponsesWebsocketV2Ingress
				}
				for range 2 {
					svc.handleOpenAIAccountUpstreamError(ctx, &account, http.StatusBadGateway, http.Header{}, []byte(`{"error":{"message":"temporary upstream failure"}}`), tc.actual)
				}
				selection, _, err := svc.SelectAccountWithSchedulerForCapability(ctx, nil, "", "", "public-alias", nil, transport, "", false, false, true)
				if selection != nil && selection.ReleaseFunc != nil {
					defer selection.ReleaseFunc()
				}
				require.ErrorIs(t, err, ErrNoAvailableAccounts, "the wire model already has a transient cooldown")
				require.Nil(t, selection)
				svc.ReportOpenAIAccountScheduleResult(&account, tc.actual, true, nil)
				selected, _, err := svc.SelectAccountWithSchedulerForCapability(ctx, nil, "", "", "public-alias", nil, transport, "", false, false, true)
				require.NoError(t, err)
				require.NotNil(t, selected)
				if selected.ReleaseFunc != nil {
					selected.ReleaseFunc()
				}
			})
		}
	}
}

func TestChannelRetirementProjectionRetainsAliasBeforeAccountMapping(t *testing.T) {
	account := &Account{ID: 8815, Platform: PlatformOpenAI, Type: AccountTypeOAuth, Credentials: map[string]any{"model_mapping": map[string]any{"gpt-6.1-sol": "gpt-6.1-sol"}}}
	group := &Group{ID: 8815, Platform: PlatformOpenAI}
	mapper := func(model string) string {
		if model == "gpt-5.5" {
			return "gpt-6.1-sol"
		}
		return model
	}
	for _, codex := range []bool{false, true} {
		t.Run(fmt.Sprint(codex), func(t *testing.T) {
			body := []byte(`{"data":[{"id":"gpt-5.5","marker":"retired"},{"id":"gpt-6.1-sol","marker":"live"}],"unknown":true}`)
			if codex {
				body = []byte(`{"models":[{"slug":"gpt-5.5","marker":"retired"},{"slug":"gpt-6.1-sol","marker":"live"}],"unknown":true}`)
			}
			projected, err := projectAccountModelsBody(body, account, group, codex, mapper)
			require.NoError(t, err)
			require.Contains(t, string(projected), "gpt-5.5", "channel mapping reaches an allowed live account target")
			field := "data"
			if codex {
				field = "models"
			}
			require.Equal(t, "live", gjson.GetBytes(projected, field+".0.marker").String(), "use metadata from the final target")
			require.True(t, gjson.GetBytes(projected, "unknown").Bool())
			require.Equal(t, "retired", gjson.GetBytes(body, field+".0.marker").String(), "shared source is unchanged")
			missingTarget := []byte(`{"data":[{"id":"gpt-5.5"}]}`)
			if codex {
				missingTarget = []byte(`{"models":[{"slug":"gpt-5.5"}]}`)
			}
			missing, err := projectAccountModelsBody(missingTarget, account, group, codex, mapper)
			require.NoError(t, err)
			require.Empty(t, gjson.GetBytes(missing, field).Array(), "do not synthesize a target absent from discovery")
			retiredAccount := *account
			retiredAccount.Credentials = map[string]any{"model_mapping": map[string]any{"gpt-6.1-sol": "gpt-5.5"}}
			retired, err := projectAccountModelsBody(body, &retiredAccount, group, codex, mapper)
			require.NoError(t, err)
			require.Empty(t, gjson.GetBytes(retired, field).Array(), "final retired account target must remain blocked")
		})
	}
}

func TestMessagesRoutingContextKeepsAccountPriorityAndLiteralFallback(t *testing.T) {
	for _, tc := range []struct {
		name, fallback, actual string
		mapping                map[string]any
		allowed                bool
	}{
		{"account-override-live", "gpt-6.1-sol", "gpt-6.1-sol", map[string]any{"claude-opus-4-6": "gpt-6.1-sol", "gpt-6.1-sol": "gpt-5.5"}, true},
		{"account-override-retired", "gpt-6.1-sol", "gpt-5.5", map[string]any{"claude-opus-4-6": "gpt-5.5", "gpt-6.1-sol": "gpt-6.1-sol"}, false},
		{"account-override-only-whitelist", "gpt-6.1-sol", "gpt-6.1-sol", map[string]any{"claude-opus-4-6": "gpt-6.1-sol"}, true},
		{"fallback-not-remapped-to-retired", "gpt-6.1-sol", "gpt-6.1-sol", map[string]any{"gpt-6.1-sol": "gpt-5.5"}, true},
		{"retired-fallback-not-concealed", "gpt-5.5", "gpt-5.5", map[string]any{"gpt-5.5": "gpt-6.1-sol"}, false},
		{"unsupported-fallback", "gpt-6.1-sol", "gpt-6.1-sol", map[string]any{"other": "gpt-6.1-sol"}, false},
		{"unrestricted-fallback", "gpt-6.1-sol", "gpt-6.1-sol", nil, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			account := &Account{Platform: PlatformOpenAI, Type: AccountTypeOAuth, Credentials: map[string]any{"model_mapping": tc.mapping}}
			ctx := WithOpenAIMessagesModelRouting(context.Background(), "claude-opus-4-6", tc.fallback)
			require.Equal(t, tc.actual, resolveOpenAIRequestModelWithContext(ctx, account, tc.fallback, false))
			require.Equal(t, tc.allowed, supportsOpenAIRequestModelWithContext(ctx, account, tc.fallback, false))
		})
	}
}

func TestChannelRetirementConfiguredAllowlistKeepsMappedAlias(t *testing.T) {
	for _, target := range []string{"gpt-6.1-sol", "gpt-5.5"} {
		t.Run(target, func(t *testing.T) {
			account := Account{ID: 8816, Platform: PlatformOpenAI, Type: AccountTypeOAuth, Credentials: map[string]any{"model_mapping": map[string]any{"channel-target": target}}}
			group := &Group{ID: 8816, Platform: PlatformOpenAI, ModelAllowlist: GroupModelAllowlist{Enabled: true, Models: []string{"gpt-5.5"}}}
			mapper := func(model string) string {
				if model == "gpt-5.5" {
					return "channel-target"
				}
				return model
			}
			models := openAIConfiguredCodexModelIDsForGroup([]Account{account}, group, mapper)
			require.Equal(t, target == "gpt-6.1-sol", containsModelID(models, "gpt-5.5"), "explicit group alias must be evaluated after its channel mapping")
		})
	}
}
