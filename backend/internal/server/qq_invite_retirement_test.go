//go:build unit

package server

import (
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/handler"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestQQInvitationRoutesRetired(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	handlers := &handler.Handlers{
		Admin:    &handler.AdminHandlers{},
		QQImage:  &handler.QQImageHandler{},
		Welfare:  &handler.WelfareHandler{},
		MCPOAuth: &handler.MCPOAuthHandler{},
		Redeem:   &handler.RedeemHandler{},
	}
	pass := func(c *gin.Context) { c.Next() }

	registerRoutes(router, handlers, pass, pass, pass, pass, pass, pass,
		nil, nil, nil, nil, nil, &config.Config{}, nil)

	registered := make(map[string]bool)
	for _, route := range router.Routes() {
		require.False(t, strings.HasPrefix(route.Path, "/api/v1/qq-invite/") ||
			strings.HasPrefix(route.Path, "/api/v1/admin/qq-invite/"), route.Method+" "+route.Path)
		registered[route.Method+" "+route.Path] = true
	}
	require.True(t, registered["POST /api/v1/qq-image/events"])
	require.True(t, registered["GET /api/v1/admin/qq-image/config"])
	require.True(t, registered["GET /api/v1/admin/welfare/settings"])
	require.True(t, registered["POST /api/v1/redeem"])
	require.True(t, registered["GET /api/v1/redeem/history"])
	require.True(t, registered["GET /api/v1/subscriptions/active"])
	require.True(t, registered["GET /api/v1/admin/redeem-codes/welfare-campaigns"])
	require.True(t, registered["POST /api/v1/admin/subscriptions/:id/adjust-welfare"])
}
