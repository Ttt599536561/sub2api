package apicompat

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestCacheWriteTTLUsageRoundTrip(t *testing.T) {
	for _, tc := range []struct{ name, details string }{
		{"30m", `{"ttl_30m_tokens":10,"ttl_12h_tokens":0}`},
		{"12h", `{"ttl_30m_tokens":0,"ttl_12h_tokens":10}`},
		{"mixed", `{"ttl_30m_tokens":3,"ttl_12h_tokens":7}`},
		{"partial", `{"ttl_12h_tokens":10}`},
		{"null", `null`},
		{"fractional", `{"ttl_30m_tokens":0.5,"ttl_12h_tokens":9.5}`},
		{"string", `{"ttl_30m_tokens":"0","ttl_12h_tokens":"10"}`},
	} {
		for _, field := range []string{"input_tokens_details", "prompt_tokens_details"} {
			t.Run(tc.name+"/"+field, func(t *testing.T) {
				raw := `{"input_tokens":12,"output_tokens":4,"` + field + `":{"cache_write_tokens":10,"cache_write_tokens_details":` + tc.details + `}}`
				var usage ResponsesUsage
				require.NoError(t, json.Unmarshal([]byte(raw), &usage), "invalid TTL metadata must not reject the response")
				response := ResponsesResponse{Usage: &usage}
				chat := ResponsesToChatCompletions(&response, "gpt-6-luna")
				encodedChat, err := json.Marshal(chat)
				require.NoError(t, err)
				require.JSONEq(t, tc.details, gjson.GetBytes(encodedChat, "usage.prompt_tokens_details.cache_write_tokens_details").Raw)
				var decodedChat ChatCompletionsResponse
				require.NoError(t, json.Unmarshal(encodedChat, &decodedChat))
				roundTrip := ChatUsageToResponsesUsage(decodedChat.Usage)
				encodedUsage, err := json.Marshal(roundTrip)
				require.NoError(t, err)
				require.Equal(t, int64(10), gjson.GetBytes(encodedUsage, "input_tokens_details.cache_write_tokens").Int())
				require.JSONEq(t, tc.details, gjson.GetBytes(encodedUsage, "input_tokens_details.cache_write_tokens_details").Raw)
			})
		}
	}
}

func TestCacheWriteTTLUsageMixedDetailAliases(t *testing.T) {
	const promptDetails = `{"ttl_30m_tokens":0,"ttl_12h_tokens":10}`
	for _, tc := range []struct{ name, inputField, want string }{
		{"missing input TTL falls back to prompt", "", promptDetails},
		{"null input TTL blocks prompt fallback", `,"cache_write_tokens_details":null`, "null"},
		{"invalid input TTL blocks prompt fallback", `,"cache_write_tokens_details":{}`, "{}"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			raw := `{"input_tokens":12,"output_tokens":4,"input_tokens_details":{"cached_tokens":1` + tc.inputField + `},"prompt_tokens_details":{"cache_write_tokens":10,"cache_write_tokens_details":` + promptDetails + `}}`
			var usage ResponsesUsage
			require.NoError(t, json.Unmarshal([]byte(raw), &usage))
			require.Equal(t, 10, usage.CacheCreationInputTokens)
			for _, roundTrip := range []*ResponsesUsage{&usage, ChatUsageToResponsesUsage(chatUsageFromResponsesUsage(&usage))} {
				body, err := json.Marshal(roundTrip)
				require.NoError(t, err)
				require.Equal(t, int64(1), gjson.GetBytes(body, "input_tokens_details.cached_tokens").Int())
				require.JSONEq(t, tc.want, gjson.GetBytes(body, "input_tokens_details.cache_write_tokens_details").Raw)
			}
		})
	}
}
