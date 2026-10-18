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

type retirementRoutingAccountRepo struct {
	openAIWSUsageHandlerAccountRepoStub
}

func (r *retirementRoutingAccountRepo) ListModelAvailabilityCandidates(context.Context, *int64, []string, bool) ([]service.Account, error) {
	return []service.Account{r.account}, nil
}

func TestOpenAIRetirementHTTPChannelReplacement(t *testing.T) {
	for _, source := range []string{"channel", "account"} {
		for _, endpoint := range []string{"messages", "responses", "chat/completions"} {
			for _, passthrough := range []bool{false, true} {
				t.Run(fmt.Sprintf("%s/%s/http_passthrough=%t", source, endpoint, passthrough), func(t *testing.T) {
					gin.SetMode(gin.TestMode)
					requests := make(chan []byte, 1)
					server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
						body, err := io.ReadAll(r.Body)
						if err != nil {
							t.Error(err)
							return
						}
						requests <- body
						w.Header().Set("Content-Type", "text/event-stream")
						_, _ = fmt.Fprintf(w, "data: {\"type\":\"response.completed\",\"response\":{\"id\":\"resp_replacement\",\"model\":\"gpt-6.1-sol\",\"status\":\"completed\",\"output\":[{\"type\":\"message\",\"role\":\"assistant\",\"content\":[{\"type\":\"output_text\",\"text\":\"ok\"}]}],\"usage\":{\"input_tokens\":1,\"output_tokens\":1}}}\n\n")
					}))
					defer server.Close()
					upstream, err := url.Parse(server.URL)
					require.NoError(t, err)
					original := http.DefaultClient
					http.DefaultClient = &http.Client{Transport: retirementWSLocalTransport{base: http.DefaultTransport, upstream: upstream}}
					defer func() { http.DefaultClient = original }()
					groupID := int64(4201)
					account := service.Account{ID: 9901, Platform: service.PlatformOpenAI, Type: service.AccountTypeOAuth, Status: service.StatusActive, Schedulable: true, Concurrency: 1,
						Credentials: map[string]any{"access_token": "local-test-token"}, Extra: map[string]any{"openai_passthrough": passthrough}}
					if source == "account" {
						account.Credentials["model_mapping"] = map[string]any{"gpt-5.5": "gpt-6.1-sol"}
					}
					repo := &retirementRoutingAccountRepo{openAIWSUsageHandlerAccountRepoStub{account: account}}
					channels := service.NewChannelService(&openAIWSUsageHandlerChannelRepoStub{
						channels:       []service.Channel{{ID: 7701, Status: service.StatusActive, GroupIDs: []int64{groupID}, ModelMapping: map[string]map[string]string{service.PlatformOpenAI: {"gpt-5.5": "gpt-6.1-sol"}}}},
						groupPlatforms: map[int64]string{groupID: service.PlatformOpenAI},
					}, nil, nil, nil, nil)
					if source == "account" {
						channels = nil
					}
					cfg := &config.Config{RunMode: config.RunModeSimple}
					billing := service.NewBillingCacheService(nil, nil, nil, nil, nil, nil, cfg, nil)
					defer billing.Stop()
					gateway := service.NewOpenAIGatewayService(repo, nil, nil, nil, nil, nil, nil, cfg, nil, nil,
						service.NewBillingService(cfg, nil), nil, billing, &compositeWSHTTPUpstream{}, &service.DeferredService{}, nil, nil, nil, channels, nil, nil, nil)
					cache := &concurrencyCacheMock{
						acquireUserSlotFn:    func(context.Context, int64, int, string) (bool, error) { return true, nil },
						acquireAccountSlotFn: func(context.Context, int64, int, string) (bool, error) { return true, nil },
					}
					h := NewOpenAIGatewayHandler(gateway, service.NewConcurrencyService(cache), billing, &service.APIKeyService{}, nil, nil, nil, nil, cfg)
					apiKey := &service.APIKey{ID: 1801, GroupID: &groupID, Group: &service.Group{ID: groupID, Platform: service.PlatformOpenAI, AllowMessagesDispatch: true}, User: &service.User{ID: 1701, Status: service.StatusActive}}
					recorder := httptest.NewRecorder()
					c, _ := gin.CreateTestContext(recorder)
					body := `{"model":"gpt-5.5","input":"hi","stream":false}`
					if endpoint != "responses" {
						body = `{"model":"gpt-5.5","max_tokens":32,"messages":[{"role":"user","content":"hi"}],"stream":false}`
					}
					c.Request = httptest.NewRequest(http.MethodPost, "/v1/"+endpoint, strings.NewReader(body))
					c.Set(string(middleware.ContextKeyAPIKey), apiKey)
					c.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: 1701, Concurrency: 1})
					switch endpoint {
					case "messages":
						h.Messages(c)
					case "chat/completions":
						h.ChatCompletions(c)
					default:
						h.Responses(c)
					}
					if source == "account" && endpoint == "responses" && passthrough {
						require.Equal(t, http.StatusNotFound, recorder.Code, recorder.Body.String())
						require.Empty(t, requests)
						return
					}
					require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
					require.Len(t, requests, 1, "request must reach local upstream")
					require.Equal(t, "gpt-6.1-sol", gjson.GetBytes(<-requests, "model").String())
				})
			}
		}
	}

}

func TestOpenAIResponsesWebSocket_HTTPPassthroughDoesNotDisableNativeMapping(t *testing.T) {
	for _, mode := range []string{service.OpenAIWSIngressModeCtxPool, service.OpenAIWSIngressModeDedicated} {
		for _, accountType := range []string{service.AccountTypeOAuth, service.AccountTypeSetupToken, service.AccountTypeAPIKey} {
			for _, passthrough := range []bool{false, true} {
				t.Run(fmt.Sprintf("%s/%s/http_passthrough=%t", mode, accountType, passthrough), func(t *testing.T) {
					configure := configureRetirementWSAccount(t, accountType, mode)
					got := runOpenAIResponsesWebSocketUsageLogCase(t, openAIResponsesWSUsageLogCase{
						firstPayload:  `{"type":"response.create","model":"gpt-5.5","input":"hi"}`,
						secondPayload: `{"type":"response.create","model":"gpt-5.5","input":"again"}`,
						ingressMode:   mode, accountModelMapping: map[string]any{"gpt-5.5": "gpt-6.1-sol"},
						configureAccount: func(account *service.Account, cfg *config.Config) {
							configure(account, cfg)
							account.Extra["openai_passthrough"] = passthrough
						},
					})
					require.Len(t, got.upstreamPayloads, 2)
					for _, payload := range got.upstreamPayloads {
						require.Equal(t, "gpt-6.1-sol", gjson.GetBytes(payload, "model").String())
					}
				})
			}
		}
	}
}
