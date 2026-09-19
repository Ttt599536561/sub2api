//go:build embed

package web_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/Wei-Shaw/sub2api/internal/web"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type welfarePublicSettingsRepo struct {
	service.SettingRepository
	values map[string]string
}

func (r welfarePublicSettingsRepo) GetMultiple(context.Context, []string) (map[string]string, error) {
	return r.values, nil
}

func TestFrontendWelfareRecoveryPreservesConfiguredFrameOrigins(t *testing.T) {
	settings := service.NewSettingService(welfarePublicSettingsRepo{values: map[string]string{
		service.SettingKeyHomeContent:     "https://home.example.test/welcome",
		service.SettingKeyCustomMenuItems: `[{"url":"https://menu.example.test/page"}]`,
	}}, &config.Config{})
	providerErr := errors.New("welfare query temporarily unavailable at startup")
	settings.SetWelfareAvailabilityProvider(func(context.Context) (bool, error) {
		return providerErr == nil, providerErr
	})
	// The router reads these origins once at startup and only refreshes them
	// after settings updates. Unrelated welfare recovery must not be required.
	origins, err := settings.GetFrameSrcOrigins(context.Background())
	require.NoError(t, err)
	server, err := web.NewFrontendServer(settings)
	require.NoError(t, err)
	router := gin.New()
	router.Use(middleware.SecurityHeaders(config.CSPConfig{Enabled: true}, func() []string { return origins }))
	router.Use(server.Middleware())

	failed := httptest.NewRecorder()
	router.ServeHTTP(failed, httptest.NewRequest(http.MethodGet, "/", nil))
	require.Equal(t, http.StatusOK, failed.Code)
	require.NotContains(t, failed.Body.String(), "window.__APP_CONFIG__=")
	require.Empty(t, failed.Header().Get("ETag"))

	providerErr = nil
	recovered := httptest.NewRecorder()
	router.ServeHTTP(recovered, httptest.NewRequest(http.MethodGet, "/", nil))
	require.Equal(t, http.StatusOK, recovered.Code)
	require.Contains(t, recovered.Body.String(), `"welfare_enabled":true`)
	for _, response := range []*httptest.ResponseRecorder{failed, recovered} {
		require.Contains(t, response.Header().Get("Content-Security-Policy"), "https://home.example.test")
		require.Contains(t, response.Header().Get("Content-Security-Policy"), "https://menu.example.test")
	}
}

func TestFrontendWelfareAvailabilityRecoversWithoutCacheInvalidation(t *testing.T) {
	settings := service.NewSettingService(welfarePublicSettingsRepo{}, &config.Config{})
	providerErr := errors.New("welfare database unavailable")
	settings.SetWelfareAvailabilityProvider(func(context.Context) (bool, error) {
		return providerErr == nil, providerErr
	})
	server, err := web.NewFrontendServer(settings)
	require.NoError(t, err)
	router := gin.New()
	router.Use(server.Middleware())

	failed := httptest.NewRecorder()
	router.ServeHTTP(failed, httptest.NewRequest(http.MethodGet, "/", nil))
	assert.Equal(t, http.StatusOK, failed.Code)
	assert.NotContains(t, failed.Body.String(), "window.__APP_CONFIG__=")
	assert.Empty(t, failed.Header().Get("ETag"), "unknown availability must not create a cache entry")

	// Database recovery alone must restore the public flag on the next request.
	providerErr = nil
	recovered := httptest.NewRecorder()
	router.ServeHTTP(recovered, httptest.NewRequest(http.MethodGet, "/", nil))
	assert.Equal(t, http.StatusOK, recovered.Code)
	assert.Contains(t, recovered.Body.String(), `"welfare_enabled":true`)
	require.NotEmpty(t, recovered.Header().Get("ETag"))

	cached := httptest.NewRecorder()
	revalidated := httptest.NewRequest(http.MethodGet, "/", nil)
	revalidated.Header.Set("If-None-Match", recovered.Header().Get("ETag"))
	router.ServeHTTP(cached, revalidated)
	assert.Equal(t, http.StatusNotModified, cached.Code)
}

func TestFrontendWelfareConfirmedDisabledRemainsCacheable(t *testing.T) {
	for _, tc := range []struct {
		name     string
		provider func(context.Context) (bool, error)
	}{
		{name: "no provider"},
		{name: "confirmed disabled", provider: func(context.Context) (bool, error) { return false, nil }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			settings := service.NewSettingService(welfarePublicSettingsRepo{}, &config.Config{})
			settings.SetWelfareAvailabilityProvider(tc.provider)
			server, err := web.NewFrontendServer(settings)
			require.NoError(t, err)
			router := gin.New()
			router.Use(server.Middleware())

			response := httptest.NewRecorder()
			router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/", nil))
			assert.Equal(t, http.StatusOK, response.Code)
			assert.Contains(t, response.Body.String(), `"welfare_enabled":false`)
			require.NotEmpty(t, response.Header().Get("ETag"))

			cached := httptest.NewRecorder()
			revalidated := httptest.NewRequest(http.MethodGet, "/", nil)
			revalidated.Header.Set("If-None-Match", response.Header().Get("ETag"))
			router.ServeHTTP(cached, revalidated)
			assert.Equal(t, http.StatusNotModified, cached.Code)
		})
	}
}
