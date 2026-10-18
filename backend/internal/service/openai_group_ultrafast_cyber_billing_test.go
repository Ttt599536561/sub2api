package service

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/pkg/ctxkey"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestOpenAIUltrafastCyberBillingPreservesActualRequest(t *testing.T) {
	testMode := gin.Mode()
	gin.SetMode(gin.TestMode)
	t.Cleanup(func() { gin.SetMode(testMode) })
	cases := []struct {
		name, entry, requested, actual, inputTier, observedTier    string
		pricingModel, billingSource, channelModel                  string
		apiKey, disabled, stream, noUsage, longTerminal, bareError bool
		wantMultiplier                                             float64
		wantTier                                                   string
	}{
		{name: "chat_astra", entry: "chat", requested: "gpt-6-astra", actual: "gpt-6-astra", observedTier: "ultrafast", wantMultiplier: 8, wantTier: "ultrafast"},
		{name: "chat_sol", entry: "chat", requested: "gpt-6.1-sol", actual: "gpt-6.1-sol", observedTier: "ultrafast", wantMultiplier: 8, wantTier: "ultrafast"},
		{name: "chat_stream", entry: "chat", requested: "gpt-6-astra", actual: "gpt-6-astra", stream: true, observedTier: "ultrafast", wantMultiplier: 8, wantTier: "ultrafast"},
		{name: "messages_buffered", entry: "messages", requested: "gpt-6-astra", actual: "gpt-6-astra", observedTier: "ultrafast", wantMultiplier: 8, wantTier: "ultrafast"},
		{name: "messages_stream", entry: "messages", requested: "gpt-6-astra", actual: "gpt-6-astra", stream: true, observedTier: "ultrafast", wantMultiplier: 8, wantTier: "ultrafast"},
		{name: "responses_stream", entry: "responses", requested: "gpt-6-astra", actual: "gpt-6-astra", stream: true, observedTier: "ultrafast", wantMultiplier: 8, wantTier: "ultrafast"},
		{name: "bare_error_then_failed", entry: "responses", requested: "gpt-6-astra", actual: "gpt-6-astra", stream: true, bareError: true, observedTier: "ultrafast", wantMultiplier: 8, wantTier: "ultrafast"},
		{name: "public_api_downgrade", entry: "chat", requested: "gpt-6-astra", actual: "gpt-6-astra", apiKey: true, observedTier: "default", wantMultiplier: 1, wantTier: "default"},
		{name: "oauth_default_not_authoritative", entry: "chat", requested: "gpt-6-astra", actual: "gpt-6-astra", observedTier: "default", wantMultiplier: 8, wantTier: "ultrafast"},
		{name: "no_response_tier", entry: "chat", requested: "gpt-6-astra", actual: "gpt-6-astra", wantMultiplier: 8, wantTier: "ultrafast"},
		{name: "disabled_group_explicit_ultra", entry: "chat", requested: "gpt-6-astra", actual: "gpt-6-astra", disabled: true, inputTier: "ultrafast", observedTier: "ultrafast", wantMultiplier: 6, wantTier: "ultrafast"},
		{name: "non_target", entry: "chat", requested: "gpt-5.4", actual: "gpt-5.4", observedTier: "default", wantMultiplier: 1},
		{name: "mapped_into_target", entry: "chat", requested: "client-alias", actual: "gpt-6-astra", observedTier: "ultrafast", wantMultiplier: 8, wantTier: "ultrafast"},
		{name: "mapped_out_of_target", entry: "chat", requested: "gpt-6-astra", actual: "gpt-5.4", observedTier: "default", wantMultiplier: 1},
		{name: "requested_price_with_target_mapping", entry: "chat", requested: "gpt-5.4", actual: "gpt-6-astra", pricingModel: "gpt-5.4", billingSource: BillingModelSourceRequested, observedTier: "ultrafast", wantMultiplier: 8, wantTier: "ultrafast"},
		{name: "channel_price_with_target_mapping", entry: "chat", requested: "client-alias", channelModel: "gpt-5.4", actual: "gpt-6-astra", pricingModel: "gpt-5.4", billingSource: BillingModelSourceChannelMapped, observedTier: "ultrafast", wantMultiplier: 8, wantTier: "ultrafast"},
		{name: "zero_usage", entry: "chat", requested: "gpt-6-astra", actual: "gpt-6-astra", observedTier: "ultrafast", noUsage: true, wantMultiplier: 8, wantTier: "ultrafast"},
		{name: "terminal_metadata_after_log_truncation", entry: "chat", stream: true, requested: "gpt-6-astra", actual: "gpt-6-astra", observedTier: "ultrafast", longTerminal: true, wantMultiplier: 8, wantTier: "ultrafast"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			group := &Group{ID: 881, Platform: PlatformOpenAI, RateMultiplier: 1, ForceOpenAIUltrafast: !tc.disabled, Status: StatusActive, Hydrated: true}
			key := &APIKey{ID: 882, User: &User{ID: 883}, GroupID: &group.ID, Group: group}
			ctx := context.WithValue(context.Background(), ctxkey.Group, group)
			forwardedModel := tc.requested
			if tc.channelModel != "" {
				forwardedModel = tc.channelModel
			}
			request := map[string]any{"model": forwardedModel, "stream": tc.stream, "messages": []any{map[string]any{"role": "user", "content": "hi"}}}
			path := "/v1/chat/completions"
			if tc.entry == "messages" {
				path = "/v1/messages"
				request["max_tokens"] = 100
			}
			if tc.entry == "responses" {
				path = "/v1/responses"
				delete(request, "messages")
				request["input"] = "hi"
			}
			if tc.inputTier != "" {
				request["service_tier"] = tc.inputTier
			}
			body, err := json.Marshal(request)
			require.NoError(t, err)
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest(http.MethodPost, path, bytes.NewReader(body)).WithContext(ctx)
			c.Request.Header.Set("Content-Type", "application/json")
			usageRepo := &openAIRecordUsageLogRepoStub{inserted: true}
			userRepo := &openAIRecordUsageUserRepoStub{}
			svc := newOpenAIRecordUsageServiceForTest(usageRepo, userRepo, &openAIRecordUsageSubRepoStub{}, nil)
			svc.resolver = NewModelPricingResolver(nil, svc.billingService)
			account := compatCyberOAuthAccount()
			if tc.apiKey {
				account.Type = AccountTypeAPIKey
				account.Credentials = map[string]any{"api_key": "sk-test"}
				account.Extra = map[string]any{"openai_responses_supported": true}
			}
			if forwardedModel != tc.actual {
				account.Credentials["model_mapping"] = map[string]any{forwardedModel: tc.actual}
			}
			inTok, outTok, cached := 1200, 300, 200
			if tc.noUsage {
				inTok = 0
				outTok = 0
				cached = 0
			}
			response := map[string]any{"id": "resp_cyber_ultra", "object": "response", "model": tc.actual, "status": "failed", "output": []any{}, "usage": map[string]any{"input_tokens": inTok, "output_tokens": outTok, "input_tokens_details": map[string]any{"cached_tokens": cached}}, "error": map[string]any{"code": "cyber_policy", "message": "blocked by upstream policy"}}
			if tc.observedTier != "" {
				response["service_tier"] = tc.observedTier
			}
			terminal, err := json.Marshal(map[string]any{"type": "response.failed", "response": response})
			require.NoError(t, err)
			if tc.longTerminal {
				terminal = []byte(`{"padding":"` + strings.Repeat("x", 5000) + `",` + string(terminal[1:]))
			}
			sse := "data: " + string(terminal) + "\n\n"
			if tc.bareError {
				sse = "data: " + `{"type":"error","error":{"code":"cyber_policy","message":"first audit evidence"}}` + "\n\n" + sse
			}
			upstream := &httpUpstreamRecorder{resp: &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": []string{"text/event-stream"}}, Body: io.NopCloser(strings.NewReader(sse))}}
			svc.httpUpstream = upstream
			switch tc.entry {
			case "chat":
				_, err = svc.ForwardAsChatCompletions(ctx, c, account, body, "", forwardedModel)
			case "messages":
				_, err = svc.ForwardAsAnthropic(ctx, c, account, body, "", forwardedModel)
			default:
				_, err = svc.Forward(ctx, c, account, body)
			}
			require.Error(t, err)
			require.Len(t, upstream.bodies, 1)
			require.Equal(t, tc.actual, gjson.GetBytes(upstream.bodies[0], "model").String())
			mark := GetOpsCyberPolicy(c)
			require.NotNil(t, mark)
			if tc.bareError {
				require.Equal(t, "first audit evidence", mark.Message)
			}
			require.Equal(t, inTok, mark.UpstreamInTok)
			require.Equal(t, outTok, mark.UpstreamOutTok)
			svc.RecordCyberPolicyUsageLog(ctx, CyberPolicyUsageInput{APIKey: key, Account: account, RequestID: "cyber_" + tc.name, Model: tc.requested, InputTokens: mark.UpstreamInTok, OutputTokens: mark.UpstreamOutTok, Billing: mark.Billing, ChannelUsageFields: ChannelUsageFields{OriginalModel: tc.requested, ChannelMappedModel: forwardedModel, BillingModelSource: tc.billingSource}})
			require.Equal(t, 1, usageRepo.calls)
			pricingModel := tc.pricingModel
			if pricingModel == "" {
				pricingModel = tc.actual
			}
			standard, err := svc.billingService.CalculateTokenCostForRequest(TokenCostRequest{Ctx: ctx, Model: pricingModel, Group: group, Tokens: UsageTokens{InputTokens: inTok - cached, OutputTokens: outTok, CacheReadTokens: cached}, RateMultiplier: 1, Resolver: svc.resolver})
			require.NoError(t, err)
			require.InDelta(t, standard.ActualCost*tc.wantMultiplier, userRepo.lastAmount, 1e-12, "error settlement must retain the actual model/tier and cache usage")
			require.Equal(t, tc.wantTier, optionalStringValue(usageRepo.lastLog.ServiceTier))
			require.Equal(t, tc.actual, mark.Billing.UpstreamModel)
			require.Equal(t, tc.actual, mark.Billing.BillingModel)
			require.Equal(t, tc.observedTier, mark.Billing.UpstreamResponseServiceTier)
			require.Equal(t, tc.actual, mark.Billing.UpstreamResponseModel)
			require.Equal(t, cached, mark.Billing.Usage.CacheReadInputTokens)
		})
	}
}

func TestOpenAIUltrafastCyberCompactKeepsOriginalBillingModel(t *testing.T) {
	group := &Group{ID: 881, Platform: PlatformOpenAI, RateMultiplier: 1, ForceOpenAIUltrafast: true, Hydrated: true, Status: StatusActive}
	key := &APIKey{ID: 882, GroupID: &group.ID, Group: group, User: &User{ID: 883}}
	ctx := context.WithValue(context.Background(), ctxkey.Group, group)
	body := []byte(`{"model":"gpt-6-astra","input":[{"role":"user","content":"hi"},{"type":"compaction_trigger"}],"stream":true}`)
	c := newOpenAICompactFallbackTestContext(t, "/v1/responses")
	c.Request = c.Request.WithContext(ctx)
	MarkOpenAINativeCompactionV2(c)
	account := &Account{ID: 884, Platform: PlatformOpenAI, Type: AccountTypeAPIKey, Credentials: map[string]any{"api_key": "sk-test"}, Extra: map[string]any{"openai_responses_supported": true}}
	logs := &openAIRecordUsageLogRepoStub{inserted: true}
	users := &openAIRecordUsageUserRepoStub{}
	svc := newOpenAIRecordUsageServiceForTest(logs, users, &openAIRecordUsageSubRepoStub{}, nil)
	svc.cfg.Gateway.OpenAICompactModel = "gpt-5.4"
	svc.resolver = NewModelPricingResolver(nil, svc.billingService)
	upstream := &httpUpstreamRecorder{responses: []*http.Response{
		{StatusCode: 400, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(strings.NewReader(`{"error":{"code":"context_length_exceeded","message":"context window exceeded"}}`))},
		{StatusCode: 200, Header: http.Header{"Content-Type": []string{"text/event-stream"}}, Body: io.NopCloser(strings.NewReader("data: " + `{"type":"response.failed","response":{"id":"cyber_compact","model":"gpt-5.4","status":"failed","service_tier":"default","error":{"code":"cyber_policy"},"usage":{"input_tokens":100,"output_tokens":50}}}` + "\n\n"))},
	}}
	svc.httpUpstream = upstream
	_, err := svc.Forward(ctx, c, account, body)
	require.Error(t, err)
	require.Len(t, upstream.bodies, 2)
	mark := GetOpsCyberPolicy(c)
	require.NotNil(t, mark)
	require.Equal(t, "gpt-5.4", mark.Billing.UpstreamModel)
	require.Equal(t, "gpt-6-astra", mark.Billing.BillingModel, "compact fallback changes upstream model, not the normal request's pricing source")
	require.Empty(t, mark.Billing.ServiceTier)
	svc.RecordCyberPolicyUsageLog(ctx, CyberPolicyUsageInput{APIKey: key, Account: account, RequestID: "cyber_compact", Model: "gpt-6-astra", InputTokens: mark.UpstreamInTok, OutputTokens: mark.UpstreamOutTok, Billing: mark.Billing})
	require.Equal(t, 1, logs.calls)
	standard, err := svc.billingService.CalculateCost("gpt-6-astra", UsageTokens{InputTokens: 100, OutputTokens: 50}, 1)
	require.NoError(t, err)
	require.InDelta(t, standard.ActualCost, users.lastAmount, 1e-12)
}
