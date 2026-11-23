//go:build integration

package service_test

import (
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/repository"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func installGenericSupplementMigration(t *testing.T, f *welfareRedemptionFixture) {
	t.Helper()
	migration, err := os.ReadFile("../../migrations/262_subscription_supplement_redemption.sql")
	require.NoError(t, err)
	_, err = f.db.ExecContext(f.ctx, string(migration))
	require.NoError(t, err)
}

func TestLegacyUnusedSupplementAcceptsActiveStandardSubscription(t *testing.T) {
	f := newWelfareRedemptionFixture(t)
	campaign := prepareSupplementFixture(t, f)
	_, err := f.client.UserSubscription.Create().SetUserID(f.userID).SetGroupID(f.group.ID).
		SetStartsAt(time.Now().Add(-time.Hour)).SetExpiresAt(time.Now().Add(24 * time.Hour)).SetMonthlyUsageUsd(100).Save(f.ctx)
	require.NoError(t, err)
	code := supplementCode(t, f, campaign, "add_quota", 10, 0)
	_, err = f.redeemer(f.codes).Redeem(f.ctx, f.userID, code.Code)
	require.NoError(t, err, "an existing ordinary subscription in the fixed group accepts unused legacy supplements")
}

func genericSupplementCode(t *testing.T, f *welfareRedemptionFixture, effect string, amount float64, days int) *service.RedeemCode {
	t.Helper()
	code := &service.RedeemCode{Code: fmt.Sprintf("GENERIC-%d", time.Now().UnixNano()), Type: service.RedeemTypeSubscription,
		Status: service.StatusUnused, GroupID: &f.group.ID, SubscriptionEffect: effect, ValidityDays: days}
	if effect == service.SubscriptionEffectAddBalance {
		code.QuotaAmountUSD = &amount
	}
	require.NoError(t, f.codes.Create(f.ctx, code))
	return code
}

func supplementBalance(t *testing.T, f *welfareRedemptionFixture, id int64) (float64, float64) {
	t.Helper()
	var granted, used float64
	require.NoError(t, f.db.QueryRowContext(f.ctx, `SELECT COALESCE(b.granted_usd,0),COALESCE(b.used_usd,0)
 FROM user_subscriptions s LEFT JOIN user_subscription_supplement_balances b ON b.subscription_id=s.id AND b.term_starts_at=s.starts_at WHERE s.id=$1`, id).Scan(&granted, &used))
	return granted, used
}

func TestSubscriptionSupplementTargetsValidEntitlementAndPreservesUsage(t *testing.T) {
	for _, kind := range []string{"standard", "welfare", "both", "expired_welfare"} {
		t.Run(kind, func(t *testing.T) {
			f := newWelfareRedemptionFixture(t)
			prepareSupplementFixture(t, f)
			now := time.Now().UTC().Truncate(time.Microsecond)
			standard, err := f.client.UserSubscription.Create().SetUserID(f.userID).SetGroupID(f.group.ID).
				SetStartsAt(now.Add(-24 * time.Hour)).SetExpiresAt(now.Add(20 * 24 * time.Hour)).
				SetDailyUsageUsd(10).SetWeeklyUsageUsd(30).SetMonthlyUsageUsd(100).SetMonthlyWindowStart(now.Add(-24 * time.Hour)).Save(f.ctx)
			require.NoError(t, err)
			target := standard
			if kind != "standard" {
				welfare, err := f.client.UserSubscription.Create().SetUserID(f.userID).SetGroupID(f.group.ID).
					SetEntitlementKind("welfare").SetWelfareGrantCount(1).SetWelfareMonthlyLimitUsd(100).
					SetStartsAt(now.Add(-24 * time.Hour)).SetExpiresAt(now.Add(15 * 24 * time.Hour)).
					SetDailyUsageUsd(10).SetWeeklyUsageUsd(30).SetMonthlyUsageUsd(100).SetMonthlyWindowStart(now.Add(-24 * time.Hour)).Save(f.ctx)
				require.NoError(t, err)
				if kind == "expired_welfare" {
					_, err = f.client.UserSubscription.UpdateOneID(welfare.ID).SetExpiresAt(now.Add(-time.Hour)).Save(f.ctx)
					require.NoError(t, err)
				} else {
					target = welfare
				}
				if kind == "welfare" {
					_, err = f.client.UserSubscription.UpdateOneID(standard.ID).SetStatus("suspended").Save(f.ctx)
					require.NoError(t, err)
				}
			}
			code := genericSupplementCode(t, f, service.SubscriptionEffectAddBalance, 12.34, 0)
			result, err := f.redeemer(f.codes).Redeem(f.ctx, f.userID, code.Code)
			require.NoError(t, err)
			require.NotNil(t, result.SubscriptionSupplementResult)
			require.Equal(t, int64(target.ID), result.SubscriptionSupplementResult.SubscriptionID)
			require.Equal(t, target.EntitlementKind, result.SubscriptionSupplementResult.EntitlementKind)
			require.Equal(t, 12.34, result.SubscriptionSupplementResult.SupplementRemainingUSD)
			got, err := f.client.UserSubscription.Get(f.ctx, target.ID)
			require.NoError(t, err)
			require.Equal(t, target.ExpiresAt, got.ExpiresAt)
			require.Equal(t, target.WelfareMonthlyLimitUsd, got.WelfareMonthlyLimitUsd)
			_, err = f.db.ExecContext(f.ctx, `UPDATE user_subscription_supplement_balances SET used_usd=20 WHERE subscription_id=$1`, target.ID)
			require.NoError(t, err)
			code2 := genericSupplementCode(t, f, service.SubscriptionEffectAddBalance, 10, 0)
			result2, err := f.redeemer(f.codes).Redeem(f.ctx, f.userID, code2.Code)
			require.NoError(t, err)
			require.InDelta(t, 2.34, result2.SubscriptionSupplementResult.SupplementRemainingUSD, 1e-9)
			addTime := genericSupplementCode(t, f, service.SubscriptionEffectExtendSubscription, 0, 7)
			_, err = f.redeemer(f.codes).Redeem(f.ctx, f.userID, addTime.Code)
			require.NoError(t, err)
			got, err = f.client.UserSubscription.Get(f.ctx, target.ID)
			require.NoError(t, err)
			require.Equal(t, target.StartsAt, got.StartsAt)
			require.Equal(t, target.ExpiresAt.AddDate(0, 0, 7), got.ExpiresAt)
			require.Equal(t, target.MonthlyUsageUsd, got.MonthlyUsageUsd)
			require.Equal(t, target.WeeklyUsageUsd, got.WeeklyUsageUsd)
			require.Equal(t, target.DailyUsageUsd, got.DailyUsageUsd)
			require.Equal(t, target.MonthlyWindowStart, got.MonthlyWindowStart)
			require.Equal(t, target.WelfareGrantCount, got.WelfareGrantCount)
			granted, used := supplementBalance(t, f, target.ID)
			require.Equal(t, 22.34, granted)
			require.Equal(t, 20.0, used)
			history, err := f.codes.GetByID(f.ctx, code.ID)
			require.NoError(t, err)
			require.Equal(t, 12.34, history.SubscriptionSupplementResult.SupplementRemainingUSD, "receipt is a snapshot")
			require.Equal(t, target.ExpiresAt, history.SubscriptionSupplementResult.ExpiresAt)
		})
	}
}

func TestSubscriptionSupplementRejectsInvalidTargetWithoutConsumption(t *testing.T) {
	for _, state := range []string{"missing", "wrong_group", "expired", "paused", "deleted", "closed", "future"} {
		t.Run(state, func(t *testing.T) {
			f := newWelfareRedemptionFixture(t)
			prepareSupplementFixture(t, f)
			if state != "missing" {
				now := time.Now()
				b := f.client.UserSubscription.Create().SetUserID(f.userID).SetGroupID(f.group.ID).SetStartsAt(now.Add(-time.Hour)).SetExpiresAt(now.Add(time.Hour))
				switch state {
				case "wrong_group":
					g, err := f.client.Group.Create().SetName("other").SetSubscriptionType("subscription").Save(f.ctx)
					require.NoError(t, err)
					b.SetGroupID(g.ID)
				case "expired":
					b.SetExpiresAt(now.Add(-time.Second))
				case "paused":
					b.SetStatus("suspended")
				case "deleted":
					b.SetDeletedAt(now)
				case "closed":
					b.SetEntitlementKind("welfare").SetWelfareMonthlyLimitUsd(100).SetWelfareGrantCount(1).SetWelfareCycleClosed(true)
				case "future":
					b.SetStartsAt(now.Add(time.Minute))
				}
				_, err := b.Save(f.ctx)
				require.NoError(t, err)
			}
			code := genericSupplementCode(t, f, service.SubscriptionEffectAddBalance, 1, 0)
			_, err := f.redeemer(f.codes).Redeem(f.ctx, f.userID, code.Code)
			require.ErrorIs(t, err, service.ErrSubscriptionSupplementNoActiveSubscription)
			unused, err := f.codes.GetByID(f.ctx, code.ID)
			require.NoError(t, err)
			require.Equal(t, service.StatusUnused, unused.Status)
			var n int
			require.NoError(t, f.db.QueryRowContext(f.ctx, `SELECT count(*) FROM user_subscription_supplements`).Scan(&n))
			require.Zero(t, n)
		})
	}
}

func TestSubscriptionSupplementGenerationWithoutCampaign(t *testing.T) {
	f := newWelfareRedemptionFixture(t)
	prepareSupplementFixture(t, f)
	admin := service.NewAdminService(nil, nil, repository.NewGroupRepository(f.client, f.db).(service.AdminGroupRepository), nil, nil, nil, f.codes, nil, nil, nil, nil, nil, nil, f.client, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)
	amount := 10.0
	input := &service.GenerateRedeemCodesInput{Count: 2, Type: service.RedeemTypeSubscription, GroupID: &f.group.ID, SubscriptionEffect: service.SubscriptionEffectAddBalance, QuotaAmountUSD: &amount}
	codes, err := admin.GenerateRedeemCodes(f.ctx, input)
	require.NoError(t, err)
	require.Len(t, codes, 2)
	for _, code := range codes {
		require.Nil(t, code.WelfareCampaignID)
		require.Equal(t, f.group.ID, *code.GroupID)
	}
	_, err = f.client.Group.UpdateOneID(f.group.ID).SetStatus("disabled").Save(f.ctx)
	require.NoError(t, err)
	_, err = admin.GenerateRedeemCodes(f.ctx, input)
	require.ErrorIs(t, err, service.ErrSubscriptionSupplementGroupInvalid)
	count, err := f.client.RedeemCode.Query().Count(f.ctx)
	require.NoError(t, err)
	require.Equal(t, 2, count)
}

func TestSubscriptionSupplementKeepsUsedLegacyReceiptReadable(t *testing.T) {
	f := newWelfareRedemptionFixture(t)
	campaign := prepareSupplementFixture(t, f)
	initial := f.reward(t, "HISTORICAL-WELFARE")
	_, err := f.redeemer(f.codes).Redeem(f.ctx, f.userID, initial.Code)
	require.NoError(t, err)
	sub, err := f.client.UserSubscription.Query().Only(f.ctx)
	require.NoError(t, err)
	legacy := supplementCode(t, f, campaign, "add_quota", 20, 0)
	_, err = f.db.ExecContext(f.ctx, `UPDATE redeem_codes SET status='used',used_by=$2,used_at=now() WHERE id=$1`, legacy.ID, f.userID)
	require.NoError(t, err)
	_, err = f.db.ExecContext(f.ctx, `INSERT INTO user_welfare_subscription_supplements(redeem_code_id,subscription_id,user_id,group_id,welfare_campaign_id,welfare_campaign_version,subscription_effect,quota_amount_usd,validity_days,before_monthly_limit_usd,after_monthly_limit_usd,monthly_usage_usd,before_expires_at,after_expires_at,before_version,after_version)
 SELECT $1,id,user_id,group_id,$2,1,'add_quota',20,0,100,120,37,expires_at,expires_at,0,1 FROM user_subscriptions WHERE id=$3`, legacy.ID, campaign, sub.ID)
	require.NoError(t, err)
	_, err = f.db.ExecContext(f.ctx, `UPDATE user_subscriptions SET welfare_monthly_limit_usd=120,monthly_usage_usd=37 WHERE id=$1`, sub.ID)
	require.NoError(t, err)
	newCode := genericSupplementCode(t, f, service.SubscriptionEffectAddBalance, 10, 0)
	_, err = f.redeemer(f.codes).Redeem(f.ctx, f.userID, newCode.Code)
	require.NoError(t, err)
	read, err := f.codes.GetByID(f.ctx, legacy.ID)
	require.NoError(t, err)
	require.Equal(t, "add_quota", read.SubscriptionEffect)
	require.NotNil(t, read.WelfareResult)
	require.Equal(t, 120.0, read.WelfareResult.MonthlyLimitUSD)
	require.Equal(t, 83.0, read.WelfareResult.RemainingUSD)
	require.Nil(t, read.SubscriptionSupplementResult)
	_, err = f.redeemer(f.codes).Redeem(f.ctx, f.userID, legacy.Code)
	require.ErrorIs(t, err, service.ErrRedeemCodeUsed)
	got, err := f.client.UserSubscription.Get(f.ctx, sub.ID)
	require.NoError(t, err)
	require.Equal(t, 120.0, *got.WelfareMonthlyLimitUsd)
	require.Equal(t, 37.0, got.MonthlyUsageUsd)
	granted, used := supplementBalance(t, f, sub.ID)
	require.Equal(t, 10.0, granted)
	require.Zero(t, used)
}

func TestSubscriptionCodeManagementPreservesHistoricalReceipt(t *testing.T) {
	f := newWelfareRedemptionFixture(t)
	campaign := prepareSupplementFixture(t, f)
	now := time.Now().UTC()
	sub, err := f.client.UserSubscription.Create().SetUserID(f.userID).SetGroupID(f.group.ID).
		SetEntitlementKind("welfare").SetWelfareGrantCount(1).SetWelfareMonthlyLimitUsd(120).
		SetStartsAt(now.Add(-time.Hour)).SetExpiresAt(now.Add(24 * time.Hour)).Save(f.ctx)
	require.NoError(t, err)

	version, amount := int64(1), 20.0
	historical := &service.RedeemCode{Code: "IMPORTED-RECEIPT", Type: service.RedeemTypeSubscription,
		Status: service.StatusExpired, GroupID: &f.group.ID, SubscriptionEffect: "add_quota",
		WelfareCampaignID: &campaign, WelfareCampaignVersion: &version, QuotaAmountUSD: &amount}
	require.NoError(t, f.codes.Create(f.ctx, historical))
	_, err = f.db.ExecContext(f.ctx, `INSERT INTO user_welfare_subscription_supplements(redeem_code_id,subscription_id,user_id,group_id,welfare_campaign_id,welfare_campaign_version,subscription_effect,quota_amount_usd,validity_days,before_monthly_limit_usd,after_monthly_limit_usd,monthly_usage_usd,before_expires_at,after_expires_at,before_version,after_version)
 SELECT $1,id,user_id,group_id,$2,1,'add_quota',20,0,100,120,37,expires_at,expires_at,0,1 FROM user_subscriptions WHERE id=$3`, historical.ID, campaign, sub.ID)
	require.NoError(t, err)

	available := genericSupplementCode(t, f, service.SubscriptionEffectAddBalance, 10, 0)
	disabled := service.StatusDisabled
	fields := service.RedeemCodeBatchUpdateFields{Status: &disabled}
	count, err := f.codes.BatchUpdate(f.ctx, []int64{available.ID}, fields)
	require.NoError(t, err)
	require.Equal(t, int64(1), count)

	_, err = f.codes.BatchUpdate(f.ctx, []int64{historical.ID}, fields)
	require.ErrorIs(t, err, service.ErrRedeemCodeUsed)
	require.ErrorIs(t, f.codes.Delete(f.ctx, historical.ID), service.ErrRedeemCodeDeleteUsed)
	saved, err := f.codes.GetByID(f.ctx, historical.ID)
	require.NoError(t, err)
	require.Equal(t, service.StatusExpired, saved.Status)
}
