package routes

import (
	"github.com/Wei-Shaw/sub2api/internal/handler"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestQQImageRoutesRegisterCompleteUserAdminAndPlatformSurface(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	h := &handler.QQImageHandler{}
	registerQQImageUserRoutes(r.Group("/api/v1/user"), h)
	registerQQImageAdminRoutes(r.Group("/api/v1/admin"), h)
	RegisterQQImagePublicRoutes(r.Group("/api/v1"), &handler.Handlers{QQImage: h})
	found := map[string]bool{}
	for _, route := range r.Routes() {
		found[route.Method+" "+route.Path] = true
	}
	for _, route := range []string{
		"GET /api/v1/user/qq-image/status", "POST /api/v1/user/qq-image/email/send-code", "POST /api/v1/user/qq-image/email/verify",
		"GET /api/v1/user/qq-image/requests/:id", "POST /api/v1/user/qq-image/requests/:id/authorize", "DELETE /api/v1/user/qq-image/grants/:id",
		"GET /api/v1/user/qq-image/orders", "GET /api/v1/user/qq-image/orders/:id", "POST /api/v1/user/qq-image/orders/:id/cancel", "GET /api/v1/user/qq-image/orders/:id/image",
		"GET /api/v1/admin/qq-image/connection", "GET /api/v1/admin/qq-image/config", "PUT /api/v1/admin/qq-image/config", "GET /api/v1/admin/qq-image/orders", "GET /api/v1/admin/qq-image/grants", "GET /api/v1/admin/qq-image/audit",
		"DELETE /api/v1/admin/qq-image/grants/:id", "POST /api/v1/admin/qq-image/orders/:id/refund", "POST /api/v1/admin/qq-image/orders/:id/release", "POST /api/v1/admin/qq-image/orders/:id/reconcile", "GET /api/v1/admin/qq-image/orders/:id/image",
		"POST /api/v1/qq-image/events", "GET /api/v1/qq-image/media",
		"GET /api/v1/qq-image/reply-templates/published",
		"GET /api/v1/admin/qq-image/reply-templates", "PUT /api/v1/admin/qq-image/reply-templates", "POST /api/v1/admin/qq-image/reply-templates/preview",
		"GET /api/v1/admin/qq-image/reply-templates/revisions", "GET /api/v1/admin/qq-image/reply-templates/revisions/:id", "POST /api/v1/admin/qq-image/reply-templates/rollback",
		"POST /api/v1/admin/qq-image/discovery/start", "GET /api/v1/admin/qq-image/discovery", "POST /api/v1/admin/qq-image/discovery/:id/close", "POST /api/v1/admin/qq-image/discovery/:id/confirm",
	} {
		require.True(t, found[route], route)
	}
}
