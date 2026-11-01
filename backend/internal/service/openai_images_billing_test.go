package service

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"testing/iotest"
	"time"

	"github.com/stretchr/testify/require"
)

func TestOpenAIGatewayServiceForwardImages_APIKeyStreamBillsOnlyObservedImages(t *testing.T) {
	const errorEvent = "data: {\"type\":\"error\",\"code\":\"moderation_blocked\",\"message\":\"blocked\"}\n\n"
	const previewEvent = "data: {\"type\":\"image_generation.partial_image\",\"b64_json\":\"cHJldmlldw==\"}\n\n"
	const imageEvent = "data: {\"type\":\"image_generation.completed\",\"id\":\"img_1\",\"b64_json\":\"b25l\",\"size\":\"1024x1024\",\"usage\":{\"input_tokens\":100,\"output_tokens\":200}}\n\n"
	const secondImageEvent = "data: {\"type\":\"image_generation.completed\",\"id\":\"img_2\",\"b64_json\":\"dHdv\",\"size\":\"1024x1024\"}\n\n"
	const doneEvent = "data: [DONE]\n\n"

	tests := []struct {
		name       string
		payload    string
		readErr    error
		imageCount int
	}{
		{name: "error event", payload: errorEvent},
		{name: "empty stream"},
		{name: "done without images", payload: doneEvent},
		{name: "preview without completed image", payload: previewEvent + errorEvent},
		{name: "one image", payload: previewEvent + imageEvent + doneEvent, imageCount: 1},
		{name: "multiple images", payload: imageEvent + secondImageEvent + doneEvent, imageCount: 2},
		{name: "delivered image before error event", payload: imageEvent + errorEvent, imageCount: 1},
		{name: "delivered image before read failure", payload: imageEvent, readErr: io.ErrUnexpectedEOF, imageCount: 1},
		{name: "read failure without image", readErr: io.ErrUnexpectedEOF},
	}
	for _, countBilling := range []bool{false, true} {
		// Exercise both the synchronous reader and the timeout-enabled reader.
		for _, interval := range []int{0, 1} {
			t.Run(fmt.Sprintf("count_billing=%t/interval=%d", countBilling, interval), func(t *testing.T) {
				for _, tt := range tests {
					t.Run(tt.name, func(t *testing.T) {
						// n must not turn an incomplete three-image request into three charges.
						body := []byte(`{"model":"gpt-image-2","prompt":"draw a cat","stream":true,"n":3,"size":"1024x1024"}`)
						c, rec := newOpenAIImagesTestContext(t, body)
						groupID := int64(902)
						price := 0.4
						apiKey := &APIKey{ID: 8, GroupID: &groupID, Group: &Group{
							ID: groupID, Platform: PlatformOpenAI, AllowImageGeneration: true,
							ImageCountBilling: countBilling, ImagePrice1K: &price,
						}}
						c.Set("api_key", apiKey)
						var reader io.Reader = strings.NewReader(tt.payload)
						if tt.readErr != nil {
							reader = io.MultiReader(reader, iotest.ErrReader(tt.readErr))
						}
						svc := newOpenAIImagesTestService(&httpUpstreamRecorder{resp: &http.Response{
							StatusCode: http.StatusOK,
							Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
							Body:       io.NopCloser(reader),
						}})
						svc.cfg.Gateway.ImageStreamDataIntervalTimeout = interval
						svc.resolver = newOpenAITokenImageChannelPricingResolverForTest(t, groupID, "gpt-image-2")
						svc.billingService = svc.resolver.billingService
						parsed, err := svc.ParseOpenAIImagesRequest(c, body)
						require.NoError(t, err)

						result, err := svc.ForwardImages(context.Background(), c, newOpenAIImagesAPIKeyAccount(), body, parsed, "")
						if tt.readErr != nil {
							require.ErrorIs(t, err, tt.readErr)
						} else {
							require.NoError(t, err, "API-key SSE passthrough must preserve its existing error-event semantics")
						}
						require.Equal(t, tt.payload, rec.Body.String(), "billing must not alter forwarded events")
						if tt.readErr != nil && tt.imageCount == 0 {
							require.Nil(t, result)
							return
						}
						require.NotNil(t, result)
						require.Equal(t, tt.imageCount, result.ImageCount, "bill only completed images observed in the stream")
						require.True(t, result.ImageEndpoint)
						require.True(t, result.Stream)
						wantInput, wantOutput := 0, 0
						if tt.imageCount > 0 {
							wantInput, wantOutput = 100, 200
						}
						require.Equal(t, wantInput, result.Usage.InputTokens)
						require.Equal(t, wantOutput, result.Usage.OutputTokens)
						cost, err := svc.calculateOpenAIRecordUsageCost(context.Background(), result, apiKey,
							[]string{"gpt-image-2"}, 1, 1, 1, 1,
							UsageTokens{InputTokens: result.Usage.InputTokens, OutputTokens: result.Usage.OutputTokens},
							"", nil, false, time.Time{})
						require.NoError(t, err)
						require.NotNil(t, cost)
						wantCost := float64(wantInput)*3e-6 + float64(wantOutput)*15e-6
						wantMode := BillingModeToken
						if countBilling && tt.imageCount > 0 {
							wantCost = float64(tt.imageCount) * price
							wantMode = BillingModeImage
						}
						require.Equal(t, string(wantMode), cost.BillingMode)
						require.InDelta(t, wantCost, cost.TotalCost, 1e-12)
						require.InDelta(t, wantCost, cost.ActualCost, 1e-12)
					})
				}
			})
		}
	}
}
