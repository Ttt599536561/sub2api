package service

import (
	"context"
	"github.com/stretchr/testify/require"
	"testing"
	"time"
)

// Apply the real repository's platform and persistent scheduling filters.
// Transient rate limits do not remove accounts from capability intersection.
type providerCatalogFilteredAccountRepo struct {
	AccountRepository
	visible, catalog []Account
	platforms        []string
}

func (r *providerCatalogFilteredAccountRepo) ListSchedulableByGroupID(context.Context, int64) ([]Account, error) {
	return append([]Account(nil), r.visible...), nil
}
func (r *providerCatalogFilteredAccountRepo) ListModelAvailabilityCandidates(_ context.Context, _ *int64, platforms []string, _ bool) ([]Account, error) {
	r.platforms = append([]string(nil), platforms...)
	accounts := make([]Account, 0, len(r.catalog))
	for _, account := range r.catalog {
		if account.Status != StatusActive || !account.Schedulable {
			continue
		}
		for _, platform := range platforms {
			if account.Platform == platform {
				accounts = append(accounts, account)
				break
			}
		}
	}
	return accounts, nil
}
func TestProviderCatalogIncludesAggregatorSyncedMetadata(t *testing.T) {
	for _, platform := range []string{PlatformCommandCode, PlatformCline} {
		for _, groupPlatform := range []string{platform, PlatformComposite} {
			t.Run(platform+"/"+groupPlatform, func(t *testing.T) {
				account := newCodexCatalogMappedAccount(101, "anthropic/claude-sonnet-4-6", "Snapshot model", []string{"high", "max"}, []string{"text", "image"}, 123456, true, nil)
				account.Platform = platform
				repo := &providerCatalogFilteredAccountRepo{visible: []Account{account}, catalog: []Account{account}}
				svc := &GatewayService{accountRepo: repo}
				body, err := svc.BuildCodexModelsManifestForGroup(context.Background(), &Group{ID: 77, Platform: groupPlatform}, "", []string{"my-coder"})
				require.NoError(t, err)
				models := decodeCodexManifestModels(t, body)
				require.Len(t, models, 1)
				require.EqualValues(t, 123456, models[0]["context_window"])
				require.Equal(t, []any{"text", "image"}, models[0]["input_modalities"])
				require.Equal(t, []string{"high", "max"}, effortsFromManifestModel(t, models[0]))
				require.Contains(t, repo.platforms, platform)
				require.NotContains(t, repo.platforms, PlatformTypeSafe)
			})
		}
	}
}
func TestProviderCatalogIntersectsTemporarilyUnavailableAggregatorAccounts(t *testing.T) {
	for _, platform := range []string{PlatformCommandCode, PlatformCline} {
		t.Run(platform, func(t *testing.T) {
			available := newCodexCatalogMappedAccount(101, "provider/model", "Available", []string{"high", "max"}, []string{"text", "image"}, 123456, true, nil)
			temporary := newCodexCatalogMappedAccount(102, "provider/model", "Temporary", []string{"high"}, []string{"text"}, 100000, true, nil)
			disabled := newCodexCatalogMappedAccount(103, "provider/model", "Disabled", []string{"low"}, []string{"text"}, 1, false, nil)
			until := time.Now().Add(time.Hour)
			temporary.RateLimitResetAt = &until
			available.Platform, temporary.Platform, disabled.Platform = platform, platform, platform
			repo := &providerCatalogFilteredAccountRepo{visible: []Account{available}, catalog: []Account{available, temporary, disabled}}
			svc := &GatewayService{accountRepo: repo}
			body, err := svc.BuildCodexModelsManifestForGroup(context.Background(), &Group{ID: 77, Platform: PlatformComposite}, "", []string{"my-coder"})
			require.NoError(t, err)
			models := decodeCodexManifestModels(t, body)
			require.Len(t, models, 1)
			require.EqualValues(t, 100000, models[0]["context_window"])
			require.Equal(t, []any{"text"}, models[0]["input_modalities"])
			require.Equal(t, []string{"high"}, effortsFromManifestModel(t, models[0]))
		})
	}
}
func TestClineDefaultAdminModelCandidatesUseProviderModel(t *testing.T) {
	svc := &adminServiceImpl{}
	candidates, err := svc.GetGroupModelsListCandidates(context.Background(), 0, PlatformCline)
	require.NoError(t, err)
	require.Equal(t, []string{DefaultClineTestModel}, candidates)
	require.Contains(t, compositeDefaultModelsListCandidateIDs(), DefaultClineTestModel)
}
