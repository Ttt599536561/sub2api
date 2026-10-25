package handler

import (
	"context"
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

type qqDiscoveryHTTPStore struct {
	qqImageStore
	calls int
	actor int64
	id    string
}

func (s *qqDiscoveryHTTPStore) result(actor int64, id, status string, command bool) (*qqimage.Discovery, error) {
	s.calls++
	s.actor = actor
	s.id = id
	d := &qqimage.Discovery{ID: id, Status: status, AppID: "app", ExpiresAt: time.Now().Add(time.Minute)}
	if command {
		d.Command = "@机器人 /识别群 one-time-code"
	}
	return d, nil
}
func (s *qqDiscoveryHTTPStore) StartDiscovery(_ context.Context, actor int64) (*qqimage.Discovery, error) {
	return s.result(actor, "window", "waiting", true)
}
func (s *qqDiscoveryHTTPStore) GetDiscovery(context.Context) (*qqimage.Discovery, error) {
	return s.result(0, "window", "found", false)
}
func (s *qqDiscoveryHTTPStore) CloseDiscovery(_ context.Context, id string, actor int64) (*qqimage.Discovery, error) {
	return s.result(actor, id, "closed", false)
}
func (s *qqDiscoveryHTTPStore) ConfirmDiscovery(_ context.Context, id string, actor int64) (*qqimage.Discovery, error) {
	return s.result(actor, id, "confirmed", false)
}
func qqDiscoveryRequest(fn gin.HandlerFunc, uid int64, role, origin, body, method string) *httptest.ResponseRecorder {
	r := gin.New()
	r.Use(func(c *gin.Context) {
		if uid > 0 {
			c.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: uid})
			c.Set(string(middleware.ContextKeyUserRole), role)
		}
	})
	r.Handle(method, "/item/:id", fn)
	req := httptest.NewRequest(method, "/item/0123456789abcdef0123456789abcdef", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	if origin != "" {
		req.Header.Set("Origin", origin)
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}
func TestQQImageDiscoveryHTTPAdministratorOriginAndStrictInput(t *testing.T) {
	for _, action := range []string{"start", "close", "confirm"} {
		t.Run(action, func(t *testing.T) {
			s := &qqDiscoveryHTTPStore{}
			h := &QQImageHandler{store: s, cfg: &config.Config{Server: config.ServerConfig{FrontendURL: "https://site.example.com"}}}
			fn := map[string]gin.HandlerFunc{"start": h.AdminStartDiscovery, "close": h.AdminCloseDiscovery, "confirm": h.AdminConfirmDiscovery}[action]
			for _, tc := range []struct {
				uid                int64
				role, origin, body string
				status             int
			}{
				{0, "", "https://site.example.com", "{}", 401},
				{42, "user", "https://site.example.com", "{}", 403},
				{42, service.RoleAdmin, "https://evil.example.com", "{}", 403},
				{42, service.RoleAdmin, "", "{}", 403},
				{42, service.RoleAdmin, "null", "{}", 403},
				{42, service.RoleAdmin, "https://site.example.com", `{"group_openid":"untrusted"}`, 400},
				{42, service.RoleAdmin, "https://site.example.com", `{} {}`, 400},
			} {
				w := qqDiscoveryRequest(fn, tc.uid, tc.role, tc.origin, tc.body, "POST")
				require.Equal(t, tc.status, w.Code, w.Body.String())
				require.Zero(t, s.calls)
			}
			w := qqDiscoveryRequest(fn, 42, service.RoleAdmin, "https://site.example.com", "{}", "POST")
			require.Equal(t, 200, w.Code, w.Body.String())
			require.Equal(t, int64(42), s.actor)
			require.Equal(t, 1, s.calls)
			require.Equal(t, "no-store", w.Header().Get("Cache-Control"))
			if action != "start" {
				require.NotContains(t, w.Body.String(), "command")
			}
		})
	}
}
func TestQQImageDiscoveryHTTPGetAdministratorOnlyAndNoCode(t *testing.T) {
	s := &qqDiscoveryHTTPStore{}
	h := &QQImageHandler{store: s}
	w := qqDiscoveryRequest(h.AdminDiscovery, 42, "user", "", "", "GET")
	require.Equal(t, 403, w.Code)
	require.Zero(t, s.calls)
	w = qqDiscoveryRequest(h.AdminDiscovery, 42, service.RoleAdmin, "", "", "GET")
	require.Equal(t, 200, w.Code, w.Body.String())
	require.NotContains(t, w.Body.String(), "command")
	require.Equal(t, "no-store", w.Header().Get("Cache-Control"))
}
