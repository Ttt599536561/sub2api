package handler

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/qqimage"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

type qqImageStore interface {
	BusinessControls(context.Context) (bool, bool, error)
	StartDiscovery(context.Context, int64) (*qqimage.Discovery, error)
	GetDiscovery(context.Context) (*qqimage.Discovery, error)
	CloseDiscovery(context.Context, string, int64) (*qqimage.Discovery, error)
	ConfirmDiscovery(context.Context, string, int64) (*qqimage.Discovery, error)
	GetConfig(context.Context) (*qqimage.Config, error)
	SaveConfig(context.Context, qqimage.Config, int64) (*qqimage.Config, error)
	CurrentEmail(context.Context, int64) (string, error)
	EmailVerified(context.Context, int64) (bool, error)
	MarkEmailVerified(context.Context, int64, string) error
	RecentAuthorization(context.Context, int64, string) (bool, error)
	MarkRecentAuthorization(context.Context, int64, string) error
	GetBindingRequest(context.Context, string) (*qqimage.BindingRequest, error)
	AuthorizeBinding(context.Context, string, int64, string) (*qqimage.BindingAuthorization, error)
	ListGrants(context.Context, int64) ([]qqimage.Grant, error)
	RevokeGrant(context.Context, string, int64, string) error
	RevokeGrantAsAdmin(context.Context, string, int64, string) error
	GetOrder(context.Context, string, int64) (*qqimage.Order, error)
	ListOrders(context.Context, int64, int) ([]qqimage.Order, error)
	CancelOrder(context.Context, string, int64) (*qqimage.Order, error)
	RefundOrder(context.Context, string, int64, string) (*qqimage.Order, error)
	ResolveOrderFailure(context.Context, string, int64, string) (*qqimage.Order, error)
	RequestReconcile(context.Context, string, int64) error
	GetArtifact(context.Context, string, int64) (*qqimage.Artifact, error)
	ReadMedia(context.Context, string) (*qqimage.Artifact, error)
	ListAudit(context.Context, int) ([]qqimage.Audit, error)
}
type qqImageEmail interface {
	SendQQImageEmailCode(context.Context, int64, ...string) (*service.SendVerifyCodeResult, error)
	VerifyOAuthEmailCode(context.Context, string, string) error
}
type QQImageHandler struct {
	connection func() qqimage.GatewayStatus
	store      qqImageStore
	email      qqImageEmail
	settings   *service.SettingService
	cfg        *config.Config
	webhook    func(context.Context, http.Header, []byte) (any, error)
}

func (h *QQImageHandler) user(c *gin.Context) (int64, bool) {
	c.Header("Cache-Control", "no-store")
	c.Header("Referrer-Policy", "no-referrer")
	subject, ok := middleware.GetAuthSubjectFromContext(c)
	if !ok || subject.UserID <= 0 {
		response.Unauthorized(c, "User not authenticated")
		return 0, false
	}
	return subject.UserID, true
}
func (h *QQImageHandler) admin(c *gin.Context) (int64, bool) {
	id, ok := h.user(c)
	if !ok {
		return 0, false
	}
	role, ok := middleware.GetUserRoleFromContext(c)
	if !ok || role != service.RoleAdmin {
		response.Forbidden(c, "Administrator access required")
		return 0, false
	}
	return id, true
}
func (h *QQImageHandler) mutation(c *gin.Context) bool {
	origin := ""
	if h.settings != nil {
		origin = h.settings.GetFrontendURL(c.Request.Context())
	} else if h.cfg != nil {
		origin = h.cfg.Server.FrontendURL
	}
	if !qqImageMutationAllowed(c, origin, h.cfg) {
		response.Forbidden(c, "Same-origin request required")
		return false
	}
	return true
}
func qqImageResult(c *gin.Context, value any, err error) {
	if err == nil {
		response.Success(c, value)
		return
	}
	switch {
	case errors.Is(err, qqimage.ErrNotFound):
		response.ErrorWithDetails(c, 404, "QQ image resource not found", "QQ_IMAGE_NOT_FOUND", nil)
	case errors.Is(err, qqimage.ErrForbidden):
		response.ErrorWithDetails(c, 403, "QQ image authorization or verified email required", "QQ_IMAGE_FORBIDDEN", nil)
	case errors.Is(err, qqimage.ErrConflict):
		response.ErrorWithDetails(c, 409, "QQ image state changed or is already bound", "QQ_IMAGE_CONFLICT", nil)
	case errors.Is(err, qqimage.ErrExpired):
		response.ErrorWithDetails(c, 410, "QQ image request expired", "QQ_IMAGE_EXPIRED", nil)
	case errors.Is(err, qqimage.ErrDisabled):
		response.ErrorWithDetails(c, 403, "QQ image entry is disabled", "QQ_IMAGE_DISABLED", nil)
	case errors.Is(err, qqimage.ErrInvalid):
		response.ErrorWithDetails(c, 400, "Invalid QQ image request", "QQ_IMAGE_INVALID", nil)
	case errors.Is(err, qqimage.ErrInsufficientBalance):
		response.ErrorWithDetails(c, 409, "Insufficient available account balance", "QQ_IMAGE_INSUFFICIENT_BALANCE", nil)
	case errors.Is(err, qqimage.ErrLimit):
		response.ErrorWithDetails(c, 429, "QQ image rate or quota limit reached", "QQ_IMAGE_LIMIT", nil)
	case errors.Is(err, qqimage.ErrEncryptionUnavailable):
		response.ErrorWithDetails(c, 503, "QQ image encryption key is not configured", "QQ_IMAGE_ENCRYPTION_UNAVAILABLE", nil)
	default:
		response.Error(c, http.StatusInternalServerError, "QQ image operation could not be completed")
	}
}
func (h *QQImageHandler) recent(c *gin.Context, id int64) bool {
	session := qqImageSessionKey(c)
	if session == "" {
		response.Forbidden(c, "Authenticated session required")
		return false
	}
	ok, err := h.store.RecentAuthorization(c.Request.Context(), id, session)
	if err != nil {
		qqImageResult(c, nil, err)
		return false
	}
	if !ok {
		response.ErrorWithDetails(c, 403, "Confirm your current email in this session before authorizing or unlinking QQ", "QQ_IMAGE_RECENT_AUTH_REQUIRED", nil)
		return false
	}
	return true
}
func (h *QQImageHandler) Status(c *gin.Context) {
	id, ok := h.user(c)
	if !ok {
		return
	}
	ctx := c.Request.Context()
	conf, err := h.store.GetConfig(ctx)
	if err != nil {
		qqImageResult(c, nil, err)
		return
	}
	email, err := h.store.CurrentEmail(ctx, id)
	if err != nil {
		qqImageResult(c, nil, err)
		return
	}
	verified, err := h.store.EmailVerified(ctx, id)
	if err != nil {
		qqImageResult(c, nil, err)
		return
	}
	recent, err := h.store.RecentAuthorization(ctx, id, qqImageSessionKey(c))
	if err != nil {
		qqImageResult(c, nil, err)
		return
	}
	grants, err := h.store.ListGrants(ctx, id)
	if err != nil {
		qqImageResult(c, nil, err)
		return
	}
	bindingEnabled, imageEnabled, err := h.store.BusinessControls(ctx)
	if err != nil {
		qqImageResult(c, nil, err)
		return
	}
	enabled := conf.Enabled && (h.cfg == nil || h.cfg.RunMode != config.RunModeSimple)
	qqImageResult(c, gin.H{"enabled": enabled, "binding_enabled": enabled && bindingEnabled, "image_enabled": enabled && imageEnabled, "email": email, "email_verified": verified, "recent_authorization": recent, "grants": grants}, nil)
}
func (h *QQImageHandler) SendEmailCode(c *gin.Context) {
	id, ok := h.user(c)
	if !ok || !h.mutation(c) {
		return
	}
	if h.email == nil || qqImageSessionKey(c) == "" {
		response.Error(c, 503, "Email verification is unavailable")
		return
	}
	result, err := h.email.SendQQImageEmailCode(c.Request.Context(), id, c.GetHeader("Accept-Language"))
	if response.ErrorFrom(c, err) {
		return
	}
	response.Success(c, result)
}
func (h *QQImageHandler) VerifyEmail(c *gin.Context) {
	id, ok := h.user(c)
	if !ok || !h.mutation(c) {
		return
	}
	var input struct {
		Code string `json:"code"`
	}
	if qqImageDecode(c, &input) != nil || len(input.Code) != 6 {
		qqImageResult(c, nil, qqimage.ErrInvalid)
		return
	}
	session := qqImageSessionKey(c)
	if h.email == nil || session == "" {
		response.Error(c, 503, "Email verification is unavailable")
		return
	}
	email, err := h.store.CurrentEmail(c.Request.Context(), id)
	if err != nil {
		qqImageResult(c, nil, err)
		return
	}
	if err = h.email.VerifyOAuthEmailCode(c.Request.Context(), email, input.Code); response.ErrorFrom(c, err) {
		return
	}
	if err = h.store.MarkEmailVerified(c.Request.Context(), id, email); err != nil {
		qqImageResult(c, nil, err)
		return
	}
	err = h.store.MarkRecentAuthorization(c.Request.Context(), id, session)
	qqImageResult(c, gin.H{"email_verified": true, "recent_authorization": true}, err)
}
func (h *QQImageHandler) BindingRequest(c *gin.Context) {
	if _, ok := h.user(c); !ok {
		return
	}
	result, err := h.store.GetBindingRequest(c.Request.Context(), c.Param("id"))
	qqImageResult(c, result, err)
}
func (h *QQImageHandler) AuthorizeBinding(c *gin.Context) {
	if h.cfg != nil && h.cfg.RunMode == config.RunModeSimple {
		qqImageResult(c, nil, qqimage.ErrDisabled)
		return
	}
	id, ok := h.user(c)
	if !ok || !h.mutation(c) {
		return
	}
	var input struct {
		Consent bool `json:"consent"`
	}
	if qqImageDecode(c, &input) != nil || !input.Consent {
		qqImageResult(c, nil, qqimage.ErrInvalid)
		return
	}
	if !h.recent(c, id) {
		return
	}
	result, err := h.store.AuthorizeBinding(c.Request.Context(), c.Param("id"), id, qqImageSessionKey(c))
	qqImageResult(c, result, err)
}
func (h *QQImageHandler) RevokeGrant(c *gin.Context) {
	id, ok := h.user(c)
	if !ok || !h.mutation(c) || !h.recent(c, id) {
		return
	}
	qqImageResult(c, gin.H{"revoked": true}, h.store.RevokeGrant(c.Request.Context(), c.Param("id"), id, "user_revoked"))
}
func qqImageLimit(c *gin.Context) (int, bool) {
	raw := c.DefaultQuery("limit", "100")
	n, err := strconv.Atoi(raw)
	if err != nil || n < 1 || n > 500 {
		qqImageResult(c, nil, qqimage.ErrInvalid)
		return 0, false
	}
	return n, true
}
func (h *QQImageHandler) Orders(c *gin.Context) {
	id, ok := h.user(c)
	if !ok {
		return
	}
	limit, ok := qqImageLimit(c)
	if !ok {
		return
	}
	result, err := h.store.ListOrders(c.Request.Context(), id, limit)
	qqImageResult(c, result, err)
}
func (h *QQImageHandler) Order(c *gin.Context) {
	id, ok := h.user(c)
	if !ok {
		return
	}
	result, err := h.store.GetOrder(c.Request.Context(), c.Param("id"), id)
	qqImageResult(c, result, err)
}
func (h *QQImageHandler) CancelOrder(c *gin.Context) {
	id, ok := h.user(c)
	if !ok || !h.mutation(c) {
		return
	}
	result, err := h.store.CancelOrder(c.Request.Context(), c.Param("id"), id)
	qqImageResult(c, result, err)
}
func qqImageArtifact(c *gin.Context, artifact *qqimage.Artifact, err error) {
	c.Header("Cache-Control", "no-store")
	c.Header("Referrer-Policy", "no-referrer")
	c.Header("X-Content-Type-Options", "nosniff")
	if err != nil {
		qqImageResult(c, nil, err)
		return
	}
	if artifact == nil {
		qqImageResult(c, nil, qqimage.ErrNotFound)
		return
	}
	c.Data(200, artifact.MIME, artifact.Data)
}
func (h *QQImageHandler) Image(c *gin.Context) {
	id, ok := h.user(c)
	if !ok {
		return
	}
	result, err := h.store.GetArtifact(c.Request.Context(), c.Param("id"), id)
	qqImageArtifact(c, result, err)
}

// AdminConnection returns only controlled operational state; credentials and
// gateway session identifiers never cross this authenticated admin endpoint.
func (h *QQImageHandler) AdminConnection(c *gin.Context) {
	if _, ok := h.admin(c); !ok {
		return
	}
	status := qqimage.GatewayStatus{State: "disabled"}
	if h.connection != nil {
		status = h.connection()
	}
	response.Success(c, status)
}
func (h *QQImageHandler) AdminConfig(c *gin.Context) {
	if _, ok := h.admin(c); !ok {
		return
	}
	result, err := h.store.GetConfig(c.Request.Context())
	if result != nil {
		clone := *result
		clone.APIKey = ""
		clone.BotSecret = ""
		result = &clone
	}
	qqImageResult(c, result, err)
}
func (h *QQImageHandler) SaveAdminConfig(c *gin.Context) {
	id, ok := h.admin(c)
	if !ok || !h.mutation(c) {
		return
	}
	var input qqimage.Config
	if qqImageDecode(c, &input) != nil {
		qqImageConfigValidationResult(c, &qqimage.ConfigValidationError{Field: "request", Issue: "invalid_request"})
		return
	}
	if h.cfg != nil && h.cfg.RunMode == config.RunModeSimple && input.Enabled {
		qqImageResult(c, nil, qqimage.ErrDisabled)
		return
	}
	result, err := h.store.SaveConfig(c.Request.Context(), input, id)
	var validation *qqimage.ConfigValidationError
	if errors.As(err, &validation) {
		qqImageConfigValidationResult(c, validation)
		return
	}
	if result != nil {
		clone := *result
		clone.APIKey = ""
		clone.BotSecret = ""
		result = &clone
	}
	qqImageResult(c, result, err)
}
func qqImageConfigValidationResult(c *gin.Context, validation *qqimage.ConfigValidationError) {
	field, issue := validation.Details()
	response.ErrorWithDetails(c, http.StatusBadRequest, "Invalid QQ image configuration", "QQ_IMAGE_CONFIG_INVALID", map[string]string{"field": field, "issue": issue})
}
func (h *QQImageHandler) AdminOrders(c *gin.Context) {
	if _, ok := h.admin(c); !ok {
		return
	}
	limit, ok := qqImageLimit(c)
	if !ok {
		return
	}
	result, err := h.store.ListOrders(c.Request.Context(), 0, limit)
	qqImageResult(c, result, err)
}
func (h *QQImageHandler) AdminGrants(c *gin.Context) {
	if _, ok := h.admin(c); !ok {
		return
	}
	result, err := h.store.ListGrants(c.Request.Context(), 0)
	qqImageResult(c, result, err)
}
func (h *QQImageHandler) AdminAudit(c *gin.Context) {
	if _, ok := h.admin(c); !ok {
		return
	}
	limit, ok := qqImageLimit(c)
	if !ok {
		return
	}
	result, err := h.store.ListAudit(c.Request.Context(), limit)
	qqImageResult(c, result, err)
}
func (h *QQImageHandler) AdminRevokeGrant(c *gin.Context) {
	id, ok := h.admin(c)
	if !ok || !h.mutation(c) {
		return
	}
	var input struct {
		Reason string `json:"reason"`
	}
	if qqImageDecode(c, &input) != nil || strings.TrimSpace(input.Reason) == "" || len(input.Reason) > 500 {
		qqImageResult(c, nil, qqimage.ErrInvalid)
		return
	}
	qqImageResult(c, gin.H{"revoked": true}, h.store.RevokeGrantAsAdmin(c.Request.Context(), c.Param("id"), id, input.Reason))
}
func (h *QQImageHandler) AdminRefund(c *gin.Context) {
	id, ok := h.admin(c)
	if !ok || !h.mutation(c) {
		return
	}
	var input struct {
		Reason string `json:"reason"`
	}
	if qqImageDecode(c, &input) != nil || strings.TrimSpace(input.Reason) == "" || len(input.Reason) > 500 {
		qqImageResult(c, nil, qqimage.ErrInvalid)
		return
	}
	result, err := h.store.RefundOrder(c.Request.Context(), c.Param("id"), id, input.Reason)
	qqImageResult(c, result, err)
}
func (h *QQImageHandler) AdminReconcile(c *gin.Context) {
	id, ok := h.admin(c)
	if !ok || !h.mutation(c) {
		return
	}
	qqImageResult(c, gin.H{"requested": true}, h.store.RequestReconcile(c.Request.Context(), c.Param("id"), id))
}
func (h *QQImageHandler) AdminImage(c *gin.Context) {
	if _, ok := h.admin(c); !ok {
		return
	}
	order, err := h.store.GetOrder(c.Request.Context(), c.Param("id"), 0)
	if err != nil {
		qqImageResult(c, nil, err)
		return
	}
	result, err := h.store.GetArtifact(c.Request.Context(), order.ID, order.UserID)
	qqImageArtifact(c, result, err)
}

// Media is a narrowly scoped, revocable short-lived platform transfer grant.
// Access logging intentionally uses URL.Path only; deployments must omit query logs.
func (h *QQImageHandler) Media(c *gin.Context) {
	token := c.Query("token")
	if len(token) < 32 || len(token) > 128 {
		qqImageArtifact(c, nil, qqimage.ErrNotFound)
		return
	}
	result, err := h.store.ReadMedia(c.Request.Context(), token)
	qqImageArtifact(c, result, err)
}
func (h *QQImageHandler) Events(c *gin.Context) {
	if h.cfg != nil && h.cfg.RunMode == config.RunModeSimple {
		response.Error(c, 503, "New QQ events are disabled in simple mode; existing website orders remain available")
		return
	}
	c.Header("Cache-Control", "no-store")
	if h.webhook == nil {
		response.Error(c, 503, "QQ event processing unavailable")
		return
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 64<<10)
	raw, err := io.ReadAll(c.Request.Body)
	if err != nil {
		response.Error(c, 413, "Event too large")
		return
	}
	result, err := h.webhook(c.Request.Context(), c.Request.Header, raw)
	if err != nil {
		status := http.StatusInternalServerError
		if errors.Is(err, qqimage.ErrQQAuthentication) || errors.Is(err, qqimage.ErrForbidden) || errors.Is(err, qqimage.ErrExpired) || errors.Is(err, qqimage.ErrConflict) || errors.Is(err, qqimage.ErrInvalid) {
			status = http.StatusForbidden
		}
		if errors.Is(err, qqimage.ErrDisabled) || errors.Is(err, qqimage.ErrEncryptionUnavailable) || errors.Is(err, qqimage.ErrNotFound) {
			status = http.StatusServiceUnavailable
		}
		response.Error(c, status, "QQ event rejected")
		return
	}
	c.JSON(200, result)
}

func (h *QQImageHandler) AdminRelease(c *gin.Context) {
	id, ok := h.admin(c)
	if !ok || !h.mutation(c) {
		return
	}
	var input struct {
		Reason string `json:"reason"`
	}
	if qqImageDecode(c, &input) != nil || strings.TrimSpace(input.Reason) == "" || len(input.Reason) > 500 {
		qqImageResult(c, nil, qqimage.ErrInvalid)
		return
	}
	result, err := h.store.ResolveOrderFailure(c.Request.Context(), c.Param("id"), id, input.Reason)
	qqImageResult(c, result, err)
}
