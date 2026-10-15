//go:build unit

package antigravity

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestStreamingProtocolReview_UsageAfterFinishReason(t *testing.T) {
	for _, wrapped := range []bool{false, true} {
		shape := "direct"
		if wrapped {
			shape = "wrapped"
		}
		for _, fragmentedUsage := range []bool{false, true} {
			name := shape + "/full usage"
			if fragmentedUsage {
				name = shape + "/fragmented usage"
			}
			t.Run(name, func(t *testing.T) {
				p := NewStreamingProcessor("claude-sonnet-4-6")
				var wire strings.Builder
				finalUsage := `{"promptTokenCount":10,"candidatesTokenCount":8,"thoughtsTokenCount":2}`
				terminalUsage := ""
				if fragmentedUsage {
					terminalUsage = `,"usageMetadata":{"candidatesTokenCount":8}`
					finalUsage = `{"thoughtsTokenCount":2}`
				}
				for i, chunk := range []string{
					`{"candidates":[{"content":{"role":"model","parts":[{"text":"partial"}]}}],"usageMetadata":{"promptTokenCount":10,"candidatesTokenCount":1}}`,
					`{"candidates":[{"finishReason":"MAX_TOKENS"}]` + terminalUsage + `}`,
					`{"usageMetadata":` + finalUsage + `}`,
				} {
					if wrapped {
						chunk = `{"response":` + chunk + `}`
					}
					events := p.ProcessLine("data: " + chunk)
					if i == 0 {
						assert.Contains(t, string(events), `"text":"partial"`, "text remains streamed immediately")
					}
					assert.NotContains(t, string(events), "event: message_stop\n", "only the actual end may close the message")
					wire.Write(events)
				}
				final, usage := p.Finish()
				wire.Write(final)
				assert.Equal(t, 10, usage.InputTokens)
				assert.Equal(t, 10, usage.OutputTokens)
				assert.Contains(t, wire.String(), `"stop_reason":"max_tokens"`)
				assert.Contains(t, wire.String(), `"output_tokens":10`, "wire usage must match final accumulated billing usage")
				assert.Equal(t, 1, strings.Count(wire.String(), "event: message_stop\n"))
				duplicate, repeatedUsage := p.Finish()
				assert.Empty(t, duplicate)
				assert.Equal(t, usage, repeatedUsage)
			})
		}
	}
}

func TestStreamingProtocolReview_ExplicitZeroUsageAndDone(t *testing.T) {
	p := NewStreamingProcessor("claude-sonnet-4-6")
	p.ProcessLine(`data: {"response":{"candidates":[{"content":{"parts":[{"text":"hi"}]}}],"usageMetadata":{"promptTokenCount":10,"candidatesTokenCount":1,"cachedContentTokenCount":4,"thoughtsTokenCount":2}}}`)
	p.ProcessLine(`data: {"response":{"candidates":[{"finishReason":"STOP"}],"usageMetadata":{"candidatesTokenCount":8,"cachedContentTokenCount":0,"thoughtsTokenCount":0}}}`)
	done := p.ProcessLine("data: [DONE]")
	require.Contains(t, string(done), "event: message_stop\n")
	final, usage := p.Finish()
	assert.Empty(t, final)
	assert.Equal(t, 10, usage.InputTokens)
	assert.Equal(t, 8, usage.OutputTokens)
	assert.Zero(t, usage.CacheReadInputTokens)
	var finalDelta map[string]any
	for _, line := range strings.Split(string(done), "\n") {
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		var event map[string]any
		require.NoError(t, json.Unmarshal([]byte(strings.TrimPrefix(line, "data: ")), &event))
		if event["type"] == "message_delta" {
			finalDelta = event
		}
	}
	require.NotNil(t, finalDelta)
	assert.Equal(t, "end_turn", finalDelta["delta"].(map[string]any)["stop_reason"])
}

func TestStreamingProtocolReview_SignedThinkingPartBoundaries(t *testing.T) {
	for _, stream := range []bool{false, true} {
		name := "collected"
		if stream {
			name = "streaming"
		}
		t.Run(name, func(t *testing.T) {
			parts := []GeminiPart{
				{Text: "before ", Thought: true}, {Text: "unsigned", Thought: true},
				{Text: "first signed", Thought: true, ThoughtSignature: "sig-1"},
				{Text: "second signed", Thought: true, ThoughtSignature: "sig-2"},
				{Text: "after ", Thought: true}, {Text: "unsigned", Thought: true},
			}
			want := []ClaudeContentItem{
				{Type: "thinking", Thinking: "before unsigned"},
				{Type: "thinking", Thinking: "first signed", Signature: "sig-1"},
				{Type: "thinking", Thinking: "second signed", Signature: "sig-2"},
				{Type: "thinking", Thinking: "after unsigned"},
			}
			var got []ClaudeContentItem
			if !stream {
				response := NewNonStreamingProcessor().Process(&GeminiResponse{Candidates: []GeminiCandidate{{Content: &GeminiContent{Role: "model", Parts: parts}}}}, "response-1", "claude-sonnet-4-6")
				got = response.Content
			} else {
				p := NewStreamingProcessor("claude-sonnet-4-6")
				var wire strings.Builder
				for _, part := range parts {
					chunk, err := json.Marshal(V1InternalResponse{Response: GeminiResponse{Candidates: []GeminiCandidate{{Content: &GeminiContent{Role: "model", Parts: []GeminiPart{part}}}}}})
					require.NoError(t, err)
					wire.Write(p.ProcessLine("data: " + string(chunk)))
				}
				final, _ := p.Finish()
				wire.Write(final)
				for _, line := range strings.Split(wire.String(), "\n") {
					if !strings.HasPrefix(line, "data: ") {
						continue
					}
					var event struct {
						Type         string            `json:"type"`
						ContentBlock ClaudeContentItem `json:"content_block"`
						Delta        struct {
							Thinking  string `json:"thinking"`
							Signature string `json:"signature"`
						} `json:"delta"`
					}
					require.NoError(t, json.Unmarshal([]byte(strings.TrimPrefix(line, "data: ")), &event))
					if event.Type == "content_block_start" {
						got = append(got, event.ContentBlock)
					}
					if event.Type == "content_block_delta" {
						require.NotEmpty(t, got)
						got[len(got)-1].Thinking += event.Delta.Thinking
						got[len(got)-1].Signature += event.Delta.Signature
					}
				}
			}
			assert.Equal(t, want, got)
		})
	}
}
