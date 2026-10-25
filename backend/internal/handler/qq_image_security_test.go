package handler

import (
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestQQImageMutationOriginAndJSONBoundary(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, tt := range []struct {
		name, origin, site string
		want               bool
	}{
		{"same origin", "https://site.example", "same-origin", true},
		{"cross site", "https://attacker.example", "cross-site", false},
		{"wrong port", "https://site.example:444", "same-site", false},
		{"null origin", "null", "cross-site", false},
		{"native bearer client", "", "", true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest("POST", "https://site.example/action", strings.NewReader(`{"consent":true}`))
			c.Request.Header.Set("Origin", tt.origin)
			c.Request.Header.Set("Sec-Fetch-Site", tt.site)
			require.Equal(t, tt.want, qqImageMutationAllowed(c, "https://site.example", nil))
		})
	}
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest("POST", "/", strings.NewReader(`{"consent":true,"user_id":999}`))
	c.Request.Header.Set("Content-Type", "application/json")
	var input struct {
		Consent bool `json:"consent"`
	}
	require.Error(t, qqImageDecode(c, &input), "unknown user identity must not be accepted")
	c.Request = httptest.NewRequest("POST", "/", strings.NewReader(`{"consent":true} {"consent":false}`))
	c.Request.Header.Set("Content-Type", "application/json")
	require.Error(t, qqImageDecode(c, &input), "multiple JSON objects must be rejected")
	c.Request = httptest.NewRequest("POST", "/", strings.NewReader(`{"consent":true}`))
	c.Request.Header.Set("Content-Type", "text/plain")
	require.Error(t, qqImageDecode(c, &input), "simple cross-site request must not mutate")
	c.Request = httptest.NewRequest("POST", "/", strings.NewReader(`{"consent":true}`))
	c.Request.Header.Set("Content-Type", "application/json")
	require.NoError(t, qqImageDecode(c, &input))
	require.True(t, input.Consent)
}
func TestQQImageSessionKeyUsesAuthenticatedFamily(t *testing.T) {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest("GET", "/", nil)
	c.Set(middleware.ContextKeySessionID, "authenticated-session")
	first := qqImageSessionKey(c)
	require.NotEmpty(t, first)
	require.NotContains(t, first, "authenticated-session")
	c.Request.Header.Set("Authorization", "Bearer refreshed")
	require.Equal(t, first, qqImageSessionKey(c), "refresh must retain the authenticated family")
	c.Set(middleware.ContextKeySessionID, "other-session")
	require.NotEqual(t, first, qqImageSessionKey(c))
	c.Set(middleware.ContextKeySessionID, "")
	c.Request.Header.Del("Authorization")
	require.Empty(t, qqImageSessionKey(c), "missing authenticated session cannot share a grant")
}

func TestQQImageMutationOriginUsesOnlyConfiguredTrustedProxy(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, tt := range []struct {
		name, target, origin, peer, frontendURL string
		trusted                                 []string
		configured                              bool
		forwarded                               []string
		want                                    bool
	}{
		{name: "direct HTTP", target: "http://site.example/action", origin: "http://site.example", want: true},
		{name: "direct TLS", target: "https://site.example/action", origin: "https://site.example", want: true},
		{name: "trusted TLS proxy", origin: "https://site.example", peer: "127.0.0.1:1234", trusted: []string{"127.0.0.1/32"}, configured: true, forwarded: []string{"https"}, want: true},
		{name: "trusted IPv6 TLS proxy", origin: "https://site.example", peer: "[::1]:1234", trusted: []string{"::1"}, configured: true, forwarded: []string{"https"}, want: true},
		{name: "untrusted protocol spoof", origin: "https://site.example", peer: "192.0.2.10:1234", trusted: []string{"127.0.0.1"}, configured: true, forwarded: []string{"https"}},
		{name: "unconfigured trust list", origin: "https://site.example", peer: "127.0.0.1:1234", trusted: []string{"127.0.0.1"}, forwarded: []string{"https"}},
		{name: "explicit empty trust list", origin: "https://site.example", peer: "127.0.0.1:1234", configured: true, forwarded: []string{"https"}},
		{name: "invalid trust list", origin: "https://site.example", peer: "127.0.0.1:1234", trusted: []string{"127.0.0.1", "invalid"}, configured: true, forwarded: []string{"https"}},
		{name: "unknown peer", origin: "https://site.example", peer: "unknown", trusted: []string{"127.0.0.1"}, configured: true, forwarded: []string{"https"}},
		{name: "missing proxy scheme", origin: "https://site.example", peer: "127.0.0.1:1234", trusted: []string{"127.0.0.1"}, configured: true},
		{name: "ambiguous proxy scheme list", origin: "http://site.example", peer: "127.0.0.1:1234", trusted: []string{"127.0.0.1"}, configured: true, forwarded: []string{"https,http"}},
		{name: "duplicate proxy scheme headers", origin: "http://site.example", peer: "127.0.0.1:1234", trusted: []string{"127.0.0.1"}, configured: true, forwarded: []string{"https", "http"}},
		{name: "unknown proxy scheme", origin: "http://site.example", peer: "127.0.0.1:1234", trusted: []string{"127.0.0.1"}, configured: true, forwarded: []string{"ftp"}},
		{name: "proxy cannot replace host", origin: "https://attacker.example", peer: "127.0.0.1:1234", trusted: []string{"127.0.0.1"}, configured: true, forwarded: []string{"https"}},
		{name: "explicit frontend URL", origin: "https://site.example", frontendURL: "https://site.example", peer: "192.0.2.10:1234", forwarded: []string{"http"}, want: true},
		{name: "explicit frontend URL rejects wrong origin", origin: "https://attacker.example", frontendURL: "https://site.example", peer: "127.0.0.1:1234", trusted: []string{"127.0.0.1"}, configured: true, forwarded: []string{"https"}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			target := tt.target
			if target == "" {
				target = "http://site.example/action"
			}
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest("PUT", target, nil)
			if tt.peer != "" {
				c.Request.RemoteAddr = tt.peer
			}
			c.Request.Header.Set("Origin", tt.origin)
			c.Request.Header.Set("Sec-Fetch-Site", "same-origin")
			c.Request.Header.Set("X-Forwarded-Host", "attacker.example")
			for _, value := range tt.forwarded {
				c.Request.Header.Add("X-Forwarded-Proto", value)
			}
			h := &QQImageHandler{cfg: &config.Config{Server: config.ServerConfig{FrontendURL: tt.frontendURL, TrustedProxies: tt.trusted, TrustedProxiesConfigured: tt.configured}}}
			require.Equal(t, tt.want, h.mutation(c))
		})
	}
}
