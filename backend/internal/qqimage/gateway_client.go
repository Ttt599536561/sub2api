package qqimage

import (
	"context"
	"crypto/sha256"
	"crypto/tls"
	"encoding/hex"
	"encoding/json"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/gorilla/websocket"
)

const gatewayMaxFrame = 64 << 10

type gatewayEndpoint struct {
	url        string
	remaining  int64
	resetAfter time.Duration
}

// The gateway URL is supplied by QQ, but is still treated as untrusted input.
// Only the documented production endpoints and JSON wire encoding are allowed.
func validGatewayURL(raw string) bool {
	if len(raw) > 2048 || strings.ContainsAny(raw, "#\\\r\n") {
		return false
	}
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "wss" || u.User != nil || u.Fragment != "" || u.Opaque != "" || u.RawPath != "" {
		return false
	}
	host := u.Hostname()
	if host != "api.bot.qq.com" && host != "api.sgroup.qq.com" {
		return false
	}
	if u.Host != host && u.Host != host+":443" {
		return false
	}
	if u.Path != "/websocket" && u.Path != "/websocket/" {
		return false
	}
	query, err := url.ParseQuery(u.RawQuery)
	if err != nil {
		return false
	}
	for key, values := range query {
		if len(values) != 1 {
			return false
		}
		switch key {
		case "encoding":
			if values[0] != "json" {
				return false
			}
		case "v":
			if values[0] != "1" && values[0] != "2" {
				return false
			}
		default:
			return false
		}
	}
	return true
}

func secureGatewayDial(ctx context.Context, target string) (*websocket.Conn, *http.Response, error) {
	d := &net.Dialer{Timeout: 10 * time.Second, KeepAlive: 30 * time.Second}
	dialer := websocket.Dialer{NetDialContext: restrictedDialContext(net.DefaultResolver, d), TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS12}, HandshakeTimeout: 15 * time.Second, ReadBufferSize: 4096, WriteBufferSize: 4096}
	return dialer.DialContext(ctx, target, nil)
}

func (g *Gateway) endpoint(ctx context.Context, token string) (gatewayEndpoint, gatewayResult) {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, qqAPIBase+"/gateway/bot", nil)
	if err != nil {
		return gatewayEndpoint{}, gatewayResult{code: "qq_gateway_invalid_request", terminal: true}
	}
	request.Header.Set("Authorization", "QQBot "+token)
	client := g.client.client
	if client == nil {
		client = secureHTTPClient(15 * time.Second)
	}
	copyClient := *client
	copyClient.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	response, err := copyClient.Do(request)
	if err != nil {
		return gatewayEndpoint{}, gatewayResult{code: "qq_gateway_unavailable"}
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		result := gatewayResult{code: "qq_gateway_http_failed"}
		switch response.StatusCode {
		case http.StatusUnauthorized:
			result.code = "qq_gateway_authentication_failed"
			result.refreshToken = true
		case http.StatusTooManyRequests:
			result.code = "qq_gateway_rate_limited"
			result.delay = time.Minute
		default:
			result.terminal = response.StatusCode < 500 && response.StatusCode != http.StatusRequestTimeout
		}
		return gatewayEndpoint{}, result
	}
	raw, err := boundedRead(response.Body, gatewayMaxFrame)
	if err != nil {
		return gatewayEndpoint{}, gatewayResult{code: "qq_gateway_invalid_response", terminal: true}
	}
	var wire struct {
		URL    string `json:"url"`
		Shards *int64 `json:"shards"`
		Limits struct {
			Total          *int64 `json:"total"`
			Remaining      *int64 `json:"remaining"`
			ResetAfter     *int64 `json:"reset_after"`
			MaxConcurrency *int64 `json:"max_concurrency"`
		} `json:"session_start_limit"`
	}
	if json.Unmarshal(raw, &wire) != nil || wire.Shards == nil || *wire.Shards < 1 || wire.Limits.Remaining == nil || *wire.Limits.Remaining < 0 || *wire.Limits.Remaining > 1000000 || wire.Limits.ResetAfter == nil || *wire.Limits.ResetAfter <= 0 || *wire.Limits.ResetAfter > 86400000 || wire.Limits.MaxConcurrency == nil || *wire.Limits.MaxConcurrency < 1 || *wire.Limits.MaxConcurrency > 10000 {
		return gatewayEndpoint{}, gatewayResult{code: "qq_gateway_invalid_response", terminal: true}
	}
	if wire.Limits.Total != nil && (*wire.Limits.Total < 1 || *wire.Limits.Total < *wire.Limits.Remaining) {
		return gatewayEndpoint{}, gatewayResult{code: "qq_gateway_invalid_response", terminal: true}
	}
	if *wire.Shards != 1 {
		return gatewayEndpoint{}, gatewayResult{code: "qq_gateway_shards_unsupported", terminal: true}
	}
	if !validGatewayURL(wire.URL) {
		return gatewayEndpoint{}, gatewayResult{code: "qq_gateway_url_rejected", terminal: true}
	}
	return gatewayEndpoint{url: wire.URL, remaining: *wire.Limits.Remaining, resetAfter: time.Duration(*wire.Limits.ResetAfter) * time.Millisecond}, gatewayResult{}
}

func (g *Gateway) invalidateToken() {
	digest := sha256.Sum256([]byte(g.config.AppID + "\x00" + g.config.BotSecret))
	g.client.mu.Lock()
	delete(g.client.tokens, hex.EncodeToString(digest[:]))
	g.client.mu.Unlock()
}
