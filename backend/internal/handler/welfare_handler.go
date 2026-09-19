package handler

import (
	"context"
	"strconv"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

type welfareHandlerService interface {
	Overview(context.Context, int64) (*service.WelfareOverview, error)
	Calendar(context.Context, int64, string) (*service.WelfareCalendar, error)
	CheckIn(context.Context, int64) (*service.WelfareOperation, error)
	Draw(context.Context, int64, string) (*service.WelfareOperation, error)
	Quote(context.Context, int64, string, string) (*service.WelfareQuote, error)
	Redeem(context.Context, int64, string, string, int64) (*service.WelfareOperation, error)
	Records(context.Context, int64, service.WelfareRecordFilter) (*service.WelfareRecords, error)
	Operation(context.Context, int64, string) (*service.WelfareOperation, error)
	OperationByKey(context.Context, int64, string, string) (*service.WelfareOperation, error)
	Rules(context.Context) (*service.WelfareRules, error)
	GetSettings(context.Context) (*service.WelfareSettings, error)
	UpdateSettings(context.Context, bool) (*service.WelfareSettings, error)
}

// WelfareHandler only accepts identity from the authenticated middleware.
// Financial results use explicit DTOs; internal rules and random inputs never
// pass through the HTTP layer.
type WelfareHandler struct {
	svc         welfareHandlerService
	settings    *service.SettingService
	cfg         *config.Config
	afterCredit func(context.Context, int64)
}

func NewWelfareHandler(svc *service.WelfareService, settings *service.SettingService, cfg *config.Config) *WelfareHandler {
	return &WelfareHandler{svc: svc, settings: settings, cfg: cfg}
}

func (h *WelfareHandler) user(c *gin.Context) (int64, bool) {
	subject, ok := middleware.GetAuthSubjectFromContext(c)
	if !ok || subject.UserID <= 0 {
		response.Unauthorized(c, "User not authenticated")
		return 0, false
	}
	if h.cfg != nil && h.cfg.RunMode == config.RunModeSimple {
		response.Forbidden(c, "Welfare is unavailable in simple mode")
		return 0, false
	}
	return subject.UserID, true
}
func (h *WelfareHandler) admin(c *gin.Context) bool {
	if _, ok := middleware.GetAuthSubjectFromContext(c); !ok {
		response.Unauthorized(c, "User not authenticated")
		return false
	}
	role, ok := middleware.GetUserRoleFromContext(c)
	if !ok || role != service.RoleAdmin {
		response.Forbidden(c, "Administrator access required")
		return false
	}
	return true
}
func welfareKey(c *gin.Context) (string, bool) {
	key := strings.TrimSpace(c.GetHeader("Idempotency-Key"))
	if key == "" || len(key) > 128 {
		response.ErrorFrom(c, service.ErrWelfareInvalidRequest)
		return "", false
	}
	return key, true
}
func welfareResult(c *gin.Context, result any, err error) {
	if response.ErrorFrom(c, err) {
		return
	}
	response.Success(c, result)
}

func (h *WelfareHandler) Overview(c *gin.Context) {
	id, ok := h.user(c)
	if !ok {
		return
	}
	result, err := h.svc.Overview(c.Request.Context(), id)
	welfareResult(c, result, err)
}
func (h *WelfareHandler) Calendar(c *gin.Context) {
	id, ok := h.user(c)
	if !ok {
		return
	}
	result, err := h.svc.Calendar(c.Request.Context(), id, c.Query("month"))
	welfareResult(c, result, err)
}
func (h *WelfareHandler) CheckIn(c *gin.Context) {
	id, ok := h.user(c)
	if !ok {
		return
	}
	result, err := h.svc.CheckIn(c.Request.Context(), id)
	welfareResult(c, result, err)
}
func (h *WelfareHandler) Draw(c *gin.Context) {
	id, ok := h.user(c)
	if !ok {
		return
	}
	key, ok := welfareKey(c)
	if !ok {
		return
	}
	result, err := h.svc.Draw(c.Request.Context(), id, key)
	welfareResult(c, result, err)
}
func (h *WelfareHandler) Quote(c *gin.Context) {
	id, ok := h.user(c)
	if !ok {
		return
	}
	var req struct {
		Mode   string `json:"mode" binding:"required"`
		Amount string `json:"amount"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		response.ErrorFrom(c, service.ErrWelfareInvalidRequest)
		return
	}
	result, err := h.svc.Quote(c.Request.Context(), id, req.Mode, req.Amount)
	welfareResult(c, result, err)
}
func (h *WelfareHandler) Redeem(c *gin.Context) {
	id, ok := h.user(c)
	if !ok {
		return
	}
	key, ok := welfareKey(c)
	if !ok {
		return
	}
	var req struct {
		Amount         string `json:"amount" binding:"required"`
		BalanceVersion *int64 `json:"welfare_balance_version"`
	}
	if err := c.ShouldBindJSON(&req); err != nil || req.BalanceVersion == nil || *req.BalanceVersion < 0 {
		response.ErrorFrom(c, service.ErrWelfareInvalidRequest)
		return
	}
	result, err := h.svc.Redeem(c.Request.Context(), id, key, req.Amount, *req.BalanceVersion)
	if err == nil && h.afterCredit != nil {
		h.afterCredit(c.Request.Context(), id)
	}
	welfareResult(c, result, err)
}
func (h *WelfareHandler) Operation(c *gin.Context) {
	id, ok := h.user(c)
	if !ok {
		return
	}
	result, err := h.svc.Operation(c.Request.Context(), id, c.Param("id"))
	welfareResult(c, result, err)
}

func (h *WelfareHandler) OperationByKey(c *gin.Context) {
	id, ok := h.user(c)
	if !ok {
		return
	}
	result, err := h.svc.OperationByKey(c.Request.Context(), id, c.Query("type"), c.Query("idempotency_key"))
	welfareResult(c, result, err)
}
func welfarePage(c *gin.Context, key string, fallback, max int) (int, bool) {
	raw := c.Query(key)
	if raw == "" {
		return fallback, true
	}
	value, err := strconv.Atoi(raw)
	if err != nil || value < 1 || value > max {
		response.ErrorFrom(c, service.ErrWelfareInvalidRequest)
		return 0, false
	}
	return value, true
}
func (h *WelfareHandler) Records(c *gin.Context) {
	id, ok := h.user(c)
	if !ok {
		return
	}
	page, ok := welfarePage(c, "page", 1, 1000000)
	if !ok {
		return
	}
	size, ok := welfarePage(c, "page_size", 20, 100)
	if !ok {
		return
	}
	filter := service.WelfareRecordFilter{Type: c.DefaultQuery("type", "all"), DateFrom: c.Query("date_from"), DateTo: c.Query("date_to"), Page: page, PageSize: size}
	result, err := h.svc.Records(c.Request.Context(), id, filter)
	welfareResult(c, result, err)
}
func (h *WelfareHandler) Rules(c *gin.Context) {
	if _, ok := h.user(c); !ok {
		return
	}
	result, err := h.svc.Rules(c.Request.Context())
	welfareResult(c, result, err)
}
func (h *WelfareHandler) GetSettings(c *gin.Context) {
	if !h.admin(c) {
		return
	}
	result, err := h.svc.GetSettings(c.Request.Context())
	welfareResult(c, result, err)
}
func (h *WelfareHandler) UpdateSettings(c *gin.Context) {
	if !h.admin(c) {
		return
	}
	var req struct {
		Enabled *bool `json:"enabled"`
	}
	if err := c.ShouldBindJSON(&req); err != nil || req.Enabled == nil {
		response.ErrorFrom(c, service.ErrWelfareInvalidRequest)
		return
	}
	result, err := h.svc.UpdateSettings(c.Request.Context(), *req.Enabled)
	if err == nil && h.settings != nil {
		h.settings.NotifyWelfareSettingsChanged()
	}
	welfareResult(c, result, err)
}
