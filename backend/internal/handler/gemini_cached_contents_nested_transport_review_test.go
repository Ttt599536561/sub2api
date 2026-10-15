//go:build unit

package handler

import (
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/repository"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestGeminiCachedContentsCreate_NestedGatewayDoesNotRetryUnknownCreation(t *testing.T) {
	for _, outcome := range []string{"lost response", "unusable success response"} {
		for _, depth := range []int{2, 3} {
			t.Run(fmt.Sprintf("%s/%d gateways", outcome, depth), func(t *testing.T) {
				var created atomic.Int32
				provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					_, _ = io.Copy(io.Discard, r.Body)
					_ = r.Body.Close()
					created.Add(1)
					if outcome == "unusable success response" {
						_, _ = w.Write([]byte(`{}`))
						return
					}
					conn, _, err := w.(http.Hijacker).Hijack()
					if err != nil {
						t.Errorf("hijack: %v", err)
						return
					}
					_ = conn.Close()
				}))
				t.Cleanup(provider.Close)
				targetServer := provider
				cfg := &config.Config{}
				var layers []*cachedBillingEnv
				for layer := 0; layer < depth; layer++ {
					env := newCachedBillingEnv(t, 500, 900, 901, 902)
					if layer > 0 {
						for _, account := range env.accounts {
							account.Credentials["base_url"] = "https://upstream-gateway.example.com"
							account.Credentials[service.GeminiExplicitCacheUpstreamCredentialKey] = true
						}
					}
					target, err := url.Parse(targetServer.URL)
					require.NoError(t, err)
					transport := &cachedCreateLostResponseTransport{client: targetServer.Client(), target: target}
					compat := service.NewGeminiMessagesCompatService(&cachedBillingAccountRepo{accounts: env.accounts}, nil, nil, nil, nil, nil, transport, nil, cfg)
					env.h.geminiCachedContentService = service.NewGeminiCachedContentService(env.repo, compat, cfg)
					layers = append(layers, env)
					targetServer = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
						body, err := io.ReadAll(r.Body)
						if err != nil {
							t.Errorf("read body: %v", err)
							return
						}
						rec := env.serve(r.Method, r.URL.String(), string(body), nil, env.h.GeminiCachedContentsCreate)
						for name, values := range rec.Header() {
							w.Header()[name] = values
						}
						w.WriteHeader(rec.Code)
						_, _ = w.Write(rec.Body.Bytes())
					}))
					t.Cleanup(targetServer.Close)
				}
				rec := layers[len(layers)-1].create()
				require.Equal(t, http.StatusBadGateway, rec.Code, rec.Body.String())
				require.Equal(t, int32(1), created.Load(), "an unknown create must remain non-retryable across supported nested gateways")
				require.Equal(t, "unknown", rec.Header().Get("X-Sub2api-Cached-Content-Outcome"))
				for _, env := range layers {
					require.Empty(t, env.repo.records)
					require.Empty(t, env.usage.totals())
				}
			})
		}
	}
}

type cachedReviewAccountRoutedTransport struct {
	service.HTTPUpstream
	target   map[int64]*url.URL
	firstErr error
}

func (u *cachedReviewAccountRoutedTransport) Do(req *http.Request, proxyURL string, accountID int64, concurrency int) (*http.Response, error) {
	routed := req.Clone(req.Context())
	routed.URL.Scheme = u.target[accountID].Scheme
	routed.URL.Host = u.target[accountID].Host
	response, err := u.HTTPUpstream.Do(routed, proxyURL, accountID, concurrency)
	if u.firstErr == nil && err != nil {
		u.firstErr = err
	}
	return response, err
}

func TestGeminiCachedContentsCreate_ConnectionRefusedStillFailsOver(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	unavailable, err := url.Parse("http://" + listener.Addr().String())
	require.NoError(t, err)
	require.NoError(t, listener.Close())
	var created atomic.Int32
	healthy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		created.Add(1)
		_, _ = w.Write([]byte(cachedBillingCreateBody(time.Now().Add(time.Hour))))
	}))
	defer healthy.Close()
	available, err := url.Parse(healthy.URL)
	require.NoError(t, err)
	cfg := &config.Config{}
	env := newCachedBillingEnv(t, 500, 900, 901)
	env.accounts[900].Priority = 0
	env.accounts[901].Priority = 1
	transport := &cachedReviewAccountRoutedTransport{HTTPUpstream: repository.NewHTTPUpstream(cfg), target: map[int64]*url.URL{900: unavailable, 901: available}}
	compat := service.NewGeminiMessagesCompatService(&cachedBillingAccountRepo{accounts: env.accounts}, nil, nil, nil, nil, nil, transport, nil, cfg)
	env.h.geminiCachedContentService = service.NewGeminiCachedContentService(env.repo, compat, cfg)

	rec := env.create()
	var dialError *net.OpError
	require.ErrorAs(t, transport.firstErr, &dialError)
	require.Equal(t, "dial", dialError.Op)
	t.Logf("confirmed failure before TCP connection: %v", dialError)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.Equal(t, int32(1), created.Load(), "a failed TCP connection cannot have created a resource and should allow the healthy account")
}

func TestGeminiCachedContentsCreate_DialFailureAfterRedirectDoesNotRetry(t *testing.T) {
	for _, status := range []int{http.StatusTemporaryRedirect, http.StatusPermanentRedirect} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			listener, err := net.Listen("tcp", "127.0.0.1:0")
			require.NoError(t, err)
			unavailable := "http://" + listener.Addr().String() + "/redirected-create"
			require.NoError(t, listener.Close())
			var created atomic.Int32
			provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				_, _ = io.Copy(io.Discard, r.Body)
				_ = r.Body.Close()
				created.Add(1)
				http.Redirect(w, r, unavailable, status)
			}))
			defer provider.Close()
			target, err := url.Parse(provider.URL)
			require.NoError(t, err)
			cfg := &config.Config{}
			env := newCachedBillingEnv(t, 500, 900, 901, 902)
			transport := &cachedReviewAccountRoutedTransport{HTTPUpstream: repository.NewHTTPUpstream(cfg), target: map[int64]*url.URL{900: target, 901: target, 902: target}}
			compat := service.NewGeminiMessagesCompatService(&cachedBillingAccountRepo{accounts: env.accounts}, nil, nil, nil, nil, nil, transport, nil, cfg)
			env.h.geminiCachedContentService = service.NewGeminiCachedContentService(env.repo, compat, cfg)

			rec := env.create()
			var dialError *net.OpError
			require.ErrorAs(t, transport.firstErr, &dialError)
			require.Equal(t, "dial", dialError.Op)
			require.Equal(t, http.StatusBadGateway, rec.Code, rec.Body.String())
			require.Equal(t, int32(1), created.Load(), "a later redirect dial failure cannot prove the first POST was not sent")

		})
	}
}
