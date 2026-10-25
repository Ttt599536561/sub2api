package handler

import (
	"encoding/hex"

	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/qqimage"
	"github.com/gin-gonic/gin"
)

func (h *QQImageHandler) discoveryMutation(c *gin.Context) (int64, bool) {
	actor, ok := h.admin(c)
	if !ok {
		return 0, false
	}
	// These browser-only operator actions require an explicit same-origin header,
	// including confirmation; possession of a candidate ID is never authority.
	if c.GetHeader("Origin") == "" {
		response.Forbidden(c, "Same-origin request required")
		return 0, false
	}
	if !h.mutation(c) {
		return 0, false
	}
	var input struct{}
	if qqImageDecode(c, &input) != nil {
		qqImageResult(c, nil, qqimage.ErrInvalid)
		return 0, false
	}
	return actor, true
}
func (h *QQImageHandler) AdminStartDiscovery(c *gin.Context) {
	actor, ok := h.discoveryMutation(c)
	if !ok {
		return
	}
	result, err := h.store.StartDiscovery(c.Request.Context(), actor)
	qqImageResult(c, result, err)
}
func (h *QQImageHandler) AdminDiscovery(c *gin.Context) {
	if _, ok := h.admin(c); !ok {
		return
	}
	result, err := h.store.GetDiscovery(c.Request.Context())
	qqImageResult(c, result, err)
}
func discoveryPathID(c *gin.Context) (string, bool) {
	id := c.Param("id")
	b, err := hex.DecodeString(id)
	if err != nil || len(id) != 32 || len(b) != 16 {
		qqImageResult(c, nil, qqimage.ErrInvalid)
		return "", false
	}
	return id, true
}
func (h *QQImageHandler) AdminCloseDiscovery(c *gin.Context) {
	actor, ok := h.discoveryMutation(c)
	if !ok {
		return
	}
	id, ok := discoveryPathID(c)
	if !ok {
		return
	}
	result, err := h.store.CloseDiscovery(c.Request.Context(), id, actor)
	qqImageResult(c, result, err)
}
func (h *QQImageHandler) AdminConfirmDiscovery(c *gin.Context) {
	actor, ok := h.discoveryMutation(c)
	if !ok {
		return
	}
	id, ok := discoveryPathID(c)
	if !ok {
		return
	}
	result, err := h.store.ConfirmDiscovery(c.Request.Context(), id, actor)
	qqImageResult(c, result, err)
}
