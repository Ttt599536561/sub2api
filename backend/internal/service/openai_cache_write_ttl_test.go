//go:build unit

package service

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/apicompat"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

// The mixed case is the sanitized usage and channel price card from #8604.
// Observe the actual response handlers and RecordUsage, including the debit and
// account-statistics cost, rather than reconstructing the billing formula there.
func TestOpenAICacheWriteTTLRecordUsage(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, tc := range []struct {
		name        string
		short, long int
		details     string
		sequence    string
	}{
		{"30m", 2399, 0, "", ""},
		{"12h", 0, 2399, "", ""},
		{"mixed", 9, 2390, "", ""},
		{"missing", 0, 0, "omit", ""},
		{"mixed_detail_aliases", 0, 2399, "", ""},
		{"input_null_blocks_prompt", 0, 0, "null", ""},
		{"input_invalid_blocks_prompt", 0, 0, "{}", ""},
		{"explicit_zero_aggregate", 0, 0, "", ""},
		{"empty_details", 0, 0, `{}`, ""},
		{"non_object", 0, 0, `[]`, ""},
		{"out_of_range", 0, 0, `{"ttl_30m_tokens":0,"ttl_12h_tokens":1e100}`, ""},
		{"null", 0, 0, "null", ""},
		{"partial", 0, 0, `{"ttl_12h_tokens":2399}`, ""},
		{"inconsistent", 0, 0, `{"ttl_30m_tokens":0,"ttl_12h_tokens":2398}`, ""},
		{"negative", 0, 0, `{"ttl_30m_tokens":-1,"ttl_12h_tokens":2400}`, ""},
		{"fractional", 0, 0, `{"ttl_30m_tokens":0.5,"ttl_12h_tokens":2398.5}`, ""},
		{"string", 0, 0, `{"ttl_30m_tokens":"0","ttl_12h_tokens":"2399"}`, ""},
		{"progressive_then_empty_terminal", 9, 2390, "", "empty"},
		{"progressive_then_missing_terminal_usage", 9, 2390, "", "missing"},
		{"progressive_then_aggregate_terminal", 0, 0, "omit", "aggregate"},
		{"conflicting_terminal_usage_sources", 9, 2390, "", "both"},
		{"top_level_terminal_usage", 9, 2390, "", "top"},
	} {
		for _, route := range []string{"json", "sse", "passthrough_json", "passthrough_sse", "messages_sse", "messages_buffered_sse", "chat_sse", "chat_buffered_sse", "typed_json", "typed_chat_json", "typed_copy"} {
			stream := strings.HasSuffix(route, "sse")
			if tc.sequence != "" && !stream {
				continue // These cases exercise successive SSE usage observations.
			}
			t.Run(tc.name+"/"+route, func(t *testing.T) {
				const groupID int64 = 777
				usageRepo := &openAIRecordUsageLogRepoStub{inserted: true}
				userRepo := &openAIRecordUsageUserRepoStub{}
				svc := newOpenAIRecordUsageServiceForTest(usageRepo, userRepo, &openAIRecordUsageSubRepoStub{}, nil)
				var pricing ChannelModelPricing
				require.NoError(t, json.Unmarshal([]byte(`{"platform":"openai","models":["gpt-6-luna"],"billing_mode":"token","input_price":0.00000065,"output_price":0.00000325,"cache_write_price":0.0000008125,"cache_write_12h_price":0.000001625,"cache_read_price":0.000000065}`), &pricing))
				channel := Channel{ID: 1, Status: StatusActive, GroupIDs: []int64{groupID}, ModelPricing: []ChannelModelPricing{pricing}, ApplyPricingToAccountStats: true}
				svc.channelService = &ChannelService{}
				svc.channelService.cache.Store(populateChannelCache([]Channel{channel}, map[int64]string{groupID: PlatformOpenAI}))
				svc.resolver = NewModelPricingResolver(svc.channelService, svc.billingService)
				svc.cfg.Gateway.MaxLineSize = defaultMaxLineSize
				account := &Account{ID: 1, Platform: PlatformOpenAI, Type: AccountTypeAPIKey}
				details := tc.details
				if details == "" {
					details = fmt.Sprintf(`{"ttl_30m_tokens":%d,"ttl_12h_tokens":%d}`, tc.short, tc.long)
				}
				detailField := `,"cache_write_tokens_details":` + details
				if details == "omit" {
					detailField = ""
				}
				aggregate := 2399
				if tc.name == "explicit_zero_aggregate" {
					aggregate = 0
				}
				// The stale top-level alias must not override an explicit nested zero.
				usageJSON := fmt.Sprintf(`{"input_tokens":2402,"cache_creation_input_tokens":2399,"input_tokens_details":{"cached_tokens":0,"cache_write_tokens":%d%s},"output_tokens":5,"total_tokens":2407}`, aggregate, detailField)
				cacheRead := 0
				if tc.name == "mixed_detail_aliases" || strings.HasSuffix(tc.name, "blocks_prompt") {
					cacheRead = 1
					inputField := detailField
					if tc.name == "mixed_detail_aliases" {
						inputField = ""
					}
					usageJSON = `{"input_tokens":2402,"output_tokens":5,"input_tokens_details":{"cached_tokens":1` + inputField + `},"prompt_tokens_details":{"cache_write_tokens":2399,"cache_write_tokens_details":{"ttl_30m_tokens":0,"ttl_12h_tokens":2399}}}`
				}
				response := `{"id":"resp_ttl","object":"response","model":"gpt-6-luna","status":"completed","output":[],"usage":` + usageJSON + `}`
				contentType := "application/json"
				if stream {
					terminal := `{"type":"response.completed","response":` + response + `}`
					progressiveUsage := `{"input_tokens":2402,"input_tokens_details":{"cache_write_tokens":2399,"cache_write_tokens_details":{"ttl_30m_tokens":9,"ttl_12h_tokens":2390}},"output_tokens":5}`
					prefix := ""
					switch tc.sequence {
					case "empty":
						prefix = "data: " + `{"type":"response.in_progress","response":{"usage":` + progressiveUsage + `}}` + "\n\n"
						terminal = strings.Replace(terminal, usageJSON, `{"input_tokens":0,"output_tokens":0}`, 1)
					case "missing":
						prefix = "data: " + `{"type":"response.in_progress","response":{"usage":` + progressiveUsage + `}}` + "\n\n"
						terminal = strings.Replace(terminal, `,"usage":`+usageJSON, "", 1)
					case "aggregate":
						prefix = "data: " + `{"type":"response.in_progress","response":{"usage":` + progressiveUsage + `}}` + "\n\n"
					case "both":
						terminal = strings.TrimSuffix(terminal, "}") + `,"usage":{"input_tokens":2402,"output_tokens":5,"input_tokens_details":{"cache_write_tokens":2399,"cache_write_tokens_details":{"ttl_30m_tokens":2399,"ttl_12h_tokens":0}}}}`
					case "top":
						terminal = strings.Replace(terminal, `,"usage":`+usageJSON, "", 1)
						terminal = strings.TrimSuffix(terminal, "}") + `,"usage":` + usageJSON + `}`
					}
					response = prefix + "data: " + terminal + "\n\n"
					contentType = "text/event-stream"
				}
				if route == "typed_json" || route == "typed_chat_json" {
					var typed apicompat.ResponsesResponse
					require.NoError(t, json.Unmarshal([]byte(response), &typed))
					if route == "typed_chat_json" {
						chat := apicompat.ResponsesToChatCompletions(&typed, "gpt-6-luna")
						body, err := json.Marshal(chat)
						require.NoError(t, err)
						var decoded apicompat.ChatCompletionsResponse
						require.NoError(t, json.Unmarshal(body, &decoded))
						typed.Usage = apicompat.ChatUsageToResponsesUsage(decoded.Usage)
					}
					body, err := json.Marshal(typed)
					require.NoError(t, err)
					response = string(body)
				}
				resp := &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {contentType}}, Body: io.NopCloser(strings.NewReader(response))}
				c, _ := gin.CreateTestContext(httptest.NewRecorder())
				// Requested retention is not evidence of a billed 12h write.
				c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(`{"prompt_cache_retention":"12h"}`))
				var usage *OpenAIUsage
				switch route {
				case "json", "typed_json", "typed_chat_json":
					result, err := svc.handleNonStreamingResponse(context.Background(), resp, c, account, "gpt-6-luna", "gpt-6-luna")
					require.NoError(t, err)
					usage = result.usage
				case "typed_copy":
					var typed apicompat.ResponsesResponse
					require.NoError(t, json.Unmarshal([]byte(response), &typed))
					copied := copyOpenAIUsageFromResponsesUsage(typed.Usage)
					usage = &copied
				case "messages_sse":
					result, err := svc.handleAnthropicStreamingResponse(resp, c, account, "gpt-6-luna", "gpt-6-luna", "gpt-6-luna", time.Now())
					require.NoError(t, err)
					usage = &result.Usage
				case "messages_buffered_sse":
					result, err := svc.handleAnthropicBufferedStreamingResponse(resp, c, account, "gpt-6-luna", "gpt-6-luna", "gpt-6-luna", time.Now())
					require.NoError(t, err)
					usage = &result.Usage
				case "chat_sse":
					result, err := svc.handleChatStreamingResponse(resp, c, account, "gpt-6-luna", "gpt-6-luna", "gpt-6-luna", time.Now(), 0)
					require.NoError(t, err)
					usage = &result.Usage
				case "chat_buffered_sse":
					result, err := svc.handleChatBufferedStreamingResponse(resp, c, account, "gpt-6-luna", "gpt-6-luna", "gpt-6-luna", time.Now())
					require.NoError(t, err)
					usage = &result.Usage
				case "sse":
					result, err := svc.handleStreamingResponse(context.Background(), resp, c, account, time.Now(), "gpt-6-luna", "gpt-6-luna")
					require.NoError(t, err)
					usage = result.usage
				case "passthrough_json":
					result, err := svc.handleNonStreamingResponsePassthrough(context.Background(), resp, c, account, "gpt-6-luna", "gpt-6-luna")
					require.NoError(t, err)
					usage = result.usage
				case "passthrough_sse":
					result, err := svc.handleStreamingResponsePassthrough(context.Background(), resp, c, account, time.Now(), "gpt-6-luna", "gpt-6-luna")
					require.NoError(t, err)
					usage = result.usage
				}
				require.NotNil(t, usage)
				err := svc.RecordUsage(context.Background(), &OpenAIRecordUsageInput{
					Result: &OpenAIForwardResult{RequestID: "resp_ttl", Model: "gpt-6-luna", Usage: *usage, Stream: stream},
					APIKey: &APIKey{ID: 1, GroupID: i64p(groupID), Group: &Group{ID: groupID, Platform: PlatformOpenAI, RateMultiplier: 0.5}},
					User:   &User{ID: 1}, Account: account,
					ChannelUsageFields: ChannelUsageFields{OriginalModel: "gpt-6-luna", BillingModelSource: BillingModelSourceRequested},
				})
				require.NoError(t, err)
				log := usageRepo.lastLog
				require.NotNil(t, log)
				assert.Equal(t, 2402-aggregate-cacheRead, log.InputTokens)
				assert.Equal(t, cacheRead, log.CacheReadTokens)
				assert.Equal(t, aggregate, log.CacheCreationTokens)
				short, long := tc.short, tc.long
				// Retain each route's existing precedence when both usage sources exist.
				if tc.sequence == "both" && (route == "sse" || route == "passthrough_sse" || route == "messages_sse") {
					short, long = 2399, 0
				}
				assert.Equal(t, short, log.CacheCreation30mTokens)
				assert.Equal(t, long, log.CacheCreation12hTokens)
				assert.Zero(t, log.CacheCreation5mTokens)
				assert.Zero(t, log.CacheCreation1hTokens)
				cacheCost := (float64(aggregate-long)*0.8125 + float64(long)*1.625) / 1e6
				totalCost := cacheCost + float64(2402-aggregate-cacheRead)*0.65/1e6 + float64(cacheRead)*0.065/1e6 + 5*3.25/1e6
				assert.InDelta(t, cacheCost, log.CacheCreationCost, 1e-12)
				assert.InDelta(t, totalCost, log.TotalCost, 1e-12)
				assert.InDelta(t, totalCost*0.5, log.ActualCost, 1e-12)
				assert.InDelta(t, totalCost*0.5, userRepo.lastAmount, 1e-12)
				require.NotNil(t, log.AccountStatsCost)
				assert.InDelta(t, totalCost, *log.AccountStatsCost, 1e-12)
			})
		}
	}
}

func TestOpenAICacheWriteTTLInvalidDetailsKeepAggregate(t *testing.T) {
	for _, details := range []string{
		`null`, `{}`, `{"ttl_12h_tokens":10}`,
		`{"ttl_30m_tokens":0,"ttl_12h_tokens":9}`,
		`{"ttl_30m_tokens":9,"ttl_12h_tokens":9}`,
		`{"ttl_30m_tokens":-1,"ttl_12h_tokens":11}`,
		`{"ttl_30m_tokens":0.5,"ttl_12h_tokens":9.5}`,
		`{"ttl_30m_tokens":"0","ttl_12h_tokens":"10"}`,
	} {
		t.Run(details, func(t *testing.T) {
			usage, ok := openAIUsageFromGJSON(gjson.Parse(`{"input_tokens":10,"input_tokens_details":{"cache_write_tokens":10,"cache_write_tokens_details":` + details + `}}`))
			require.True(t, ok)
			require.Equal(t, 10, usage.CacheCreationInputTokens)
			require.Zero(t, usage.CacheCreation30mInputTokens)
			require.Zero(t, usage.CacheCreation12hInputTokens)
		})
	}
}

func TestOpenAICacheWriteTTLPrices(t *testing.T) {
	service := NewBillingService(nil, nil)
	base, err := service.GetModelPricing("gpt-6-luna")
	require.NoError(t, err)
	tokens := UsageTokens{InputTokens: 3, OutputTokens: 5, CacheCreationTokens: 2399, CacheCreation30mTokens: 9, CacheCreation12hTokens: 2390}

	t.Run("catalog 12h rate", func(t *testing.T) {
		cost := service.computeTokenBreakdown(base, tokens, 1, "", false)
		require.InDelta(t, (9*0.125+2390*0.25)/1e6, cost.CacheCreationCost, 1e-12)
	})
	t.Run("legacy flat channel price", func(t *testing.T) {
		p := *base
		applyChannelTokenPriceOverrides(&p, &ChannelModelPricing{CacheWritePrice: float64Ptr(0.8125e-6)})
		cost := service.computeTokenBreakdown(&p, tokens, 1, "", false)
		require.InDelta(t, 2399*0.8125e-6, cost.CacheCreationCost, 1e-12)
	})
	for _, tier := range []struct {
		name string
		rate float64
	}{{"default", 1}, {"fast", 2}, {"flex", 0.5}} {
		t.Run(tier.name+"/long context and group rate", func(t *testing.T) {
			p := *base
			applyChannelTokenPriceOverrides(&p, &ChannelModelPricing{
				InputPrice: float64Ptr(0.65e-6), OutputPrice: float64Ptr(3.25e-6),
				CacheWritePrice: float64Ptr(0.8125e-6), CacheWrite12hPrice: float64Ptr(1.625e-6),
			})
			p.LongContextInputThreshold = 1
			p.LongContextInputMultiplier = 2
			p.LongContextOutputMultiplier = 1.5
			cost := service.computeTokenBreakdown(&p, tokens, 0.5, tier.name, true)
			cacheCost := (9*0.8125 + 2390*1.625) / 1e6 * 2 * tier.rate
			want := cacheCost + 3*0.65e-6*2*tier.rate + 5*3.25e-6*1.5*tier.rate
			require.InDelta(t, cacheCost, cost.CacheCreationCost, 1e-12)
			require.InDelta(t, want*0.5, cost.ActualCost, 1e-12)
		})
	}
	t.Run("Astra catalog ultrafast", func(t *testing.T) {
		p, err := service.GetModelPricing("gpt-6-astra")
		require.NoError(t, err)
		cost := service.computeTokenBreakdown(p, UsageTokens{CacheCreationTokens: 1_000_000, CacheCreation12hTokens: 1_000_000}, 1, "ultrafast", false)
		require.InDelta(t, 150, cost.CacheCreationCost, 1e-12)
	})
	t.Run("explicit free 12h", func(t *testing.T) {
		p := *base
		applyChannelTokenPriceOverrides(&p, &ChannelModelPricing{CacheWritePrice: float64Ptr(1e-6), CacheWrite12hPrice: float64Ptr(0)})
		cost := service.computeTokenBreakdown(&p, tokens, 1, "", false)
		require.InDelta(t, 9e-6, cost.CacheCreationCost, 1e-12)
	})
	t.Run("explicit free 30m with fast 12h", func(t *testing.T) {
		p := *base
		applyChannelTokenPriceOverrides(&p, &ChannelModelPricing{InputPrice: float64Ptr(0), CacheWritePrice: float64Ptr(0), CacheWrite12hPrice: float64Ptr(2e-6)})
		cost := service.computeTokenBreakdown(&p, tokens, 1, "fast", false)
		require.InDelta(t, 2390*4e-6, cost.CacheCreationCost, 1e-12)
	})
	t.Run("missing TTL does not infer a 12h write", func(t *testing.T) {
		p := *base
		applyChannelTokenPriceOverrides(&p, &ChannelModelPricing{CacheWritePrice: float64Ptr(1e-6), CacheWrite12hPrice: float64Ptr(2e-6)})
		cost := service.computeTokenBreakdown(&p, UsageTokens{CacheCreationTokens: 2399}, 1, "", false)
		require.InDelta(t, 2399e-6, cost.CacheCreationCost, 1e-12)
	})
	t.Run("interval scales both write rates", func(t *testing.T) {
		p := *base
		applyChannelTokenPriceOverrides(&p, &ChannelModelPricing{CacheWritePrice: float64Ptr(1e-6), CacheWrite12hPrice: float64Ptr(2e-6)})
		iv := intervalToModelPricing(&PricingInterval{CacheWriteMultiplier: float64Ptr(3)}, &p, nil)
		cost := service.computeTokenBreakdown(iv, tokens, 1, "fast", false)
		require.InDelta(t, (9*1e-6+2390*2e-6)*3*2, cost.CacheCreationCost, 1e-12)
		require.InDelta(t, 2e-6, *p.CacheCreation12hPricePerToken, 1e-12, "the shared base price is immutable")
	})
	t.Run("account stats custom TTL prices", func(t *testing.T) {
		cost := calculateStatsCost(&ChannelModelPricing{
			BillingMode: BillingModeToken, CacheWritePrice: float64Ptr(1e-6), CacheWrite12hPrice: float64Ptr(2e-6),
		}, tokens, 1)
		require.NotNil(t, cost)
		require.InDelta(t, 9e-6+2390*2e-6, *cost, 1e-12)
	})
}
