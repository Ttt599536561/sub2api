package handler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/qqimage"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type qqHTTPStoreStub struct {
	qqImageStore
	user     int64
	email    string
	session  string
	calls    int
	verified bool
}

func (s *qqHTTPStoreStub) GetConfig(context.Context) (*qqimage.Config, error) {
	return &qqimage.Config{Enabled: true, APIKey: "must-not-leak", BotSecret: "must-not-leak"}, nil
}
func (s *qqHTTPStoreStub) RecentAuthorization(context.Context, int64, string) (bool, error) {
	return true, nil
}
func (s *qqHTTPStoreStub) AuthorizeBinding(_ context.Context, _ string, uid int64, sid string) (*qqimage.BindingAuthorization, error) {
	s.user = uid
	s.session = sid
	s.calls++
	return &qqimage.BindingAuthorization{ConfirmationCode: "ABCDEF12", ExpiresAt: time.Now().Add(time.Minute)}, nil
}
func (s *qqHTTPStoreStub) CurrentEmail(_ context.Context, uid int64) (string, error) {
	s.user = uid
	return "owner@example.test", nil
}
func (s *qqHTTPStoreStub) MarkEmailVerified(_ context.Context, uid int64, email string) error {
	s.user = uid
	s.email = email
	s.verified = true
	return nil
}
func (s *qqHTTPStoreStub) MarkRecentAuthorization(_ context.Context, uid int64, sid string) error {
	s.user = uid
	s.session = sid
	return nil
}
func (s *qqHTTPStoreStub) GetArtifact(_ context.Context, _ string, uid int64) (*qqimage.Artifact, error) {
	s.user = uid
	return &qqimage.Artifact{Data: []byte("image"), MIME: "image/png"}, nil
}

type qqHTTPEmailStub struct {
	email string
	err   error
}

func (s *qqHTTPEmailStub) SendQQImageEmailCode(_ context.Context, _ int64, _ ...string) (*service.SendVerifyCodeResult, error) {
	s.email = "owner@example.test"
	return &service.SendVerifyCodeResult{Countdown: 60}, s.err
}
func (s *qqHTTPEmailStub) VerifyOAuthEmailCode(_ context.Context, email, code string) error {
	s.email = email
	return s.err
}
func qqHTTPRequest(h *QQImageHandler, method, path, body string, uid int64, role string, fn gin.HandlerFunc) *httptest.ResponseRecorder {
	r := gin.New()
	r.Use(func(c *gin.Context) {
		if uid > 0 {
			c.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: uid})
			c.Set(string(middleware.ContextKeyUserRole), role)
			c.Set(middleware.ContextKeySessionID, "session-42")
		}
	})
	r.Handle(method, "/item/:id", fn)
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}
func TestQQImageHTTPIdentityAndExplicitConsent(t *testing.T) {
	s := &qqHTTPStoreStub{}
	h := &QQImageHandler{store: s}
	w := qqHTTPRequest(h, "POST", "/item/request", `{"consent":true}`, 0, "", h.AuthorizeBinding)
	require.Equal(t, 401, w.Code)
	w = qqHTTPRequest(h, "POST", "/item/request", `{"consent":true,"user_id":999}`, 42, "user", h.AuthorizeBinding)
	require.Equal(t, 400, w.Code)
	w = qqHTTPRequest(h, "POST", "/item/request", `{"consent":false}`, 42, "user", h.AuthorizeBinding)
	require.Equal(t, 400, w.Code)
	require.Zero(t, s.calls)
	w = qqHTTPRequest(h, "POST", "/item/request", `{"consent":true}`, 42, "user", h.AuthorizeBinding)
	require.Equal(t, 200, w.Code, w.Body.String())
	require.Equal(t, int64(42), s.user)
	require.NotEmpty(t, s.session)
	require.NotEqual(t, "session-42", s.session)
}
func TestQQImageHTTPCurrentEmailAndPrivateOwner(t *testing.T) {
	s := &qqHTTPStoreStub{}
	e := &qqHTTPEmailStub{}
	h := &QQImageHandler{store: s, email: e}
	w := qqHTTPRequest(h, "POST", "/item/email", `{"code":"123456","email":"someone@example.test"}`, 42, "user", h.VerifyEmail)
	require.Equal(t, 400, w.Code)
	require.False(t, s.verified)
	w = qqHTTPRequest(h, "POST", "/item/email", `{"code":"123456"}`, 42, "user", h.VerifyEmail)
	require.Equal(t, 200, w.Code, w.Body.String())
	require.Equal(t, "owner@example.test", e.email)
	require.Equal(t, e.email, s.email)
	w = qqHTTPRequest(h, "GET", "/item/order", "", 0, "", h.Image)
	require.Equal(t, 401, w.Code)
	w = qqHTTPRequest(h, "GET", "/item/order?user_id=999", "", 42, "user", h.Image)
	require.Equal(t, 200, w.Code)
	require.Equal(t, int64(42), s.user)
	require.Equal(t, "no-store", w.Header().Get("Cache-Control"))
	require.Equal(t, "nosniff", w.Header().Get("X-Content-Type-Options"))
}
func TestQQImageHTTPAdminConfigNeverReturnsSecrets(t *testing.T) {
	s := &qqHTTPStoreStub{}
	h := &QQImageHandler{store: s}
	w := qqHTTPRequest(h, "GET", "/item/config", "", 42, "user", h.AdminConfig)
	require.Equal(t, 403, w.Code)
	w = qqHTTPRequest(h, "GET", "/item/config", "", 42, "admin", h.AdminConfig)
	require.Equal(t, 200, w.Code)
	require.NotContains(t, w.Body.String(), "must-not-leak")
}
func (s *qqHTTPStoreStub) RevokeGrant(_ context.Context, _ string, uid int64, reason string) error {
	s.user = uid
	s.email = reason
	s.calls++
	return nil
}
func (s *qqHTTPStoreStub) RevokeGrantAsAdmin(_ context.Context, _ string, uid int64, reason string) error {
	s.user = uid
	s.email = reason
	s.calls++
	return nil
}
func TestQQImageAdminRevokePreservesActorAndReason(t *testing.T) {
	s := &qqHTTPStoreStub{}
	h := &QQImageHandler{store: s}
	w := qqHTTPRequest(h, "DELETE", "/item/grant", `{"reason":"owner requested recovery"}`, 42, "admin", h.AdminRevokeGrant)
	require.Equal(t, 200, w.Code)
	require.Equal(t, int64(42), s.user)
	require.Equal(t, "owner requested recovery", s.email)
}
func TestQQImageWebhookRejectsOversizedAndReportsUnavailable(t *testing.T) {
	h := &QQImageHandler{webhook: func(context.Context, http.Header, []byte) (any, error) { return nil, qqimage.ErrDisabled }}
	w := qqHTTPRequest(h, "POST", "/item/event", `{}`, 0, "", h.Events)
	require.Equal(t, 503, w.Code)
	w = qqHTTPRequest(h, "POST", "/item/event", strings.Repeat("x", 65<<10), 0, "", h.Events)
	require.Equal(t, 413, w.Code)
}

func TestQQImageSimpleModePreservesOldImagesButBlocksNewPlatformEvents(t *testing.T) {
	s := &qqHTTPStoreStub{}
	called := false
	h := &QQImageHandler{store: s, cfg: &config.Config{RunMode: config.RunModeSimple}, webhook: func(context.Context, http.Header, []byte) (any, error) {
		called = true
		return map[string]int{"op": 12}, nil
	}}
	image := qqHTTPRequest(h, "GET", "/item/old-order", "", 42, "user", h.Image)
	require.Equal(t, 200, image.Code, "existing paid orders must remain privately retrievable")
	event := qqHTTPRequest(h, "POST", "/item/event", `{}`, 0, "", h.Events)
	require.Equal(t, 503, event.Code)
	require.False(t, called, "simple mode cannot admit new QQ commands")
	auth := qqHTTPRequest(h, "POST", "/item/request", `{"consent":true}`, 42, "user", h.AuthorizeBinding)
	require.Equal(t, 403, auth.Code)
	require.Zero(t, s.calls)
}

func (s *qqHTTPStoreStub) ResolveOrderFailure(_ context.Context, _ string, actor int64, reason string) (*qqimage.Order, error) {
	s.user = actor
	s.email = reason
	s.calls++
	return &qqimage.Order{FundsStatus: "released"}, nil
}
func TestQQImageAdminReleaseRequiresActorAndExplicitReason(t *testing.T) {
	s := &qqHTTPStoreStub{}
	h := &QQImageHandler{store: s}
	w := qqHTTPRequest(h, "POST", "/item/order", `{"reason":"confirmed provider failure"}`, 42, "user", h.AdminRelease)
	require.Equal(t, 403, w.Code)
	w = qqHTTPRequest(h, "POST", "/item/order", `{"reason":""}`, 42, "admin", h.AdminRelease)
	require.Equal(t, 400, w.Code)
	w = qqHTTPRequest(h, "POST", "/item/order", `{"reason":"confirmed provider failure"}`, 42, "admin", h.AdminRelease)
	require.Equal(t, 200, w.Code)
	require.Equal(t, int64(42), s.user)
	require.Equal(t, "confirmed provider failure", s.email)
	require.Equal(t, 1, s.calls)
}
