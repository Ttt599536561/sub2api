package handler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type welfareHandlerStub struct {
	welfareHandlerService
	userID        int64
	key           string
	filter        service.WelfareRecordFilter
	settingsCalls int
}

func (s *welfareHandlerStub) Draw(_ context.Context, id int64, key string) (*service.WelfareOperation, error) {
	s.userID, s.key = id, key
	return &service.WelfareOperation{OperationID: "operation", Status: "committed"}, nil
}
func (s *welfareHandlerStub) Records(_ context.Context, id int64, filter service.WelfareRecordFilter) (*service.WelfareRecords, error) {
	s.userID, s.filter = id, filter
	return &service.WelfareRecords{Items: []service.WelfareRecord{}, Page: filter.Page, PageSize: filter.PageSize}, nil
}
func (s *welfareHandlerStub) UpdateSettings(_ context.Context, enabled bool) (*service.WelfareSettings, error) {
	s.settingsCalls++
	return &service.WelfareSettings{Enabled: enabled, RulesVersion: 2}, nil
}

func welfareRequest(h *WelfareHandler, method, path, body, key string, userID int64, admin bool, handle gin.HandlerFunc) *httptest.ResponseRecorder {
	r := gin.New()
	r.Use(func(c *gin.Context) {
		if userID > 0 {
			c.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: userID})
			role := "user"
			if admin {
				role = "admin"
			}
			c.Set(string(middleware.ContextKeyUserRole), role)
		}
	})
	r.Handle(method, "/welfare", handle)
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Idempotency-Key", key)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func TestWelfareDrawRequiresAuthenticatedSubjectAndIdempotency(t *testing.T) {
	s := &welfareHandlerStub{}
	h := &WelfareHandler{svc: s}
	w := welfareRequest(h, "POST", "/welfare", "{}", "operation-key", 0, false, h.Draw)
	require.Equal(t, http.StatusUnauthorized, w.Code)
	w = welfareRequest(h, "POST", "/welfare", "{}", "", 42, false, h.Draw)
	require.Equal(t, http.StatusBadRequest, w.Code)
	w = welfareRequest(h, "POST", "/welfare", `{"user_id":999}`, "same-operation", 42, false, h.Draw)
	require.Equal(t, http.StatusOK, w.Code)
	require.Equal(t, int64(42), s.userID)
	require.Equal(t, "same-operation", s.key)
}
func TestWelfareRecordsValidatePaginationAndUseCurrentUser(t *testing.T) {
	s := &welfareHandlerStub{}
	h := &WelfareHandler{svc: s}
	w := welfareRequest(h, "GET", "/welfare?page=0", "", "", 42, false, h.Records)
	require.Equal(t, http.StatusBadRequest, w.Code)
	w = welfareRequest(h, "GET", "/welfare?type=draw&date_from=2026-09-01&date_to=2026-09-19&page=2&page_size=25", "", "", 42, false, h.Records)
	require.Equal(t, http.StatusOK, w.Code)
	require.Equal(t, int64(42), s.userID)
	require.Equal(t, "draw", s.filter.Type)
	require.Equal(t, "2026-09-01", s.filter.DateFrom)
	require.Equal(t, 2, s.filter.Page)
}
func TestWelfareAdminSettingsRequireAdminAndExplicitBoolean(t *testing.T) {
	s := &welfareHandlerStub{}
	h := &WelfareHandler{svc: s}
	w := welfareRequest(h, "PUT", "/welfare", `{"enabled":true}`, "", 42, false, h.UpdateSettings)
	require.Equal(t, http.StatusForbidden, w.Code)
	w = welfareRequest(h, "PUT", "/welfare", `{}`, "", 42, true, h.UpdateSettings)
	require.Equal(t, http.StatusBadRequest, w.Code)
	w = welfareRequest(h, "PUT", "/welfare", `{"enabled":false}`, "", 42, true, h.UpdateSettings)
	require.Equal(t, http.StatusOK, w.Code)
	require.Equal(t, 1, s.settingsCalls)
}
func TestWelfareUserActionsDisabledInSimpleMode(t *testing.T) {
	h := &WelfareHandler{svc: &welfareHandlerStub{}, cfg: &config.Config{RunMode: config.RunModeSimple}}
	w := welfareRequest(h, "POST", "/welfare", `{}`, "operation", 42, false, h.Draw)
	require.Equal(t, http.StatusForbidden, w.Code)
}
