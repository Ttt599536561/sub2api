package handler

import (
	"context"
	"errors"
	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/napcat"
	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/qqinvite"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
)

type qqInviteStore interface {
	GetConfig(context.Context) (qqinvite.Config, error)
	RuntimeConfig(context.Context) (qqinvite.Config, error)
	SaveConfig(context.Context, qqinvite.Config, int64) (qqinvite.Config, error)
	ListCampaigns(context.Context) (qqinvite.CampaignList, error)
	SaveCampaign(context.Context, qqinvite.Campaign, int64) (qqinvite.Campaign, error)
	PreviewCodes(context.Context, int64, []string) (qqinvite.CodePreview, error)
	ImportCodes(context.Context, int64, []string, int64) (qqinvite.CodePreview, error)
	GenerateCodes(context.Context, int64, int, int64) (qqinvite.CodePreview, error)
	ListCodes(context.Context, int64, int, int) (qqinvite.CodeList, error)
	ListRecords(context.Context, int64, string, int, int) (qqinvite.RecordList, error)
	ReviewRecord(context.Context, int64, string, string, int64) error
	RecheckRecord(context.Context, int64, int64) error
	RedeliverReward(context.Context, int64, int64) error
	AcceptEvent(context.Context, qqinvite.Event) error
}
type qqInviteControl interface {
	Status(context.Context) (napcat.Status, error)
	QRCode(context.Context) (napcat.QRCode, error)
	RefreshQRCode(context.Context) (napcat.QRCode, error)
	Restart(context.Context) error
}
type QQInviteHandler struct {
	store    qqInviteStore
	control  func(context.Context) (qqInviteControl, error)
	baseline func(context.Context, int64, int64) error
	settings *service.SettingService
	cfg      *config.Config
	disabled bool
}

func (h *QQInviteHandler) private(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	c.Header("Referrer-Policy", "no-referrer")
	c.Header("X-Content-Type-Options", "nosniff")
}
func (h *QQInviteHandler) authorize(c *gin.Context, write bool) (int64, bool) {
	h.private(c)
	guard := QQImageHandler{settings: h.settings, cfg: h.cfg}
	id, ok := guard.admin(c)
	if !ok {
		return 0, false
	}
	if write && !guard.mutation(c) {
		return 0, false
	}
	if h.disabled {
		qqInviteResult(c, nil, qqinvite.ErrDisabled)
		return 0, false
	}
	return id, true
}
func qqInviteResult(c *gin.Context, value any, err error) {
	if err == nil {
		response.Success(c, value)
		return
	}
	status, code, message := 500, "QQ_INVITE_UNAVAILABLE", "Invitation reward operation unavailable"
	switch {
	case errors.Is(err, qqinvite.ErrInvalid), errors.Is(err, napcat.ErrInvalid):
		status, code, message = 400, "QQ_INVITE_INVALID", "Invalid invitation reward settings or request"
	case errors.Is(err, qqinvite.ErrConflict):
		status, code, message = 409, "QQ_INVITE_CONFLICT", "The record changed or conflicts with current activity"
	case errors.Is(err, qqinvite.ErrNotFound):
		status, code, message = 404, "QQ_INVITE_NOT_FOUND", "Invitation reward record not found"
	case errors.Is(err, qqinvite.ErrDisabled):
		status, code, message = 403, "QQ_INVITE_DISABLED", "Invitation rewards are disabled"
	case errors.Is(err, qqinvite.ErrEncryptionUnavailable):
		status, code, message = 503, "QQ_INVITE_ENCRYPTION_UNAVAILABLE", "Invitation encryption key is not configured"
	case errors.Is(err, napcat.ErrNotConfigured):
		status, code, message = 503, "NAPCAT_NOT_CONFIGURED", "NapCat connection has not been configured"
	case errors.Is(err, napcat.ErrUnauthorized):
		status, code, message = 502, "NAPCAT_UNAUTHORIZED", "NapCat rejected the configured credential"
	case errors.Is(err, napcat.ErrTwoFactor):
		status, code, message = 502, "NAPCAT_TWO_FACTOR", "NapCat WebUI requires additional authentication"
	case errors.Is(err, napcat.ErrOffline):
		status, code, message = 409, "NAPCAT_OFFLINE", "NapCat QQ account is offline or does not match configuration"
	case errors.Is(err, napcat.ErrRateLimited):
		status, code, message = 429, "NAPCAT_RATE_LIMITED", "NapCat is rate limited; retry later"
	case errors.Is(err, napcat.ErrProtocol):
		status, code, message = 502, "NAPCAT_PROTOCOL_ERROR", "NapCat returned an incompatible response"
	case errors.Is(err, napcat.ErrUnavailable):
		status, code, message = 502, "NAPCAT_UNAVAILABLE", "NapCat service is unreachable or unavailable"
	}
	response.ErrorWithDetails(c, status, message, code, nil)
}
func (h *QQInviteHandler) GetConfig(c *gin.Context) {
	if _, ok := h.authorize(c, false); !ok {
		return
	}
	if h.store == nil {
		qqInviteResult(c, nil, napcat.ErrNotConfigured)
		return
	}
	v, err := h.store.GetConfig(c.Request.Context())
	v.OneBotToken = ""
	v.WebUIToken = ""
	v.EventSecret = ""
	qqInviteResult(c, v, err)
}
func (h *QQInviteHandler) SaveConfig(c *gin.Context) {
	actor, ok := h.authorize(c, true)
	if !ok {
		return
	}
	var v qqinvite.Config
	if qqImageDecode(c, &v) != nil {
		qqInviteResult(c, nil, qqinvite.ErrInvalid)
		return
	}
	// Validate deployment-owned target allowlist before storing any credentials.
	if _, err := napcat.NewClient(napcat.Config{OneBotURL: v.OneBotURL, WebUIURL: v.WebUIURL, OneBotToken: "validation", WebUIToken: "validation", AllowedHosts: inviteAllowedHosts()}); err != nil {
		qqInviteResult(c, nil, err)
		return
	}
	saved, err := h.store.SaveConfig(c.Request.Context(), v, actor)
	saved.OneBotToken = ""
	saved.WebUIToken = ""
	saved.EventSecret = ""
	qqInviteResult(c, saved, err)
}
func inviteAllowedHosts() []string {
	var out []string
	for _, host := range strings.Split(os.Getenv("QQ_INVITE_ALLOWED_HOSTS"), ",") {
		if host = strings.TrimSpace(host); host != "" {
			out = append(out, host)
		}
	}
	return out
}
func (h *QQInviteHandler) operateLogin(c *gin.Context, action string) {
	if _, ok := h.authorize(c, action != "status"); !ok {
		return
	}
	if h.control == nil {
		qqInviteResult(c, nil, napcat.ErrNotConfigured)
		return
	}
	client, err := h.control(c.Request.Context())
	if err != nil {
		qqInviteResult(c, nil, err)
		return
	}
	switch action {
	case "status":
		v, e := client.Status(c.Request.Context())
		qqInviteResult(c, v, e)
	case "qrcode":
		v, e := client.QRCode(c.Request.Context())
		qqInviteResult(c, v, e)
	case "refresh":
		v, e := client.RefreshQRCode(c.Request.Context())
		qqInviteResult(c, v, e)
	case "restart":
		qqInviteResult(c, map[string]bool{"restarting": true}, client.Restart(c.Request.Context()))
	}
}
func (h *QQInviteHandler) Connection(c *gin.Context)    { h.operateLogin(c, "status") }
func (h *QQInviteHandler) QRCode(c *gin.Context)        { h.operateLogin(c, "qrcode") }
func (h *QQInviteHandler) RefreshQRCode(c *gin.Context) { h.operateLogin(c, "refresh") }
func (h *QQInviteHandler) Restart(c *gin.Context)       { h.operateLogin(c, "restart") }
func (h *QQInviteHandler) Events(c *gin.Context) {
	h.private(c)
	if h.disabled || h.store == nil {
		c.Status(http.StatusServiceUnavailable)
		return
	}
	raw, err := io.ReadAll(http.MaxBytesReader(c.Writer, c.Request.Body, 1<<20))
	if err != nil {
		c.Status(http.StatusRequestEntityTooLarge)
		return
	}
	cfg, err := h.store.RuntimeConfig(c.Request.Context())
	if err != nil {
		c.Status(http.StatusServiceUnavailable)
		return
	}
	if !napcat.VerifySignature(cfg.EventSecret, c.GetHeader("X-Signature"), raw) {
		c.Status(http.StatusUnauthorized)
		return
	}
	event, supported, err := parseQQInviteEvent(raw)
	if err != nil {
		c.Status(http.StatusBadRequest)
		return
	}
	if !supported {
		c.JSON(http.StatusOK, map[string]bool{"accepted": true})
		return
	}
	if event.SelfID != cfg.BotQQ || (c.GetHeader("X-Self-ID") != "" && c.GetHeader("X-Self-ID") != event.SelfID) {
		c.Status(http.StatusForbidden)
		return
	}
	if !cfg.Enabled {
		c.JSON(http.StatusOK, map[string]bool{"accepted": false, "disabled": true})
		return
	}
	if err = h.store.AcceptEvent(c.Request.Context(), event); err != nil {
		// Never ACK a database failure: NapCat must be able to retry delivery.
		if errors.Is(err, qqinvite.ErrInvalid) {
			c.Status(http.StatusBadRequest)
		} else {
			c.Status(http.StatusServiceUnavailable)
		}
		return
	}
	c.JSON(http.StatusOK, map[string]bool{"accepted": true})
}
func inviteParam(c *gin.Context) (int64, bool) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id <= 0 {
		qqInviteResult(c, nil, qqinvite.ErrInvalid)
		return 0, false
	}
	return id, true
}
func invitePage(c *gin.Context) (int, int) {
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	size, _ := strconv.Atoi(c.DefaultQuery("page_size", "20"))
	if page < 1 {
		page = 1
	}
	if size < 1 {
		size = 20
	}
	if size > 100 {
		size = 100
	}
	return page, size
}
