package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/qqimage"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type replyHTTPStore struct {
	qqImageStore
	calls int
	actor int64
	err   error
}

func (s *replyHTTPStore) GetReplyTemplates(context.Context) (*qqimage.ReplyTemplateSnapshot, error) {
	s.calls++
	return &qqimage.ReplyTemplateSnapshot{SchemaVersion: 1, CatalogVersion: 1, Revision: 2, Overrides: map[string]string{}, Catalog: qqimage.ReplyTemplateCatalog()}, s.err
}
func (s *replyHTTPStore) SaveReplyTemplates(ctx context.Context, expected int64, overrides map[string]string, actor int64) (*qqimage.ReplyTemplateSnapshot, error) {
	s.actor = actor
	return s.GetReplyTemplates(ctx)
}
func (s *replyHTTPStore) RollbackReplyTemplates(ctx context.Context, expected, revision, actor int64) (*qqimage.ReplyTemplateSnapshot, error) {
	s.actor = actor
	return s.GetReplyTemplates(ctx)
}
func (s *replyHTTPStore) ListReplyTemplateRevisions(context.Context) ([]qqimage.ReplyTemplateRevisionInfo, error) {
	s.calls++
	return []qqimage.ReplyTemplateRevisionInfo{{Revision: 2, ActorID: 42, CreatedAt: time.Now()}}, s.err
}
func (s *replyHTTPStore) GetReplyTemplateRevision(context.Context, int64) (*qqimage.ReplyTemplateRevision, error) {
	s.calls++
	return &qqimage.ReplyTemplateRevision{Overrides: map[string]string{}}, s.err
}

func TestQQReplyTemplatesAdminAuthOriginAndValidation(t *testing.T) {
	s := &replyHTTPStore{}
	h := &QQImageHandler{store: s, cfg: &config.Config{Server: config.ServerConfig{FrontendURL: "https://site.example.com"}}}
	for _, fn := range []gin.HandlerFunc{h.AdminReplyTemplates, h.AdminReplyTemplateRevisions, h.AdminReplyTemplateRevision} {
		require.Equal(t, 401, qqHTTPRequest(h, "GET", "/item/1", "", 0, "", fn).Code)
		require.Equal(t, 403, qqHTTPRequest(h, "GET", "/item/1", "", 42, "user", fn).Code)
	}
	for _, tc := range []struct {
		fn   gin.HandlerFunc
		body string
	}{
		{h.SaveAdminReplyTemplates, `{"expected_revision":0,"overrides":{}}`},
		{h.RollbackAdminReplyTemplates, `{"expected_revision":0,"revision":1}`},
		{h.PreviewAdminReplyTemplate, `{"key":"iq.ack","text":"hello"}`},
	} {
		for _, origin := range []string{"", "https://evil.test", "null"} {
			require.Equal(t, 403, qqDiscoveryRequest(tc.fn, 42, "admin", origin, tc.body, "POST").Code)
		}
	}
	require.Zero(t, s.calls)
	for _, body := range []string{`{}`, `{"expected_revision":0,"overrides":null}`, `{"expected_revision":0,"overrides":{"unknown":"secret@example.com"}}`, `{"expected_revision":0,"overrides":{"iq.caption":"{{unknown}}"}}`, `{"expected_revision":0,"overrides":{},"token":"bad"}`} {
		w := qqDiscoveryRequest(h.SaveAdminReplyTemplates, 42, "admin", "https://site.example.com", body, "PUT")
		require.Equal(t, 400, w.Code, w.Body.String())
		require.NotContains(t, w.Body.String(), "secret@example.com")
	}
	require.Zero(t, s.calls)
	w := qqDiscoveryRequest(h.SaveAdminReplyTemplates, 42, "admin", "https://site.example.com", `{"expected_revision":0,"overrides":{"iq.ack":"hello"}}`, "PUT")
	require.Equal(t, 200, w.Code, w.Body.String())
	require.Equal(t, int64(42), s.actor)
	require.Equal(t, "no-store", w.Header().Get("Cache-Control"))
	s.err = qqimage.ErrConflict
	w = qqDiscoveryRequest(h.SaveAdminReplyTemplates, 42, "admin", "https://site.example.com", `{"expected_revision":0,"overrides":{}}`, "PUT")
	require.Equal(t, 409, w.Code)
}
func TestQQReplyTemplatePreviewIsPureFakeData(t *testing.T) {
	s := &replyHTTPStore{}
	h := &QQImageHandler{store: s, cfg: &config.Config{Server: config.ServerConfig{FrontendURL: "https://site.example.com"}}}
	w := qqDiscoveryRequest(h.PreviewAdminReplyTemplate, 42, "admin", "https://site.example.com", `{"key":"order.accepted","text":"订单{{order_id}} {{price}} {{settlement_rules}} {{order_url}}"}`, "POST")
	require.Equal(t, 200, w.Code, w.Body.String())
	require.Contains(t, w.Body.String(), "example-order")
	require.Contains(t, w.Body.String(), "example.invalid")
	require.Zero(t, s.calls)
	w = qqDiscoveryRequest(h.PreviewAdminReplyTemplate, 42, "admin", "https://site.example.com", `{"key":"order.accepted","text":"no facts"}`, "POST")
	require.Equal(t, 200, w.Code)
	require.Contains(t, w.Body.String(), `"valid":true`)
	require.Contains(t, w.Body.String(), `"text":"no facts"`)
	w = qqDiscoveryRequest(h.PreviewAdminReplyTemplate, 42, "admin", "https://site.example.com", `{"key":"order.accepted","text":"{{unknown}}"}`, "POST")
	require.Equal(t, 200, w.Code)
	require.Contains(t, w.Body.String(), `"valid":false`)
	w = qqDiscoveryRequest(h.PreviewAdminReplyTemplate, 42, "admin", "https://site.example.com", `{"key":"image.help","text":"{{availability}}"}`, "POST")
	require.Equal(t, 200, w.Code)
	require.Contains(t, w.Body.String(), `"valid":false`)
	require.Contains(t, w.Body.String(), `"empty_rendered_text"`)
	require.Zero(t, s.calls)
}

func TestQQReplyTemplateFullCatalogMaximumLengthSave(t *testing.T) {
	s := &replyHTTPStore{}
	h := &QQImageHandler{store: s, cfg: &config.Config{Server: config.ServerConfig{FrontendURL: "https://site.example.com"}}}
	overrides := map[string]string{}
	for _, def := range qqimage.ReplyTemplateCatalog() {
		_, worst, errs := qqimage.ValidateReplyTemplate(def.Key, def.DefaultText)
		require.Empty(t, errs)
		overrides[def.Key] = strings.Repeat("文", qqimage.ReplyTemplateMaxLength-worst) + def.DefaultText
	}
	body, err := json.Marshal(map[string]any{"expected_revision": 0, "overrides": overrides})
	require.NoError(t, err)
	require.Greater(t, len(body), 64<<10)
	w := qqDiscoveryRequest(h.SaveAdminReplyTemplates, 42, "admin", "https://site.example.com", string(body), "PUT")
	require.Equal(t, 200, w.Code, w.Body.String())
}
func publishedRequest(h *QQImageHandler, auth string) *httptest.ResponseRecorder {
	r := gin.New()
	r.GET("/published", h.PublishedReplyTemplates)
	req := httptest.NewRequest("GET", "/published", nil)
	req.Header.Set("Authorization", auth)
	req.AddCookie(&http.Cookie{Name: "session", Value: "admin"})
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}
func TestQQReplyTemplatesMachineIndependentSecretAndMinimalShape(t *testing.T) {
	s := &replyHTTPStore{}
	h := &QQImageHandler{store: s}
	for _, token := range []string{"", "too-short"} {
		t.Setenv("QQ_REPLY_TEMPLATE_SYNC_TOKEN", token)
		require.Equal(t, 503, publishedRequest(h, "Bearer "+token).Code)
	}
	token := strings.Repeat("s", 32)
	t.Setenv("QQ_REPLY_TEMPLATE_SYNC_TOKEN", token)
	for _, auth := range []string{"", "Bearer wrong", "Basic " + token} {
		require.Equal(t, 401, publishedRequest(h, auth).Code)
	}
	require.Zero(t, s.calls)
	w := publishedRequest(h, "Bearer "+token)
	require.Equal(t, 200, w.Code, w.Body.String())
	require.Equal(t, "no-store", w.Header().Get("Cache-Control"))
	var body map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	require.Len(t, body, 5)
	var templates map[string]string
	require.NoError(t, json.Unmarshal(body["templates"], &templates))
	require.Len(t, templates, 6)
	require.NotContains(t, w.Body.String(), "actor_id")
	require.NotContains(t, w.Body.String(), "catalog\"")
	require.NotContains(t, w.Body.String(), "binding.request")
}

type qqReplyAuditCapture struct {
	service.AuditLogRepository
	mu   sync.Mutex
	logs []*service.AuditLog
}

func (r *qqReplyAuditCapture) BatchInsert(_ context.Context, logs []*service.AuditLog) (int64, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.logs = append(r.logs, logs...)
	return int64(len(logs)), nil
}

func TestQQReplyTemplatePreviewAuditKeepsDraftsPrivate(t *testing.T) {
	gin.SetMode(gin.TestMode)
	const canary = "unsaved-reply-draft-canary"
	for _, tc := range []struct {
		name, role, origin, body string
		uid                      int64
		status                   int
		wantAudit                bool
	}{
		{"successful preview", "admin", "https://site.example.com", `{"key":"iq.ack","text":"` + canary + `"}`, 42, 200, false},
		{"template validation feedback", "admin", "https://site.example.com", `{"key":"common.menu","text":"` + canary + `"}`, 42, 200, false},
		{"malformed JSON", "admin", "https://site.example.com", `{"text":"` + canary + `",`, 42, 400, true},
		{"unknown input field", "admin", "https://site.example.com", `{"key":"iq.ack","text":"` + canary + `","extra":true}`, 42, 400, true},
		{"unauthenticated", "", "https://site.example.com", `{"key":"iq.ack","text":"` + canary + `"}`, 0, 401, true},
		{"non administrator", "user", "https://site.example.com", `{"key":"iq.ack","text":"` + canary + `"}`, 42, 403, true},
		{"cross origin", "admin", "https://evil.example.com", `{"key":"iq.ack","text":"` + canary + `"}`, 42, 403, true},
		{"missing origin", "admin", "", `{"key":"iq.ack","text":"` + canary + `"}`, 42, 403, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			store := &replyHTTPStore{}
			h := &QQImageHandler{store: store, cfg: &config.Config{Server: config.ServerConfig{FrontendURL: "https://site.example.com"}}}
			repository := &qqReplyAuditCapture{}
			audit := service.NewAuditLogService(repository, nil)
			audit.Start()
			router := gin.New()
			router.Use(func(c *gin.Context) {
				if tc.uid > 0 {
					c.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: tc.uid})
					c.Set(string(middleware.ContextKeyUserRole), tc.role)
				}
				c.Next()
			})
			// Real audit middleware surrounds the real handler, including its early failures.
			router.Use(gin.HandlerFunc(middleware.NewAuditLogMiddleware(audit)))
			router.POST("/api/v1/admin/qq-image/reply-templates/preview", h.PreviewAdminReplyTemplate)
			request := httptest.NewRequest(http.MethodPost, "/api/v1/admin/qq-image/reply-templates/preview", strings.NewReader(tc.body))
			request.Header.Set("Content-Type", "application/json")
			request.Header.Set("Origin", tc.origin)
			recorder := httptest.NewRecorder()
			router.ServeHTTP(recorder, request)
			audit.Stop()
			require.Equal(t, tc.status, recorder.Code)
			require.Zero(t, store.calls)
			if tc.wantAudit {
				require.Len(t, repository.logs, 1)
				entry := repository.logs[0]
				require.Equal(t, tc.status, entry.StatusCode)
				require.Equal(t, "<credential-bearing body omitted>", entry.RequestBody)
				encoded, err := json.Marshal(entry)
				require.NoError(t, err)
				require.NotContains(t, string(encoded), canary)
			} else {
				require.Empty(t, repository.logs, "pure preview must not persist a draft or an audit row")
			}
		})
	}
}

func TestQQReplyTemplateWritesRemainAudited(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, tc := range []struct{ method, path, body string }{
		{http.MethodPut, "/api/v1/admin/qq-image/reply-templates", `{"expected_revision":0,"overrides":{"iq.ack":"saved reply"}}`},
		{http.MethodPost, "/api/v1/admin/qq-image/reply-templates/rollback", `{"expected_revision":2,"revision":1}`},
	} {
		t.Run(tc.method+" "+tc.path, func(t *testing.T) {
			store := &replyHTTPStore{}
			h := &QQImageHandler{store: store, cfg: &config.Config{Server: config.ServerConfig{FrontendURL: "https://site.example.com"}}}
			repository := &qqReplyAuditCapture{}
			audit := service.NewAuditLogService(repository, nil)
			audit.Start()
			router := gin.New()
			router.Use(func(c *gin.Context) {
				c.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: 42})
				c.Set(string(middleware.ContextKeyUserRole), "admin")
				c.Next()
			})
			router.Use(gin.HandlerFunc(middleware.NewAuditLogMiddleware(audit)))
			router.PUT("/api/v1/admin/qq-image/reply-templates", h.SaveAdminReplyTemplates)
			router.POST("/api/v1/admin/qq-image/reply-templates/rollback", h.RollbackAdminReplyTemplates)
			request := httptest.NewRequest(tc.method, tc.path, strings.NewReader(tc.body))
			request.Header.Set("Content-Type", "application/json")
			request.Header.Set("Origin", "https://site.example.com")
			recorder := httptest.NewRecorder()
			router.ServeHTTP(recorder, request)
			audit.Stop()
			require.Equal(t, http.StatusOK, recorder.Code)
			require.Len(t, repository.logs, 1)
			require.Equal(t, tc.path, repository.logs[0].Path)
			require.Equal(t, http.StatusOK, repository.logs[0].StatusCode)
			require.NotEmpty(t, repository.logs[0].RequestBody)
			require.Equal(t, int64(42), *repository.logs[0].ActorUserID)
		})
	}
}
