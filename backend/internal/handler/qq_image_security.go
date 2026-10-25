package handler

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net"
	"net/http"
	"net/url"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/ip"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/gin-gonic/gin"
)

// Browser mutations must be same-origin. Non-browser clients still require JWT
// authentication; cookies are never an alternative authentication mechanism.
func qqImageMutationAllowed(c *gin.Context, frontendURL string, cfg *config.Config) bool {
	if strings.EqualFold(c.GetHeader("Sec-Fetch-Site"), "cross-site") {
		return false
	}
	origin := c.GetHeader("Origin")
	if origin == "" {
		return true
	}
	got, err := url.Parse(origin)
	if err != nil || got.User != nil || got.Host == "" || got.Path != "" || got.RawQuery != "" || got.Fragment != "" || (got.Scheme != "https" && got.Scheme != "http") {
		return false
	}
	expected, err := url.Parse(frontendURL)
	if err != nil || expected.Host == "" {
		scheme := "http"
		if c.Request.TLS != nil {
			scheme = "https"
		} else if qqImageTrustedProxy(c, cfg) {
			// Only the configured direct proxy may describe the public scheme.
			// Lists or duplicate values are ambiguous; never infer it from Origin.
			values := c.Request.Header.Values("X-Forwarded-Proto")
			if len(values) > 0 {
				if len(values) != 1 {
					return false
				}
				scheme = strings.ToLower(strings.TrimSpace(values[0]))
				if scheme != "http" && scheme != "https" {
					return false
				}
			}
		}
		expected = &url.URL{Scheme: scheme, Host: c.Request.Host}
	}
	return strings.EqualFold(got.Host, expected.Host) && got.Scheme == expected.Scheme
}

// Match the explicit server.trusted_proxies policy used by Gin. The legacy
// forwarded-client-IP switch is not authority for browser origin validation.
func qqImageTrustedProxy(c *gin.Context, cfg *config.Config) bool {
	if cfg == nil || !cfg.Server.TrustedProxiesConfigured || len(ip.ValidateIPPatterns(cfg.Server.TrustedProxies)) != 0 {
		return false
	}
	peer, _, err := net.SplitHostPort(strings.TrimSpace(c.Request.RemoteAddr))
	return err == nil && ip.MatchesAnyPattern(peer, cfg.Server.TrustedProxies)
}
func qqImageDecode(c *gin.Context, target any) error {
	typ, _, err := mime.ParseMediaType(c.GetHeader("Content-Type"))
	if err != nil || typ != "application/json" {
		return errors.New("json_body_required")
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 64<<10)
	decoder := json.NewDecoder(c.Request.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		return errors.New("single_json_body_required")
	}
	return nil
}
func qqImageSessionKey(c *gin.Context) string {
	value := c.GetString(middleware.ContextKeySessionID)
	if value == "" {
		parts := strings.SplitN(c.GetHeader("Authorization"), " ", 2)
		if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") || strings.TrimSpace(parts[1]) == "" {
			return ""
		}
		value = "legacy:" + strings.TrimSpace(parts[1])
	}
	digest := sha256.Sum256([]byte("qq-image-session:" + value))
	return hex.EncodeToString(digest[:])
}
