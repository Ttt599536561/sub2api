//go:build unit

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

func TestOpenAIGroupUltrafastMessagesFallback(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, model := range []string{"gpt-6.1-sol", "gpt-6-astra"} {
		for _, action := range []string{BetaPolicyActionPass, BetaPolicyActionBlock} {
			t.Run(model+"/"+action, func(t *testing.T) {
				body := []byte(`{"model":"` + model + `","max_tokens":32,"messages":[{"role":"user","content":"hello"}],"stream":false}`)
				rec := httptest.NewRecorder()
				c, _ := gin.CreateTestContext(rec)
				ctx := context.WithValue(context.Background(), ctxkey.Group, &Group{
					ID: 8, Platform: PlatformOpenAI, Status: StatusActive, Hydrated: true, ForceOpenAIUltrafast: true,
				})
				c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", bytes.NewReader(body)).WithContext(ctx)
				c.Request.Header.Set("Content-Type", "application/json")
				upstream := &httpUpstreamRecorder{resp: &http.Response{
					StatusCode: http.StatusOK,
					Header:     http.Header{"Content-Type": []string{"application/json"}},
					Body:       io.NopCloser(strings.NewReader(`{"id":"chatcmpl_ultrafast","object":"chat.completion","model":"` + model + `","choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}],"usage":{"prompt_tokens":3,"completion_tokens":2,"total_tokens":5}}`)),
				}}
				svc := newOpenAIGatewayServiceWithSettings(t, &OpenAIFastPolicySettings{Rules: []OpenAIFastPolicyRule{{
					ServiceTier: OpenAIFastTierUltrafast, Action: action, Scope: BetaPolicyScopeAll,
					ModelWhitelist: []string{model}, FallbackAction: BetaPolicyActionPass, ErrorMessage: "ultrafast denied",
				}}})
				svc.cfg = rawChatCompletionsTestConfig()
				svc.httpUpstream = upstream
				result, err := svc.ForwardAsAnthropic(ctx, c, forceChatMessagesFallbackAccount(), body, "", "")
				if action == BetaPolicyActionBlock {
					var blocked *OpenAIFastBlockedError
					require.ErrorAs(t, err, &blocked, "global ultrafast block must reject before any upstream call")
					require.Nil(t, result)
					require.Empty(t, upstream.requests)
					require.Equal(t, http.StatusForbidden, rec.Code)
					require.Equal(t, "error", gjson.Get(rec.Body.String(), "type").String())
					require.Equal(t, "forbidden_error", gjson.Get(rec.Body.String(), "error.type").String())
					require.Equal(t, "ultrafast denied", gjson.Get(rec.Body.String(), "error.message").String())
					require.Equal(t, OpsClientBusinessLimitedReasonLocalPolicyDenied, OpsClientBusinessLimitedReason(c))
					return
				}
				require.NoError(t, err)
				require.NotNil(t, result)
				require.Len(t, upstream.requests, 1)
				require.Equal(t, "/v1/chat/completions", upstream.lastReq.URL.Path)
				require.Equal(t, OpenAIFastTierUltrafast, gjson.GetBytes(upstream.lastBody, "service_tier").String())
				require.NotNil(t, result.ServiceTier)
				require.Equal(t, OpenAIFastTierUltrafast, *result.ServiceTier)
				require.Equal(t, 3, result.Usage.InputTokens)
				require.Equal(t, 2, result.Usage.OutputTokens)
			})
		}
	}
}

func TestOpenAIGroupUltrafastMessagesFallbackPolicyBoundaries(t *testing.T) {
	gin.SetMode(gin.TestMode)
	tests := []struct {
		name            string
		model           string
		mappedModel     string
		dispatchModel   string
		groupMode       string
		accountPlatform string
		stream          bool
		inboundTier     string
		policyTier      string
		action          string
		scope           string
		wantTier        string
		wantResultTier  string
		wantBlocked     bool
	}{
		{
			name: "stream mapped Astra keeps ultrafast under filter", model: "public-model", mappedModel: "gpt-6-astra",
			stream: true, action: BetaPolicyActionFilter, wantTier: OpenAIFastTierUltrafast, wantResultTier: OpenAIFastTierUltrafast,
		},
		{
			name: "stream dispatched Sol keeps ultrafast under force priority", model: "public-model", dispatchModel: "gpt-6.1-sol",
			stream: true, action: OpenAIFastPolicyActionForcePriority, wantTier: OpenAIFastTierUltrafast, wantResultTier: OpenAIFastTierUltrafast,
		},
		{
			name: "stream block uses final mapped model and apikey scope", model: "public-model", mappedModel: "gpt-6-astra",
			stream: true, action: BetaPolicyActionBlock, scope: BetaPolicyScopeAPIKey, wantBlocked: true,
		},
		{
			name: "oauth block does not affect apikey account", action: BetaPolicyActionBlock, scope: BetaPolicyScopeOAuth,
			wantTier: OpenAIFastTierUltrafast, wantResultTier: OpenAIFastTierUltrafast,
		},
		{name: "disabled group stays standard", groupMode: "off"},
		{name: "missing group stays standard", groupMode: "none"},
		{name: "non target model stays standard", model: "gpt-6-sol"},
		{name: "target mapped to non target stays standard", mappedModel: "gpt-6-sol"},
		{name: "composite group does not force ultrafast", groupMode: "composite"},
		{
			name: "other platform retains inbound tier metadata without injection", accountPlatform: PlatformZhipu,
			inboundTier: OpenAIFastTierPriority, policyTier: OpenAIFastTierMissing, action: OpenAIFastPolicyActionForcePriority,
			wantResultTier: OpenAIFastTierPriority,
		},
		{
			name: "group fast uses shared policy", groupMode: "fast", policyTier: OpenAIFastTierPriority,
			wantTier: OpenAIFastTierPriority, wantResultTier: OpenAIFastTierPriority,
		},
		{name: "group fast may be filtered", groupMode: "fast", policyTier: OpenAIFastTierPriority, action: BetaPolicyActionFilter},
		{
			name: "group fast may be blocked", groupMode: "fast", policyTier: OpenAIFastTierPriority,
			action: BetaPolicyActionBlock, wantBlocked: true,
		},
		{
			name: "explicit missing tier rule forces priority", groupMode: "off", policyTier: OpenAIFastTierMissing,
			action: OpenAIFastPolicyActionForcePriority, wantTier: OpenAIFastTierPriority, wantResultTier: OpenAIFastTierPriority,
		},
		{
			name: "any tier rule leaves missing tier unchanged", groupMode: "off", policyTier: OpenAIFastTierAny,
			action: OpenAIFastPolicyActionForcePriority,
		},
		{name: "ignored inbound tier is not billed", groupMode: "off", inboundTier: OpenAIFastTierPriority},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			model := tt.model
			if model == "" {
				model = "gpt-6-astra"
			}
			upstreamModel := model
			if tt.mappedModel != "" {
				upstreamModel = tt.mappedModel
			} else if tt.dispatchModel != "" {
				upstreamModel = tt.dispatchModel
			}
			account := forceChatMessagesFallbackAccount()
			if tt.accountPlatform != "" {
				account.Platform = tt.accountPlatform
				account.Credentials["api_protocol"] = APIProtocolChatCompletions
			}
			if tt.mappedModel != "" {
				account.Credentials["model_mapping"] = map[string]any{model: tt.mappedModel}
			}
			ctx := context.Background()
			if tt.groupMode != "none" {
				group := &Group{ID: 8, Platform: PlatformOpenAI, Status: StatusActive, Hydrated: true, ForceOpenAIUltrafast: true}
				switch tt.groupMode {
				case "off":
					group.ForceOpenAIUltrafast = false
				case "fast":
					group.ForceOpenAIUltrafast = false
					group.ForceOpenAIFast = true
				case "composite":
					group.Platform = PlatformComposite
				}
				ctx = context.WithValue(ctx, ctxkey.Group, group)
			}
			body, err := json.Marshal(map[string]any{
				"model": model, "max_tokens": 32, "stream": tt.stream,
				"messages": []map[string]string{{"role": "user", "content": "hello"}}, "service_tier": tt.inboundTier,
			})
			require.NoError(t, err)
			rec := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(rec)
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", bytes.NewReader(body)).WithContext(ctx)
			c.Request.Header.Set("Content-Type", "application/json")
			responseBody := `{"id":"chatcmpl_policy","object":"chat.completion","model":"` + upstreamModel + `","service_tier":"default","choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}],"usage":{"prompt_tokens":3,"completion_tokens":2,"total_tokens":5}}`
			contentType := "application/json"
			if tt.stream {
				responseBody = "data: " + `{"id":"chatcmpl_policy","object":"chat.completion.chunk","model":"` + upstreamModel + `","service_tier":"default","choices":[{"index":0,"delta":{"role":"assistant","content":"ok"},"finish_reason":"stop"}],"usage":{"prompt_tokens":3,"completion_tokens":2,"total_tokens":5}}` + "\n\ndata: [DONE]\n\n"
				contentType = "text/event-stream"
			}
			upstream := &httpUpstreamRecorder{resp: &http.Response{
				StatusCode: http.StatusOK, Header: http.Header{"Content-Type": []string{contentType}},
				Body: io.NopCloser(strings.NewReader(responseBody)),
			}}
			policyTier, action, scope := tt.policyTier, tt.action, tt.scope
			if policyTier == "" {
				policyTier = OpenAIFastTierUltrafast
			}
			if action == "" {
				action = BetaPolicyActionPass
			}
			if scope == "" {
				scope = BetaPolicyScopeAll
			}
			svc := newOpenAIGatewayServiceWithSettings(t, &OpenAIFastPolicySettings{Rules: []OpenAIFastPolicyRule{{
				ServiceTier: policyTier, Action: action, Scope: scope, ModelWhitelist: []string{upstreamModel},
				FallbackAction: BetaPolicyActionPass, ErrorMessage: "policy denied",
			}}})
			svc.cfg = rawChatCompletionsTestConfig()
			svc.httpUpstream = upstream
			result, err := svc.ForwardAsAnthropic(ctx, c, account, body, "", tt.dispatchModel)
			if tt.wantBlocked {
				var blocked *OpenAIFastBlockedError
				require.ErrorAs(t, err, &blocked)
				require.Nil(t, result)
				require.Empty(t, upstream.requests)
				require.Equal(t, http.StatusForbidden, rec.Code)
				require.Equal(t, "error", gjson.Get(rec.Body.String(), "type").String())
				require.Equal(t, "forbidden_error", gjson.Get(rec.Body.String(), "error.type").String())
				require.Equal(t, "policy denied", gjson.Get(rec.Body.String(), "error.message").String())
				require.Equal(t, OpsClientBusinessLimitedReasonLocalPolicyDenied, OpsClientBusinessLimitedReason(c))
				return
			}
			require.NoError(t, err)
			require.NotNil(t, result)
			require.Len(t, upstream.requests, 1)
			require.Equal(t, "/v1/chat/completions", upstream.lastReq.URL.Path)
			require.Equal(t, upstreamModel, gjson.GetBytes(upstream.lastBody, "model").String())
			require.Equal(t, upstreamModel, result.UpstreamModel)
			require.Equal(t, tt.wantTier, gjson.GetBytes(upstream.lastBody, "service_tier").String())
			if tt.wantTier == "" {
				require.False(t, gjson.GetBytes(upstream.lastBody, "service_tier").Exists())
			}
			if tt.wantResultTier == "" {
				require.Nil(t, result.ServiceTier)
			} else {
				require.NotNil(t, result.ServiceTier)
				require.Equal(t, tt.wantResultTier, *result.ServiceTier)
			}
			require.Equal(t, "default", result.UpstreamResponseServiceTier, "observed downgrade remains separate for usage reconciliation")
			require.Equal(t, 3, result.Usage.InputTokens)
			require.Equal(t, 2, result.Usage.OutputTokens)
			require.Equal(t, tt.stream, result.Stream)
			require.Equal(t, http.StatusOK, rec.Code)
			if tt.stream {
				require.Contains(t, rec.Body.String(), "event: message_stop")
			}
		})
	}
}
