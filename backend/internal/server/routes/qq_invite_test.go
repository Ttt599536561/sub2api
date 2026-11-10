package routes

import (
	"github.com/Wei-Shaw/sub2api/internal/handler"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestQQInviteRoutes(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	h := &handler.QQInviteHandler{}
	registerQQInviteAdminRoutes(r.Group("/api/v1/admin"), h)
	RegisterQQInvitePublicRoutes(r.Group("/api/v1"), &handler.Handlers{QQInvite: h})
	found := map[string]bool{}
	for _, route := range r.Routes() {
		found[route.Method+" "+route.Path] = true
	}
	for _, want := range []string{"GET /api/v1/admin/qq-invite/config", "PUT /api/v1/admin/qq-invite/config", "GET /api/v1/admin/qq-invite/connection", "POST /api/v1/admin/qq-invite/login/qrcode", "POST /api/v1/admin/qq-invite/login/refresh", "POST /api/v1/admin/qq-invite/login/restart", "GET /api/v1/admin/qq-invite/campaigns", "POST /api/v1/admin/qq-invite/campaigns", "PUT /api/v1/admin/qq-invite/campaigns/:id", "POST /api/v1/admin/qq-invite/campaigns/:id/baseline", "GET /api/v1/admin/qq-invite/campaigns/:id/codes", "POST /api/v1/admin/qq-invite/campaigns/:id/codes/preview", "POST /api/v1/admin/qq-invite/campaigns/:id/codes/import", "POST /api/v1/admin/qq-invite/campaigns/:id/codes/generate", "GET /api/v1/admin/qq-invite/campaigns/:id/records", "POST /api/v1/admin/qq-invite/records/:id/recheck", "POST /api/v1/admin/qq-invite/records/:id/review", "POST /api/v1/admin/qq-invite/rewards/:id/redeliver", "POST /api/v1/qq-invite/events"} {
		require.True(t, found[want], want)
	}
}
