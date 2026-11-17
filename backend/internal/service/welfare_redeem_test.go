//go:build unit

package service

import (
	"context"
	"encoding/json"
	"math"
	"testing"

	"github.com/stretchr/testify/require"
)

type supplementGenerateRepo struct{ RedeemCodeRepository }

func (*supplementGenerateRepo) Create(_ context.Context, _ *RedeemCode) error { return nil }

func TestWelfareSupplementGenerateRejectsWrongType(t *testing.T) {
	var input GenerateRedeemCodesInput
	require.NoError(t, json.Unmarshal([]byte(`{"Count":1,"Type":"balance","SubscriptionEffect":"add_quota","WelfareCampaignID":1,"QuotaAmountUSD":12.5}`), &input))
	svc := &adminServiceImpl{redeemCodeRepo: &supplementGenerateRepo{}}
	_, err := svc.GenerateRedeemCodes(context.Background(), &input)
	require.Error(t, err, "a welfare supplement must never silently become a balance code")
}

func TestWelfareSupplementGenerateRejectsMetadataWithoutEffect(t *testing.T) {
	var input GenerateRedeemCodesInput
	require.NoError(t, json.Unmarshal([]byte(`{"Count":1,"Type":"balance","WelfareCampaignID":1}`), &input))
	svc := &adminServiceImpl{redeemCodeRepo: &supplementGenerateRepo{}}
	_, err := svc.GenerateRedeemCodes(context.Background(), &input)
	require.Error(t, err, "campaign metadata without an effect must be rejected")
}

func TestWelfareSupplementValidation(t *testing.T) {
	campaign := int64(1)
	amount := 12.34
	valid := GenerateRedeemCodesInput{Count: 1, Type: RedeemTypeSubscription, SubscriptionEffect: SubscriptionEffectAddQuota, WelfareCampaignID: &campaign, QuotaAmountUSD: &amount}
	require.NoError(t, validateWelfareSupplement(&valid))
	for _, value := range []float64{0, -1, 1e10, 0.001, math.NaN(), math.Inf(1)} {
		input := valid
		input.QuotaAmountUSD = &value
		require.Error(t, validateWelfareSupplement(&input))
	}
	input := valid
	input.ValidityDays = 1
	require.Error(t, validateWelfareSupplement(&input))
	input = valid
	input.SubscriptionEffect = SubscriptionEffectExtendDuration
	input.QuotaAmountUSD = nil
	input.ValidityDays = 45
	require.NoError(t, validateWelfareSupplement(&input))
	input.ValidityDays = MaxValidityDays + 1
	require.Error(t, validateWelfareSupplement(&input))
	input.ValidityDays = 0
	require.Error(t, validateWelfareSupplement(&input))
}

func TestGenericSubscriptionSupplementValidation(t *testing.T) {
	groupID := int64(3)
	amount := 12.34
	valid := GenerateRedeemCodesInput{Count: 1, Type: RedeemTypeSubscription, GroupID: &groupID,
		SubscriptionEffect: "add_balance", QuotaAmountUSD: &amount}
	require.NoError(t, validateWelfareSupplement(&valid), "generic supplements need a group, not an invitation campaign")
	for _, value := range []float64{0, -1, 1e10, 0.001, math.NaN(), math.Inf(1)} {
		input := valid
		input.QuotaAmountUSD = &value
		require.Error(t, validateWelfareSupplement(&input))
	}
	input := valid
	input.GroupID = nil
	require.Error(t, validateWelfareSupplement(&input))
	input = valid
	input.SubscriptionEffect, input.QuotaAmountUSD, input.ValidityDays = "extend_subscription", nil, 45
	require.NoError(t, validateWelfareSupplement(&input))
	input.ValidityDays = 0
	require.Error(t, validateWelfareSupplement(&input))
	input.ValidityDays = MaxValidityDays + 1
	require.Error(t, validateWelfareSupplement(&input))
}
