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

func TestQQInviteAuditOmitsCredentialsAndCodes(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, tc := range []struct{ method, path, body string }{
		{"PUT", "/api/v1/admin/qq-invite/config", `{"onebot_token":"secret-canary","webui_token":"secret-canary","event_secret":"secret-canary"}`},
		{"POST", "/api/v1/admin/qq-invite/campaigns/:id/codes/preview", `{"codes":["private-code-canary"]}`},
		{"POST", "/api/v1/admin/qq-invite/campaigns/:id/codes/import", `{"codes":["private-code-canary"]}`},
	} {
		t.Run(tc.path, func(t *testing.T) {
			repo := &auditCaptureRepository{}
			audit := service.NewAuditLogService(repo, nil)
			audit.Start()
			router := gin.New()
			router.Use(gin.HandlerFunc(NewAuditLogMiddleware(audit)))
			router.Handle(tc.method, tc.path, func(c *gin.Context) {
				b, err := io.ReadAll(c.Request.Body)
				require.NoError(t, err)
				require.Equal(t, tc.body, string(b))
				c.Status(http.StatusBadRequest)
			})
			req := httptest.NewRequest(tc.method, strings.ReplaceAll(tc.path, ":id", "1"), strings.NewReader(tc.body))
			req.Header.Set("Content-Type", "application/json")
			router.ServeHTTP(httptest.NewRecorder(), req)
			audit.Stop()
			require.Len(t, repo.logs, 1)
			require.Equal(t, "<credential-bearing body omitted>", repo.logs[0].RequestBody)
			require.NotContains(t, repo.logs[0].RequestBody, "canary")
		})
	}
}
