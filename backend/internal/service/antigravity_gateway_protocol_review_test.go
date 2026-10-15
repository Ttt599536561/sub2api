//go:build unit

package service

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/antigravity"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func antigravityProtocolReviewContext() (*gin.Context, *httptest.ResponseRecorder) {
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/", nil)
	return c, rec
}

func antigravityProtocolReviewResponse(chunks ...string) *http.Response {
	var sse strings.Builder
	for _, chunk := range chunks {
		sse.WriteString("data: {\"response\":")
		sse.WriteString(chunk)
		sse.WriteString("}\n\n")
	}
	return &http.Response{StatusCode: http.StatusOK, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(sse.String()))}
}

func TestAntigravityProtocolReview_ClaudeCollectPreservesSignedContentOrder(t *testing.T) {
	gin.SetMode(gin.TestMode)
	svc := newAntigravityTestService(&config.Config{})
	c, _ := antigravityProtocolReviewContext()
	resp := antigravityProtocolReviewResponse(
		`{"candidates":[{"content":{"role":"model","parts":[{"text":"before "}]}}]}`,
		`{"candidates":[{"content":{"role":"model","parts":[{"text":"signed ","thoughtSignature":"signed-part"}]}}]}`,
		`{"candidates":[{"content":{"role":"model","parts":[{"text":"after"}]},"finishReason":"STOP"}]}`,
	)
	body, _, err := svc.collectClaudeStreamResponse(c, resp, time.Now(), "claude-sonnet-4-6")
	require.NoError(t, err)
	var got antigravity.ClaudeResponse
	require.NoError(t, json.Unmarshal(body, &got))
	require.Equal(t, []antigravity.ClaudeContentItem{
		{Type: "text", Text: "before "},
		{Type: "text", Text: "signed "},
		{Type: "thinking", Signature: "signed-part"},
		{Type: "text", Text: "after"},
	}, got.Content)
}

func TestAntigravityProtocolReview_ClaudePreservesTrailingEmptySignature(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, stream := range []bool{false, true} {
		name := "collected"
		if stream {
			name = "streaming"
		}
		t.Run(name, func(t *testing.T) {
			svc := newAntigravityTestService(&config.Config{})
			c, rec := antigravityProtocolReviewContext()
			resp := antigravityProtocolReviewResponse(
				`{"candidates":[{"content":{"role":"model","parts":[{"text":"hello"}]}}]}`,
				`{"candidates":[{"content":{"role":"model","parts":[{"text":"","thoughtSignature":"tail-signature"}]},"finishReason":"STOP"}]}`,
			)
			if stream {
				_, err := svc.handleClaudeStreamingResponse(c, resp, time.Now(), "claude-sonnet-4-6")
				require.NoError(t, err)
				require.Contains(t, rec.Body.String(), `"signature":"tail-signature"`)
			} else {
				body, _, err := svc.collectClaudeStreamResponse(c, resp, time.Now(), "claude-sonnet-4-6")
				require.NoError(t, err)
				var got antigravity.ClaudeResponse
				require.NoError(t, json.Unmarshal(body, &got))
				require.Equal(t, []antigravity.ClaudeContentItem{{Type: "text", Text: "hello"}, {Type: "thinking", Signature: "tail-signature"}}, got.Content)
			}
		})
	}
}

func TestAntigravityProtocolReview_CollectorsPreserveTerminalMetadata(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, chunks := range []struct {
		name   string
		values []string
	}{
		{"terminal usage together", []string{
			`{"candidates":[{"content":{"role":"model","parts":[{"text":"partial"}]}}],"usageMetadata":{"promptTokenCount":10,"candidatesTokenCount":1}}`,
			`{"candidates":[{"finishReason":"MAX_TOKENS"}],"usageMetadata":{"promptTokenCount":10,"candidatesTokenCount":8,"thoughtsTokenCount":2}}`,
		}},
		{"usage fields in separate chunks", []string{
			`{"candidates":[{"content":{"role":"model","parts":[{"text":"partial"}]}}],"usageMetadata":{"promptTokenCount":10,"candidatesTokenCount":1}}`,
			`{"candidates":[{"finishReason":"MAX_TOKENS"}],"usageMetadata":{"candidatesTokenCount":8}}`,
			`{"usageMetadata":{"thoughtsTokenCount":2}}`,
		}},
		{"terminal then usage only", []string{
			`{"candidates":[{"content":{"role":"model","parts":[{"text":"partial"}]}}],"usageMetadata":{"promptTokenCount":10,"candidatesTokenCount":1}}`,
			`{"candidates":[{"finishReason":"MAX_TOKENS"}]}`,
			`{"usageMetadata":{"promptTokenCount":10,"candidatesTokenCount":8,"thoughtsTokenCount":2}}`,
		}},
	} {
		t.Run(chunks.name, func(t *testing.T) {
			for _, protocol := range []string{"claude", "gemini"} {
				t.Run(protocol, func(t *testing.T) {
					svc := newAntigravityTestService(&config.Config{})
					c, rec := antigravityProtocolReviewContext()
					resp := antigravityProtocolReviewResponse(chunks.values...)
					if protocol == "claude" {
						body, result, err := svc.collectClaudeStreamResponse(c, resp, time.Now(), "claude-sonnet-4-6")
						require.NoError(t, err)
						var got antigravity.ClaudeResponse
						require.NoError(t, json.Unmarshal(body, &got))
						assert.Equal(t, "max_tokens", got.StopReason)
						assert.Equal(t, 10, got.Usage.OutputTokens)
						assert.Equal(t, 10, result.usage.OutputTokens)
						assert.Equal(t, 10, result.usage.InputTokens)
						require.Len(t, got.Content, 1)
						assert.Equal(t, "partial", got.Content[0].Text)
					} else {
						result, err := svc.handleGeminiStreamToNonStreaming(c, resp, time.Now())
						require.NoError(t, err)
						var got antigravity.GeminiResponse
						require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &got))
						require.Len(t, got.Candidates, 1)
						assert.Equal(t, "MAX_TOKENS", got.Candidates[0].FinishReason)
						require.NotNil(t, got.UsageMetadata)
						assert.Equal(t, 8, got.UsageMetadata.CandidatesTokenCount)
						assert.Equal(t, 10, result.usage.OutputTokens)
						assert.Equal(t, 10, result.usage.InputTokens)
						require.NotNil(t, got.Candidates[0].Content)
						require.Len(t, got.Candidates[0].Content.Parts, 1)
						assert.Equal(t, "partial", got.Candidates[0].Content.Parts[0].Text)
					}
				})
			}
		})
	}
}
func TestAntigravityProtocolReview_ClaudePreservesConsecutiveEmptySignatures(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, stream := range []bool{false, true} {
		name := "collected"
		if stream {
			name = "streaming"
		}
		t.Run(name, func(t *testing.T) {
			svc := newAntigravityTestService(&config.Config{})
			c, rec := antigravityProtocolReviewContext()
			resp := antigravityProtocolReviewResponse(
				`{"candidates":[{"content":{"role":"model","parts":[{"text":"hello"}]}}]}`,
				`{"candidates":[{"content":{"role":"model","parts":[{"text":"","thoughtSignature":"first-empty-signature"},{"text":"","thoughtSignature":"second-empty-signature"}]},"finishReason":"STOP"}]}`,
			)
			if stream {
				_, err := svc.handleClaudeStreamingResponse(c, resp, time.Now(), "claude-sonnet-4-6")
				require.NoError(t, err)
				assert.Contains(t, rec.Body.String(), `"signature":"first-empty-signature"`)
				assert.Contains(t, rec.Body.String(), `"signature":"second-empty-signature"`)
			} else {
				body, _, err := svc.collectClaudeStreamResponse(c, resp, time.Now(), "claude-sonnet-4-6")
				require.NoError(t, err)
				var got antigravity.ClaudeResponse
				require.NoError(t, json.Unmarshal(body, &got))
				require.Equal(t, []antigravity.ClaudeContentItem{{Type: "text", Text: "hello"}, {Type: "thinking", Signature: "first-empty-signature"}, {Type: "thinking", Signature: "second-empty-signature"}}, got.Content)
			}
		})
	}
}

func TestGeminiProtocolReview_CollectPreservesSeparateTerminalMetadata(t *testing.T) {
	for _, oauth := range []bool{false, true} {
		name := "direct"
		if oauth {
			name = "wrapped"
		}
		t.Run(name, func(t *testing.T) {
			chunks := []string{
				`{"modelVersion":"gemini-version","responseId":"response-1","candidates":[{"content":{"role":"model","parts":[{"text":"partial"}]}}],"usageMetadata":{"promptTokenCount":10,"candidatesTokenCount":1}}`,
				`{"candidates":[{"finishReason":"MAX_TOKENS"}]}`,
				`{"usageMetadata":{"promptTokenCount":10,"candidatesTokenCount":8,"thoughtsTokenCount":2}}`,
			}
			var source strings.Builder
			for _, chunk := range chunks {
				source.WriteString("data: ")
				if oauth {
					source.WriteString(`{"response":`)
					source.WriteString(chunk)
					source.WriteString("}")
				} else {
					source.WriteString(chunk)
				}
				source.WriteString("\n\n")
			}
			source.WriteString("data: [DONE]\n\n")
			got, usage, err := collectGeminiSSE(strings.NewReader(source.String()), oauth)
			require.NoError(t, err)
			assert.Equal(t, "MAX_TOKENS", extractGeminiFinishReason(got))
			assert.Equal(t, "gemini-version", got["modelVersion"])
			assert.Equal(t, "response-1", got["responseId"])
			assert.Equal(t, []map[string]any{{"text": "partial"}}, extractGeminiParts(got))
			assert.Equal(t, 10, usage.OutputTokens)
			metadata, ok := got["usageMetadata"].(map[string]any)
			require.True(t, ok)
			assert.Equal(t, float64(8), metadata["candidatesTokenCount"])
			assert.Equal(t, float64(2), metadata["thoughtsTokenCount"])
		})
	}
}

func TestGeminiProtocolReview_EmptyTerminalPartsPreserveToolCall(t *testing.T) {
	for _, empty := range []string{"[]", "null"} {
		t.Run(empty, func(t *testing.T) {
			source := "data: " + `{"candidates":[{"content":{"role":"model","parts":[{"functionCall":{"name":"lookup","args":{"key":"x"}},"thoughtSignature":"tool-signature"}]}}]}` + "\n\n" +
				"data: " + `{"candidates":[{"content":{"parts":` + empty + `},"finishReason":"STOP"}],"usageMetadata":{"promptTokenCount":10,"candidatesTokenCount":8}}` + "\n\n"
			got, usage, err := collectGeminiSSE(strings.NewReader(source), false)
			require.NoError(t, err)
			assert.Equal(t, "STOP", extractGeminiFinishReason(got))
			parts := extractGeminiParts(got)
			require.Len(t, parts, 1)
			assert.Equal(t, "tool-signature", parts[0]["thoughtSignature"])
			assert.Equal(t, map[string]any{"name": "lookup", "args": map[string]any{"key": "x"}}, parts[0]["functionCall"])
			assert.Equal(t, 8, usage.OutputTokens)
		})
	}
}

func TestAntigravityProtocolReview_MetadataMergeKeepsRawSignedParts(t *testing.T) {
	gin.SetMode(gin.TestMode)
	svc := newAntigravityTestService(&config.Config{})
	c, rec := antigravityProtocolReviewContext()
	resp := antigravityProtocolReviewResponse(
		`{"candidates":[{"content":{"role":"model","parts":[{"text":"before"}]}}],"modelVersion":"version-1","responseId":"response-1"}`,
		`{"candidates":[{"content":{"parts":[{"text":"signed","thoughtSignature":"part-signature"},{"text":"after"},{"text":"","thoughtSignature":"empty-signature"}]}}]}`,
		`{"candidates":[{"finishReason":"MAX_TOKENS"}],"modelVersion":"version-2"}`,
		`{"usageMetadata":{"promptTokenCount":10,"candidatesTokenCount":8,"thoughtsTokenCount":2}}`,
	)
	result, err := svc.handleGeminiStreamToNonStreaming(c, resp, time.Now())
	require.NoError(t, err)
	var got map[string]any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &got))
	assert.Equal(t, []map[string]any{
		{"text": "before"}, {"text": "signed", "thoughtSignature": "part-signature"}, {"text": "after"}, {"text": "", "thoughtSignature": "empty-signature"},
	}, extractGeminiParts(got))
	assert.Equal(t, "MAX_TOKENS", extractGeminiFinishReason(got))
	assert.Equal(t, "version-2", got["modelVersion"])
	assert.Equal(t, "response-1", got["responseId"])
	assert.Equal(t, 10, result.usage.OutputTokens)
}

func TestAntigravityProtocolReview_SignatureHandlingKeepsToolsAndImages(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, stream := range []bool{false, true} {
		name := "collected"
		if stream {
			name = "streaming"
		}
		t.Run(name, func(t *testing.T) {
			svc := newAntigravityTestService(&config.Config{})
			c, rec := antigravityProtocolReviewContext()
			resp := antigravityProtocolReviewResponse(
				`{"candidates":[{"content":{"role":"model","parts":[{"text":"before"},{"functionCall":{"id":"call-1","name":"lookup","args":{"key":"x"}},"thoughtSignature":"tool-signature"},{"inlineData":{"mimeType":"image/png","data":"AA=="},"thoughtSignature":"image-signature"},{"text":"after"}]},"finishReason":"STOP"}]}`,
			)
			if stream {
				_, err := svc.handleClaudeStreamingResponse(c, resp, time.Now(), "claude-sonnet-4-6")
				require.NoError(t, err)
				body := rec.Body.String()
				assert.Contains(t, body, `"type":"tool_use"`)
				assert.Contains(t, body, `"signature":"tool-signature"`)
				assert.Contains(t, body, `![image](data:image/png;base64,AA==)`)
				assert.Contains(t, body, `"text":"after"`)
				assert.Contains(t, body, `"stop_reason":"tool_use"`)
			} else {
				body, _, err := svc.collectClaudeStreamResponse(c, resp, time.Now(), "claude-sonnet-4-6")
				require.NoError(t, err)
				var got antigravity.ClaudeResponse
				require.NoError(t, json.Unmarshal(body, &got))
				assert.Equal(t, []antigravity.ClaudeContentItem{
					{Type: "text", Text: "before"},
					{Type: "tool_use", ID: "call-1", Name: "lookup", Input: map[string]any{"key": "x"}, Signature: "tool-signature"},
					{Type: "text", Text: "![image](data:image/png;base64,AA==)"},
					{Type: "text", Text: "after"},
				}, got.Content)
				assert.Equal(t, "tool_use", got.StopReason)
			}
		})
	}
}

func TestAntigravityProtocolReview_NullResponseStillTriggersFailover(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, payload := range []string{`null`, `{"response":null}`} {
		t.Run(payload, func(t *testing.T) {
			svc := newAntigravityTestService(&config.Config{})
			c, rec := antigravityProtocolReviewContext()
			resp := &http.Response{StatusCode: http.StatusOK, Header: http.Header{}, Body: io.NopCloser(strings.NewReader("data: " + payload + "\n\n"))}
			result, err := svc.handleGeminiStreamToNonStreaming(c, resp, time.Now())
			var failover *UpstreamFailoverError
			require.ErrorAs(t, err, &failover)
			assert.Nil(t, result)
			assert.Empty(t, rec.Body.String())
		})
	}
}
