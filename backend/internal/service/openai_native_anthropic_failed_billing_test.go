package service

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/apicompat"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type nativeAnthropicBillingReadFailure struct{}

func (nativeAnthropicBillingReadFailure) Read([]byte) (int, error) {
	return 0, errors.New("upstream connection reset")
}

func TestNativeAnthropicBufferedFailurePerRequestBilling(t *testing.T) {
	const upstreamError = "event: error\ndata: {\"type\":\"error\",\"error\":{\"type\":\"overloaded_error\",\"message\":\"Overloaded\"}}\n\n"
	const messageStart = "event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"id\":\"msg_test\",\"type\":\"message\",\"role\":\"assistant\",\"model\":\"glm-4.7\",\"content\":[],\"usage\":{\"input_tokens\":10,\"output_tokens\":1}}}\n\n"
	const zeroUsageStart = "event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"id\":\"msg_test\",\"type\":\"message\",\"role\":\"assistant\",\"model\":\"glm-4.7\",\"content\":[]}}\n\n"
	const cacheOnlyUsage = "event: message_delta\ndata: {\"type\":\"message_delta\",\"usage\":{\"cache_read_input_tokens\":12}}\n\n"
	for _, endpoint := range []string{"responses", "chat_completions"} {
		for _, tc := range []struct {
			name       string
			body       string
			readError  bool
			wantCharge bool
		}{
			{name: "error_before_start", body: upstreamError},
			{name: "read_failure_before_start", readError: true},
			{name: "partial_usage_then_error", body: messageStart + upstreamError, wantCharge: true},
			{name: "started_without_usage_then_error", body: zeroUsageStart + upstreamError, wantCharge: true},
			{name: "usage_without_start_then_error", body: cacheOnlyUsage + upstreamError, wantCharge: true},
		} {
			t.Run(endpoint+"/"+tc.name, func(t *testing.T) {
				gin.SetMode(gin.TestMode)
				logs := &openAIRecordUsageLogRepoStub{inserted: true}
				users := &openAIRecordUsageUserRepoStub{}
				svc := newOpenAIRecordUsageServiceForTest(logs, users, &openAIRecordUsageSubRepoStub{}, nil)
				svc.resolver = NewModelPricingResolver(nil, svc.billingService)
				t.Cleanup(svc.CloseOpenAIWSPool)
				recorder := httptest.NewRecorder()
				c, _ := gin.CreateTestContext(recorder)
				c.Request = httptest.NewRequest(http.MethodPost, "/v1/"+endpoint, nil)
				var body io.Reader = strings.NewReader(tc.body)
				if tc.readError {
					body = io.MultiReader(body, nativeAnthropicBillingReadFailure{})
				}
				resp := &http.Response{StatusCode: http.StatusOK, Header: http.Header{"X-Request-Id": []string{"failed-native-" + tc.name}}, Body: io.NopCloser(body)}
				var result *OpenAIForwardResult
				var err error
				if endpoint == "responses" {
					result, err = svc.handleResponsesBufferedFromNativeAnthropic(resp, c, "glm-4.7", "glm-4.7", "glm-4.7", nil, time.Now(), apicompat.ResponsesClientToolMapping{})
				} else {
					result, err = svc.handleCCBufferedFromNativeAnthropic(resp, c, "glm-4.7", "glm-4.7", "glm-4.7", nil, time.Now())
				}
				require.Error(t, err)
				require.Equal(t, http.StatusBadGateway, recorder.Code)
				price := 2.0
				group := &Group{ID: 885, Platform: PlatformZhipu, Hydrated: true, RateMultiplier: 1,
					ModelPricing: []ChannelModelPricing{{Models: []string{"glm-4.7"}, BillingMode: BillingModePerRequest, PerRequestPrice: &price}}}
				// Both HTTP handlers settle a non-nil partial result even when the
				// forwarder returns an error. Exercise that boundary through billing.
				if result != nil {
					err = svc.RecordUsage(context.Background(), &OpenAIRecordUsageInput{Result: result,
						APIKey: &APIKey{ID: 1, GroupID: &group.ID, Group: group}, User: &User{ID: 2},
						Account: &Account{ID: 3, Platform: PlatformZhipu, Type: AccountTypeAPIKey}})
					require.NoError(t, err)
				}
				if tc.wantCharge {
					require.NotNil(t, result, "started or metered work must still settle")
					require.Equal(t, 1, users.deductCalls)
					require.InDelta(t, price, users.lastAmount, 1e-12)
				} else {
					require.Zero(t, users.deductCalls, "upstream rejection before any response or usage must not charge a flat fee; charged=%v", users.lastAmount)
					require.Nil(t, result)
				}
			})
		}
	}
}
