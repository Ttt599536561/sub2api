package handler

import (
	"encoding/json"
	"github.com/Wei-Shaw/sub2api/internal/pkg/claude"
	middleware2 "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestClineUnmappedModelsUseProviderDefaultAcrossCatalogs(t *testing.T) {
	gin.SetMode(gin.TestMode)
	const groupID int64 = 777
	for _, tc := range []struct {
		name            string
		allowlist, want []string
	}{
		{name: "default", want: []string{service.DefaultClineTestModel}},
		{name: "allowlisted", allowlist: []string{service.DefaultClineTestModel, "claude-sonnet-4-6"}, want: []string{service.DefaultClineTestModel}},
		{name: "unavailable allowlist", allowlist: []string{"claude-sonnet-4-6"}, want: []string{}},
	} {
		for _, codex := range []bool{false, true} {
			name := tc.name + "/models"
			if codex {
				name = tc.name + "/codex"
			}
			t.Run(name, func(t *testing.T) {
				repo := &gatewayModelsAccountRepoStub{byGroup: map[int64][]service.Account{groupID: {{ID: 1, Platform: service.PlatformCline, Type: service.AccountTypeAPIKey, Credentials: map[string]any{"api_key": "test"}}}}}
				h := newGatewayModelsHandlerForTest(repo)
				group := &service.Group{ID: groupID, Platform: service.PlatformCline}
				if tc.allowlist != nil {
					group.ModelAllowlist = service.GroupModelAllowlist{Enabled: true, Models: tc.allowlist}
				}
				rec := httptest.NewRecorder()
				c, _ := gin.CreateTestContext(rec)
				c.Request = httptest.NewRequest(http.MethodGet, "/v1/models", nil)
				c.Set(string(middleware2.ContextKeyAPIKey), &service.APIKey{Group: group})
				if codex {
					h.CodexModels(c)
					var got codexModelsResponseForTest
					require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &got))
					require.ElementsMatch(t, tc.want, codexModelSlugsForTest(got.Models))
				} else {
					h.Models(c)
					var got gatewayModelsResponseForTest
					require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &got))
					require.Equal(t, "list", got.Object)
					require.ElementsMatch(t, tc.want, modelIDsForTest(got.Data))
					for _, model := range got.Data {
						// Preserve the existing non-OpenAI catalog representation.
						require.Empty(t, model.Object)
						require.Empty(t, model.OwnedBy)
					}
				}
				require.Equal(t, http.StatusOK, rec.Code)
			})
		}
	}
	require.Equal(t, []string{service.DefaultClineTestModel}, defaultModelIDsForPlatform(service.PlatformCline))
}
func TestAnthropicDefaultModelsPreserveOriginalDescriptors(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h := newGatewayModelsHandlerForTest(&gatewayModelsAccountRepoStub{})
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodGet, "/v1/models", nil)
	c.Set(string(middleware2.ContextKeyAPIKey), &service.APIKey{Group: &service.Group{ID: 777, Platform: service.PlatformAnthropic}})
	h.Models(c)
	expected, err := json.Marshal(gin.H{"object": "list", "data": claude.DefaultModels})
	require.NoError(t, err)
	require.JSONEq(t, string(expected), rec.Body.String())
}
