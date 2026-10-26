//go:build embed

package web_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/Wei-Shaw/sub2api/internal/web"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestFrontendQQControlsInjectionRefresh(t *testing.T) {
	values := map[string]string{}
	settings := service.NewSettingService(welfarePublicSettingsRepo{values: values}, &config.Config{})
	server, err := web.NewFrontendServer(settings)
	require.NoError(t, err)
	router := gin.New()
	router.Use(server.Middleware())
	request := func() *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
		require.Equal(t, http.StatusOK, rec.Code)
		return rec
	}
	first := request()
	require.Contains(t, first.Body.String(), `"qq_binding_enabled":true`)
	require.Contains(t, first.Body.String(), `"qq_image_enabled":true`)
	values["qq_binding_enabled"] = "false"
	values["qq_image_enabled"] = "false"
	server.InvalidateCache()
	after := request()
	require.Contains(t, after.Body.String(), `"qq_binding_enabled":false`)
	require.Contains(t, after.Body.String(), `"qq_image_enabled":false`)
	require.NotEqual(t, first.Header().Get("ETag"), after.Header().Get("ETag"))
}
