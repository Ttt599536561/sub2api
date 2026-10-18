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
)

func TestOpenAICyberHeaderOnlyTerminalCompletesBilling(t *testing.T) {
	for _, passthrough := range []bool{false, true} {
		name := "responses"
		if passthrough {
			name = "passthrough"
		}
		t.Run(name, func(t *testing.T) {
			group := &Group{ID: 881, Platform: PlatformOpenAI, RateMultiplier: 1, ForceOpenAIUltrafast: true, Status: StatusActive, Hydrated: true}
			key := &APIKey{ID: 882, User: &User{ID: 883}, GroupID: &group.ID, Group: group}
			ctx := context.WithValue(context.Background(), ctxkey.Group, group)
			body := []byte(`{"model":"gpt-6-astra","stream":true,"input":"hi"}`)
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", bytes.NewReader(body)).WithContext(ctx)
			c.Request.Header.Set("Content-Type", "application/json")
			usageRepo := &openAIRecordUsageLogRepoStub{inserted: true}
			userRepo := &openAIRecordUsageUserRepoStub{}
			svc := newOpenAIRecordUsageServiceForTest(usageRepo, userRepo, &openAIRecordUsageSubRepoStub{}, nil)
			svc.resolver = NewModelPricingResolver(nil, svc.billingService)
			account := compatCyberOAuthAccount()
			account.Extra = map[string]any{"openai_passthrough": passthrough}
			sse := "event: error\ndata: " + `{"error":{"code":"cyber_policy","message":"first evidence"}}` + "\n\n" +
				"event: response.failed\ndata: " + `{"response":{"id":"resp_cyber_header","model":"gpt-6-astra","status":"failed","service_tier":"ultrafast","usage":{"input_tokens":1200,"output_tokens":300,"input_tokens_details":{"cached_tokens":200}},"error":{"code":"cyber_policy","message":"terminal evidence"}}}` + "\n\n"
			svc.httpUpstream = &httpUpstreamRecorder{resp: &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": []string{"text/event-stream"}}, Body: io.NopCloser(strings.NewReader(sse))}}
			_, err := svc.Forward(ctx, c, account, body)
			require.Error(t, err)
			mark := GetOpsCyberPolicy(c)
			require.NotNil(t, mark)
			require.Equal(t, "first evidence", mark.Message)
			require.Equal(t, 1200, mark.UpstreamInTok)
			require.Equal(t, 300, mark.UpstreamOutTok)
			require.Equal(t, 200, mark.Billing.Usage.CacheReadInputTokens)
			require.Equal(t, "ultrafast", mark.Billing.UpstreamResponseServiceTier)
			svc.RecordCyberPolicyUsageLog(ctx, CyberPolicyUsageInput{APIKey: key, Account: account, RequestID: "header_" + name, Model: "gpt-6-astra", InputTokens: mark.UpstreamInTok, OutputTokens: mark.UpstreamOutTok, Billing: mark.Billing})
			require.Equal(t, 1, usageRepo.calls)
			standard, err := svc.billingService.CalculateTokenCostForRequest(TokenCostRequest{Ctx: ctx, Model: "gpt-6-astra", Group: group, Tokens: UsageTokens{InputTokens: 1000, OutputTokens: 300, CacheReadTokens: 200}, RateMultiplier: 1, Resolver: svc.resolver})
			require.NoError(t, err)
			require.InDelta(t, standard.ActualCost*8, userRepo.lastAmount, 1e-12)
		})
	}
}
