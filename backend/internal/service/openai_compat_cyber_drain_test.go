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
	"testing/synctest"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/ctxkey"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

const compatCyberBareError = `{"type":"error","error":{"code":"cyber_policy","message":"first audit evidence"}}`
const compatCyberFailedUsage = `{"type":"response.failed","response":{"id":"resp_cyber_drain","model":"gpt-6-astra","service_tier":"ultrafast","status":"failed","error":{"code":"cyber_policy","message":"terminal evidence"},"usage":{"input_tokens":1200,"output_tokens":300,"input_tokens_details":{"cached_tokens":200}}}}`

func TestOpenAICompatCyberBareErrorDrainsTerminalBilling(t *testing.T) {
	testOpenAICompatCyberBareErrorDrainsTerminalBilling(t, true)
}

func TestOpenAICompatCyberBufferedBareErrorDrainsTerminalBilling(t *testing.T) {
	testOpenAICompatCyberBareErrorDrainsTerminalBilling(t, false)
}

func testOpenAICompatCyberBareErrorDrainsTerminalBilling(t *testing.T, stream bool) {
	for _, entry := range []string{"chat", "messages"} {
		for _, async := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/async=%t", entry, async), func(t *testing.T) {
				recorder := httptest.NewRecorder()
				group := &Group{ID: 881, Platform: PlatformOpenAI, RateMultiplier: 1, ForceOpenAIUltrafast: true, Status: StatusActive, Hydrated: true}
				key := &APIKey{ID: 882, User: &User{ID: 883}, GroupID: &group.ID, Group: group}
				ctx := context.WithValue(context.Background(), ctxkey.Group, group)
				body := []byte(fmt.Sprintf(`{"model":"gpt-6-astra","stream":%t,"max_tokens":400,"messages":[{"role":"user","content":"hi"}]}`, stream))
				c, _ := gin.CreateTestContext(recorder)
				c.Request = httptest.NewRequest(http.MethodPost, "/v1/"+entry, bytes.NewReader(body)).WithContext(ctx)
				logs := &openAIRecordUsageLogRepoStub{inserted: true}
				users := &openAIRecordUsageUserRepoStub{}
				svc := newOpenAIRecordUsageServiceForTest(logs, users, &openAIRecordUsageSubRepoStub{}, nil)
				svc.resolver = NewModelPricingResolver(nil, svc.billingService)
				if async {
					if stream {
						svc.cfg.Gateway.StreamKeepaliveInterval = 1
					} else {
						svc.cfg.Gateway.StreamDataIntervalTimeout = 1
					}
				}
				sse := "data: " + compatCyberBareError + "\n\ndata: " + compatCyberBareError + "\n\ndata: " + compatCyberFailedUsage + "\n\ndata: " + strings.ReplaceAll(compatCyberFailedUsage, "1200", "9999") + "\n\n"
				upstream := &httpUpstreamRecorder{resp: &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": []string{"text/event-stream"}}, Body: io.NopCloser(strings.NewReader(sse))}}
				svc.httpUpstream = upstream
				account := compatCyberOAuthAccount()
				var err error
				if entry == "chat" {
					_, err = svc.ForwardAsChatCompletions(ctx, c, account, body, "", "")
				} else {
					_, err = svc.ForwardAsAnthropic(ctx, c, account, body, "", "")
				}
				require.Error(t, err)
				require.Len(t, upstream.requests, 1)
				mark := GetOpsCyberPolicy(c)
				require.NotNil(t, mark)
				require.Equal(t, "first audit evidence", mark.Message)
				require.Equal(t, 1200, mark.UpstreamInTok)
				require.Equal(t, 300, mark.UpstreamOutTok)
				require.Equal(t, 200, mark.Billing.Usage.CacheReadInputTokens)
				require.Equal(t, "ultrafast", mark.Billing.UpstreamResponseServiceTier)
				require.Equal(t, "gpt-6-astra", mark.Billing.UpstreamResponseModel)
				require.Equal(t, 1, strings.Count(recorder.Body.String(), "first audit evidence"))
				require.NotContains(t, recorder.Body.String(), "terminal evidence")
				if stream && entry == "chat" {
					require.Equal(t, 1, strings.Count(recorder.Body.String(), "[DONE]"))
				} else if stream {
					require.Equal(t, 1, strings.Count(recorder.Body.String(), "event: error"))
				}
				svc.RecordCyberPolicyUsageLog(ctx, CyberPolicyUsageInput{APIKey: key, Account: account, RequestID: "cyber_drain_" + entry, Model: "gpt-6-astra", InputTokens: mark.UpstreamInTok, OutputTokens: mark.UpstreamOutTok, Billing: mark.Billing})
				require.Equal(t, 1, logs.calls)
				standard, err := svc.billingService.CalculateTokenCostForRequest(TokenCostRequest{Ctx: ctx, Model: "gpt-6-astra", Group: group, Tokens: UsageTokens{InputTokens: 1000, OutputTokens: 300, CacheReadTokens: 200}, RateMultiplier: 1, Resolver: svc.resolver})
				require.NoError(t, err)
				require.InDelta(t, standard.ActualCost*8, users.lastAmount, 1e-12)
			})
		}
	}
}

func TestOpenAICompatCyberBareErrorDrainTermination(t *testing.T) {
	testOpenAICompatCyberBareErrorDrainTermination(t, true)
}

func TestOpenAICompatCyberBufferedBareErrorDrainTermination(t *testing.T) {
	testOpenAICompatCyberBareErrorDrainTermination(t, false)
}

func testOpenAICompatCyberBareErrorDrainTermination(t *testing.T, stream bool) {
	for _, entry := range []string{"chat", "messages"} {
		for _, async := range []bool{false, true} {
			for _, ending := range []string{"terminal", "completed", "other_failure", "eof", "done", "idle", "read_error", "hang", "repeated_errors"} {
				t.Run(fmt.Sprintf("%s/async=%t/%s", entry, async, ending), func(t *testing.T) {
					synctest.Test(t, func(t *testing.T) {
						recorder := httptest.NewRecorder()
						c, _ := gin.CreateTestContext(recorder)
						c.Request = httptest.NewRequest(http.MethodPost, "/v1/"+entry, nil)
						svc := &OpenAIGatewayService{cfg: &config.Config{}}
						if async {
							if stream {
								svc.cfg.Gateway.StreamKeepaliveInterval = 1
							} else {
								svc.cfg.Gateway.StreamDataIntervalTimeout = 1
							}
						}
						reader, writer := io.Pipe()
						writerDone := make(chan struct{})
						defer func() { _ = reader.Close(); _ = writer.Close(); <-writerDone }()
						resp := &http.Response{StatusCode: http.StatusOK, Header: http.Header{}, Body: reader}
						go func() {
							defer close(writerDone)
							_, _ = fmt.Fprintf(writer, "data: %s\n\n", compatCyberBareError)
							time.Sleep(250 * time.Millisecond)
							switch ending {
							case "terminal":
								_, _ = fmt.Fprintf(writer, "data: %s\n\n", compatCyberFailedUsage)
							case "completed":
								payload := strings.ReplaceAll(compatCyberFailedUsage, `"failed"`, `"completed"`)
								payload = strings.ReplaceAll(payload, "response.failed", "response.completed")
								payload = strings.ReplaceAll(payload, `,"error":{"code":"cyber_policy","message":"terminal evidence"}`, "")
								_, _ = fmt.Fprintf(writer, "data: %s\n\n", payload)
							case "other_failure":
								payload := strings.ReplaceAll(compatCyberFailedUsage, "cyber_policy", "server_error")
								_, _ = fmt.Fprintf(writer, "data: %s\n\n", payload)
							case "read_error":
								_ = writer.CloseWithError(io.ErrUnexpectedEOF)
							case "eof":
								_ = writer.Close()
							case "done":
								_, _ = fmt.Fprint(writer, "data: [DONE]\n\n")
							case "hang", "repeated_errors":
								for {
									frame := ": still-open\n\n"
									if ending == "repeated_errors" {
										frame = "data: " + compatCyberBareError + "\n\n"
									}
									if _, err := fmt.Fprint(writer, frame); err != nil {
										return
									}
									time.Sleep(100 * time.Millisecond)
								}
							}
						}()
						started := time.Now()
						var err error
						if !stream && entry == "chat" {
							_, err = svc.handleChatBufferedStreamingResponse(resp, c, compatCyberOAuthAccount(), "gpt-6-astra", "gpt-6-astra", "gpt-6-astra", started)
						} else if !stream {
							_, err = svc.handleAnthropicBufferedStreamingResponse(resp, c, compatCyberOAuthAccount(), "gpt-6-astra", "gpt-6-astra", "gpt-6-astra", started)
						} else if entry == "chat" {
							_, err = svc.handleChatStreamingResponse(resp, c, compatCyberOAuthAccount(), "gpt-6-astra", "gpt-6-astra", "gpt-6-astra", started, 1)
						} else {
							_, err = svc.handleAnthropicStreamingResponse(resp, c, compatCyberOAuthAccount(), "gpt-6-astra", "gpt-6-astra", "gpt-6-astra", started)
						}
						require.GreaterOrEqual(t, time.Since(started), 250*time.Millisecond, "must wait for terminal billing instead of returning at the bare error")
						require.LessOrEqual(t, time.Since(started), 3*time.Second, "error billing drain must have a total bound even with upstream heartbeats")
						mark := GetOpsCyberPolicy(c)
						require.NotNil(t, mark)
						require.Equal(t, "first audit evidence", mark.Message)
						require.Equal(t, 1, strings.Count(recorder.Body.String(), "first audit evidence"))
						var failover *UpstreamFailoverError
						require.NotErrorAs(t, err, &failover)
						if ending == "terminal" || ending == "completed" || ending == "other_failure" {
							require.Equal(t, 1200, mark.UpstreamInTok)
							require.Equal(t, "ultrafast", mark.Billing.UpstreamResponseServiceTier)
						} else {
							require.Zero(t, mark.UpstreamInTok)
						}
					})
				})
			}
		}
	}
}

func TestOpenAICompatCyberTerminalRequiresPriorEvidence(t *testing.T) {
	for _, eventType := range []string{"response.failed", "response.completed"} {
		t.Run(eventType, func(t *testing.T) {
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			payload := []byte(fmt.Sprintf(`{"type":%q,"response":{"model":"gpt-6-astra","service_tier":"default","error":{"code":"server_error"},"usage":{"input_tokens":1200,"output_tokens":300,"input_tokens_details":{"cached_tokens":200}}}}`, eventType))
			require.False(t, markOpenAICyberPolicyEvent(c, payload, http.StatusOK, nil))
			require.Nil(t, GetOpsCyberPolicy(c), "a non-cyber terminal must never create cyber evidence")
			require.True(t, markOpenAICyberPolicyEvent(c, []byte(compatCyberBareError), http.StatusOK, nil))
			require.True(t, markOpenAICyberPolicyEvent(c, payload, http.StatusOK, nil))
			mark := GetOpsCyberPolicy(c)
			require.Equal(t, compatCyberBareError, mark.Body)
			require.Equal(t, "first audit evidence", mark.Message)
			require.Equal(t, "cyber_policy", mark.Code)
			require.Equal(t, 1200, mark.UpstreamInTok)
			require.Equal(t, 300, mark.UpstreamOutTok)
			require.Equal(t, 200, mark.Billing.Usage.CacheReadInputTokens)
			require.Equal(t, "gpt-6-astra", mark.Billing.UpstreamResponseModel)
			require.Equal(t, "default", mark.Billing.UpstreamResponseServiceTier)
		})
	}
}
