package routes

import (
	"github.com/Wei-Shaw/sub2api/internal/handler"
	"github.com/gin-gonic/gin"
)

// User and administrator groups already carry the site's authentication,
// activity, rate-limit and audit middleware. Welfare availability is unrelated.
func registerQQImageUserRoutes(user *gin.RouterGroup, h *handler.QQImageHandler) {
	if h == nil {
		return
	}
	q := user.Group("/qq-image")
	q.GET("/status", h.Status)
	q.POST("/email/send-code", h.SendEmailCode)
	q.POST("/email/verify", h.VerifyEmail)
	q.GET("/requests/:id", h.BindingRequest)
	q.POST("/requests/:id/authorize", h.AuthorizeBinding)
	q.DELETE("/grants/:id", h.RevokeGrant)
	q.GET("/orders", h.Orders)
	q.GET("/orders/:id", h.Order)
	q.POST("/orders/:id/cancel", h.CancelOrder)
	q.GET("/orders/:id/image", h.Image)
}
func registerQQImageAdminRoutes(admin *gin.RouterGroup, h *handler.QQImageHandler) {
	if h == nil {
		return
	}
	q := admin.Group("/qq-image")
	q.GET("/connection", h.AdminConnection)
	q.GET("/config", h.AdminConfig)
	q.PUT("/config", h.SaveAdminConfig)
	q.GET("/reply-templates", h.AdminReplyTemplates)
	q.PUT("/reply-templates", h.SaveAdminReplyTemplates)
	q.POST("/reply-templates/preview", h.PreviewAdminReplyTemplate)
	q.GET("/reply-templates/revisions", h.AdminReplyTemplateRevisions)
	q.GET("/reply-templates/revisions/:id", h.AdminReplyTemplateRevision)
	q.POST("/reply-templates/rollback", h.RollbackAdminReplyTemplates)
	q.POST("/discovery/start", h.AdminStartDiscovery)
	q.GET("/discovery", h.AdminDiscovery)
	q.POST("/discovery/:id/close", h.AdminCloseDiscovery)
	q.POST("/discovery/:id/confirm", h.AdminConfirmDiscovery)
	q.GET("/orders", h.AdminOrders)
	q.GET("/grants", h.AdminGrants)
	q.GET("/audit", h.AdminAudit)
	q.DELETE("/grants/:id", h.AdminRevokeGrant)
	q.POST("/orders/:id/refund", h.AdminRefund)
	q.POST("/orders/:id/release", h.AdminRelease)
	q.POST("/orders/:id/reconcile", h.AdminReconcile)
	q.GET("/orders/:id/image", h.AdminImage)
}

// Platform events authenticate the original bytes using the QQ signature.
// Media access is a short-lived purpose-specific transfer grant, never a user API.
func RegisterQQImagePublicRoutes(v1 *gin.RouterGroup, h *handler.Handlers) {
	if h == nil || h.QQImage == nil {
		return
	}
	v1.POST("/qq-image/events", h.QQImage.Events)
	v1.GET("/qq-image/media", h.QQImage.Media)
	v1.GET("/qq-image/reply-templates/published", h.QQImage.PublishedReplyTemplates)
}
