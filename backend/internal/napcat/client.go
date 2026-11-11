// Package napcat implements the bounded NapCat v4.18.42 WebUI and OneBot HTTP
// contracts. Credentials and upstream errors never leave this package.
package napcat

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha1"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

var (
	ErrUnauthorized    = errors.New("napcat: authentication failed")
	ErrTwoFactor       = errors.New("napcat: WebUI two-factor authentication requires manual configuration")
	ErrUnavailable     = errors.New("napcat: service unavailable")
	ErrInvalid         = errors.New("napcat: invalid input or endpoint configuration")
	ErrRateLimited     = errors.New("napcat: rate limited")
	ErrOffline         = errors.New("napcat: QQ account offline")
	ErrUnknownLevel    = errors.New("napcat: QQ account level unavailable")
	ErrDeliveryUnknown = errors.New("napcat: message delivery outcome unknown")
	ErrProtocol        = errors.New("napcat: invalid upstream response")
	ErrNotConfigured   = errors.New("napcat: endpoint or credential not configured")
)

const maxResponseBytes = 1 << 20

type Config struct {
	OneBotURL   string
	OneBotToken string
	WebUIURL    string
	WebUIToken  string
	// AllowedHosts extends the default exact hosts. Populate only from trusted
	// deployment configuration, never from an HTTP request or database setting.
	AllowedHosts []string
}

type Client struct {
	oneBotURL, oneBotToken, webUIURL, webUIToken string
	http                                         *http.Client
	credentialMu                                 sync.Mutex
	credential                                   string
	credentialExpires                            time.Time
}

type Status struct {
	Online          bool   `json:"online"`
	Offline         bool   `json:"offline"`
	CoreReady       bool   `json:"core_ready"`
	QRLoginAccepted bool   `json:"qr_login_accepted"`
	LoginPhase      string `json:"login_phase"`
	QQ              string `json:"qq"`
	Nickname        string `json:"nickname"`
}

// QRCode contains a QQ login payload to encode locally as a QR image. It is
// never fetched by this package, and must not be used as an image source URL.
type QRCode struct {
	URL        string `json:"url"`
	Restarting bool   `json:"restarting"`
}

// Close releases pooled connections when the owning configuration is replaced.
// In-flight calls retain their caller's context and can complete normally.
func (c *Client) Close() {
	if c != nil && c.http != nil {
		c.http.CloseIdleConnections()
	}
}

func NewClient(cfg Config) (*Client, error) {
	if cfg.OneBotURL == "" || cfg.WebUIURL == "" || strings.TrimSpace(cfg.OneBotToken) == "" || strings.TrimSpace(cfg.WebUIToken) == "" {
		return nil, ErrNotConfigured
	}
	if strings.ContainsAny(cfg.OneBotToken, "\r\n") || strings.ContainsAny(cfg.WebUIToken, "\r\n") {
		return nil, ErrInvalid
	}
	hosts := map[string]bool{"napcat": true, "localhost": true, "127.0.0.1": true}
	for _, host := range cfg.AllowedHosts {
		if host == "" || host != strings.ToLower(host) || strings.ContainsAny(host, " /\\@?#*%\r\n\t") || strings.HasSuffix(host, ".") {
			return nil, ErrInvalid
		}
		if strings.Contains(host, ":") && net.ParseIP(host) == nil {
			return nil, ErrInvalid
		}
		hosts[host] = true
	}
	oneBot, err := validateEndpoint(cfg.OneBotURL, hosts)
	if err != nil {
		return nil, err
	}
	webUI, err := validateEndpoint(cfg.WebUIURL, hosts)
	if err != nil {
		return nil, err
	}
	// Do not use ProxyFromEnvironment: a machine-wide proxy must not receive
	// internal credentials or change the intended Docker network destination.
	transport := &http.Transport{DialContext: (&net.Dialer{Timeout: 5 * time.Second, KeepAlive: 30 * time.Second}).DialContext, TLSHandshakeTimeout: 5 * time.Second, ResponseHeaderTimeout: 15 * time.Second, IdleConnTimeout: 30 * time.Second, MaxIdleConns: 10, MaxIdleConnsPerHost: 5}
	return &Client{oneBotURL: oneBot, oneBotToken: cfg.OneBotToken, webUIURL: webUI, webUIToken: cfg.WebUIToken, http: &http.Client{Transport: transport, Timeout: 15 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}, nil
}

func validateEndpoint(raw string, hosts map[string]bool) (string, error) {
	u, err := url.Parse(raw)
	if err != nil || u == nil || (u.Scheme != "http" && u.Scheme != "https") || u.User != nil || u.Host == "" || !hosts[u.Hostname()] || (u.Path != "" && u.Path != "/") || u.RawPath != "" || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || strings.Contains(raw, "#") {
		return "", ErrInvalid
	}
	if port := u.Port(); port != "" {
		n, e := strconv.Atoi(port)
		if e != nil || n < 1 || n > 65535 || strconv.Itoa(n) != port {
			return "", ErrInvalid
		}
	}
	return strings.TrimSuffix(u.String(), "/"), nil
}

// post never returns an upstream response body in an error. ambiguous marks
// failures after a potentially successful send; callers must not retry them.
func (c *Client) post(ctx context.Context, base, path, token string, payload any) (body []byte, ambiguous bool, err error) {
	encoded, e := json.Marshal(payload)
	if e != nil {
		return nil, false, ErrInvalid
	}
	req, e := http.NewRequestWithContext(ctx, http.MethodPost, base+path, bytes.NewReader(encoded))
	if e != nil {
		return nil, false, ErrInvalid
	}
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, e := c.http.Do(req)
	if e != nil {
		return nil, true, ErrUnavailable
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		switch resp.StatusCode {
		case 401, 403:
			return nil, false, ErrUnauthorized
		case 429:
			return nil, false, ErrRateLimited
		default:
			return nil, resp.StatusCode >= 500 || resp.StatusCode == 408, ErrUnavailable
		}
	}
	body, e = io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes+1))
	if e != nil {
		return nil, true, ErrUnavailable
	}
	if len(body) > maxResponseBytes {
		return nil, true, ErrProtocol
	}
	return body, false, nil
}

type webEnvelope struct {
	Code    *int            `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data"`
}

func decodeWeb(body []byte) (json.RawMessage, error) {
	var envelope webEnvelope
	if json.Unmarshal(body, &envelope) != nil || envelope.Code == nil {
		return nil, ErrProtocol
	}
	if *envelope.Code != 0 {
		return nil, upstreamError(envelope.Message)
	}
	return envelope.Data, nil
}

func upstreamError(message string) error {
	switch strings.ToLower(strings.TrimSpace(message)) {
	case "unauthorized", "authorization failed", "token is invalid", "token has been revoked", "token verify failed!", "token验证失败":
		return ErrUnauthorized
	case "login rate limit", "rate limited":
		return ErrRateLimited
	case "qq is offline", "bot is not online", "bot not online", "账号未登录", "尚未登录":
		return ErrOffline
	case "qq is logined":
		return ErrInvalid
	default:
		return ErrUnavailable
	}
}

func (c *Client) getCredential(ctx context.Context) (string, error) {
	c.credentialMu.Lock()
	defer c.credentialMu.Unlock()
	if c.credential != "" && time.Now().Before(c.credentialExpires) {
		return c.credential, nil
	}
	hash := sha256.Sum256([]byte(c.webUIToken + ".napcat"))
	body, _, err := c.post(ctx, c.webUIURL, "/api/auth/login", "", map[string]string{"hash": hex.EncodeToString(hash[:])})
	if err != nil {
		return "", err
	}
	data, err := decodeWeb(body)
	if err != nil {
		return "", err
	}
	var login struct {
		Credential string `json:"Credential"`
		Require2FA bool   `json:"require2FA"`
	}
	if json.Unmarshal(data, &login) != nil {
		return "", ErrProtocol
	}
	if login.Require2FA {
		return "", ErrTwoFactor
	}
	if login.Credential == "" || len(login.Credential) > 8192 || strings.ContainsAny(login.Credential, "\r\n") {
		return "", ErrProtocol
	}
	c.credential = login.Credential
	c.credentialExpires = time.Now().Add(50 * time.Minute)
	return c.credential, nil
}

func (c *Client) invalidateCredential(used string) {
	c.credentialMu.Lock()
	defer c.credentialMu.Unlock()
	if used == "" || c.credential == used {
		c.credential = ""
		c.credentialExpires = time.Time{}
	}
}

func (c *Client) web(ctx context.Context, path string, payload any, out any) error {
	for attempt := 0; attempt < 2; attempt++ {
		credential, err := c.getCredential(ctx)
		if err != nil {
			return err
		}
		body, _, err := c.post(ctx, c.webUIURL, path, credential, payload)
		var data json.RawMessage
		if err == nil {
			data, err = decodeWeb(body)
		}
		if errors.Is(err, ErrUnauthorized) && attempt == 0 {
			c.invalidateCredential(credential)
			continue
		}
		if err != nil {
			return err
		}
		if out != nil && json.Unmarshal(data, out) != nil {
			return ErrProtocol
		}
		return nil
	}
	return ErrUnauthorized
}

func (c *Client) Status(ctx context.Context) (Status, error) {
	var state struct {
		IsLogin         *bool  `json:"isLogin"`
		IsOffline       bool   `json:"isOffline"`
		CoreReady       *bool  `json:"coreReady"`
		QRLoginAccepted bool   `json:"qrLoginAccepted"`
		LoginPhase      string `json:"loginPhase"`
	}
	if err := c.web(ctx, "/api/QQLogin/CheckLoginStatus", struct{}{}, &state); err != nil {
		return Status{}, err
	}
	if state.IsLogin == nil || state.CoreReady == nil {
		return Status{}, ErrProtocol
	}
	phase := state.LoginPhase
	switch phase {
	case "waiting_qrcode", "generating_qrcode", "qrcode_scanned", "initializing", "ready", "offline", "reconnecting":
	default:
		phase = "unknown"
	}
	var info struct {
		Uin    scalarID `json:"uin"`
		Nick   string   `json:"nick"`
		Online *bool    `json:"online"`
	}
	if err := c.web(ctx, "/api/QQLogin/GetQQLoginInfo", struct{}{}, &info); err != nil {
		return Status{}, err
	}
	qq := string(info.Uin)
	if qq != "" && !validID(qq) {
		return Status{}, ErrProtocol
	}
	online := *state.IsLogin && *state.CoreReady && (info.Online == nil || *info.Online)
	return Status{Online: online, Offline: state.IsOffline, CoreReady: *state.CoreReady, QRLoginAccepted: state.QRLoginAccepted, LoginPhase: phase, QQ: qq, Nickname: info.Nick}, nil
}

func (c *Client) QRCode(ctx context.Context) (QRCode, error) {
	var data struct {
		QRCode string `json:"qrcode"`
	}
	if err := c.web(ctx, "/api/QQLogin/GetQQLoginQrcode", struct{}{}, &data); err != nil {
		return QRCode{}, err
	}
	if !validQR(data.QRCode) {
		return QRCode{}, ErrProtocol
	}
	return QRCode{URL: data.QRCode}, nil
}

func (c *Client) RefreshQRCode(ctx context.Context) (QRCode, error) {
	var data struct {
		URL        string `json:"qrcodeurl"`
		Restarting bool   `json:"restarting"`
	}
	if err := c.web(ctx, "/api/QQLogin/RefreshQRcode", struct{}{}, &data); err != nil {
		return QRCode{}, err
	}
	if data.URL == "" && data.Restarting {
		return QRCode{Restarting: true}, nil
	}
	if !validQR(data.URL) {
		return QRCode{}, ErrProtocol
	}
	return QRCode{URL: data.URL, Restarting: data.Restarting}, nil
}

func validQR(raw string) bool {
	if len(raw) == 0 || len(raw) > 8192 {
		return false
	}
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.User != nil || u.Fragment != "" || u.Port() != "" {
		return false
	}
	switch u.Host {
	case "txz.qq.com", "login.qq.com", "ssl.ptlogin2.qq.com", "ptlogin2.qq.com":
		return true
	default:
		return false
	}
}

// Restart is an explicit administrator operation. No background path invokes it.
func (c *Client) Restart(ctx context.Context) error {
	err := c.web(ctx, "/api/QQLogin/RestartNapCat", struct{}{}, nil)
	c.invalidateCredential("")
	return err
}

func (c *Client) QuickLogin(ctx context.Context, qq string) error {
	if !validID(qq) {
		return ErrInvalid
	}
	return c.web(ctx, "/api/QQLogin/SetQuickLogin", map[string]string{"uin": qq}, nil)
}

type oneBotEnvelope struct {
	Status  string          `json:"status"`
	Retcode *int            `json:"retcode"`
	Message string          `json:"message"`
	Wording string          `json:"wording"`
	Data    json.RawMessage `json:"data"`
}

func (c *Client) oneBot(ctx context.Context, path string, payload any, out any, sending bool) error {
	body, ambiguous, err := c.post(ctx, c.oneBotURL, path, c.oneBotToken, payload)
	if err != nil {
		if sending && ambiguous {
			return ErrDeliveryUnknown
		}
		return err
	}
	var env oneBotEnvelope
	if json.Unmarshal(body, &env) != nil || env.Retcode == nil || env.Status == "" {
		if sending {
			return ErrDeliveryUnknown
		}
		return ErrProtocol
	}
	if env.Status != "ok" || *env.Retcode != 0 {
		if env.Status == "async" && sending {
			return ErrDeliveryUnknown
		}
		if *env.Retcode == 1403 || *env.Retcode == 403 {
			return ErrUnauthorized
		}
		if *env.Retcode == 429 {
			return ErrRateLimited
		}
		return upstreamError(env.Message)
	}
	if out != nil && json.Unmarshal(env.Data, out) != nil {
		if sending {
			return ErrDeliveryUnknown
		}
		return ErrProtocol
	}
	return nil
}

func (c *Client) GetLevel(ctx context.Context, qq string) (int, error) {
	if !validID(qq) {
		return 0, ErrInvalid
	}
	var data struct {
		Level json.RawMessage `json:"qqLevel"`
	}
	if err := c.oneBot(ctx, "/get_stranger_info", map[string]any{"user_id": qq, "no_cache": true}, &data, false); err != nil {
		return 0, err
	}
	var level int
	// NapCat can expose missing native level as zero; do not report that as
	// a known account level. Invitation rewards do not use this information.
	if len(data.Level) == 0 || json.Unmarshal(data.Level, &level) != nil || level <= 0 || level > 1000 {
		return 0, ErrUnknownLevel
	}
	return level, nil
}

func (c *Client) GroupMembers(ctx context.Context, group string) ([]string, error) {
	if !validID(group) {
		return nil, ErrInvalid
	}
	var data []struct {
		UserID scalarID `json:"user_id"`
	}
	if err := c.oneBot(ctx, "/get_group_member_list", map[string]any{"group_id": group, "no_cache": true}, &data, false); err != nil {
		return nil, err
	}
	if data == nil {
		return nil, ErrProtocol
	}
	members := make([]string, 0, len(data))
	seen := make(map[string]bool, len(data))
	for _, entry := range data {
		id := string(entry.UserID)
		if !validID(id) {
			return nil, ErrProtocol
		}
		if !seen[id] {
			members = append(members, id)
			seen[id] = true
		}
	}
	return members, nil
}

type segment struct {
	Type string            `json:"type"`
	Data map[string]string `json:"data"`
}

func (c *Client) SendGroup(ctx context.Context, group, text, mentionQQ string) (string, error) {
	if !validID(group) || (mentionQQ != "" && !validID(mentionQQ)) || text == "" || len(text) > 16384 {
		return "", ErrInvalid
	}
	segments := make([]segment, 0, 2)
	if mentionQQ != "" {
		segments = append(segments, segment{Type: "at", Data: map[string]string{"qq": mentionQQ}})
	}
	segments = append(segments, segment{Type: "text", Data: map[string]string{"text": text}})
	return c.send(ctx, "/send_group_msg", map[string]any{"group_id": group, "message": segments})
}

func (c *Client) SendPrivate(ctx context.Context, qq, text string) (string, error) {
	if !validID(qq) || text == "" || len(text) > 16384 {
		return "", ErrInvalid
	}
	return c.send(ctx, "/send_private_msg", map[string]any{"user_id": qq, "message": []segment{{Type: "text", Data: map[string]string{"text": text}}}})
}

func (c *Client) send(ctx context.Context, path string, payload any) (string, error) {
	var data struct {
		MessageID scalarID `json:"message_id"`
	}
	if err := c.oneBot(ctx, path, payload, &data, true); err != nil {
		return "", err
	}
	id := string(data.MessageID)
	n, err := strconv.ParseInt(id, 10, 64)
	if err != nil || n == 0 || strconv.FormatInt(n, 10) != id {
		return "", ErrDeliveryUnknown
	}
	return id, nil
}

func validID(id string) bool {
	if id == "" || id[0] == '0' {
		return false
	}
	for _, r := range id {
		if r < '0' || r > '9' {
			return false
		}
	}
	_, err := strconv.ParseUint(id, 10, 64)
	return err == nil
}

type scalarID string

func (id *scalarID) UnmarshalJSON(data []byte) error {
	if bytes.Equal(data, []byte("null")) {
		return ErrProtocol
	}
	if len(data) > 0 && data[0] == '"' {
		var s string
		if json.Unmarshal(data, &s) != nil {
			return ErrProtocol
		}
		*id = scalarID(s)
		return nil
	}
	var n json.Number
	if json.Unmarshal(data, &n) != nil {
		return ErrProtocol
	}
	if _, err := strconv.ParseInt(n.String(), 10, 64); err != nil {
		return ErrProtocol
	}
	*id = scalarID(n.String())
	return nil
}

// VerifySignature verifies NapCat HTTP-client token HMAC over the original
// request bytes. X-Self-ID is unsigned; compare it to the signed self_id body.
func VerifySignature(secret, signature string, body []byte) bool {
	if secret == "" || !strings.HasPrefix(signature, "sha1=") || len(signature) != 45 {
		return false
	}
	provided, err := hex.DecodeString(strings.TrimPrefix(signature, "sha1="))
	if err != nil {
		return false
	}
	mac := hmac.New(sha1.New, []byte(secret))
	_, _ = mac.Write(body)
	return hmac.Equal(provided, mac.Sum(nil))
}
