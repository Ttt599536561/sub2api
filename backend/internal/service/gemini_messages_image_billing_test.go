//go:build unit

package service

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Messages intentionally omits image parts. Upstream image observations and the
// standard-model fallback must not turn the remaining text into image charges.
func TestGeminiMessages_DroppedImagePartsKeepTokenBilling(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, mode := range []string{"json", "buffered_sse", "stream"} {
		for _, model := range []string{"private-image-alias", "gemini-3-pro-image", "mapped-image-alias"} {
			for _, imageKey := range []string{"fileData", "file_data", "inlineData", "inline_data"} {
				for _, countBilling := range []bool{false, true} {
					t.Run(fmt.Sprintf("%s/%s/%s/count_billing=%t", mode, model, imageKey, countBilling), func(t *testing.T) {
						payload := geminiMessagesImageBillingPayload(imageKey, true)
						svc, upstream := newGeminiVertexAPIKeyTestService()
						account := geminiVertexAPIKeyTestAccount("")
						if model == "mapped-image-alias" {
							account.Credentials["model_mapping"] = map[string]any{model: "gemini-3-pro-image"}
						}
						contentType, upstreamBody := "application/json", payload
						if mode != "json" {
							contentType, upstreamBody = "text/event-stream", "data: "+payload+"\n\n"
						}
						if mode == "buffered_sse" {
							svc.tokenProvider = &GeminiTokenProvider{}
							account.Type = AccountTypeOAuth
							account.Credentials["access_token"] = "synthetic-token"
							account.Credentials["project_id"] = "synthetic-project"
							upstreamBody = "data: {\"response\":" + payload + "}\n\n"
						}
						upstream.response = &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": []string{contentType}}, Body: io.NopCloser(strings.NewReader(upstreamBody))}
						rec := httptest.NewRecorder()
						c, _ := gin.CreateTestContext(rec)
						body := []byte(fmt.Sprintf(`{"model":%q,"max_tokens":16,"stream":%t,"messages":[{"role":"user","content":"draw"}]}`, model, mode == "stream"))
						c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", bytes.NewReader(body))
						key := geminiMessagesImageBillingKey(countBilling)
						c.Set("api_key", key)
						result, err := svc.Forward(context.Background(), c, account, body)
						require.NoError(t, err)
						require.NotNil(t, result)
						require.Contains(t, rec.Body.String(), "retained text")
						require.NotContains(t, rec.Body.String(), "example.test")
						require.NotContains(t, rec.Body.String(), geminiTestPNG)
						assert.Zero(t, result.ImageCount, "the Messages response did not contain an image")
						assert.False(t, result.ImageEndpoint)
						require.Equal(t, 10, result.Usage.InputTokens)
						require.Equal(t, 5, result.Usage.OutputTokens)
						resolver := newOpenAITokenImageChannelPricingResolverForTest(t, key.Group.ID, model)
						billing := &GatewayService{billingService: resolver.billingService, resolver: resolver}
						cost := billing.calculateRecordUsageCost(context.Background(), result, key, model, 1, 1, time.Time{})
						require.Equal(t, string(BillingModeToken), cost.BillingMode)
						require.InDelta(t, 0.000105, cost.TotalCost, 1e-12, "keep collected token costs, including when the group forces per-image prices")
					})
				}
			}
		}
	}
}

func TestGeminiMessages_DroppedImagesAfterFinishAndDisconnectKeepTokenBilling(t *testing.T) {
	for _, model := range []string{"private-image-alias", "gemini-3-pro-image"} {
		for _, imageKey := range []string{"fileData", "inlineData"} {
			t.Run(model+"/"+imageKey, func(t *testing.T) {
				body := []byte(fmt.Sprintf(`{"model":%q,"max_tokens":16,"stream":true,"messages":[{"role":"user","content":"draw"}]}`, model))
				payload := "data: " + geminiMessagesImageBillingPayload(imageKey, true) + "\n\n"
				svc, c, rec := newGeminiAbortingStreamService(t, "/v1/messages", body, payload, true)
				key := geminiMessagesImageBillingKey(true)
				c.Set("api_key", key)
				result, err := svc.Forward(context.Background(), c, geminiVertexAPIKeyTestAccount(""), body)
				require.NoError(t, err)
				require.True(t, result.ClientDisconnect)
				require.Contains(t, rec.Body.String(), "retained text")
				require.NotContains(t, rec.Body.String(), "example.test")
				require.NotContains(t, rec.Body.String(), geminiTestPNG)
				assert.Zero(t, result.ImageCount)
				assert.False(t, result.ImageEndpoint)
				resolver := newOpenAITokenImageChannelPricingResolverForTest(t, key.Group.ID, model)
				billing := &GatewayService{billingService: resolver.billingService, resolver: resolver}
				cost := billing.calculateRecordUsageCost(context.Background(), result, key, model, 1, 1, time.Time{})
				require.Equal(t, string(BillingModeToken), cost.BillingMode)
				require.InDelta(t, 0.000105, cost.TotalCost, 1e-12)
			})
		}
	}
}

// Chat's non-streaming conversion does preserve inlineData as a Markdown image.
// A Messages billing fix must leave that delivered-image path intact.
func TestGeminiMessagesBillingFix_ChatDeliveredInlineImageStillBilled(t *testing.T) {
	const model = "gemini-3-pro-image"
	svc, upstream := newGeminiVertexAPIKeyTestService()
	upstream.response = &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(strings.NewReader(geminiMessagesImageBillingPayload("inlineData", true)))}
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	body := []byte(`{"model":"gemini-3-pro-image","messages":[{"role":"user","content":"draw"}]}`)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewReader(body))
	key := geminiMessagesImageBillingKey(true)
	c.Set("api_key", key)
	result, err := svc.ForwardAsChatCompletions(context.Background(), c, geminiVertexAPIKeyTestAccount(""), body)
	require.NoError(t, err)
	require.Contains(t, rec.Body.String(), "data:image/png;base64,"+geminiTestPNG)
	require.Equal(t, 1, result.ImageCount)
	require.True(t, result.ImageEndpoint)
	resolver := newOpenAITokenImageChannelPricingResolverForTest(t, key.Group.ID, model)
	billing := &GatewayService{billingService: resolver.billingService, resolver: resolver}
	cost := billing.calculateRecordUsageCost(context.Background(), result, key, model, 1, 1, time.Time{})
	require.Equal(t, string(BillingModeImage), cost.BillingMode)
	require.InDelta(t, 0.25, cost.TotalCost, 1e-12)
}

func geminiMessagesImageBillingKey(countBilling bool) *APIKey {
	groupID := int64(9922)
	return &APIKey{GroupID: &groupID, Group: &Group{ID: groupID, Platform: PlatformComposite, AllowImageGeneration: true, ImageCountBilling: countBilling, ImagePrice1K: f64p(0.25), ImagePrice2K: f64p(0.25)}}
}

func geminiMessagesImageBillingPayload(imageKey string, finished bool) string {
	data := `"mimeType":"image/png","data":"` + geminiTestPNG + `"`
	switch imageKey {
	case "fileData":
		data = `"mimeType":"image/png","fileUri":"https://example.test/image.png"`
	case "file_data":
		data = `"mime_type":"image/png","file_uri":"https://example.test/image.png"`
	case "inline_data":
		data = `"mime_type":"image/png","data":"` + geminiTestPNG + `"`
	}
	finish := ""
	if finished {
		finish = `,"finishReason":"STOP"`
	}
	return `{"candidates":[{"content":{"parts":[{"text":"retained text"},{"` + imageKey + `":{` + data + `}}],"role":"model"}` + finish + `}],"usageMetadata":{"promptTokenCount":10,"candidatesTokenCount":5,"totalTokenCount":15}}`
}

// This was the regression even with image_count_billing disabled: the group
// image price turned an empty Messages response for a custom alias into a bill.
func TestGeminiMessages_DroppedFileImageDoesNotUseGroupImagePrice(t *testing.T) {
	gin.SetMode(gin.TestMode)
	const model = "private-image-alias"
	svc, upstream := newGeminiVertexAPIKeyTestService()
	payload := strings.Replace(geminiMessagesImageBillingPayload("fileData", true), `{"text":"retained text"},`, "", 1)
	upstream.response = &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(strings.NewReader(payload))}
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	body := []byte(`{"model":"private-image-alias","max_tokens":16,"messages":[{"role":"user","content":"draw"}]}`)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", bytes.NewReader(body))
	key := geminiMessagesImageBillingKey(false)
	c.Set("api_key", key)
	result, err := svc.Forward(context.Background(), c, geminiVertexAPIKeyTestAccount(""), body)
	require.NoError(t, err)
	require.Contains(t, rec.Body.String(), `"content":[]`)
	assert.Zero(t, result.ImageCount)
	billing := &GatewayService{billingService: NewBillingService(&config.Config{}, nil)}
	cost := billing.calculateRecordUsageCost(context.Background(), result, key, model, 1, 1, time.Time{})
	require.Zero(t, cost.TotalCost, "an omitted file image must not activate the group image price")
	require.Zero(t, cost.ActualCost)
}
