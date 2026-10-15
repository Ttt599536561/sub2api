//go:build unit

package service

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/antigravity"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGeminiMetadataIdentityReview_PrimaryIndexIsStable(t *testing.T) {
	for _, indexField := range []string{"", `"index":0,`} {
		t.Run(indexField, func(t *testing.T) {
			source := "data: " + `{"candidates":[{"index":1,"finishReason":"STOP"}]}` + "\n\n" +
				"data: " + `{"candidates":[{` + indexField + `"content":{"parts":[{"text":"primary answer"}]}}]}` + "\n\n" +
				"data: " + `{"candidates":[{` + indexField + `"finishReason":"SAFETY"}]}` + "\n\n"
			got, _, err := collectGeminiSSE(strings.NewReader(source), false)
			require.NoError(t, err)
			candidates, ok := got["candidates"].([]any)
			require.True(t, ok)
			require.Len(t, candidates, 2, "one update must not create another primary candidate")
			primary := candidates[0].(map[string]any)
			assert.Equal(t, "SAFETY", primary["finishReason"])
			assert.Equal(t, "STOP", candidates[1].(map[string]any)["finishReason"])
			assert.Equal(t, float64(1), candidates[1].(map[string]any)["index"])
			assert.Equal(t, []map[string]any{{"text": "primary answer"}}, extractGeminiParts(got))
			raw, err := json.Marshal(got)
			require.NoError(t, err)
			signal, present := detectGeminiResponseSignalInBody(raw)
			require.True(t, present)
			assert.Equal(t, "SAFETY", signal.Reason)
		})
	}
}

func TestGeminiMetadataIdentityReview_PrimaryFinishAfterSecondaryMetadata(t *testing.T) {
	gin.SetMode(gin.TestMode)
	chunks := []string{
		`{"candidates":[{"index":1,"finishReason":"STOP"}]}`,
		`{"candidates":[{"index":0,"content":{"parts":[{"text":"primary answer"}]},"finishReason":"MAX_TOKENS"}]}`,
	}
	for _, protocol := range []string{"public direct", "public wrapped", "antigravity gemini", "antigravity claude"} {
		t.Run(protocol, func(t *testing.T) {
			var response map[string]any
			switch protocol {
			case "public direct", "public wrapped":
				var source strings.Builder
				for _, chunk := range chunks {
					source.WriteString("data: ")
					if protocol == "public wrapped" {
						chunk = `{"response":` + chunk + `}`
					}
					source.WriteString(chunk + "\n\n")
				}
				got, _, err := collectGeminiSSE(strings.NewReader(source.String()), protocol == "public wrapped")
				require.NoError(t, err)
				raw, err := json.Marshal(got)
				require.NoError(t, err)
				message, _ := convertGeminiToClaudeMessage(got, "gemini-test", raw, false)
				assert.Equal(t, "max_tokens", message["stop_reason"], "earlier secondary metadata must not replace the primary completion state")
				response = got
			case "antigravity gemini":
				svc := newAntigravityTestService(&config.Config{})
				c, rec := antigravityProtocolReviewContext()
				_, err := svc.handleGeminiStreamToNonStreaming(c, antigravityProtocolReviewResponse(chunks...), time.Now())
				require.NoError(t, err)
				require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &response))
			case "antigravity claude":
				svc := newAntigravityTestService(&config.Config{})
				c, _ := antigravityProtocolReviewContext()
				raw, _, err := svc.collectClaudeStreamResponse(c, antigravityProtocolReviewResponse(chunks...), time.Now(), "claude-sonnet-4-6")
				require.NoError(t, err)
				var message antigravity.ClaudeResponse
				require.NoError(t, json.Unmarshal(raw, &message))
				assert.Equal(t, "max_tokens", message.StopReason)
				require.Len(t, message.Content, 1)
				assert.Equal(t, "primary answer", message.Content[0].Text)
				return
			}
			assert.Equal(t, "MAX_TOKENS", extractGeminiFinishReason(response))
			assert.Equal(t, []map[string]any{{"text": "primary answer"}}, extractGeminiParts(response))
		})
	}
}
