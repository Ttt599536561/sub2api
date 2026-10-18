//go:build unit

package service

import (
	"context"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/pkg/openai"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func channelRetirementCatalogService(groupID int64, mapping map[string]string) *ChannelService {
	return newTestChannelService(makeStandardRepo(Channel{
		ID: 1, Status: StatusActive, GroupIDs: []int64{groupID},
		ModelMapping: map[string]map[string]string{PlatformOpenAI: mapping},
	}, map[int64]string{groupID: PlatformOpenAI}))
}

func TestChannelRetirementCatalogDefaultAndGeneratedLists(t *testing.T) {
	parentID := int64(90)
	oauth := Account{ID: 1, Platform: PlatformOpenAI, Type: AccountTypeOAuth}
	api := Account{ID: 2, Platform: PlatformOpenAI, Type: AccountTypeAPIKey}
	for _, tc := range []struct {
		name       string
		accounts   []Account
		mapping    map[string]string
		wantLegacy bool
	}{
		{"oauth-replacement", []Account{oauth}, map[string]string{"gpt-5.5": "gpt-6.1-sol"}, true},
		{"setup-token-replacement", []Account{{ID: 1, Platform: PlatformOpenAI, Type: AccountTypeSetupToken}}, map[string]string{"gpt-5.5": "gpt-6.1-sol"}, true},
		{"shadow-replacement", []Account{{ID: 1, Platform: PlatformOpenAI, Type: AccountTypeAPIKey, ParentAccountID: &parentID}}, map[string]string{"gpt-5.5": "gpt-6.1-sol"}, true},
		{"oauth-retired-target", []Account{oauth}, map[string]string{"gpt-5.5": "gpt-5.5-2026-04-23"}, false},
		{"oauth-unmapped", []Account{oauth}, nil, false},
		{"apikey-exception", []Account{api}, nil, true},
		{"mixed-source-exception", []Account{oauth, api}, nil, true},
		{"account-remaps-back-to-retired", []Account{{ID: 1, Platform: PlatformOpenAI, Type: AccountTypeOAuth, Credentials: map[string]any{"model_mapping": map[string]any{"gpt-5.5": "gpt-6.1-sol", "gpt-6.1-sol": "gpt-5.5"}}}}, map[string]string{"gpt-5.5": "gpt-6.1-sol"}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			group := &Group{ID: 81, Platform: PlatformOpenAI}
			repo := codexModelsVisibilityAccountRepo{byGroup: map[int64][]Account{group.ID: tc.accounts}}
			channel := channelRetirementCatalogService(group.ID, tc.mapping)
			gateway := &GatewayService{accountRepo: repo, channelService: channel}
			t.Run("ordinary", func(t *testing.T) {
				models := gateway.GetAvailableModels(context.Background(), &group.ID, PlatformOpenAI)
				if models == nil {
					models = openai.DefaultModelIDs()
				}
				require.Equal(t, tc.wantLegacy, containsModelID(models, "gpt-5.5"), models)
			})
			t.Run("gateway-codex", func(t *testing.T) {
				body, err := gateway.BuildCodexModelsManifestForGroup(context.Background(), group, "", []string{"gpt-5.5", "gpt-6.1-sol"})
				require.NoError(t, err)
				require.Equal(t, tc.wantLegacy, containsManifestModel(body, "gpt-5.5"))
			})
			t.Run("configured-codex", func(t *testing.T) {
				// Configured discovery only runs when some account explicitly maps a
				// public name; unmapped members then supplement its default catalog.
				configuredAccounts := append([]Account(nil), tc.accounts...)
				configuredAccounts = append(configuredAccounts, Account{ID: 99, Platform: PlatformOpenAI, Type: AccountTypeOAuth,
					Credentials: map[string]any{"model_mapping": map[string]any{"catalog-extra": "gpt-6.1-sol"}}})
				configuredRepo := codexModelsVisibilityAccountRepo{byGroup: map[int64][]Account{group.ID: configuredAccounts}}
				svc := &OpenAIGatewayService{accountRepo: configuredRepo, channelService: channel}
				response, configured, err := svc.BuildGroupConfiguredCodexModelsManifest(context.Background(), group, "")
				require.NoError(t, err)
				require.True(t, configured)
				require.Equal(t, tc.wantLegacy, containsManifestModel(response.Body, "gpt-5.5"))
			})
		})
	}
}

func TestChannelRetirementCatalogFinalFilterAndETag(t *testing.T) {
	group := &Group{ID: 82, Platform: PlatformOpenAI, CodexModelsManifestConfig: GroupCodexModelsManifestConfig{Enabled: true}}
	svc := &OpenAIGatewayService{
		accountRepo:    codexModelsVisibilityAccountRepo{byGroup: map[int64][]Account{group.ID: {{ID: 1, Platform: PlatformOpenAI, Type: AccountTypeOAuth}}}},
		channelService: channelRetirementCatalogService(group.ID, map[string]string{"gpt-5.5": "gpt-6.1-sol"}),
	}
	for _, tc := range []struct {
		name, body string
		changed    bool
	}{
		{"unchanged", `{ "models": [{"slug":"gpt-5.5","display_name":"Public alias","unknown":{"keep":1}}], "unknown_top":true }`, false},
		{"retired-source-removed", `{"models":[{"slug":"gpt-5.5","display_name":"Public alias","unknown":{"keep":1}},{"slug":"gpt-5.5-2026-04-23"}],"unknown_top":true}`, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			original := []byte(tc.body)
			etag := CodexModelsManifestETag(original)
			response := &OpenAIModelsResponse{Body: append([]byte(nil), original...), ETag: etag}
			require.NoError(t, svc.MergeGroupConfiguredCodexModels(context.Background(), group, response, ""))
			require.True(t, containsManifestModel(response.Body, "gpt-5.5"), string(response.Body))
			require.False(t, containsManifestModel(response.Body, "gpt-5.5-2026-04-23"))
			require.Equal(t, "Public alias", gjson.GetBytes(response.Body, "models.0.display_name").String())
			require.Equal(t, int64(1), gjson.GetBytes(response.Body, "models.0.unknown.keep").Int())
			require.True(t, gjson.GetBytes(response.Body, "unknown_top").Bool())
			if tc.changed {
				require.NotEqual(t, etag, response.ETag)
			} else {
				require.Equal(t, original, response.Body)
				require.Equal(t, etag, response.ETag)
			}
			require.Equal(t, CodexModelsManifestETag(response.Body), response.ETag)
			finalETag := response.ETag
			require.NoError(t, svc.MergeGroupConfiguredCodexModels(context.Background(), group, response, finalETag))
			require.True(t, response.NotModified)
			require.Equal(t, finalETag, response.ETag)
			require.Equal(t, tc.body, string(original), "shared source must not be mutated")
		})
	}
}

func TestChannelRetirementCatalogPinnedProjection(t *testing.T) {
	body := `{"models":[{"slug":"gpt-5.5","display_name":"Public alias","unknown":{"keep":1}},{"slug":"gpt-5.5-2026-04-23"},{"slug":"gpt-6.1-sol"}],"unknown_top":true}`
	_, calls := newCodexModelsOAuthCacheServer(t, body)
	account := newCodexModelsTestAccount()
	account.Status, account.Schedulable = StatusActive, true
	group := &Group{ID: 83, Platform: PlatformOpenAI, CodexModelsManifestConfig: GroupCodexModelsManifestConfig{Enabled: true, AccountIDs: []int64{account.ID}}}
	svc := &OpenAIGatewayService{
		accountRepo:    splitCodexModelsAccountRepo{all: map[int64][]Account{group.ID: {*account}}},
		channelService: channelRetirementCatalogService(group.ID, map[string]string{"gpt-5.5": "gpt-6.1-sol"}),
	}
	t.Run("ordinary", func(t *testing.T) {
		response, _, err := svc.FetchPinnedOpenAIModelsList(context.Background(), group, 1, "")
		require.NoError(t, err)
		require.Equal(t, "gpt-5.5", gjson.GetBytes(response.Body, "data.0.id").String())
		require.Equal(t, "Public alias", gjson.GetBytes(response.Body, "data.0.display_name").String())
		require.Len(t, gjson.GetBytes(response.Body, "data").Array(), 2)
		next, _, err := svc.FetchPinnedOpenAIModelsList(context.Background(), group, 1, response.ETag)
		require.NoError(t, err)
		require.True(t, next.NotModified)
	})
	t.Run("codex", func(t *testing.T) {
		response, _, err := svc.FetchPinnedCodexModelsManifest(context.Background(), group, CodexCanonicalClientVersion())
		require.NoError(t, err)
		require.True(t, containsManifestModel(response.Body, "gpt-5.5"), string(response.Body))
		require.False(t, containsManifestModel(response.Body, "gpt-5.5-2026-04-23"))
		require.Equal(t, int64(1), gjson.GetBytes(response.Body, "models.0.unknown.keep").Int())
		require.True(t, gjson.GetBytes(response.Body, "unknown_top").Bool())
		require.Equal(t, CodexModelsManifestETag(response.Body), response.ETag)
	})
	require.EqualValues(t, 1, calls.Load(), "account discovery cache should stay shared across projections")
}
