//go:build unit

package admin

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type ultrafastPartialUpdatePolicy struct {
	fast      bool
	ultrafast bool
	free      bool
}

func ultrafastPartialUpdatePolicyFromGroup(group *service.Group) ultrafastPartialUpdatePolicy {
	return ultrafastPartialUpdatePolicy{
		fast:      group.ForceOpenAIFast,
		ultrafast: group.ForceOpenAIUltrafast,
		free:      group.FreeOpenAIFast,
	}
}

type ultrafastPartialUpdateGroupRepo struct {
	service.AdminGroupRepository
	saved       service.Group
	updateCalls int
}

func (r *ultrafastPartialUpdateGroupRepo) GetByID(_ context.Context, id int64) (*service.Group, error) {
	if id != r.saved.ID {
		return nil, service.ErrGroupNotFound
	}
	// Return a copy so mutating the loaded group cannot masquerade as a save.
	group := r.saved
	return &group, nil
}

func (r *ultrafastPartialUpdateGroupRepo) Update(_ context.Context, group *service.Group) error {
	r.saved = *group
	r.updateCalls++
	return nil
}

type ultrafastPartialUpdateAuthInvalidator struct {
	service.APIKeyAuthCacheInvalidator
	repo          *ultrafastPartialUpdateGroupRepo
	groupIDs      []int64
	savedPolicies []ultrafastPartialUpdatePolicy
}

func (s *ultrafastPartialUpdateAuthInvalidator) InvalidateAuthCacheByGroupID(_ context.Context, id int64) {
	s.groupIDs = append(s.groupIDs, id)
	s.savedPolicies = append(s.savedPolicies, ultrafastPartialUpdatePolicyFromGroup(&s.repo.saved))
}

func TestGroupHandlerUpdateOpenAIFastPolicyPartialUpdates(t *testing.T) {
	previousMode := gin.Mode()
	gin.SetMode(gin.TestMode)
	t.Cleanup(func() { gin.SetMode(previousMode) })

	tests := []struct {
		name             string
		platform         string
		initialFast      bool
		initialUltrafast bool
		body             string
		wantPlatform     string
		wantFast         bool
		wantUltrafast    bool
	}{
		{
			name:     "fast_only_replaces_stored_ultrafast",
			platform: service.PlatformOpenAI, initialUltrafast: true,
			body:         `{"force_openai_fast":true}`,
			wantPlatform: service.PlatformOpenAI, wantFast: true,
		},
		{
			name:     "fast_only_preserves_existing_fast",
			platform: service.PlatformOpenAI, initialFast: true,
			body:         `{"force_openai_fast":true}`,
			wantPlatform: service.PlatformOpenAI, wantFast: true,
		},
		{
			name:     "explicit_both_true_prioritizes_ultrafast",
			platform: service.PlatformOpenAI, initialFast: true,
			body:         `{"force_openai_fast":true,"force_openai_ultrafast":true}`,
			wantPlatform: service.PlatformOpenAI, wantUltrafast: true,
		},
		{
			name:     "ultrafast_only_replaces_stored_fast",
			platform: service.PlatformOpenAI, initialFast: true,
			body:         `{"force_openai_ultrafast":true}`,
			wantPlatform: service.PlatformOpenAI, wantUltrafast: true,
		},
		{
			name:     "explicit_fast_on_ultrafast_off",
			platform: service.PlatformOpenAI, initialUltrafast: true,
			body:         `{"force_openai_fast":true,"force_openai_ultrafast":false}`,
			wantPlatform: service.PlatformOpenAI, wantFast: true,
		},
		{
			name:     "disabling_fast_does_not_disable_ultrafast",
			platform: service.PlatformOpenAI, initialUltrafast: true,
			body:         `{"force_openai_fast":false}`,
			wantPlatform: service.PlatformOpenAI, wantUltrafast: true,
		},
		{
			name:     "disabling_ultrafast_does_not_disable_fast",
			platform: service.PlatformOpenAI, initialFast: true,
			body:         `{"force_openai_ultrafast":false}`,
			wantPlatform: service.PlatformOpenAI, wantFast: true,
		},
		{
			name:     "disabling_ultrafast_preserves_disabled_fast",
			platform: service.PlatformOpenAI, initialUltrafast: true,
			body:         `{"force_openai_ultrafast":false}`,
			wantPlatform: service.PlatformOpenAI,
		},
		{
			name:     "omitted_policy_fields_preserve_ultrafast",
			platform: service.PlatformOpenAI, initialUltrafast: true,
			body:         `{"name":"renamed-ultrafast"}`,
			wantPlatform: service.PlatformOpenAI, wantUltrafast: true,
		},
		{
			name:     "omitted_policy_fields_preserve_fast",
			platform: service.PlatformOpenAI, initialFast: true,
			body:         `{"name":"renamed-fast"}`,
			wantPlatform: service.PlatformOpenAI, wantFast: true,
		},
		{
			name:         "empty_update_preserves_disabled_policy",
			platform:     service.PlatformOpenAI,
			body:         `{}`,
			wantPlatform: service.PlatformOpenAI,
		},
		{
			name:     "switch_to_composite_clears_ultrafast",
			platform: service.PlatformOpenAI, initialUltrafast: true,
			body:         `{"platform":"composite"}`,
			wantPlatform: service.PlatformComposite,
		},
		{
			name:     "composite_rejects_ultrafast_without_clearing_fast",
			platform: service.PlatformComposite, initialFast: true,
			body:         `{"force_openai_ultrafast":true}`,
			wantPlatform: service.PlatformComposite, wantFast: true,
		},
		{
			name:         "composite_both_true_keeps_fast_only",
			platform:     service.PlatformComposite,
			body:         `{"force_openai_fast":true,"force_openai_ultrafast":true}`,
			wantPlatform: service.PlatformComposite, wantFast: true,
		},
		{
			name:     "switch_to_anthropic_clears_openai_policies",
			platform: service.PlatformOpenAI, initialUltrafast: true,
			body:         `{"platform":"anthropic"}`,
			wantPlatform: service.PlatformAnthropic,
		},
		{
			name:         "anthropic_normalizes_both_force_flags_to_false",
			platform:     service.PlatformAnthropic,
			body:         `{"force_openai_fast":true,"force_openai_ultrafast":true}`,
			wantPlatform: service.PlatformAnthropic,
		},
	}

	for _, tt := range tests {
		for _, initialFree := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/free_%t", tt.name, initialFree), func(t *testing.T) {
				repo := &ultrafastPartialUpdateGroupRepo{saved: service.Group{
					ID: 42, Name: "openai-policy-update", Platform: tt.platform,
					Status: service.StatusActive, RateMultiplier: 1,
					SubscriptionType:     service.SubscriptionTypeStandard,
					ForceOpenAIFast:      tt.initialFast,
					ForceOpenAIUltrafast: tt.initialUltrafast,
					FreeOpenAIFast:       initialFree,
				}}
				invalidator := &ultrafastPartialUpdateAuthInvalidator{repo: repo}
				adminService := service.NewAdminService(
					nil, nil, repo, nil, nil, nil, nil, nil, nil, nil, nil, nil,
					invalidator, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil,
				)
				handler := NewGroupHandler(adminService, nil, nil)
				router := gin.New()
				router.PUT("/api/v1/admin/groups/:id", handler.Update)

				request := httptest.NewRequest(http.MethodPut, "/api/v1/admin/groups/42", strings.NewReader(tt.body))
				request.Header.Set("Content-Type", "application/json")
				recorder := httptest.NewRecorder()
				router.ServeHTTP(recorder, request)

				require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
				var response struct {
					Data struct {
						ID             int64  `json:"id"`
						Platform       string `json:"platform"`
						ForceFast      *bool  `json:"force_openai_fast"`
						ForceUltrafast *bool  `json:"force_openai_ultrafast"`
						FreeFast       *bool  `json:"free_openai_fast"`
					} `json:"data"`
				}
				require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &response))
				require.NotNil(t, response.Data.ForceFast)
				require.NotNil(t, response.Data.ForceUltrafast)
				require.NotNil(t, response.Data.FreeFast)
				wantFree := initialFree
				if tt.wantPlatform != service.PlatformOpenAI && tt.wantPlatform != service.PlatformComposite {
					wantFree = false
				}
				wantPolicy := ultrafastPartialUpdatePolicy{
					fast: tt.wantFast, ultrafast: tt.wantUltrafast, free: wantFree,
				}
				responsePolicy := ultrafastPartialUpdatePolicy{
					fast:      *response.Data.ForceFast,
					ultrafast: *response.Data.ForceUltrafast,
					free:      *response.Data.FreeFast,
				}
				t.Logf("body=%s response=%+v saved=%+v invalidations=%v", tt.body, responsePolicy, ultrafastPartialUpdatePolicyFromGroup(&repo.saved), invalidator.groupIDs)
				assert.Equal(t, int64(42), response.Data.ID)
				assert.Equal(t, tt.wantPlatform, response.Data.Platform)
				assert.Equal(t, wantPolicy, responsePolicy, "admin response policy")
				assert.Equal(t, 1, repo.updateCalls, "persist exactly once")
				assert.Equal(t, tt.wantPlatform, repo.saved.Platform)
				assert.Equal(t, wantPolicy, ultrafastPartialUpdatePolicyFromGroup(&repo.saved), "persisted policy")
				assert.Equal(t, []int64{42}, invalidator.groupIDs, "invalidate bound API keys")
				assert.Equal(t, []ultrafastPartialUpdatePolicy{wantPolicy}, invalidator.savedPolicies, "invalidation observes the saved policy")
			})
		}
	}
}
