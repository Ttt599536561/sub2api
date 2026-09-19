//go:build unit

package middleware

import (
	"context"
	"errors"
	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"net/http"
	"net/http/httptest"
	"testing"
)

type welfareAdmissionUserRepo struct {
	service.UserRepository
	balance float64
	err     error
	reads   int
}

func (r *welfareAdmissionUserRepo) GetByID(context.Context, int64) (*service.User, error) {
	r.reads++
	return &service.User{Balance: r.balance}, r.err
}

func TestWelfareCreditRefreshesStaleAuthBalanceForBothProtocols(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, google := range []bool{false, true} {
		for _, outcome := range []string{"credited", "unavailable", "sufficient_snapshot"} {
			t.Run(outcome+map[bool]string{false: "_standard", true: "_google"}[google], func(t *testing.T) {
				users := &welfareAdmissionUserRepo{balance: 20}
				if outcome == "unavailable" {
					users.err = errors.New("database unavailable")
				}
				keys := &stubApiKeyRepo{getByKey: func(context.Context, string) (*service.APIKey, error) {
					balance := 0.0
					if outcome == "sufficient_snapshot" {
						balance = 10
					}
					return &service.APIKey{ID: 1, UserID: 7, Key: "test", Status: service.StatusActive, User: &service.User{ID: 7, Status: service.StatusActive, Role: service.RoleUser, Balance: balance, Concurrency: 1}}, nil
				}}
				cfg := &config.Config{}
				svc := service.NewAPIKeyService(keys, users, nil, nil, nil, nil, cfg)
				router := gin.New()
				if google {
					router.Use(APIKeyAuthGoogle(svc, cfg))
				} else {
					router.Use(gin.HandlerFunc(NewAPIKeyAuthMiddleware(svc, nil, cfg)))
				}
				router.GET("/test", func(c *gin.Context) { c.Status(http.StatusOK) })
				req := httptest.NewRequest(http.MethodGet, "/test", nil)
				req.Header.Set("Authorization", "Bearer test")
				w := httptest.NewRecorder()
				router.ServeHTTP(w, req)
				if outcome == "unavailable" {
					require.Equal(t, http.StatusServiceUnavailable, w.Code)
				} else {
					require.Equal(t, http.StatusOK, w.Code)
				}
				if outcome == "sufficient_snapshot" {
					require.Zero(t, users.reads)
				} else {
					require.Equal(t, 1, users.reads)
				}
			})
		}
	}
}
