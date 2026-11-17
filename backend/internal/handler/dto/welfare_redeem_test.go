package dto

import (
	"encoding/json"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
	"testing"
	"time"
)

func TestRedeemCodeWelfareSupplementOptionalContract(t *testing.T) {
	ordinary, err := json.Marshal(RedeemCodeFromService(&service.RedeemCode{Type: service.RedeemTypeSubscription}))
	require.NoError(t, err)
	for _, field := range []string{"subscription_effect", "welfare_campaign_id", "welfare_campaign_name", "quota_amount_usd", "welfare_result"} {
		require.NotContains(t, string(ordinary), field)
	}
	campaign := int64(9)
	quota := 20.0
	expiry := time.Date(2026, 12, 1, 0, 0, 0, 0, time.UTC)
	code := &service.RedeemCode{Type: service.RedeemTypeSubscription, SubscriptionEffect: "add_quota", WelfareCampaignID: &campaign, WelfareCampaignName: "welcome", QuotaAmountUSD: &quota,
		WelfareResult: &service.WelfareSupplementResult{SubscriptionID: 3, MonthlyLimitUSD: 120, MonthlyUsageUSD: 70, RemainingUSD: 50, ExpiresAt: expiry}}
	got := RedeemCodeFromService(code)
	require.Equal(t, "add_quota", got.SubscriptionEffect)
	require.Equal(t, int64(9), *got.WelfareCampaignID)
	require.Equal(t, 50.0, got.WelfareResult.RemainingUSD)
	data, err := json.Marshal(got)
	require.NoError(t, err)
	require.Contains(t, string(data), `"subscription_id":3`)
	require.Contains(t, string(data), `"expires_at":"2026-12-01T00:00:00Z"`)
}

func TestSubscriptionSupplementPublicContract(t *testing.T) {
	sub := UserSubscriptionFromService(&service.UserSubscription{ID: 3, EntitlementKind: "standard", SupplementGrantedUSD: 10, SupplementUsedUSD: 12})
	data, err := json.Marshal(sub)
	require.NoError(t, err)
	require.Contains(t, string(data), `"supplement_granted_usd":10`)
	require.Contains(t, string(data), `"supplement_used_usd":12`)
	require.Contains(t, string(data), `"supplement_remaining_usd":0`)
	code := RedeemCodeFromService(&service.RedeemCode{Type: service.RedeemTypeSubscription, SubscriptionEffect: service.SubscriptionEffectAddBalance,
		SubscriptionSupplementResult: &service.SubscriptionSupplementResult{SubscriptionID: 3, GroupID: 2, EntitlementKind: "standard", SubscriptionEffect: service.SubscriptionEffectAddBalance, AmountUSD: 10, SupplementGrantedUSD: 10, SupplementRemainingUSD: 10}})
	data, err = json.Marshal(code)
	require.NoError(t, err)
	require.Contains(t, string(data), `"subscription_supplement_result":{"subscription_id":3`)
	require.NotContains(t, string(data), `"welfare_result"`)
}
