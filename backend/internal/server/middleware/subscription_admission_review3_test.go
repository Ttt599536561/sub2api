//go:build unit

package middleware

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestReview3APIKeyAuthDeletedGroupIsForbidden(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, protocol := range []string{"standard", "google"} {
		for _, groupType := range []string{service.SubscriptionTypeStandard, service.SubscriptionTypeSubscription} {
			t.Run(protocol+"/"+groupType, func(t *testing.T) {
				group := &service.Group{ID: 2, Status: service.StatusActive, Hydrated: true, SubscriptionType: groupType}
				user := &service.User{ID: 1, Status: service.StatusActive, Role: service.RoleUser, Balance: 10}
				key := &service.APIKey{ID: 3, Key: "review3-deleted-group", UserID: user.ID, User: user, Status: service.StatusActive, GroupID: &group.ID, Group: group}
				cfg := &config.Config{RunMode: config.RunModeStandard}
				groupRepo := &admissionGroupRepo{err: fmt.Errorf("load group: %w", service.ErrGroupNotFound)}
				subService := service.NewSubscriptionService(groupRepo, &stubUserSubscriptionRepo{}, nil, nil, cfg)
				t.Cleanup(subService.Stop)
				keyService := service.NewAPIKeyService(&stubApiKeyRepo{getByKey: func(context.Context, string) (*service.APIKey, error) {
					copy := *key
					return &copy, nil
				}}, nil, nil, nil, nil, nil, cfg)
				router := gin.New()
				if protocol == "google" {
					router.Use(APIKeyAuthWithSubscriptionGoogle(keyService, subService, cfg))
				} else {
					router.Use(gin.HandlerFunc(NewAPIKeyAuthMiddleware(keyService, subService, cfg)))
				}
				forwarded := false
				router.GET("/admission", func(c *gin.Context) { forwarded = true; c.Status(http.StatusOK) })
				request := httptest.NewRequest(http.MethodGet, "/admission", nil)
				request.Header.Set("x-api-key", key.Key)
				response := httptest.NewRecorder()

				router.ServeHTTP(response, request)

				require.Equal(t, http.StatusForbidden, response.Code, response.Body.String())
				require.False(t, forwarded)
				require.Equal(t, 1, groupRepo.reads, "fresh group state must supersede the auth snapshot")
			})
		}
	}
}
