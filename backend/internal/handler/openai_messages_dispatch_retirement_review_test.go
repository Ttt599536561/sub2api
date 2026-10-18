package handler

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

// Messages account mappings take precedence over the group dispatch fallback.
// Selection and forwarding must inspect the same final model for retirement.
func TestMessagesDispatchRetirementUsesActualAccountMapping(t *testing.T) {
	for _, channelMapped := range []bool{false, true} {
		for _, passthrough := range []bool{false, true} {
			for _, tc := range []struct {
				name, accountTarget, defaultTarget string
				wantStatus                         int
			}{
				{"account-target-retired", "gpt-5.5", "gpt-6.1-sol", http.StatusNotFound},
				{"account-target-live", "gpt-6.1-sol", "gpt-5.5", http.StatusOK},
				{"live-control", "gpt-6.1-sol", "gpt-6.1-sol", http.StatusOK},
			} {
				t.Run(fmt.Sprintf("%s/channel=%t/http_passthrough=%t", tc.name, channelMapped, passthrough), func(t *testing.T) {
					gin.SetMode(gin.TestMode)
					requests := make(chan []byte, 8)
					server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
						body, err := io.ReadAll(r.Body)
						if err != nil {
							t.Error(err)
							return
						}
						requests <- body
						w.Header().Set("Content-Type", "text/event-stream")
						_, _ = fmt.Fprintf(w, "data: {\"type\":\"response.completed\",\"response\":{\"id\":\"resp_dispatch_review\",\"model\":%q,\"status\":\"completed\",\"output\":[{\"type\":\"message\",\"role\":\"assistant\",\"content\":[{\"type\":\"output_text\",\"text\":\"ok\"}]}],\"usage\":{\"input_tokens\":1,\"output_tokens\":1}}}\n\n", gjson.GetBytes(body, "model").String())
					}))
					defer server.Close()
					upstream, err := url.Parse(server.URL)
					require.NoError(t, err)
					original := http.DefaultClient
					http.DefaultClient = &http.Client{Transport: retirementWSLocalTransport{base: http.DefaultTransport, upstream: upstream}}
					defer func() { http.DefaultClient = original }()

					groupID := int64(4299)
					requestedModel := "claude-opus-4-6"
					routingModel := requestedModel
					var channels *service.ChannelService
					if channelMapped {
						routingModel = "channel-opus"
						channels = service.NewChannelService(&openAIWSUsageHandlerChannelRepoStub{
							channels:       []service.Channel{{ID: 7799, Status: service.StatusActive, GroupIDs: []int64{groupID}, ModelMapping: map[string]map[string]string{service.PlatformOpenAI: {requestedModel: routingModel}}}},
							groupPlatforms: map[int64]string{groupID: service.PlatformOpenAI},
						}, nil, nil, nil, nil)
					}
					account := service.Account{ID: 9991, Platform: service.PlatformOpenAI, Type: service.AccountTypeOAuth, Status: service.StatusActive, Schedulable: true, Concurrency: 1,
						Credentials: map[string]any{"access_token": "local-test-token", "model_mapping": map[string]any{routingModel: tc.accountTarget, "gpt-6.1-sol": tc.defaultTarget}},
						Extra:       map[string]any{"openai_passthrough": passthrough}}
					repo := &retirementRoutingAccountRepo{openAIWSUsageHandlerAccountRepoStub{account: account}}
					cfg := &config.Config{RunMode: config.RunModeSimple}
					billing := service.NewBillingCacheService(nil, nil, nil, nil, nil, nil, cfg, nil)
					defer billing.Stop()
					gateway := service.NewOpenAIGatewayService(repo, nil, nil, nil, nil, nil, nil, cfg, nil, nil,
						service.NewBillingService(cfg, nil), nil, billing, &compositeWSHTTPUpstream{}, &service.DeferredService{}, nil, nil, nil, channels, nil, nil, nil)
					cache := &concurrencyCacheMock{
						acquireUserSlotFn:    func(context.Context, int64, int, string) (bool, error) { return true, nil },
						acquireAccountSlotFn: func(context.Context, int64, int, string) (bool, error) { return true, nil },
					}
					handler := NewOpenAIGatewayHandler(gateway, service.NewConcurrencyService(cache), billing, &service.APIKeyService{}, nil, nil, nil, nil, cfg)
					apiKey := &service.APIKey{ID: 1899, GroupID: &groupID, Group: &service.Group{ID: groupID, Platform: service.PlatformOpenAI, AllowMessagesDispatch: true,
						MessagesDispatchModelConfig: service.OpenAIMessagesDispatchModelConfig{OpusMappedModel: "gpt-6.1-sol"}}, User: &service.User{ID: 1799, Status: service.StatusActive}}
					recorder := httptest.NewRecorder()
					c, _ := gin.CreateTestContext(recorder)
					body := fmt.Sprintf(`{"model":%q,"max_tokens":32,"messages":[{"role":"user","content":"hi"}],"stream":false}`, requestedModel)
					c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(body))
					c.Set(string(middleware.ContextKeyAPIKey), apiKey)
					c.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: 1799, Concurrency: 1})
					handler.Messages(c)
					require.Equal(t, tc.wantStatus, recorder.Code, recorder.Body.String())
					if tc.wantStatus != http.StatusOK {
						require.Empty(t, requests, "retired final model must never reach the upstream")
						return
					}
					require.Len(t, requests, 1)
					require.Equal(t, "gpt-6.1-sol", gjson.GetBytes(<-requests, "model").String())
				})
			}
		}
	}
}
