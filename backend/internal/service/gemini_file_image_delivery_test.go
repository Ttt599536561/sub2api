//go:build unit

package service

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// The custom native streaming path only commits image counts once the SSE event
// is delivered. Upstream fileData support must preserve that boundary for aliases.
func TestGeminiForwardNative_FileDataImagesRequireDeliveredEvent(t *testing.T) {
	gin.SetMode(gin.TestMode)
	const model = "private-image-alias"
	const payload = `data: {"candidates":[{"content":{"parts":[{"fileData":{"mimeType":"image/png","fileUri":"https://example.test/one.png"}},{"file_data":{"mime_type":"image/jpeg","file_uri":"https://example.test/two.jpg"}}],"role":"model"},"finishReason":"STOP"}],"usageMetadata":{"promptTokenCount":10,"candidatesTokenCount":1290,"totalTokenCount":1300}}`
	require.False(t, isImageGenerationModel(model), "the model-name heuristic must not satisfy this test")
	for _, tc := range []struct {
		name, suffix       string
		failAt, wantImages int
	}{
		{"complete", "\n\n", 99, 2},
		{"clean EOF", "", 99, 2},
		{"single newline EOF", "\n", 99, 2},
		{"data write failed", "\n\n", 1, 0},
		{"delimiter write failed", "\n\n", 2, 0},
		{"EOF delimiter write failed", "", 2, 0},
		{"EOF newline delimiter write failed", "\n", 2, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			reqBody := []byte(`{"contents":[{"role":"user","parts":[{"text":"draw two cats"}]}]}`)
			writer := &geminiNthFailResponseWriter{header: http.Header{}, failAt: tc.failAt}
			c, _ := gin.CreateTestContext(writer)
			c.Request = httptest.NewRequest(http.MethodPost, "/v1beta/models/"+model+":streamGenerateContent", bytes.NewReader(reqBody))
			svc, upstream := newGeminiVertexAPIKeyTestService()
			upstream.response = &http.Response{
				StatusCode: http.StatusOK,
				Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
				Body:       io.NopCloser(bytes.NewReader([]byte(payload + tc.suffix))),
			}
			result, err := svc.ForwardNative(context.Background(), c, geminiVertexAPIKeyTestAccount(""), model, "streamGenerateContent", true, reqBody)
			require.NoError(t, err)
			require.NotNil(t, result)
			require.Equal(t, tc.wantImages, result.ImageCount)
			require.Equal(t, tc.failAt != 99, result.ClientDisconnect)
			require.Equal(t, 10, result.Usage.InputTokens)
			require.Equal(t, 1290, result.Usage.OutputTokens)
			if tc.wantImages > 0 {
				require.Equal(t, payload+"\n\n", writer.body.String())
			} else {
				require.NotContains(t, writer.body.String(), "\n\n")
			}
		})
	}
}
