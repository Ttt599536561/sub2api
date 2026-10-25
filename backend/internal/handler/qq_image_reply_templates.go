package handler

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"os"
	"strconv"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/qqimage"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/gin-gonic/gin"
)

type qqReplyTemplateManager interface {
	GetReplyTemplates(context.Context) (*qqimage.ReplyTemplateSnapshot, error)
	SaveReplyTemplates(context.Context, int64, map[string]string, int64) (*qqimage.ReplyTemplateSnapshot, error)
	RollbackReplyTemplates(context.Context, int64, int64, int64) (*qqimage.ReplyTemplateSnapshot, error)
	ListReplyTemplateRevisions(context.Context) ([]qqimage.ReplyTemplateRevisionInfo, error)
	GetReplyTemplateRevision(context.Context, int64) (*qqimage.ReplyTemplateRevision, error)
}

// A complete 26-entry Chinese catalog can exceed the legacy 64 KiB limit.
// Keep this bounded decoder local so no unrelated mutation gains a larger body.
func qqReplyTemplateDecode(c *gin.Context, target any) error {
	typ, _, err := mime.ParseMediaType(c.GetHeader("Content-Type"))
	if err != nil || typ != "application/json" {
		return errors.New("json_body_required")
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 256<<10)
	decoder := json.NewDecoder(c.Request.Body)
	decoder.DisallowUnknownFields()
	if err = decoder.Decode(target); err != nil {
		return err
	}
	var extra any
	if err = decoder.Decode(&extra); err != io.EOF {
		return errors.New("single_json_body_required")
	}
	return nil
}

func (h *QQImageHandler) replyTemplateManager(c *gin.Context) (qqReplyTemplateManager, bool) {
	manager, ok := h.store.(qqReplyTemplateManager)
	if !ok {
		response.Error(c, 503, "QQ reply templates are unavailable")
	}
	return manager, ok
}
func (h *QQImageHandler) replyTemplateMutation(c *gin.Context) (int64, bool) {
	actor, ok := h.admin(c)
	if !ok {
		return 0, false
	}
	if c.GetHeader("Origin") == "" {
		response.Forbidden(c, "Same-origin request required")
		return 0, false
	}
	if !h.mutation(c) {
		return 0, false
	}
	return actor, true
}
func replyTemplateInvalid(c *gin.Context, issues []string) {
	c.JSON(http.StatusBadRequest, gin.H{"code": http.StatusBadRequest, "message": "Invalid QQ reply templates", "reason": "QQ_REPLY_TEMPLATE_INVALID", "errors": issues})
}
func replyTemplateResult(c *gin.Context, result any, err error) {
	var invalid *qqimage.ReplyTemplateValidationError
	if errors.As(err, &invalid) {
		replyTemplateInvalid(c, invalid.Errors)
		return
	}
	qqImageResult(c, result, err)
}
func (h *QQImageHandler) AdminReplyTemplates(c *gin.Context) {
	if _, ok := h.admin(c); !ok {
		return
	}
	manager, ok := h.replyTemplateManager(c)
	if !ok {
		return
	}
	result, err := manager.GetReplyTemplates(c.Request.Context())
	replyTemplateResult(c, result, err)
}
func (h *QQImageHandler) SaveAdminReplyTemplates(c *gin.Context) {
	actor, ok := h.replyTemplateMutation(c)
	if !ok {
		return
	}
	var input struct {
		ExpectedRevision *int64             `json:"expected_revision"`
		Overrides        *map[string]string `json:"overrides"`
	}
	if qqReplyTemplateDecode(c, &input) != nil || input.ExpectedRevision == nil || *input.ExpectedRevision < 0 || input.Overrides == nil {
		replyTemplateInvalid(c, []string{"invalid_request"})
		return
	}
	clean, err := qqimage.ValidateReplyTemplateOverrides(*input.Overrides)
	if err != nil {
		replyTemplateResult(c, nil, err)
		return
	}
	manager, ok := h.replyTemplateManager(c)
	if !ok {
		return
	}
	result, err := manager.SaveReplyTemplates(c.Request.Context(), *input.ExpectedRevision, clean, actor)
	replyTemplateResult(c, result, err)
}
func (h *QQImageHandler) PreviewAdminReplyTemplate(c *gin.Context) {
	if _, ok := h.replyTemplateMutation(c); !ok {
		return
	}
	var input struct {
		Key  string `json:"key"`
		Text string `json:"text"`
	}
	if qqReplyTemplateDecode(c, &input) != nil {
		replyTemplateInvalid(c, []string{"invalid_request"})
		return
	}
	// Fake values are generated in-process. No store, user data, QQ or provider
	// client is consulted, including when the templates table is unavailable.
	// Successful draft previews are read-only and may run after every edit.
	// Failures before authentication, origin checks or decoding remain audited.
	middleware.SkipAudit(c)
	response.Success(c, qqimage.PreviewReplyTemplate(input.Key, input.Text))
}
func (h *QQImageHandler) AdminReplyTemplateRevisions(c *gin.Context) {
	if _, ok := h.admin(c); !ok {
		return
	}
	manager, ok := h.replyTemplateManager(c)
	if !ok {
		return
	}
	result, err := manager.ListReplyTemplateRevisions(c.Request.Context())
	replyTemplateResult(c, result, err)
}
func (h *QQImageHandler) AdminReplyTemplateRevision(c *gin.Context) {
	if _, ok := h.admin(c); !ok {
		return
	}
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id <= 0 {
		replyTemplateInvalid(c, []string{"invalid_revision"})
		return
	}
	manager, ok := h.replyTemplateManager(c)
	if !ok {
		return
	}
	result, err := manager.GetReplyTemplateRevision(c.Request.Context(), id)
	replyTemplateResult(c, result, err)
}
func (h *QQImageHandler) RollbackAdminReplyTemplates(c *gin.Context) {
	actor, ok := h.replyTemplateMutation(c)
	if !ok {
		return
	}
	var input struct {
		ExpectedRevision *int64 `json:"expected_revision"`
		Revision         int64  `json:"revision"`
	}
	if qqReplyTemplateDecode(c, &input) != nil || input.ExpectedRevision == nil || *input.ExpectedRevision < 0 || input.Revision <= 0 {
		replyTemplateInvalid(c, []string{"invalid_request"})
		return
	}
	manager, ok := h.replyTemplateManager(c)
	if !ok {
		return
	}
	result, err := manager.RollbackReplyTemplates(c.Request.Context(), *input.ExpectedRevision, input.Revision, actor)
	replyTemplateResult(c, result, err)
}

// PublishedReplyTemplates uses a separate deployment secret. Admin cookies,
// JWTs, QQ app credentials and public config are deliberately not involved.
func (h *QQImageHandler) PublishedReplyTemplates(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	c.Header("Referrer-Policy", "no-referrer")
	secret := os.Getenv("QQ_REPLY_TEMPLATE_SYNC_TOKEN")
	if len(secret) < 32 || strings.TrimSpace(secret) != secret {
		response.Error(c, 503, "Reply template synchronization is not configured")
		return
	}
	parts := strings.SplitN(c.GetHeader("Authorization"), " ", 2)
	provided := ""
	if len(parts) == 2 && strings.EqualFold(parts[0], "Bearer") {
		provided = parts[1]
	}
	expectedHash, providedHash := sha256.Sum256([]byte(secret)), sha256.Sum256([]byte(provided))
	if subtle.ConstantTimeCompare(expectedHash[:], providedHash[:]) != 1 {
		response.Unauthorized(c, "Invalid synchronization token")
		return
	}
	manager, ok := h.replyTemplateManager(c)
	if !ok {
		return
	}
	snapshot, err := manager.GetReplyTemplates(c.Request.Context())
	if err != nil || snapshot == nil {
		response.Error(c, 503, "Reply template synchronization is unavailable")
		return
	}
	c.JSON(http.StatusOK, snapshot.Published())
}
