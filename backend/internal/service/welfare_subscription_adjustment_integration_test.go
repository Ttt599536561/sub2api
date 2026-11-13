//go:build integration

package service_test

import (
	"strings"
	"testing"
	"time"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/ent/usersubscription"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/Wei-Shaw/sub2api/migrations"
	"github.com/stretchr/testify/require"
)

func newWelfareAdjustmentFixture(t *testing.T) *welfareRedemptionFixture {
	t.Helper()
	f := newWelfareRedemptionFixture(t)
	// Ent creates the subscription fields but not migration 254's raw CHECK.
	// Install the actual old constraint before applying the real 257 migration.
	previous, err := migrations.FS.ReadFile("254_welfare_subscription_entitlements.sql")
	require.NoError(t, err)
	alter := strings.SplitN(string(previous), "DROP INDEX", 2)[0]
	start := strings.Index(alter, "ADD CONSTRAINT user_subscriptions_welfare_limits_check")
	require.GreaterOrEqual(t, start, 0)
	_, err = f.db.ExecContext(f.ctx, "ALTER TABLE user_subscriptions "+alter[start:])
	require.NoError(t, err)
	migration, err := migrations.FS.ReadFile("257_welfare_subscription_adjustments.sql")
	require.NoError(t, err)
	_, err = f.db.ExecContext(f.ctx, string(migration))
	require.NoError(t, err)
	return f
}

func seedWelfareAdjustmentSubscription(t *testing.T, f *welfareRedemptionFixture, code string) *dbent.UserSubscription {
	t.Helper()
	reward := f.reward(t, code)
	_, err := f.redeemer(f.codes).Redeem(f.ctx, f.userID, reward.Code)
	require.NoError(t, err)
	sub, err := f.client.UserSubscription.Query().Where(usersubscription.UserIDEQ(f.userID),
		usersubscription.EntitlementKindEQ(service.SubscriptionEntitlementWelfare),
		usersubscription.WelfareCycleClosedEQ(false)).Only(f.ctx)
	require.NoError(t, err)
	return sub
}

func welfareAdjustmentRowSnapshot(t *testing.T, f *welfareRedemptionFixture, id int64) string {
	t.Helper()
	var snapshot string
	require.NoError(t, f.db.QueryRowContext(f.ctx, `SELECT row_to_json(s)::text FROM user_subscriptions s WHERE id=$1`, id).Scan(&snapshot))
	return snapshot
}

func welfareAdjustmentAuditCount(t *testing.T, f *welfareRedemptionFixture) int {
	t.Helper()
	var count int
	require.NoError(t, f.db.QueryRowContext(f.ctx, `SELECT count(*) FROM user_welfare_subscription_adjustments`).Scan(&count))
	return count
}

func TestWelfareAdjustmentZeroQuotaPreservesConsumptionPaidTermAndGrantReceipts(t *testing.T) {
	f := newWelfareAdjustmentFixture(t)
	now := time.Now().UTC().Truncate(time.Microsecond)
	paid, err := f.client.UserSubscription.Create().SetUserID(f.userID).SetGroupID(f.group.ID).
		SetStartsAt(now).SetExpiresAt(now.Add(60 * 24 * time.Hour)).
		SetDailyUsageUsd(3).SetWeeklyUsageUsd(4).SetMonthlyUsageUsd(5).Save(f.ctx)
	require.NoError(t, err)
	paidSnapshot := welfareAdjustmentRowSnapshot(t, f, paid.ID)
	welfare := seedWelfareAdjustmentSubscription(t, f, "WELFARE-ADJUST-ZERO")
	welfare, err = f.client.UserSubscription.UpdateOneID(welfare.ID).
		SetDailyUsageUsd(7).SetWeeklyUsageUsd(17).SetMonthlyUsageUsd(27).Save(f.ctx)
	require.NoError(t, err)
	zero := 0.0
	updated, err := f.subs.AdjustWelfareSubscription(f.ctx, welfare.ID, &service.AdjustWelfareSubscriptionInput{
		MonthlyLimitUSD: &zero, ExpectedVersion: welfare.DailyResetVersion, ExpectedGrantCount: welfare.WelfareGrantCount,
		Reason: "  管理员复核邀请异常  ", ActorID: 0, ActorKeyID: "legacy-admin-key",
	})
	require.NoError(t, err)
	require.NotNil(t, updated.WelfareMonthlyLimitUSD)
	require.Zero(t, *updated.WelfareMonthlyLimitUSD)
	require.Equal(t, welfare.StartsAt, updated.StartsAt)
	require.Equal(t, welfare.ExpiresAt, updated.ExpiresAt)
	require.Equal(t, welfare.DailyWindowStart, updated.DailyWindowStart)
	require.Equal(t, welfare.WeeklyWindowStart, updated.WeeklyWindowStart)
	require.Equal(t, welfare.MonthlyWindowStart, updated.MonthlyWindowStart)
	require.Equal(t, 7.0, updated.DailyUsageUSD)
	require.Equal(t, 17.0, updated.WeeklyUsageUSD)
	require.Equal(t, 27.0, updated.MonthlyUsageUSD)
	require.Equal(t, welfare.WelfareGrantCount, updated.WelfareGrantCount)
	require.Equal(t, welfare.DailyResetVersion+1, updated.DailyResetVersion)
	require.False(t, updated.WelfareCycleClosed)
	require.Equal(t, paidSnapshot, welfareAdjustmentRowSnapshot(t, f, paid.ID))
	var receipts int
	require.NoError(t, f.db.QueryRowContext(f.ctx, `SELECT count(*) FROM user_welfare_subscription_grants WHERE subscription_id=$1`, welfare.ID).Scan(&receipts))
	require.Equal(t, 1, receipts)
	var actorID, grantCount, beforeVersion, afterVersion int64
	var keyID, reason, beforeStatus, afterStatus string
	var beforeQuota, afterQuota float64
	var beforeExpiry, afterExpiry time.Time
	require.NoError(t, f.db.QueryRowContext(f.ctx, `SELECT actor_id,actor_key_id,reason,
		before_monthly_limit_usd,after_monthly_limit_usd,before_expires_at,after_expires_at,
		before_status,after_status,before_version,after_version,welfare_grant_count
		FROM user_welfare_subscription_adjustments WHERE subscription_id=$1`, welfare.ID).
		Scan(&actorID, &keyID, &reason, &beforeQuota, &afterQuota, &beforeExpiry, &afterExpiry,
			&beforeStatus, &afterStatus, &beforeVersion, &afterVersion, &grantCount))
	require.Zero(t, actorID)
	require.Equal(t, "legacy-admin-key", keyID)
	require.Equal(t, "管理员复核邀请异常", reason)
	require.Equal(t, 100.0, beforeQuota)
	require.Zero(t, afterQuota)
	require.Equal(t, welfare.ExpiresAt, beforeExpiry)
	require.Equal(t, beforeExpiry, afterExpiry)
	require.Equal(t, welfare.Status, beforeStatus)
	require.Equal(t, beforeStatus, afterStatus)
	require.Equal(t, welfare.DailyResetVersion, beforeVersion)
	require.Equal(t, updated.DailyResetVersion, afterVersion)
	require.Equal(t, welfare.WelfareGrantCount, grantCount)

	// A subsequent real pass contributes finite quota on top of zero and keeps
	// the consumption/history from before the administrative correction.
	newGrant := f.reward(t, "WELFARE-ADJUST-ZERO-NEW-GRANT")
	_, err = f.redeemer(f.codes).Redeem(f.ctx, f.userID, newGrant.Code)
	require.NoError(t, err)
	regranted, err := f.client.UserSubscription.Get(f.ctx, welfare.ID)
	require.NoError(t, err)
	require.Equal(t, 100.0, *regranted.WelfareMonthlyLimitUsd)
	require.Equal(t, 27.0, regranted.MonthlyUsageUsd)
	require.EqualValues(t, 2, regranted.WelfareGrantCount)
	require.Equal(t, welfare.ExpiresAt, regranted.ExpiresAt)
}

func TestWelfareAdjustmentRejectsFormMadeStaleByNewGrant(t *testing.T) {
	f := newWelfareAdjustmentFixture(t)
	observed := seedWelfareAdjustmentSubscription(t, f, "WELFARE-ADJUST-STALE-A")
	newGrant := f.reward(t, "WELFARE-ADJUST-STALE-B")
	_, err := f.redeemer(f.codes).Redeem(f.ctx, f.userID, newGrant.Code)
	require.NoError(t, err)
	before := welfareAdjustmentRowSnapshot(t, f, observed.ID)
	quota := 10.0
	_, err = f.subs.AdjustWelfareSubscription(f.ctx, observed.ID, &service.AdjustWelfareSubscriptionInput{
		MonthlyLimitUSD: &quota, ExpectedVersion: observed.DailyResetVersion, ExpectedGrantCount: observed.WelfareGrantCount,
		Reason: "表单打开后有新兑换", ActorID: f.userID,
	})
	require.ErrorIs(t, err, service.ErrWelfareAdjustmentConflict)
	require.Equal(t, before, welfareAdjustmentRowSnapshot(t, f, observed.ID))
	require.Zero(t, welfareAdjustmentAuditCount(t, f))
}

func TestWelfareAdjustmentRejectsPaidAndClosedWelfarePeriods(t *testing.T) {
	f := newWelfareAdjustmentFixture(t)
	now := time.Now().UTC().Truncate(time.Microsecond)
	paid, err := f.client.UserSubscription.Create().SetUserID(f.userID).SetGroupID(f.group.ID).
		SetStartsAt(now).SetExpiresAt(now.Add(60 * 24 * time.Hour)).Save(f.ctx)
	require.NoError(t, err)
	closed := seedWelfareAdjustmentSubscription(t, f, "WELFARE-ADJUST-CLOSED")
	closed, err = f.client.UserSubscription.UpdateOneID(closed.ID).SetWelfareCycleClosed(true).
		SetStatus(service.SubscriptionStatusExpired).SetExpiresAt(now.Add(-time.Hour)).Save(f.ctx)
	require.NoError(t, err)
	current := seedWelfareAdjustmentSubscription(t, f, "WELFARE-ADJUST-CURRENT")
	currentSnapshot := welfareAdjustmentRowSnapshot(t, f, current.ID)
	for _, rejected := range []*dbent.UserSubscription{paid, closed} {
		before := welfareAdjustmentRowSnapshot(t, f, rejected.ID)
		days := 30
		_, err := f.subs.AdjustWelfareSubscription(f.ctx, rejected.ID, &service.AdjustWelfareSubscriptionInput{
			RemainingDays: &days, ExpectedVersion: rejected.DailyResetVersion, ExpectedGrantCount: 1,
			Reason: "禁止跨订阅种类或周期恢复", ActorID: f.userID,
		})
		require.ErrorIs(t, err, service.ErrWelfareSubscriptionFixed)
		require.Equal(t, before, welfareAdjustmentRowSnapshot(t, f, rejected.ID))
	}
	require.Equal(t, currentSnapshot, welfareAdjustmentRowSnapshot(t, f, current.ID))
	require.Zero(t, welfareAdjustmentAuditCount(t, f))
}

func TestWelfareAdjustmentAuditFailureRollsBackQuotaExpiryAndVersion(t *testing.T) {
	f := newWelfareAdjustmentFixture(t)
	welfare := seedWelfareAdjustmentSubscription(t, f, "WELFARE-ADJUST-AUDIT-FAIL")
	_, err := f.client.UserSubscription.UpdateOneID(welfare.ID).SetMonthlyUsageUsd(17).Save(f.ctx)
	require.NoError(t, err)
	before := welfareAdjustmentRowSnapshot(t, f, welfare.ID)
	_, err = f.db.ExecContext(f.ctx, `CREATE FUNCTION reject_welfare_adjustment_audit() RETURNS trigger LANGUAGE plpgsql AS $$
		BEGIN RAISE EXCEPTION 'adjustment audit unavailable'; END $$;
		CREATE TRIGGER reject_welfare_adjustment_audit BEFORE INSERT ON user_welfare_subscription_adjustments
		FOR EACH ROW EXECUTE FUNCTION reject_welfare_adjustment_audit();`)
	require.NoError(t, err)
	quota, days := 5.0, 0
	_, err = f.subs.AdjustWelfareSubscription(f.ctx, welfare.ID, &service.AdjustWelfareSubscriptionInput{
		MonthlyLimitUSD: &quota, RemainingDays: &days,
		ExpectedVersion: welfare.DailyResetVersion, ExpectedGrantCount: welfare.WelfareGrantCount,
		Reason: "验证审计失败保持原权益", ActorID: f.userID,
	})
	require.ErrorContains(t, err, "adjustment audit unavailable")
	require.Equal(t, before, welfareAdjustmentRowSnapshot(t, f, welfare.ID))
	require.Zero(t, welfareAdjustmentAuditCount(t, f))
}

func TestWelfareAdjustmentRemainingDaysZeroExpiresWithoutClosingPeriod(t *testing.T) {
	f := newWelfareAdjustmentFixture(t)
	welfare := seedWelfareAdjustmentSubscription(t, f, "WELFARE-ADJUST-DAYS")
	welfare, err := f.client.UserSubscription.UpdateOneID(welfare.ID).SetMonthlyUsageUsd(9).Save(f.ctx)
	require.NoError(t, err)
	for _, days := range []int{0, 45} {
		before := time.Now()
		updated, err := f.subs.AdjustWelfareSubscription(f.ctx, welfare.ID, &service.AdjustWelfareSubscriptionInput{
			RemainingDays: &days, ExpectedVersion: welfare.DailyResetVersion, ExpectedGrantCount: welfare.WelfareGrantCount,
			Reason: "从当前时刻设置剩余天数", ActorID: f.userID,
		})
		after := time.Now()
		require.NoError(t, err)
		duration := time.Duration(days) * 24 * time.Hour
		require.False(t, updated.ExpiresAt.Before(before.Add(duration-time.Millisecond)))
		require.False(t, updated.ExpiresAt.After(after.Add(duration+time.Millisecond)))
		require.False(t, updated.WelfareCycleClosed)
		require.Equal(t, 9.0, updated.MonthlyUsageUSD)
		require.Equal(t, 100.0, *updated.WelfareMonthlyLimitUSD)
		if days == 0 {
			require.Equal(t, service.SubscriptionStatusExpired, updated.Status)
		} else {
			require.Equal(t, service.SubscriptionStatusActive, updated.Status)
		}
		welfare, err = f.client.UserSubscription.Get(f.ctx, welfare.ID)
		require.NoError(t, err)
	}
	require.Equal(t, 2, welfareAdjustmentAuditCount(t, f))
}
