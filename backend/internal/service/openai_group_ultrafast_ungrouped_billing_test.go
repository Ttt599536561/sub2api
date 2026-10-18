package service

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestOpenAIGatewayServiceRecordUsage_UngroupedPreservesLegacyLongContextBilling(t *testing.T) {
	for _, tc := range []struct {
		name               string
		withResolver       bool
		accountLongContext bool
	}{
		{name: "resolver_account_off", withResolver: true},
		{name: "resolver_account_on", withResolver: true, accountLongContext: true},
		{name: "no_resolver_account_off"},
		{name: "no_resolver_account_on", accountLongContext: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			usageRepo := &openAIRecordUsageLogRepoStub{inserted: true}
			userRepo := &openAIRecordUsageUserRepoStub{}
			svc := newOpenAIRecordUsageServiceForTest(usageRepo, userRepo, &openAIRecordUsageSubRepoStub{}, nil)
			swapInOpenAILadderCatalog(t, svc)
			if tc.withResolver {
				svc.resolver = NewModelPricingResolver(nil, svc.billingService)
			}
			user := &User{ID: 2881}
			apiKey := &APIKey{ID: 1881, User: user}
			account := &Account{
				ID: 3881, Platform: PlatformOpenAI, Type: AccountTypeAPIKey,
				Extra: map[string]any{openAILongContextBillingEnabledKey: tc.accountLongContext},
			}
			require.Equal(t, tc.accountLongContext, account.IsOpenAILongContextBillingEnabled())

			err := svc.RecordUsage(context.Background(), &OpenAIRecordUsageInput{
				APIKey: apiKey, User: user, Account: account,
				Result: &OpenAIForwardResult{
					RequestID: "resp_ungrouped_long_context_" + tc.name,
					Model:     "gpt-5.4", UpstreamModel: "gpt-5.4",
					Usage: OpenAIUsage{InputTokens: 300000, OutputTokens: 2000},
				},
			})
			require.NoError(t, err)
			require.NotNil(t, usageRepo.lastLog)
			wantTotal := 0.78 // 300000 * 2.5e-6 + 2000 * 15e-6
			if tc.accountLongContext {
				wantTotal = 1.545 // 300000 * 5e-6 + 2000 * 22.5e-6
			}
			require.InDelta(t, wantTotal, usageRepo.lastLog.TotalCost, 1e-12)
			require.InDelta(t, wantTotal*1.1, usageRepo.lastLog.ActualCost, 1e-12)
			require.InDelta(t, wantTotal*1.1, userRepo.lastAmount, 1e-12)
			require.Equal(t, tc.accountLongContext, usageRepo.lastLog.LongContextBillingApplied)
		})
	}
}

func TestOpenAIGatewayServiceRecordUsage_NoResolverWithGroupPreservesLegacyBilling(t *testing.T) {
	for _, tc := range []struct {
		name           string
		forceUltrafast bool
	}{
		{name: "disabled_group"},
		{name: "enabled_group_non_target_model", forceUltrafast: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			usageRepo := &openAIRecordUsageLogRepoStub{inserted: true}
			userRepo := &openAIRecordUsageUserRepoStub{}
			svc := newOpenAIRecordUsageServiceForTest(usageRepo, userRepo, &openAIRecordUsageSubRepoStub{}, nil)
			require.Nil(t, svc.resolver)
			// The old no-resolver path does not apply reasoning-effort multipliers.
			// An unrelated request must not silently switch to the unified semantics.
			svc.billingService.fallbackPrices["gpt-5.4"].ReasoningEffortMultipliers = map[string]float64{"high": 3}
			group := &Group{
				ID: 882, Platform: PlatformOpenAI, Hydrated: true, RateMultiplier: 0.5,
				ForceOpenAIUltrafast: tc.forceUltrafast,
			}
			user := &User{ID: 2882}
			apiKey := &APIKey{ID: 1882, GroupID: &group.ID, Group: group, User: user}
			effort := "high"
			tokens := UsageTokens{InputTokens: 1000, OutputTokens: 500}
			legacy, err := svc.billingService.calculateCostWithServiceTierPolicy("gpt-5.4", tokens, group.RateMultiplier, "", false)
			require.NoError(t, err)

			err = svc.RecordUsage(context.Background(), &OpenAIRecordUsageInput{
				APIKey: apiKey, User: user,
				Account: &Account{ID: 3882, Platform: PlatformOpenAI, Type: AccountTypeAPIKey},
				Result: &OpenAIForwardResult{
					RequestID: "resp_group_no_resolver_" + tc.name,
					Model:     "gpt-5.4", UpstreamModel: "gpt-5.4", ReasoningEffort: &effort,
					Usage: OpenAIUsage{InputTokens: tokens.InputTokens, OutputTokens: tokens.OutputTokens},
				},
			})
			require.NoError(t, err)
			require.NotNil(t, usageRepo.lastLog)
			require.InDelta(t, legacy.TotalCost, usageRepo.lastLog.TotalCost, 1e-12)
			require.InDelta(t, legacy.ActualCost, usageRepo.lastLog.ActualCost, 1e-12)
			require.InDelta(t, legacy.ActualCost, userRepo.lastAmount, 1e-12)
		})
	}
}

func TestOpenAIGatewayServiceRecordUsage_GroupUltrafastWithoutResolverKeepsEightTimesStandard(t *testing.T) {
	for _, model := range []string{"gpt-6.1-sol", "gpt-6-astra"} {
		t.Run(model, func(t *testing.T) {
			usageRepo := &openAIRecordUsageLogRepoStub{inserted: true}
			userRepo := &openAIRecordUsageUserRepoStub{}
			svc := newOpenAIRecordUsageServiceForTest(usageRepo, userRepo, &openAIRecordUsageSubRepoStub{}, nil)
			require.Nil(t, svc.resolver)
			group := &Group{
				ID: 883, Platform: PlatformOpenAI, Hydrated: true, RateMultiplier: 0.5,
				ForceOpenAIUltrafast: true, FreeOpenAIFast: true,
			}
			user := &User{ID: 2883}
			apiKey := &APIKey{ID: 1883, GroupID: &group.ID, Group: group, User: user}
			tier := OpenAIFastTierUltrafast
			tokens := UsageTokens{InputTokens: 100, OutputTokens: 50, CacheReadTokens: 25, CacheCreationTokens: 10}
			standard, err := svc.billingService.calculateCostWithServiceTierPolicy(model, tokens, group.RateMultiplier, "", false)
			require.NoError(t, err)

			err = svc.RecordUsage(context.Background(), &OpenAIRecordUsageInput{
				APIKey: apiKey, User: user,
				Account: &Account{ID: 3883, Platform: PlatformOpenAI, Type: AccountTypeAPIKey},
				Result: &OpenAIForwardResult{
					RequestID: "resp_group_ultrafast_no_resolver_" + model,
					Model:     model, UpstreamModel: model, ServiceTier: &tier,
					Usage: OpenAIUsage{
						InputTokens:  tokens.InputTokens + tokens.CacheReadTokens + tokens.CacheCreationTokens,
						OutputTokens: tokens.OutputTokens, CacheReadInputTokens: tokens.CacheReadTokens,
						CacheCreationInputTokens: tokens.CacheCreationTokens,
					},
				},
			})
			require.NoError(t, err)
			require.NotNil(t, usageRepo.lastLog)
			require.InDelta(t, standard.TotalCost*8, usageRepo.lastLog.TotalCost, 1e-12)
			require.InDelta(t, standard.ActualCost*8, usageRepo.lastLog.ActualCost, 1e-12)
			require.InDelta(t, standard.CacheReadCost*8, usageRepo.lastLog.CacheReadCost, 1e-12)
			require.InDelta(t, standard.CacheCreationCost*8, usageRepo.lastLog.CacheCreationCost, 1e-12)
			require.InDelta(t, standard.ActualCost*8, userRepo.lastAmount, 1e-12)
			require.Equal(t, tier, *usageRepo.lastLog.ServiceTier)
		})
	}
}
