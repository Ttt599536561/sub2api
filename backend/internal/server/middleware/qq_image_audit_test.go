package middleware

import (
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestQQImageAuditDoesNotPersistCredentials(t *testing.T) {
	for _, route := range []string{"PUT /api/v1/admin/qq-image/config", "POST /api/v1/user/qq-image/email/verify", "POST /api/v1/user/qq-image/requests/:id/authorize"} {
		require.Contains(t, auditBodyOmittedRoutes, route)
	}
	require.NotEmpty(t, auditSensitiveReads["GET /api/v1/admin/qq-image/orders/:id/image"])
}

func TestQQReplyTemplatePreviewAuditOmitsBodyOnErrorsOnlyForExactRoute(t *testing.T) {
	gin.SetMode(gin.TestMode)
	const path = "/api/v1/admin/qq-image/reply-templates/preview"
	const draft = `{"key":"iq.ack","text":"unsaved-draft-private-canary"}`
	for _, method := range []string{http.MethodPost, http.MethodPut} {
		t.Run(method, func(t *testing.T) {
			repository := &auditCaptureRepository{}
			audit := service.NewAuditLogService(repository, nil)
			audit.Start()
			router := gin.New()
			router.Use(gin.HandlerFunc(NewAuditLogMiddleware(audit)))
			router.Handle(method, path, func(c *gin.Context) {
				body, err := io.ReadAll(c.Request.Body)
				require.NoError(t, err)
				require.Equal(t, draft, string(body), "audit omission must preserve the complete body for the handler")
				c.Status(http.StatusBadRequest)
			})
			request := httptest.NewRequest(method, path, strings.NewReader(draft))
			request.Header.Set("Content-Type", "application/json")
			router.ServeHTTP(httptest.NewRecorder(), request)
			audit.Stop()
			require.Len(t, repository.logs, 1)
			entry := repository.logs[0]
			require.Equal(t, http.StatusBadRequest, entry.StatusCode)
			if method == http.MethodPost {
				require.Equal(t, "<credential-bearing body omitted>", entry.RequestBody)
				require.NotContains(t, entry.RequestBody, "unsaved-draft-private-canary")
			} else {
				require.Contains(t, entry.RequestBody, "unsaved-draft-private-canary", "the exception must not broaden to other methods")
			}
		})
	}
}
