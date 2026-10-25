package middleware

import (
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestQQImageBindingPagePrivacySurvivesSPACacheHeaders(t *testing.T) {
	for _, path := range []string{"/qq/bind?request=private", "/qq/bind/", "/dashboard"} {
		r := gin.New()
		r.Use(QQImageBindingPagePrivacy())
		r.NoRoute(func(c *gin.Context) {
			c.Header("Cache-Control", "no-cache")
			c.Header("Referrer-Policy", "strict-origin-when-cross-origin")
			c.Header("ETag", "public-settings")
			c.String(200, "page")
		})
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, path, nil))
		if path == "/dashboard" {
			require.Equal(t, "no-cache", w.Header().Get("Cache-Control"))
			require.Equal(t, "strict-origin-when-cross-origin", w.Header().Get("Referrer-Policy"))
		} else {
			require.Equal(t, "no-store", w.Header().Get("Cache-Control"))
			require.Equal(t, "no-referrer", w.Header().Get("Referrer-Policy"))
			require.Empty(t, w.Header().Get("ETag"))
		}
	}
}
