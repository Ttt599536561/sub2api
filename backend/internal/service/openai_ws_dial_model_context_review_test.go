package service

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	coderws "github.com/coder/websocket"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type reviewWS502Dialer struct{}

func (reviewWS502Dialer) Dial(context.Context, string, http.Header, string) (openAIWSClientConn, int, http.Header, error) {
	return nil, http.StatusBadGateway, http.Header{"X-Request-Id": []string{"review-ws-502"}}, errors.New("local simulated upstream handshake failure")
}

func TestNativeWSDialFailureCooldownUsesActualModel(t *testing.T) {
	for _, mode := range []string{OpenAIWSIngressModeCtxPool, OpenAIWSIngressModeDedicated} {
		for _, httpPassthrough := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/http_passthrough=%t", mode, httpPassthrough), func(t *testing.T) {
				gin.SetMode(gin.TestMode)
				cfg := &config.Config{}
				cfg.Security.URLAllowlist.Enabled = false
				cfg.Security.URLAllowlist.AllowInsecureHTTP = true
				cfg.Gateway.OpenAIWS.Enabled = true
				cfg.Gateway.OpenAIWS.APIKeyEnabled = true
				cfg.Gateway.OpenAIWS.ResponsesWebsocketsV2 = true
				cfg.Gateway.OpenAIWS.ModeRouterV2Enabled = true
				cfg.Gateway.OpenAIWS.MaxConnsPerAccount = 1
				cfg.Gateway.OpenAIWS.MinIdlePerAccount = 0
				cfg.Gateway.OpenAIWS.MaxIdlePerAccount = 1
				cfg.Gateway.OpenAIWS.QueueLimitPerConn = 8
				cfg.Gateway.OpenAIWS.DialTimeoutSeconds = 3
				cfg.Gateway.OpenAIWS.ReadTimeoutSeconds = 3
				cfg.Gateway.OpenAIWS.WriteTimeoutSeconds = 3
				pool := newOpenAIWSConnPool(cfg)
				pool.setClientDialerForTest(reviewWS502Dialer{})
				defer pool.Close()
				svc := &OpenAIGatewayService{
					cfg: cfg, httpUpstream: &httpUpstreamRecorder{}, cache: &stubGatewayCache{},
					openaiWSResolver: NewOpenAIWSProtocolResolver(cfg), toolCorrector: NewCodexToolCorrector(), openaiWSPool: pool,
					rateLimitService: NewRateLimitService(transientCooldownAccountRepo{}, nil, cfg, nil, nil),
				}
				account := &Account{ID: 8891, Platform: PlatformOpenAI, Type: AccountTypeAPIKey,
					Status: StatusActive, Schedulable: true, Concurrency: 1,
					Credentials: map[string]any{"api_key": "local-test-key", "model_mapping": map[string]any{"public-alias": "gpt-6.1-sol"}},
					Extra:       map[string]any{"openai_passthrough": httpPassthrough, "openai_apikey_responses_websockets_v2_mode": mode, "responses_websockets_v2_enabled": true},
				}
				serverErrs := make(chan error, 2)
				wsServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					conn, err := coderws.Accept(w, r, nil)
					if err != nil {
						serverErrs <- err
						return
					}
					defer func() { _ = conn.CloseNow() }()
					readCtx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
					_, firstMessage, err := conn.Read(readCtx)
					cancel()
					if err != nil {
						serverErrs <- err
						return
					}
					ginCtx, _ := gin.CreateTestContext(httptest.NewRecorder())
					ginCtx.Request = r.Clone(r.Context())
					ginCtx.Request.Header.Set("User-Agent", "unit-test-agent/1.0")
					serverErrs <- svc.ProxyResponsesWebSocketFromClient(r.Context(), ginCtx, conn, account, "local-test-key", firstMessage, nil)
				}))
				defer wsServer.Close()
				for range 2 {
					ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
					client, _, err := coderws.Dial(ctx, "ws"+strings.TrimPrefix(wsServer.URL, "http"), nil)
					require.NoError(t, err)
					require.NoError(t, client.Write(ctx, coderws.MessageText, []byte(`{"type":"response.create","model":"public-alias","input":"hi"}`)))
					select {
					case proxyErr := <-serverErrs:
						var dialErr *openAIWSDialError
						require.ErrorAs(t, proxyErr, &dialErr, "exercise the actual upstream acquire error path")
						require.Equal(t, http.StatusBadGateway, dialErr.StatusCode)
					case <-ctx.Done():
						t.Fatal("proxy did not complete the simulated handshake failure")
					}
					_ = client.CloseNow()
					cancel()
				}
				state := svc.getOpenAIAccountModelTransientState()
				now := time.Now()
				require.True(t, state.isBlocked(account.ID, openAIAccountModelTransientModel("gpt-6.1-sol"), now), "native WS must cool down its actual mapped model")
				require.False(t, state.isBlocked(account.ID, openAIAccountModelTransientModel("public-alias"), now), "the unmapped public name was never sent upstream")
			})
		}
	}
}
