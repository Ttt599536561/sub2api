package handler

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

// OAuth uses a fixed production URL. Redirect only that host to the fixture's
// loopback server and reject every other non-loopback destination.
type retirementWSLocalTransport struct {
	base     http.RoundTripper
	upstream *url.URL
}

func (r retirementWSLocalTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	local := req.Clone(req.Context())
	if req.URL.Hostname() == "chatgpt.com" {
		copied := *req.URL
		copied.Scheme, copied.Host = r.upstream.Scheme, r.upstream.Host
		local.URL = &copied
		local.Host = r.upstream.Host
	} else if req.URL.Hostname() != "127.0.0.1" && req.URL.Hostname() != "localhost" {
		return nil, fmt.Errorf("test forbids external request to %s", req.URL.Host)
	}
	return r.base.RoundTrip(local)
}

func configureRetirementWSAccount(t *testing.T, accountType, mode string) func(*service.Account, *config.Config) {
	t.Helper()
	return func(account *service.Account, cfg *config.Config) {
		account.Type = accountType
		account.Credentials["access_token"] = "local-test-access-token"
		account.Extra["openai_oauth_responses_websockets_v2_enabled"] = true
		account.Extra["openai_oauth_responses_websockets_v2_mode"] = mode
		cfg.Gateway.OpenAIWS.OAuthEnabled = true
		upstream, err := url.Parse(account.GetCredential("base_url"))
		require.NoError(t, err)
		original := http.DefaultClient
		http.DefaultClient = &http.Client{Transport: retirementWSLocalTransport{base: http.DefaultTransport, upstream: upstream}}
		t.Cleanup(func() { http.DefaultClient = original })
	}
}

func TestOpenAIResponsesWebSocket_RetiredChannelTargetOnLaterTurn(t *testing.T) {
	for _, mode := range []string{service.OpenAIWSIngressModePassthrough, service.OpenAIWSIngressModeDedicated, service.OpenAIWSIngressModeCtxPool} {
		for _, accountType := range []string{service.AccountTypeOAuth, service.AccountTypeSetupToken, service.AccountTypeAPIKey} {
			t.Run(mode+"/"+accountType, func(t *testing.T) {
				rejected := accountType != service.AccountTypeAPIKey
				got := runOpenAIResponsesWebSocketUsageLogCase(t, openAIResponsesWSUsageLogCase{
					firstPayload:            `{"type":"response.create","model":"gpt-6.1-sol","input":"hi"}`,
					secondPayload:           `{"type":"response.create","model":"public-retired","input":"again"}`,
					ingressMode:             mode,
					channelMapping:          map[string]string{"public-retired": "gpt-5.5"},
					configureAccount:        configureRetirementWSAccount(t, accountType, mode),
					secondTurnCloseExpected: rejected,
					closeReason:             "retired",
				})
				if !rejected {
					require.Len(t, got.upstreamPayloads, 2)
					require.Equal(t, "gpt-5.5", gjson.GetBytes(got.upstreamPayloads[1], "model").String())
				}
			})
		}
	}
}

func TestOpenAIResponsesWebSocket_RetirementUsesActualMappedTarget(t *testing.T) {
	for _, mode := range []string{service.OpenAIWSIngressModePassthrough, service.OpenAIWSIngressModeCtxPool} {
		for _, accountType := range []string{service.AccountTypeOAuth, service.AccountTypeSetupToken, service.AccountTypeAPIKey} {
			t.Run(mode+"/"+accountType, func(t *testing.T) {
				rejected := mode == service.OpenAIWSIngressModePassthrough && accountType != service.AccountTypeAPIKey
				got := runOpenAIResponsesWebSocketUsageLogCase(t, openAIResponsesWSUsageLogCase{
					firstPayload:            `{"type":"response.create","model":"gpt-5.5","input":"hi"}`,
					secondPayload:           `{"type":"response.create","model":"gpt-5.5","input":"again"}`,
					ingressMode:             mode,
					accountModelMapping:     map[string]any{"gpt-5.5": "gpt-6.1-sol"},
					configureAccount:        configureRetirementWSAccount(t, accountType, mode),
					firstFrameCloseExpected: rejected,
					closeReason:             "retired",
				})
				if !rejected {
					require.Len(t, got.upstreamPayloads, 2)
					target := "gpt-6.1-sol"
					if mode == service.OpenAIWSIngressModePassthrough {
						target = "gpt-5.5"
					}
					for _, payload := range got.upstreamPayloads {
						require.Equal(t, target, gjson.GetBytes(payload, "model").String())
					}
				}
			})
		}
	}
}

func TestOpenAIResponsesWebSocket_RetiredChannelTargetAfterRefresh(t *testing.T) {
	for _, mode := range []string{service.OpenAIWSIngressModePassthrough, service.OpenAIWSIngressModeCtxPool} {
		for _, accountType := range []string{service.AccountTypeOAuth, service.AccountTypeSetupToken} {
			t.Run(mode+"/"+accountType, func(t *testing.T) {
				runOpenAIResponsesWebSocketUsageLogCase(t, openAIResponsesWSUsageLogCase{
					firstPayload:     `{"type":"response.create","model":"public-model","input":"hi"}`,
					secondPayload:    `{"type":"response.create","input":"again"}`,
					ingressMode:      mode,
					channelMapping:   map[string]string{"public-model": "gpt-6.1-sol"},
					configureAccount: configureRetirementWSAccount(t, accountType, mode),
					afterFirstUpstreamRequest: func(channels *service.ChannelService) error {
						_, err := channels.Update(context.Background(), 7701, &service.UpdateChannelInput{
							ModelMapping: map[string]map[string]string{service.PlatformOpenAI: {"public-model": "gpt-5.5"}},
						})
						return err
					},
					secondTurnCloseExpected: true,
					closeReason:             "retired",
				})
			})
		}
	}
}

func TestShouldReportOpenAIWSProxyAccountFailure_RetiredModelIsLocal(t *testing.T) {
	require.False(t, shouldReportOpenAIWSProxyAccountFailure(fmt.Errorf("forward websocket: %w", service.ErrOpenAIRetiredSubscriptionModel)))
}

func TestOpenAIResponsesWebSocket_RetiredPassthroughSessionUpdate(t *testing.T) {
	for _, accountType := range []string{service.AccountTypeOAuth, service.AccountTypeSetupToken} {
		t.Run(accountType, func(t *testing.T) {
			mode := service.OpenAIWSIngressModePassthrough
			runOpenAIResponsesWebSocketUsageLogCase(t, openAIResponsesWSUsageLogCase{
				firstPayload:          `{"type":"response.create","model":"gpt-6.1-sol","input":"hi"}`,
				midPayload:            `{"type":"session.update","session":{"model":"gpt-5.5"}}`,
				ingressMode:           mode,
				configureAccount:      configureRetirementWSAccount(t, accountType, mode),
				midFrameCloseExpected: true,
				closeReason:           "retired",
			})
		})
	}
}
