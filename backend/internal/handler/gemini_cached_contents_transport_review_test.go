//go:build unit

package handler

import (
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

type cachedCreateLostResponseTransport struct {
	service.HTTPUpstream
	client *http.Client
	target *url.URL
}

func (u *cachedCreateLostResponseTransport) Do(req *http.Request, _ string, _ int64, _ int) (*http.Response, error) {
	routed := req.Clone(req.Context())
	routed.URL.Scheme = u.target.Scheme
	routed.URL.Host = u.target.Host
	return u.client.Do(routed)
}

func TestGeminiCachedContentsCreate_LostResponseDoesNotCreateMoreCaches(t *testing.T) {
	var created atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		_ = r.Body.Close()
		// The provider accepted the body and persisted the cache, but its
		// response is lost before any status or resource name reaches us.
		created.Add(1)
		conn, _, err := w.(http.Hijacker).Hijack()
		if err != nil {
			t.Errorf("hijack: %v", err)
			return
		}
		_ = conn.Close()
	}))
	defer upstream.Close()
	target, err := url.Parse(upstream.URL)
	require.NoError(t, err)

	env := newCachedBillingEnv(t, 500, 900, 901, 902)
	cfg := &config.Config{}
	transport := &cachedCreateLostResponseTransport{client: upstream.Client(), target: target}
	compat := service.NewGeminiMessagesCompatService(&cachedBillingAccountRepo{accounts: env.accounts}, nil, nil, nil, nil, nil, transport, nil, cfg)
	env.h.geminiCachedContentService = service.NewGeminiCachedContentService(env.repo, compat, cfg)

	rec := env.create()
	require.Equal(t, http.StatusBadGateway, rec.Code, rec.Body.String())
	require.Equal(t, int32(1), created.Load(), "an uncertain POST must not create additional orphan caches on failover accounts")
	require.Empty(t, env.repo.records)
	require.Empty(t, env.usage.totals())
}

func TestGeminiCachedContentsCreate_PreSendFailureStillFailsOver(t *testing.T) {
	env := newCachedBillingEnv(t, 500, 900, 901)
	env.accounts[900].Priority = 0
	env.accounts[900].Credentials["base_url"] = "http://insecure.example.com"
	env.accounts[900].Credentials[service.GeminiExplicitCacheUpstreamCredentialKey] = true
	env.accounts[901].Priority = 1
	env.upstream.createBody = cachedBillingCreateBody(time.Now().Add(time.Hour))

	rec := env.create()
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.Equal(t, 1, env.upstream.methodCalls(http.MethodPost))
	require.Len(t, env.repo.records, 1)
	for _, record := range env.repo.records {
		require.Equal(t, int64(901), record.AccountID)
	}
}

func TestGeminiCachedContentsCreate_ExplicitRejectionStillFailsOver(t *testing.T) {
	for _, status := range []int{http.StatusUnauthorized, http.StatusForbidden, http.StatusTooManyRequests, http.StatusServiceUnavailable} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			env := newCachedBillingEnv(t, 500, 900, 901, 902)
			env.upstream.failures = map[string]cachedBillingUpstreamFailure{
				http.MethodPost: {status: status, body: `{"error":{"message":"rejected"}}`},
			}
			rec := env.create()
			require.NotEqual(t, http.StatusOK, rec.Code)
			require.Equal(t, 3, env.upstream.methodCalls(http.MethodPost))
			require.Empty(t, env.repo.records)
			require.Empty(t, env.usage.totals())
		})
	}
}
