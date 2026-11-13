package handler

import (
	"context"
	"crypto/hmac"
	"crypto/sha1"
	"encoding/hex"
	"github.com/Wei-Shaw/sub2api/internal/qqinvite"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"net/http/httptest"
	"strings"
	"testing"
)

type inviteHTTPStub struct {
	qqInviteStore
	calls int
	cfg   qqinvite.Config
}

func (s *inviteHTTPStub) GetConfig(context.Context) (qqinvite.Config, error)     { return s.cfg, nil }
func (s *inviteHTTPStub) RuntimeConfig(context.Context) (qqinvite.Config, error) { return s.cfg, nil }
func (s *inviteHTTPStub) AcceptEvent(context.Context, qqinvite.Event) error      { s.calls++; return nil }
func inviteHTTPRequest(h *QQInviteHandler, method, path, body, role string, fn gin.HandlerFunc, headers map[string]string) *httptest.ResponseRecorder {
	r := gin.New()
	r.Use(func(c *gin.Context) {
		if role != "" {
			c.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: 42})
			c.Set(string(middleware.ContextKeyUserRole), role)
		}
	})
	r.Handle(method, path, fn)
	req := httptest.NewRequest(method, strings.ReplaceAll(path, ":id", "1"), strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}
func TestQQInviteAdminConfigAccessAndRedaction(t *testing.T) {
	gin.SetMode(gin.TestMode)
	s := &inviteHTTPStub{cfg: qqinvite.Config{OneBotToken: "one-canary", WebUIToken: "web-canary", EventSecret: "event-canary"}}
	h := &QQInviteHandler{store: s}
	for _, tc := range []struct {
		role   string
		status int
	}{{"", 401}, {"user", 403}, {"admin", 200}} {
		w := inviteHTTPRequest(h, "GET", "/config", "", tc.role, h.GetConfig, nil)
		require.Equal(t, tc.status, w.Code, w.Body.String())
		require.NotContains(t, w.Body.String(), "canary")
		require.Equal(t, "no-store", w.Header().Get("Cache-Control"))
	}
}
func TestQQInviteMutationsRejectCrossSite(t *testing.T) {
	h := &QQInviteHandler{}
	w := inviteHTTPRequest(h, "POST", "/login", "", "admin", h.RefreshQRCode, map[string]string{"Sec-Fetch-Site": "cross-site"})
	require.Equal(t, 403, w.Code)
}
func TestQQInviteWebhookRequiresExactBodySignatureAndAccount(t *testing.T) {
	body := `{"time":1794312345,"self_id":12345,"post_type":"notice","notice_type":"group_increase","sub_type":"invite","group_id":77777,"user_id":88888,"operator_id":99999}`
	s := &inviteHTTPStub{cfg: qqinvite.Config{Enabled: true, BotQQ: "12345", EventSecret: "test-secret"}}
	h := &QQInviteHandler{store: s}
	mac := hmac.New(sha1.New, []byte("test-secret"))
	_, _ = mac.Write([]byte(body))
	sig := "sha1=" + hex.EncodeToString(mac.Sum(nil))
	w := inviteHTTPRequest(h, "POST", "/events", body, "", h.Events, nil)
	require.Equal(t, 401, w.Code)
	require.Zero(t, s.calls)
	w = inviteHTTPRequest(h, "POST", "/events", body+" ", "", h.Events, map[string]string{"X-Signature": sig, "X-Self-ID": "12345"})
	require.Equal(t, 401, w.Code)
	require.Zero(t, s.calls)
	w = inviteHTTPRequest(h, "POST", "/events", body, "", h.Events, map[string]string{"X-Signature": sig, "X-Self-ID": "54321"})
	require.Equal(t, 403, w.Code)
	require.Zero(t, s.calls)
	w = inviteHTTPRequest(h, "POST", "/events", body, "", h.Events, map[string]string{"X-Signature": sig, "X-Self-ID": "12345"})
	require.Equal(t, 200, w.Code, w.Body.String())
	require.Equal(t, 1, s.calls)
	s.cfg.BotQQ = "54321"
	w = inviteHTTPRequest(h, "POST", "/events", body, "", h.Events, map[string]string{"X-Signature": sig, "X-Self-ID": "12345"})
	require.Equal(t, 403, w.Code)
	require.Equal(t, 1, s.calls)
}

func TestQQInviteDepartureAcceptedWhileRewardsDisabled(t *testing.T) {
	s := &inviteHTTPStub{cfg: qqinvite.Config{Enabled: false, BotQQ: "12345", EventSecret: "test-secret"}}
	h := &QQInviteHandler{store: s}
	body := `{"time":1794312345,"self_id":12345,"post_type":"notice","notice_type":"group_decrease","sub_type":"leave","group_id":77777,"user_id":88888}`
	mac := hmac.New(sha1.New, []byte("test-secret"))
	_, _ = mac.Write([]byte(body))
	sig := "sha1=" + hex.EncodeToString(mac.Sum(nil))
	w := inviteHTTPRequest(h, "POST", "/events", body, "", h.Events, map[string]string{"X-Signature": sig})
	require.Equal(t, 200, w.Code)
	require.Equal(t, 1, s.calls)
}

func TestQQInviteCancellationClaimAcceptedWhileRewardsDisabled(t *testing.T) {
	s := &inviteHTTPStub{cfg: qqinvite.Config{Enabled: false, BotQQ: "12345", EventSecret: "test-secret"}}
	h := &QQInviteHandler{store: s}
	body := `{"time":1794312345,"self_id":12345,"post_type":"message","message_type":"private","user_id":88888,"raw_message":"领取奖励"}`
	mac := hmac.New(sha1.New, []byte("test-secret"))
	_, _ = mac.Write([]byte(body))
	w := inviteHTTPRequest(h, "POST", "/events", body, "", h.Events, map[string]string{"X-Signature": "sha1=" + hex.EncodeToString(mac.Sum(nil))})
	require.Equal(t, 200, w.Code)
	require.Equal(t, 1, s.calls)
}

func (s *inviteHTTPStub) GenerateCodes(_ context.Context, _ int64, n int, _ int64) (qqinvite.CodePreview, error) {
	s.calls++
	return qqinvite.CodePreview{ValidCount: n}, nil
}
func TestQQInviteGenerateSupportsUIBatchLimit(t *testing.T) {
	s := &inviteHTTPStub{}
	h := &QQInviteHandler{store: s}
	w := inviteHTTPRequest(h, "POST", "/campaigns/:id/codes/generate", `{"count":500}`, "admin", h.GenerateCodes, nil)
	require.Equal(t, 200, w.Code, w.Body.String())
	require.Equal(t, 1, s.calls)
	w = inviteHTTPRequest(h, "POST", "/campaigns/:id/codes/generate", `{"count":501}`, "admin", h.GenerateCodes, nil)
	require.Equal(t, 400, w.Code)
	require.Equal(t, 1, s.calls)
}
