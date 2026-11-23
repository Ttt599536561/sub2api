//go:build unit

package antigravity

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// Gemini usageMetadata 只有 cachedContentTokenCount（缓存命中），没有缓存写入的 token 类别：
// 两条转换路径产出的 Claude usage 中 cache_creation_input_tokens 恒为 0，缓存命中计入
// cache_read_input_tokens 并从 input_tokens 中扣除。
func TestGeminiUsageMapping_NoCacheCreationTokens(t *testing.T) {
	const geminiBody = `{"candidates":[{"content":{"parts":[{"text":"hi"}],"role":"model"},"finishReason":"STOP"}],` +
		`"usageMetadata":{"promptTokenCount":100,"candidatesTokenCount":20,"cachedContentTokenCount":30,"thoughtsTokenCount":5}}`

	t.Run("non-stream", func(t *testing.T) {
		_, usage, err := TransformGeminiToClaude([]byte(geminiBody), "gemini-3.1-pro-preview")
		require.NoError(t, err)
		require.NotNil(t, usage)
		require.Equal(t, 70, usage.InputTokens)
		require.Equal(t, 25, usage.OutputTokens)
		require.Equal(t, 30, usage.CacheReadInputTokens)
		require.Zero(t, usage.CacheCreationInputTokens)
	})

	t.Run("stream", func(t *testing.T) {
		p := NewStreamingProcessor("gemini-3.1-pro-preview")
		out := p.ProcessLine(`data: {"response":` + geminiBody + `}`)
		require.True(t, strings.Contains(string(out), `"message_start"`))
		require.NotContains(t, string(out), `"cache_creation_input_tokens"`, "message_start 的 usage 不应携带缓存写入分项")
		_, usage := p.Finish()
		require.NotNil(t, usage)
		require.Equal(t, 70, usage.InputTokens)
		require.Equal(t, 30, usage.CacheReadInputTokens)
		require.Zero(t, usage.CacheCreationInputTokens)
	})
}

// usageMetadata 缺 promptTokenCount、只有 cachedContentTokenCount 时，input_tokens 下限为 0，
// 缓存命中仍计入 cache_read_input_tokens。
func TestGeminiUsageMapping_MissingPromptTokenCountKeepsInputNonNegative(t *testing.T) {
	const geminiBody = `{"candidates":[{"content":{"parts":[{"text":"hi"}],"role":"model"},"finishReason":"STOP"}],` +
		`"usageMetadata":{"cachedContentTokenCount":30}}`

	t.Run("non-stream", func(t *testing.T) {
		_, usage, err := TransformGeminiToClaude([]byte(geminiBody), "gemini-3.1-pro-preview")
		require.NoError(t, err)
		require.NotNil(t, usage)
		require.Zero(t, usage.InputTokens)
		require.Equal(t, 30, usage.CacheReadInputTokens)
	})

	t.Run("stream", func(t *testing.T) {
		p := NewStreamingProcessor("gemini-3.1-pro-preview")
		out := p.ProcessLine(`data: {"response":` + geminiBody + `}`)
		require.True(t, strings.Contains(string(out), `"message_start"`))
		require.NotContains(t, string(out), `"input_tokens":-`, "message_start 的 input_tokens 不应为负")
		_, usage := p.Finish()
		require.NotNil(t, usage)
		require.Zero(t, usage.InputTokens)
		require.Equal(t, 30, usage.CacheReadInputTokens)
	})
}

func TestGeminiImageInputForAccountStats(t *testing.T) {
	for _, tc := range []struct {
		name, cache string
		want        int
	}{
		{"uncached", "", 8},
		{"cached split", `,"cachedContentTokenCount":4,"cacheTokensDetails":[{"modality":"IMAGE","tokenCount":3},{"modality":"TEXT","tokenCount":1}]`, 5},
		{"cache split unavailable", `,"cachedContentTokenCount":4`, 0},
		{"malformed cache detail", `,"cachedContentTokenCount":3,"cacheTokensDetails":"unknown"`, 0},
		{"missing cached total", `,"cacheTokensDetails":[{"modality":"IMAGE","tokenCount":3}]`, 0},
		{"unspecified cached modality", `,"cachedContentTokenCount":3,"cacheTokensDetails":[{"tokenCount":3}]`, 0},
		{"incomplete split", `,"cachedContentTokenCount":4,"cacheTokensDetails":[{"modality":"TEXT","tokenCount":1}]`, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			body := `{"candidates":[{"content":{"parts":[{"text":"ok"}]}}],"usageMetadata":{"promptTokenCount":12,"promptTokensDetails":[{"modality":"IMAGE","tokenCount":8}]` + tc.cache + `}}`
			wire, u, err := TransformGeminiToClaude([]byte(body), "gemini")
			require.NoError(t, err)
			require.Equal(t, tc.want, u.AccountStatsImageInputTokens)
			require.NotContains(t, string(wire), "image_input_tokens")
			stream := NewStreamingProcessor("gemini")
			stream.ProcessLine("data: " + body)
			stream.ProcessLine(`data: {"usageMetadata":{"candidatesTokenCount":4}}`)
			wire, u = stream.Finish()
			require.Equal(t, tc.want, u.AccountStatsImageInputTokens)
			require.NotContains(t, string(wire), "image_input_tokens")
		})
	}
}
