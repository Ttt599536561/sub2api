//go:build unit

package middleware

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type admissionGroupGuardRepo struct {
	service.GroupRepository
	group *service.Group
	err   error
	reads int
}

func (r *admissionGroupGuardRepo) GetByID(context.Context, int64) (*service.Group, error) {
	r.reads++
	return r.group, r.err
}

func TestAPIKeyAuthAdmissionPreservesOfficialGroupGuards(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, protocol := range []string{"standard", "google"} {
		for _, scenario := range []struct {
			name         string
			freshStatus  string
			staleStatus  string
			missing      bool
			groupErr     error
			exclusive    bool
			subscription bool
			invalidSub   bool
			subErr       error
			wantStatus   int
			wantCode     string
			wantMessage  string
			wantReject   IngressRejectReason
		}{
			{name: "missing group", missing: true, wantStatus: 403, wantCode: "GROUP_DELETED", wantMessage: "API Key 所属分组已删除", wantReject: IngressRejectGroupDeleted},
			{name: "deleted group not found", groupErr: fmt.Errorf("load group: %w", service.ErrGroupNotFound), wantStatus: 403, wantCode: "GROUP_DELETED", wantMessage: "API Key 所属分组已删除", wantReject: IngressRejectGroupDeleted},
			{name: "deleted group status", freshStatus: "deleted", wantStatus: 403, wantCode: "GROUP_DELETED", wantMessage: "API Key 所属分组已删除", wantReject: IngressRejectGroupDeleted},
			{name: "disabled group", freshStatus: service.StatusDisabled, wantStatus: 403, wantCode: "GROUP_DISABLED", wantMessage: "API Key 所属分组已停用", wantReject: IngressRejectGroupDisabled},
			{name: "exclusive permission revoked", exclusive: true, wantStatus: 403, wantCode: "GROUP_NOT_ALLOWED", wantMessage: "API Key 所属专属分组不再允许当前用户使用", wantReject: IngressRejectGroupNotAllowed},
			{name: "group database unavailable", groupErr: errors.New("group database unavailable"), wantStatus: 503, wantCode: "BILLING_SERVICE_UNAVAILABLE", wantMessage: service.ErrBillingServiceUnavailable.Message},
			{name: "infrastructure error takes precedence over nested subscription cause", groupErr: service.ErrBillingServiceUnavailable.WithCause(service.ErrSubscriptionInvalid), wantStatus: 503, wantCode: "BILLING_SERVICE_UNAVAILABLE", wantMessage: service.ErrBillingServiceUnavailable.Message},
			{name: "stale disabled group database unavailable", staleStatus: service.StatusDisabled, groupErr: errors.New("group database unavailable"), wantStatus: 503, wantCode: "BILLING_SERVICE_UNAVAILABLE", wantMessage: service.ErrBillingServiceUnavailable.Message},
			{name: "subscription database unavailable", subscription: true, subErr: errors.New("subscription database unavailable"), wantStatus: 503, wantCode: "BILLING_SERVICE_UNAVAILABLE", wantMessage: service.ErrBillingServiceUnavailable.Message},
			{name: "missing subscription keeps subscription error", subscription: true, subErr: service.ErrSubscriptionNotFound, wantStatus: 403, wantCode: "SUBSCRIPTION_NOT_FOUND", wantMessage: service.ErrSubscriptionNotFound.Message},
			{name: "invalid subscription keeps subscription error", subscription: true, invalidSub: true, wantStatus: 403, wantCode: "SUBSCRIPTION_INVALID", wantMessage: service.ErrSubscriptionInvalid.Message},
			{name: "stale disabled group is now active", staleStatus: service.StatusDisabled, wantStatus: 200},
		} {
			t.Run(protocol+"/"+scenario.name, func(t *testing.T) {
				groupID := int64(2)
				freshGroup := &service.Group{ID: groupID, Status: service.StatusActive, Hydrated: true, SubscriptionType: service.SubscriptionTypeStandard, IsExclusive: scenario.exclusive}
				if scenario.freshStatus != "" {
					freshGroup.Status = scenario.freshStatus
				}
				if scenario.subscription {
					freshGroup.SubscriptionType = service.SubscriptionTypeSubscription
				}
				if scenario.missing || scenario.groupErr != nil {
					freshGroup = nil
				}
				staleGroup := &service.Group{ID: groupID, Status: service.StatusActive, Hydrated: true, SubscriptionType: service.SubscriptionTypeStandard}
				if scenario.staleStatus != "" {
					staleGroup.Status = scenario.staleStatus
				}
				user := &service.User{ID: 1, Status: service.StatusActive, Role: service.RoleUser, Balance: 10}
				key := &service.APIKey{ID: 3, Key: "group-guard-admission", UserID: user.ID, User: user, Status: service.StatusActive, GroupID: &groupID, Group: staleGroup}
				groupRepo := &admissionGroupGuardRepo{group: freshGroup, err: scenario.groupErr}
				subRepo := &stubUserSubscriptionRepo{getActive: func(context.Context, int64, int64) (*service.UserSubscription, error) {
					if scenario.invalidSub {
						return &service.UserSubscription{ID: 4, UserID: user.ID, GroupID: groupID, Status: service.StatusDisabled}, nil
					}
					return nil, scenario.subErr
				}}
				cfg := &config.Config{RunMode: config.RunModeStandard}
				subService := service.NewSubscriptionService(groupRepo, subRepo, nil, nil, cfg)
				t.Cleanup(subService.Stop)
				keyService := service.NewAPIKeyService(&stubApiKeyRepo{getByKey: func(context.Context, string) (*service.APIKey, error) {
					copy := *key
					return &copy, nil
				}}, nil, nil, nil, nil, nil, cfg)
				router := gin.New()
				var businessLimited bool
				var businessReason string
				var rejectReason IngressRejectReason
				var rejected bool
				var fallbackGroup *service.Group
				router.Use(func(c *gin.Context) {
					c.Next()
					businessLimited = service.HasOpsClientBusinessLimited(c)
					businessReason = c.GetString(service.OpsClientBusinessLimitedReasonKey)
					rejectReason, rejected = GetIngressRejectReason(c)
					fallback, ok := GetOpsFallbackAPIKey(c)
					require.True(t, ok)
					fallbackGroup = fallback.Group
				})
				if protocol == "google" {
					router.Use(APIKeyAuthWithSubscriptionGoogle(keyService, subService, cfg))
				} else {
					router.Use(gin.HandlerFunc(NewAPIKeyAuthMiddleware(keyService, subService, cfg)))
				}
				forwarded := false
				router.GET("/admission", func(c *gin.Context) {
					forwarded = true
					actualKey, ok := GetAPIKeyFromContext(c)
					require.True(t, ok)
					require.Equal(t, freshGroup, actualKey.Group)
					c.Status(http.StatusOK)
				})
				request := httptest.NewRequest(http.MethodGet, "/admission", nil)
				request.Header.Set("x-api-key", key.Key)
				response := httptest.NewRecorder()
				router.ServeHTTP(response, request)

				assert.Equal(t, scenario.wantStatus, response.Code, response.Body.String())
				assert.Equal(t, scenario.wantStatus == http.StatusOK, forwarded)
				assert.Equal(t, 1, groupRepo.reads, "fresh group state must supersede the auth snapshot")
				if scenario.wantCode != "" {
					if protocol == "google" {
						var body googleErrorResponse
						require.NoError(t, json.Unmarshal(response.Body.Bytes(), &body))
						assert.Equal(t, scenario.wantStatus, body.Error.Code)
						assert.Equal(t, scenario.wantMessage, body.Error.Message)
						wantGoogleStatus := "PERMISSION_DENIED"
						if scenario.wantStatus == http.StatusServiceUnavailable {
							wantGoogleStatus = "INTERNAL"
						}
						assert.Equal(t, wantGoogleStatus, body.Error.Status)
					} else {
						var body ErrorResponse
						require.NoError(t, json.Unmarshal(response.Body.Bytes(), &body))
						assert.Equal(t, scenario.wantCode, body.Code)
						assert.Equal(t, scenario.wantMessage, body.Message)
					}
				}
				assert.Equal(t, scenario.wantReject != "", businessLimited)
				assert.Equal(t, scenario.wantReject != "", rejected)
				assert.Equal(t, scenario.wantReject, rejectReason)
				if scenario.wantReject != "" {
					assert.Equal(t, service.OpsClientBusinessLimitedReasonAPIKeyGroupUnavailable, businessReason)
					if freshGroup == nil {
						assert.Nil(t, fallbackGroup, "Ops must receive the authoritative group")
					} else if assert.NotNil(t, fallbackGroup) {
						assert.Equal(t, freshGroup.Status, fallbackGroup.Status)
						assert.Equal(t, freshGroup.IsExclusive, fallbackGroup.IsExclusive)
					}
				} else {
					assert.Empty(t, businessReason)
				}
			})
		}
	}
}
