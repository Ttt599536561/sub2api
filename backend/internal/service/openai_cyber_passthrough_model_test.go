//go:build unit

package service

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestCyberPassthroughCompactMatchesNormalBilling(t *testing.T) {
	previousMode := gin.Mode()
	gin.SetMode(gin.TestMode)
	t.Cleanup(func() { gin.SetMode(previousMode) })
	for _, source := range []string{"", BillingModelSourceResponse, BillingModelSourceRequested, BillingModelSourceChannelMapped} {
		t.Run(source, func(t *testing.T) {
			amounts := map[bool]float64{}
			for _, failed := range []bool{false, true} {
				logs := &openAIRecordUsageLogRepoStub{inserted: true}
				users := &openAIRecordUsageUserRepoStub{}
				svc := newOpenAIRecordUsageServiceForTest(logs, users, &openAIRecordUsageSubRepoStub{}, nil)
				cheap, pricey, cheapCost, priceyCost := orderedResponseBillingModels(t, svc.billingService, UsageTokens{InputTokens: 20, OutputTokens: 10}, openAICheapFixtureModel, openAIPriceyFixtureModel)
				svc.cfg.Gateway.OpenAICompactModel = cheap
				body := []byte(fmt.Sprintf(`{"model":%q,"stream":true,"input":[{"role":"user","content":"hi"},{"type":"compaction_trigger"}]}`, pricey))
				c, _ := gin.CreateTestContext(httptest.NewRecorder())
				c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(string(body)))
				MarkOpenAINativeCompactionV2(c)
				kind, status, errPart := "response.completed", "completed", ""
				if failed {
					kind, status, errPart = "response.failed", "failed", `,"error":{"code":"cyber_policy"}`
				}
				event := fmt.Sprintf(`{"type":%q,"response":{"id":"compact_cyber","model":%q,"status":%q,"output":[],"usage":{"input_tokens":20,"output_tokens":10}%s}}`, kind, cheap, status, errPart)
				upstream := &httpUpstreamRecorder{responses: []*http.Response{
					{StatusCode: 400, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(`{"error":{"code":"context_length_exceeded","message":"context window exceeded"}}`))},
					{StatusCode: 200, Header: http.Header{"Content-Type": {"text/event-stream"}}, Body: io.NopCloser(strings.NewReader("data: " + event + "\n\n"))},
				}}
				svc.httpUpstream = upstream
				account := &Account{ID: 30, Platform: PlatformOpenAI, Type: AccountTypeAPIKey, Credentials: map[string]any{"api_key": "test"}, Extra: map[string]any{"openai_passthrough": true, "openai_responses_supported": true}}
				result, err := svc.Forward(context.Background(), c, account, body)
				require.Len(t, upstream.bodies, 2)
				key := &APIKey{ID: 10, User: &User{ID: 20}}
				fields := ChannelUsageFields{OriginalModel: pricey, ChannelMappedModel: pricey, BillingModelSource: source}
				if failed {
					require.Error(t, err)
					mark := GetOpsCyberPolicy(c)
					require.NotNil(t, mark)
					t.Logf("source=%q mark billing=%q upstream=%q observed=%q", source, mark.Billing.BillingModel, mark.Billing.UpstreamModel, mark.Billing.UpstreamResponseModel)
					require.Contains(t, mark.Body, fmt.Sprintf(`"model":%q`, cheap), "audit evidence must retain the upstream model before client restoration")
					if mark.Billing.UpstreamResponseModel != cheap {
						t.Errorf("failed compact captured client model %q, want actual response %q", mark.Billing.UpstreamResponseModel, cheap)
					}
					svc.RecordCyberPolicyUsageLog(context.Background(), CyberPolicyUsageInput{APIKey: key, Account: account, RequestID: "error_compact", Model: pricey, Billing: mark.Billing, InputTokens: mark.UpstreamInTok, OutputTokens: mark.UpstreamOutTok, ChannelUsageFields: fields})
				} else {
					require.NoError(t, err)
					t.Logf("source=%q normal billing=%q upstream=%q observed=%q", source, result.BillingModel, result.UpstreamModel, result.UpstreamResponseModel)
					require.NoError(t, svc.RecordUsage(context.Background(), &OpenAIRecordUsageInput{APIKey: key, User: key.User, Account: account, Result: result, ChannelUsageFields: fields}))
					expectedCost := priceyCost.ActualCost
					if source == BillingModelSourceResponse {
						expectedCost = cheapCost.ActualCost
					}
					require.InDelta(t, expectedCost, users.lastAmount, 1e-12)
				}
				amounts[failed] = users.lastAmount
			}
			require.InDelta(t, amounts[false], amounts[true], 1e-12, "same compact request and usage must cost the same on completed and cyber failed responses")
		})
	}
}
