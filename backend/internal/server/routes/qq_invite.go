package routes

import (
	"github.com/Wei-Shaw/sub2api/internal/handler"
	"github.com/gin-gonic/gin"
)

func registerQQInviteAdminRoutes(admin *gin.RouterGroup, h *handler.QQInviteHandler) {
	if h == nil {
		return
	}
	q := admin.Group("/qq-invite")
	q.GET("/config", h.GetConfig)
	q.PUT("/config", h.SaveConfig)
	q.GET("/connection", h.Connection)
	q.POST("/login/qrcode", h.QRCode)
	q.POST("/login/refresh", h.RefreshQRCode)
	q.POST("/login/restart", h.Restart)
	q.GET("/campaigns", h.Campaigns)
	q.POST("/campaigns", h.SaveCampaign)
	q.PUT("/campaigns/:id", h.SaveCampaign)
	q.POST("/campaigns/:id/baseline", h.Baseline)
	q.GET("/campaigns/:id/codes", h.Codes)
	q.POST("/campaigns/:id/codes/preview", h.PreviewCodes)
	q.POST("/campaigns/:id/codes/import", h.ImportCodes)
	q.POST("/campaigns/:id/codes/generate", h.GenerateCodes)
	q.GET("/campaigns/:id/records", h.Records)
	q.POST("/records/:id/recheck", h.Recheck)
	q.POST("/records/:id/review", h.Review)
	q.POST("/rewards/:id/redeliver", h.Redeliver)
}
func RegisterQQInvitePublicRoutes(v1 *gin.RouterGroup, h *handler.Handlers) {
	if h != nil && h.QQInvite != nil {
		v1.POST("/qq-invite/events", h.QQInvite.Events)
	}
}
