package service

import (
	"bytes"
	"context"
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

func TestOpenAIGroupUltrafastPartialStreamRetainsBillingMetadata(t *testing.T) {
	previousMode := gin.Mode()
	gin.SetMode(gin.TestMode)
	t.Cleanup(func() { gin.SetMode(previousMode) })
	for _, entry := range []string{"chat", "messages"} {
		for _, failure := range []string{"truncated", "failed_terminal"} {
			t.Run(entry+"/"+failure, func(t *testing.T) {
				group := &Group{ID: 881, Platform: PlatformOpenAI, RateMultiplier: 0.5, ForceOpenAIUltrafast: true, Status: StatusActive, Hydrated: true}
				key := &APIKey{ID: 882, User: &User{ID: 883}, GroupID: &group.ID, Group: group}
				ctx := context.WithValue(context.Background(), ctxkey.Group, group)
				body := []byte(`{"model":"gpt-6-astra","stream":true,"max_tokens":2048,"messages":[{"role":"user","content":"hello"}],"reasoning_effort":"high","thinking":{"type":"enabled","budget_tokens":1024}}`)
				path := "/v1/chat/completions"
				if entry == "messages" {
					path = "/v1/messages"
				}
				c, _ := gin.CreateTestContext(httptest.NewRecorder())
				c.Request = httptest.NewRequest(http.MethodPost, path, bytes.NewReader(body)).WithContext(ctx)
				c.Request.Header.Set("Content-Type", "application/json")
				usageRepo := &openAIRecordUsageLogRepoStub{inserted: true}
				userRepo := &openAIRecordUsageUserRepoStub{}
				svc := newOpenAIRecordUsageServiceForTest(usageRepo, userRepo, &openAIRecordUsageSubRepoStub{}, nil)
				svc.resolver = NewModelPricingResolver(nil, svc.billingService)
				account := &Account{ID: 1, Platform: PlatformOpenAI, Type: AccountTypeAPIKey, Credentials: map[string]any{"api_key": "sk-test"}, Extra: map[string]any{"openai_responses_supported": true}}
				sse := "data: " + `{"type":"response.created","response":{"id":"resp_partial_ultra","model":"gpt-6-astra","status":"in_progress","output":[]}}` + "\n\n" +
					"data: " + `{"type":"response.output_text.delta","output_index":0,"content_index":0,"delta":"partial answer","usage":{"input_tokens":1200,"output_tokens":300,"input_tokens_details":{"cached_tokens":200}}}` + "\n\n"
				if failure == "failed_terminal" {
					sse += "data: " + `{"type":"response.failed","response":{"id":"resp_partial_ultra","model":"gpt-6-astra","service_tier":"default","status":"failed","output":[],"error":{"type":"invalid_request_error","message":"Content policy violation"},"usage":{"input_tokens":1200,"output_tokens":300,"input_tokens_details":{"cached_tokens":200}}}}` + "\n\n"
				}
				upstream := &httpUpstreamRecorder{resp: &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": []string{"text/event-stream"}}, Body: io.NopCloser(strings.NewReader(sse))}}
				svc.httpUpstream = upstream
				var result *OpenAIForwardResult
				var err error
				if entry == "chat" {
					result, err = svc.ForwardAsChatCompletions(ctx, c, account, body, "", "")
				} else {
					result, err = svc.ForwardAsAnthropic(ctx, c, account, body, "", "")
				}
				require.Error(t, err)
				require.NotNil(t, result, "partial usage must remain available for handler settlement")
				require.Nil(t, GetOpsCyberPolicy(c), "this is ordinary partial usage, not the separate cyber billing path")
				require.Len(t, upstream.bodies, 1)
				require.Equal(t, OpenAIFastTierUltrafast, gjson.GetBytes(upstream.bodies[0], "service_tier").String())
				require.Equal(t, 1200, result.Usage.InputTokens)
				require.Equal(t, 300, result.Usage.OutputTokens)
				require.Equal(t, 200, result.Usage.CacheReadInputTokens)
				require.Equal(t, OpenAIFastTierUltrafast, optionalStringValue(result.ServiceTier), "an errored stream must retain the tier actually sent upstream")
				outboundReasoning := gjson.GetBytes(upstream.bodies[0], "reasoning.effort").String()
				require.NotEmpty(t, outboundReasoning)
				require.Equal(t, outboundReasoning, optionalStringValue(result.ReasoningEffort))
				require.NoError(t, svc.RecordUsage(ctx, &OpenAIRecordUsageInput{Result: result, APIKey: key, User: key.User, Account: account}))
				standard, err := svc.billingService.CalculateTokenCostForRequest(TokenCostRequest{Ctx: ctx, Model: "gpt-6-astra", Group: group, Tokens: UsageTokens{InputTokens: 1000, OutputTokens: 300, CacheReadTokens: 200}, RateMultiplier: group.RateMultiplier, ReasoningEffort: outboundReasoning, Resolver: svc.resolver})
				require.NoError(t, err)
				require.InDelta(t, standard.ActualCost*8, userRepo.lastAmount, 1e-12)
				require.Equal(t, OpenAIFastTierUltrafast, optionalStringValue(usageRepo.lastLog.ServiceTier))
			})
		}
	}
}
