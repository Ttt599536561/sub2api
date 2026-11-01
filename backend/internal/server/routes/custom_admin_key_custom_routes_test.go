//go:build unit

package routes

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/handler"
	adminhandler "github.com/Wei-Shaw/sub2api/internal/handler/admin"
	"github.com/Wei-Shaw/sub2api/internal/pkg/adminaccess"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestScopedAdminKeysCannotInheritCustomAdminRoutes(t *testing.T) {
	gin.SetMode(gin.TestMode)
	all := adminaccess.Policy{}
	resources := adminaccess.Rule{AllGroups: true, IncludeUngrouped: true, AccountTypes: []string{"oauth", "apikey"}}
	for _, permission := range adminaccess.PermissionCatalog() {
		if permission.Scope == "global" {
			all.GlobalPermissions = append(all.GlobalPermissions, permission.ID)
		} else {
			resources.Permissions = append(resources.Permissions, permission.ID)
		}
	}
	all.Rules = []adminaccess.Rule{resources}
	settings := service.NewSettingService(nil, nil)
	settings.SetScopedAdminKeyRepository(&scopedRouteKeys{})
	key, _, err := settings.CreateScopedAdminKey(context.Background(), service.ScopedAdminKeyInput{
		Name: "all explicit upstream permissions", Enabled: true, Policy: all,
	}, 9)
	require.NoError(t, err)
	users := service.NewUserService(&scopedRouteUsers{}, nil, nil, nil)
	auth := middleware.NewAdminAuthMiddleware(nil, users, settings, nil)
	handlers := &handler.Handlers{
		Admin:   &handler.AdminHandlers{Setting: adminhandler.NewSettingHandler(settings, nil, nil, nil, nil, nil, nil)},
		Welfare: &handler.WelfareHandler{},
		QQImage: &handler.QQImageHandler{},
	}
	router := gin.New()
	RegisterAdminRoutes(router.Group("/api/v1"), handlers, auth,
		middleware.AuditLogMiddleware(func(c *gin.Context) { c.Next() }),
		middleware.StepUpAuthMiddleware(func(c *gin.Context) { c.Next() }), nil, nil)
	prefixes := []string{"welfare", "qq-image", "prompt-audit", "affiliates", "channel-monitors", "channel-monitor-templates", "channel-monitor-v2"}
	for _, prefix := range prefixes {
		t.Run(prefix, func(t *testing.T) {
			count := 0
			for _, route := range router.Routes() {
				if route.Path != "/api/v1/admin/"+prefix && !strings.HasPrefix(route.Path, "/api/v1/admin/"+prefix+"/") {
					continue
				}
				count++
				parts := strings.Split(route.Path, "/")
				for i, part := range parts {
					if strings.HasPrefix(part, ":") {
						parts[i] = "1"
					}
				}
				request := httptest.NewRequest(route.Method, strings.Join(parts, "/"), nil)
				request.Header.Set("x-api-key", key)
				recorder := httptest.NewRecorder()
				router.ServeHTTP(recorder, request)
				require.Equal(t, http.StatusForbidden, recorder.Code, route.Method+" "+route.Path+": "+recorder.Body.String())
				require.Contains(t, recorder.Body.String(), "管理密钥未获此操作权限", "denial must come from scoped authentication")
			}
			require.Positive(t, count, "custom routes must actually be registered")
		})
	}
}
