//go:build unit

package middleware

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestAPIKeyAuthUsesDatabaseSubscriptionAdmission(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, protocol := range []string{"standard", "google"} {
		for _, scenario := range []struct {
			name       string
			usage      float64
			dbError    bool
			expired    bool
			wantStatus int
		}{
			{name: "fresh permission and limit", usage: 60, wantStatus: http.StatusTooManyRequests},
			{name: "database unavailable", dbError: true, wantStatus: http.StatusServiceUnavailable},
			{name: "previously reset subscription expired", expired: true, wantStatus: http.StatusForbidden},
			{name: "valid subscription replaces cached group", usage: 10, wantStatus: http.StatusOK},
		} {
			t.Run(protocol+"/"+scenario.name, func(t *testing.T) {
				limit := 50.0
				group := &service.Group{ID: 2, Status: service.StatusActive, Hydrated: true, SubscriptionType: service.SubscriptionTypeSubscription, DailyLimitUSD: &limit, AllowSubscriptionDayReset: !scenario.expired}
				staleGroup := *group
				staleGroup.SubscriptionType = service.SubscriptionTypeStandard
				staleGroup.AllowSubscriptionDayReset = false
				user := &service.User{ID: 1, Status: service.StatusActive, Balance: 10, Role: service.RoleUser}
				key := &service.APIKey{ID: 3, Key: "admission-key", UserID: 1, Status: service.StatusActive, User: user, GroupID: &group.ID, Group: &staleGroup}
				now := time.Now()
				sub := &service.UserSubscription{ID: 4, UserID: 1, GroupID: 2, Status: service.SubscriptionStatusActive, StartsAt: now.Add(-48 * time.Hour), ExpiresAt: now.Add(10 * 24 * time.Hour), DailyWindowStart: &now, WeeklyWindowStart: &now, MonthlyWindowStart: &now, DailyUsageUSD: scenario.usage, PreserveCalendarDailyReset: scenario.expired}
				if scenario.expired {
					sub.ExpiresAt = now.Add(-time.Second)
				}
				groupRepo := &admissionGroupRepo{group: group}
				if scenario.dbError {
					groupRepo.err = errors.New("database unavailable")
				}
				subRepo := &stubUserSubscriptionRepo{getActive: func(context.Context, int64, int64) (*service.UserSubscription, error) {
					copy := *sub
					return &copy, nil
				}}
				cfg := &config.Config{RunMode: config.RunModeStandard}
				subService := service.NewSubscriptionService(groupRepo, subRepo, nil, nil, cfg)
				t.Cleanup(subService.Stop)
				keyService := service.NewAPIKeyService(&stubApiKeyRepo{getByKey: func(context.Context, string) (*service.APIKey, error) { copy := *key; return &copy, nil }}, nil, nil, nil, nil, nil, cfg)
				router := gin.New()
				if protocol == "google" {
					router.Use(APIKeyAuthWithSubscriptionGoogle(keyService, subService, cfg))
				} else {
					router.Use(gin.HandlerFunc(NewAPIKeyAuthMiddleware(keyService, subService, cfg)))
				}
				router.GET("/admission", func(c *gin.Context) {
					actualKey, ok := GetAPIKeyFromContext(c)
					require.True(t, ok)
					require.True(t, actualKey.Group.IsSubscriptionType())
					actualSub, ok := GetSubscriptionFromContext(c)
					require.True(t, ok)
					require.Equal(t, sub.ID, actualSub.ID)
					c.Status(http.StatusOK)
				})
				request := httptest.NewRequest(http.MethodGet, "/admission", nil)
				request.Header.Set("x-api-key", key.Key)
				response := httptest.NewRecorder()
				router.ServeHTTP(response, request)
				require.Equal(t, scenario.wantStatus, response.Code, response.Body.String())
				require.Positive(t, groupRepo.reads)
			})
		}
	}
}
