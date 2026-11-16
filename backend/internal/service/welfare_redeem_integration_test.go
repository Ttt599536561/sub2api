//go:build integration

package service_test

import (
	"fmt"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/repository"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func prepareSupplementFixture(t *testing.T, f *welfareRedemptionFixture) int64 {
	t.Helper()
	_, err := f.db.ExecContext(f.ctx, `CREATE TABLE qq_invite_campaigns(id BIGSERIAL PRIMARY KEY,version BIGINT NOT NULL,payload JSONB NOT NULL)`)
	require.NoError(t, err)
	migration, err := os.ReadFile("../../migrations/259_welfare_redeem_supplements.sql")
	require.NoError(t, err)
	_, err = f.db.ExecContext(f.ctx, string(migration))
	require.NoError(t, err)
	var id int64
	require.NoError(t, f.db.QueryRowContext(f.ctx, `INSERT INTO qq_invite_campaigns(version,payload) VALUES(1,jsonb_build_object('name','paused campaign','enabled',false,'subscription_group_id',$1::bigint)) RETURNING id`, f.group.ID).Scan(&id))
	return id
}

func supplementCode(t *testing.T, f *welfareRedemptionFixture, campaignID int64, effect string, amount float64, days int) *service.RedeemCode {
	t.Helper()
	version := int64(1)
	c := &service.RedeemCode{Code: fmt.Sprintf("SUP-%d", time.Now().UnixNano()), Type: service.RedeemTypeSubscription,
		Status: service.StatusUnused, GroupID: &f.group.ID, ValidityDays: days, SubscriptionEffect: effect,
		WelfareCampaignID: &campaignID, WelfareCampaignVersion: &version, WelfareCampaignName: "paused campaign"}
	if effect == "add_quota" {
		c.QuotaAmountUSD = &amount
	}
	require.NoError(t, f.codes.Create(f.ctx, c))
	return c
}

func TestWelfareSupplementQuotaAndDurationPreserveCycle(t *testing.T) {
	f := newWelfareRedemptionFixture(t)
	campaignID := prepareSupplementFixture(t, f)
	reward := f.reward(t, "INITIAL-WELFARE")
	_, err := f.redeemer(f.codes).Redeem(f.ctx, f.userID, reward.Code)
	require.NoError(t, err)
	welfare, err := f.client.UserSubscription.Query().Only(f.ctx)
	require.NoError(t, err)
	start := time.Now().UTC().AddDate(0, 0, -35).Truncate(time.Microsecond)
	welfare, err = f.client.UserSubscription.UpdateOneID(welfare.ID).SetDailyUsageUsd(3).SetWeeklyUsageUsd(4).
		SetMonthlyUsageUsd(35).SetMonthlyWindowStart(start).Save(f.ctx)
	require.NoError(t, err)
	paid, err := f.client.UserSubscription.Create().SetUserID(f.userID).SetGroupID(f.group.ID).
		SetStartsAt(start).SetExpiresAt(welfare.ExpiresAt).SetMonthlyUsageUsd(7).Save(f.ctx)
	require.NoError(t, err)
	quota := supplementCode(t, f, campaignID, "add_quota", 12.34, 0)
	result, err := f.redeemer(f.codes).Redeem(f.ctx, f.userID, quota.Code)
	require.NoError(t, err)
	require.NotNil(t, result.WelfareResult)
	require.Equal(t, 112.34, result.WelfareResult.MonthlyLimitUSD)
	require.Equal(t, 77.34, result.WelfareResult.RemainingUSD)
	duration := supplementCode(t, f, campaignID, "extend_duration", 0, 45)
	_, err = f.redeemer(f.codes).Redeem(f.ctx, f.userID, duration.Code)
	require.NoError(t, err)
	got, err := f.client.UserSubscription.Get(f.ctx, welfare.ID)
	require.NoError(t, err)
	require.Equal(t, 112.34, *got.WelfareMonthlyLimitUsd)
	require.Equal(t, welfare.MonthlyUsageUsd, got.MonthlyUsageUsd)
	require.Equal(t, welfare.DailyUsageUsd, got.DailyUsageUsd)
	require.Equal(t, welfare.WeeklyUsageUsd, got.WeeklyUsageUsd)
	require.Equal(t, welfare.MonthlyWindowStart, got.MonthlyWindowStart)
	require.Equal(t, welfare.DailyWindowStart, got.DailyWindowStart)
	require.Equal(t, welfare.WeeklyWindowStart, got.WeeklyWindowStart)
	require.Equal(t, welfare.WelfareGrantCount, got.WelfareGrantCount)
	require.Equal(t, welfare.DailyResetVersion+2, got.DailyResetVersion)
	require.WithinDuration(t, welfare.ExpiresAt.AddDate(0, 0, 45), got.ExpiresAt, time.Microsecond)
	unchanged, err := f.client.UserSubscription.Get(f.ctx, paid.ID)
	require.NoError(t, err)
	require.Equal(t, paid.ExpiresAt, unchanged.ExpiresAt)
	require.Equal(t, paid.MonthlyUsageUsd, unchanged.MonthlyUsageUsd)
	history, err := f.codes.ListByUser(f.ctx, f.userID, 10)
	require.NoError(t, err)
	for _, c := range history {
		if c.ID == quota.ID {
			require.WithinDuration(t, welfare.ExpiresAt, c.WelfareResult.ExpiresAt, time.Microsecond)
		}
	}
	var grants, receipts int
	require.NoError(t, f.db.QueryRowContext(f.ctx, `SELECT count(*) FROM user_welfare_subscription_grants`).Scan(&grants))
	require.NoError(t, f.db.QueryRowContext(f.ctx, `SELECT count(*) FROM user_welfare_subscription_supplements`).Scan(&receipts))
	require.Equal(t, 1, grants)
	require.Equal(t, 2, receipts)
	_, err = f.redeemer(f.codes).Redeem(f.ctx, f.userID, quota.Code)
	require.ErrorIs(t, err, service.ErrRedeemCodeUsed)
}

func TestWelfareSupplementRejectsIneligibleCyclesWithoutConsumingCode(t *testing.T) {
	for _, state := range []string{"missing", "expired", "closed", "paused", "deleted"} {
		t.Run(state, func(t *testing.T) {
			f := newWelfareRedemptionFixture(t)
			campaignID := prepareSupplementFixture(t, f)
			if state != "missing" {
				now := time.Now()
				b := f.client.UserSubscription.Create().SetUserID(f.userID).SetGroupID(f.group.ID).
					SetEntitlementKind(service.SubscriptionEntitlementWelfare).SetWelfareGrantCount(1).SetWelfareMonthlyLimitUsd(100).
					SetStartsAt(now.AddDate(0, 0, -1)).SetExpiresAt(now.AddDate(0, 0, 29))
				if state == "expired" {
					b.SetExpiresAt(now.Add(-time.Hour))
				}
				if state == "closed" {
					b.SetWelfareCycleClosed(true)
				}
				if state == "paused" {
					b.SetStatus("paused")
				}
				if state == "deleted" {
					b.SetDeletedAt(now)
				}
				_, err := b.Save(f.ctx)
				require.NoError(t, err)
			}
			code := supplementCode(t, f, campaignID, "add_quota", 10, 0)
			_, err := f.redeemer(f.codes).Redeem(f.ctx, f.userID, code.Code)
			require.ErrorIs(t, err, service.ErrWelfareSupplementNoActiveCycle)
			unused, err := f.codes.GetByID(f.ctx, code.ID)
			require.NoError(t, err)
			require.Equal(t, service.StatusUnused, unused.Status)
		})
	}
}

func TestWelfareSupplementConcurrentCodesAndReceiptRollback(t *testing.T) {
	f := newWelfareRedemptionFixture(t)
	campaignID := prepareSupplementFixture(t, f)
	reward := f.reward(t, "CONCURRENT-WELFARE")
	_, err := f.redeemer(f.codes).Redeem(f.ctx, f.userID, reward.Code)
	require.NoError(t, err)
	codes := []*service.RedeemCode{supplementCode(t, f, campaignID, "add_quota", 0.1, 0), supplementCode(t, f, campaignID, "add_quota", 0.2, 0)}
	var wg sync.WaitGroup
	errs := make(chan error, 2)
	for _, c := range codes {
		wg.Go(func() { _, err := f.redeemer(f.codes).Redeem(f.ctx, f.userID, c.Code); errs <- err })
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		require.NoError(t, err)
	}
	welfare, err := f.client.UserSubscription.Query().Only(f.ctx)
	require.NoError(t, err)
	require.Equal(t, 100.3, *welfare.WelfareMonthlyLimitUsd)
	code := supplementCode(t, f, campaignID, "add_quota", 10, 0)
	_, err = f.db.ExecContext(f.ctx, `CREATE FUNCTION reject_supplement() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'receipt unavailable'; END $$; CREATE TRIGGER reject_supplement BEFORE INSERT ON user_welfare_subscription_supplements FOR EACH ROW EXECUTE FUNCTION reject_supplement()`)
	require.NoError(t, err)
	_, err = f.redeemer(f.codes).Redeem(f.ctx, f.userID, code.Code)
	require.ErrorContains(t, err, "receipt unavailable")
	unused, err := f.codes.GetByID(f.ctx, code.ID)
	require.NoError(t, err)
	require.Equal(t, service.StatusUnused, unused.Status)
	got, err := f.client.UserSubscription.Get(f.ctx, welfare.ID)
	require.NoError(t, err)
	require.Equal(t, *welfare.WelfareMonthlyLimitUsd, *got.WelfareMonthlyLimitUsd)
	require.Equal(t, welfare.DailyResetVersion, got.DailyResetVersion)
	var protected float64
	require.NoError(t, f.db.QueryRowContext(f.ctx, `SELECT protected_quota_usd FROM user_welfare_subscription_supplement_quota WHERE subscription_id=$1`, welfare.ID).Scan(&protected))
	require.Equal(t, 0.3, protected, "failed supplement receipt must roll back its projection increase")
}

func TestWelfareSupplementGenerationDerivesAndSnapshotsCampaign(t *testing.T) {
	f := newWelfareRedemptionFixture(t)
	campaignID := prepareSupplementFixture(t, f)
	admin := service.NewAdminService(nil, nil, repository.NewGroupRepository(f.client, f.db).(service.AdminGroupRepository), nil, nil, nil, f.codes, nil, nil, nil, nil, nil, nil, f.client, nil, nil, nil, nil, nil, nil, nil, nil, nil)
	amount := 25.5
	input := &service.GenerateRedeemCodesInput{Count: 2, Type: service.RedeemTypeSubscription, SubscriptionEffect: "add_quota", WelfareCampaignID: &campaignID, QuotaAmountUSD: &amount}
	codes, err := admin.GenerateRedeemCodes(f.ctx, input)
	require.NoError(t, err)
	require.Len(t, codes, 2)
	require.Equal(t, f.group.ID, *codes[0].GroupID)
	require.EqualValues(t, 1, *codes[0].WelfareCampaignVersion)
	require.Equal(t, 25.5, *codes[0].QuotaAmountUSD)
	options, err := f.redeemer(f.codes).ListWelfareRedeemCampaigns(f.ctx)
	require.NoError(t, err)
	require.Len(t, options, 1)
	require.Equal(t, campaignID, options[0].ID)
	badGroup := int64(999)
	input.GroupID = &badGroup
	_, err = admin.GenerateRedeemCodes(f.ctx, input)
	require.ErrorIs(t, err, service.ErrWelfareSupplementCampaignInvalid)
	reward := f.reward(t, "FIXED-SUPPLEMENT")
	_, err = f.redeemer(f.codes).Redeem(f.ctx, f.userID, reward.Code)
	require.NoError(t, err)
	_, err = f.db.ExecContext(f.ctx, `UPDATE qq_invite_campaigns SET version=version+1,payload=jsonb_set(payload,'{name}','"renamed"') WHERE id=$1`, campaignID)
	require.NoError(t, err)
	_, err = f.client.Group.UpdateOneID(f.group.ID).SetMonthlyLimitUsd(999).Save(f.ctx)
	require.NoError(t, err)
	redeemed, err := f.redeemer(f.codes).Redeem(f.ctx, f.userID, codes[0].Code)
	require.NoError(t, err)
	require.Equal(t, 125.5, redeemed.WelfareResult.MonthlyLimitUSD)
	require.Equal(t, "paused campaign", redeemed.WelfareCampaignName)
	input.GroupID = nil
	_, err = f.client.Group.UpdateOneID(f.group.ID).SetStatus("disabled").Save(f.ctx)
	require.NoError(t, err)
	_, err = admin.GenerateRedeemCodes(f.ctx, input)
	require.ErrorIs(t, err, service.ErrWelfareSupplementCampaignInvalid)
	_, err = f.redeemer(f.codes).Redeem(f.ctx, f.userID, codes[1].Code)
	require.ErrorIs(t, err, service.ErrWelfareSupplementCampaignInvalid)
	unused, err := f.codes.GetByID(f.ctx, codes[1].ID)
	require.NoError(t, err)
	require.Equal(t, service.StatusUnused, unused.Status)
	options, err = f.redeemer(f.codes).ListWelfareRedeemCampaigns(f.ctx)
	require.NoError(t, err)
	require.Empty(t, options)
}

func TestWelfareSupplementAdvancesAdjustmentVersion(t *testing.T) {
	f := newWelfareAdjustmentFixture(t)
	campaignID := prepareSupplementFixture(t, f)
	welfare := seedWelfareAdjustmentSubscription(t, f, "BEFORE-SUPPLEMENT")
	code := supplementCode(t, f, campaignID, "extend_duration", 0, 1)
	_, err := f.redeemer(f.codes).Redeem(f.ctx, f.userID, code.Code)
	require.NoError(t, err)
	quota := 88.0
	_, err = f.subs.AdjustWelfareSubscription(f.ctx, welfare.ID, &service.AdjustWelfareSubscriptionInput{MonthlyLimitUSD: &quota, ExpectedVersion: welfare.DailyResetVersion, ExpectedGrantCount: welfare.WelfareGrantCount, Reason: "stale administrator form"})
	require.ErrorIs(t, err, service.ErrWelfareAdjustmentConflict)
}

func TestWelfareSupplementSurvivesOriginalQQGrantRevocation(t *testing.T) {
	f := newWelfareInviteFixture(t)
	migration, err := os.ReadFile("../../migrations/259_welfare_redeem_supplements.sql")
	require.NoError(t, err)
	_, err = f.db.ExecContext(f.ctx, string(migration))
	require.NoError(t, err)
	reward := f.award(t, "99111")
	_, err = f.redeemer(f.codes).Redeem(f.ctx, f.userID, reward.Code)
	require.NoError(t, err)
	welfare := f.currentWelfare(t)
	_, err = f.client.UserSubscription.UpdateOneID(welfare.ID).SetMonthlyUsageUsd(30).Save(f.ctx)
	require.NoError(t, err)
	code := supplementCode(t, f.welfareRedemptionFixture, f.campaign.ID, "add_quota", 20, 0)
	_, err = f.redeemer(f.codes).Redeem(f.ctx, f.userID, code.Code)
	require.NoError(t, err)
	require.NoError(t, f.leave("99111"))
	got, err := f.client.UserSubscription.Get(f.ctx, welfare.ID)
	require.NoError(t, err)
	require.Equal(t, 20.0, *got.WelfareMonthlyLimitUsd)
	require.Equal(t, 30.0, got.MonthlyUsageUsd)
}

func TestWelfareSupplementConcurrentAdministratorAdjustment(t *testing.T) {
	f := newWelfareAdjustmentFixture(t)
	campaignID := prepareSupplementFixture(t, f)
	welfare := seedWelfareAdjustmentSubscription(t, f, "CONCURRENT-ADMIN-SUP")
	code := supplementCode(t, f, campaignID, "add_quota", 10, 0)
	barrier := &inviteRedemptionBarrier{RedeemCodeRepository: f.codes, afterUse: true, arrived: make(chan struct{}, 1), release: make(chan struct{})}
	done := make(chan error, 1)
	go func() { _, err := f.redeemer(barrier).Redeem(f.ctx, f.userID, code.Code); done <- err }()
	select {
	case <-barrier.arrived:
	case <-f.ctx.Done():
		t.Fatal("redemption barrier timed out")
	}
	quota := 50.0
	_, err := f.subs.AdjustWelfareSubscription(f.ctx, welfare.ID, &service.AdjustWelfareSubscriptionInput{MonthlyLimitUSD: &quota, ExpectedVersion: welfare.DailyResetVersion, ExpectedGrantCount: welfare.WelfareGrantCount, Reason: "concurrent adjustment"})
	close(barrier.release)
	require.NoError(t, err)
	require.NoError(t, <-done)
	got, err := f.client.UserSubscription.Get(f.ctx, welfare.ID)
	require.NoError(t, err)
	require.Equal(t, 60.0, *got.WelfareMonthlyLimitUsd)
	require.Equal(t, welfare.DailyResetVersion+2, got.DailyResetVersion)
	require.Equal(t, welfare.WelfareGrantCount, got.WelfareGrantCount)
}

func TestWelfareSupplementSameCodeConcurrentOnlyOneReceipt(t *testing.T) {
	f := newWelfareRedemptionFixture(t)
	campaignID := prepareSupplementFixture(t, f)
	reward := f.reward(t, "REPLAY-SUPPLEMENT")
	_, err := f.redeemer(f.codes).Redeem(f.ctx, f.userID, reward.Code)
	require.NoError(t, err)
	code := supplementCode(t, f, campaignID, "add_quota", 10, 0)
	results := make(chan error, 2)
	var wg sync.WaitGroup
	for range 2 {
		wg.Go(func() { _, err := f.redeemer(f.codes).Redeem(f.ctx, f.userID, code.Code); results <- err })
	}
	wg.Wait()
	close(results)
	successes := 0
	for err := range results {
		if err == nil {
			successes++
		} else {
			require.ErrorIs(t, err, service.ErrRedeemCodeUsed)
		}
	}
	require.Equal(t, 1, successes)
	welfare, err := f.client.UserSubscription.Query().Only(f.ctx)
	require.NoError(t, err)
	require.Equal(t, 110.0, *welfare.WelfareMonthlyLimitUsd)
	var receipts int
	require.NoError(t, f.db.QueryRowContext(f.ctx, `SELECT count(*) FROM user_welfare_subscription_supplements`).Scan(&receipts))
	require.Equal(t, 1, receipts)
}

func TestWelfareSupplementBoundsRollbackAndFixedGroup(t *testing.T) {
	f := newWelfareRedemptionFixture(t)
	campaignID := prepareSupplementFixture(t, f)
	reward := f.reward(t, "SUPPLEMENT-BOUNDS")
	_, err := f.redeemer(f.codes).Redeem(f.ctx, f.userID, reward.Code)
	require.NoError(t, err)
	welfare, err := f.client.UserSubscription.Query().Only(f.ctx)
	require.NoError(t, err)
	_, err = f.db.ExecContext(f.ctx, `UPDATE user_subscriptions SET welfare_monthly_limit_usd=9999999999.99,expires_at=$2 WHERE id=$1`, welfare.ID, service.MaxExpiresAt.AddDate(0, 0, -1))
	require.NoError(t, err)
	for _, c := range []*service.RedeemCode{supplementCode(t, f, campaignID, "add_quota", 0.01, 0), supplementCode(t, f, campaignID, "extend_duration", 0, 2)} {
		_, err = f.redeemer(f.codes).Redeem(f.ctx, f.userID, c.Code)
		require.ErrorIs(t, err, service.ErrWelfareSupplementLimitExceeded)
		unused, err := f.codes.GetByID(f.ctx, c.ID)
		require.NoError(t, err)
		require.Equal(t, service.StatusUnused, unused.Status)
	}
	c := supplementCode(t, f, campaignID, "add_quota", 1, 0)
	_, err = f.redeemer(f.codes).BatchUpdate(f.ctx, &service.RedeemCodeBatchUpdateInput{IDs: []int64{c.ID}, Fields: service.RedeemCodeBatchUpdateFields{GroupID: service.NullableInt64Update{Set: true, Value: &f.group.ID}}})
	require.ErrorIs(t, err, service.ErrWelfareSupplementInvalid)
	var receipts int
	require.NoError(t, f.db.QueryRowContext(f.ctx, `SELECT count(*) FROM user_welfare_subscription_supplements`).Scan(&receipts))
	require.Zero(t, receipts)
}

// QQ provenance remains immutable after an administrator changes the effective
// total. Revoking that old grant must not reclaim a later manual supplement,
// nor may the immutable supplement history resurrect an administrator's cuts.
func TestWelfareSupplementProtectionAfterAdministratorChanges(t *testing.T) {
	type operation struct {
		kind   string
		amount float64
	}
	for _, tc := range []struct {
		name string
		ops  []operation
		want float64
	}{
		{"cut_then_supplement", []operation{{"adjust", 0}, {"supplement", 20}, {"revoke_old", 0}}, 20},
		{"cut_removes_old_protection", []operation{{"supplement", 20}, {"adjust", 0}, {"new_qq", 0}, {"revoke_new", 0}}, 0},
		{"partial_cut_and_new_supplement", []operation{{"supplement", 20}, {"adjust", 10}, {"supplement", 5}, {"revoke_old", 0}}, 15},
		{"duration_does_not_protect_quota", []operation{{"adjust", 20}, {"duration", 5}, {"revoke_old", 0}}, 0},
		{"administrator_duration_preserves_protection", []operation{{"supplement", 20}, {"adjust_duration", 5}, {"revoke_old", 0}}, 20},
		{"multiple_and_duplicate_revocations", []operation{{"supplement", 20}, {"new_qq", 0}, {"adjust", 10}, {"supplement", 5}, {"revoke_new", 0}, {"revoke_old", 0}, {"revoke_new", 0}, {"revoke_old", 0}}, 15},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newWelfareInviteFixture(t)
			installSupplementAdjustmentMigrations(t, f)
			original := f.award(t, "99221")
			_, err := f.redeemer(f.codes).Redeem(f.ctx, f.userID, original.Code)
			require.NoError(t, err)
			sub := f.currentWelfare(t)
			_, err = f.client.UserSubscription.UpdateOneID(sub.ID).SetMonthlyUsageUsd(37).SetDailyUsageUsd(3).SetWeeklyUsageUsd(9).Save(f.ctx)
			require.NoError(t, err)
			for _, op := range tc.ops {
				switch op.kind {
				case "adjust":
					current := f.currentWelfare(t)
					_, err = f.subs.AdjustWelfareSubscription(f.ctx, current.ID, &service.AdjustWelfareSubscriptionInput{MonthlyLimitUSD: &op.amount, ExpectedVersion: current.DailyResetVersion, ExpectedGrantCount: current.WelfareGrantCount, Reason: "protect later supplements"})
					require.NoError(t, err)
				case "adjust_duration":
					current := f.currentWelfare(t)
					days := int(op.amount)
					_, err = f.subs.AdjustWelfareSubscription(f.ctx, current.ID, &service.AdjustWelfareSubscriptionInput{RemainingDays: &days, ExpectedVersion: current.DailyResetVersion, ExpectedGrantCount: current.WelfareGrantCount, Reason: "change duration only"})
					require.NoError(t, err)
				case "supplement", "duration":
					effect, days := "add_quota", 0
					if op.kind == "duration" {
						effect = "extend_duration"
						days = int(op.amount)
					}
					code := supplementCode(t, f.welfareRedemptionFixture, f.campaign.ID, effect, op.amount, days)
					_, err = f.redeemer(f.codes).Redeem(f.ctx, f.userID, code.Code)
					require.NoError(t, err)
				case "new_qq":
					code := f.award(t, "99222")
					_, err = f.redeemer(f.codes).Redeem(f.ctx, f.userID, code.Code)
					require.NoError(t, err)
				case "revoke_old":
					require.NoError(t, f.leave("99221"))
				case "revoke_new":
					require.NoError(t, f.leave("99222"))
				}
			}
			got := f.currentWelfare(t)
			require.Equal(t, tc.want, *got.WelfareMonthlyLimitUsd)
			require.Equal(t, 37.0, got.MonthlyUsageUsd)
			require.Equal(t, 3.0, got.DailyUsageUsd)
			require.Equal(t, 9.0, got.WeeklyUsageUsd)
			require.Equal(t, sub.ID, got.ID)
			var protected float64
			require.NoError(t, f.db.QueryRowContext(f.ctx, `SELECT COALESCE((SELECT protected_quota_usd FROM user_welfare_subscription_supplement_quota WHERE subscription_id=$1),0)`, sub.ID).Scan(&protected))
			require.Equal(t, tc.want, protected)
			if tc.name == "duration_does_not_protect_quota" {
				var count int
				require.NoError(t, f.db.QueryRowContext(f.ctx, `SELECT count(*) FROM user_welfare_subscription_supplement_quota`).Scan(&count))
				require.Zero(t, count, "duration-only supplements never create attribution")
			}
		})
	}
}

func installSupplementAdjustmentMigrations(t *testing.T, f *welfareInviteFixture) {
	t.Helper()
	// The invitation fixture starts before adjustment and supplement migrations.
	_, err := f.db.ExecContext(f.ctx, `ALTER TABLE user_subscriptions ADD CONSTRAINT user_subscriptions_welfare_limits_check CHECK(true)`)
	require.NoError(t, err)
	for _, name := range []string{"257_welfare_subscription_adjustments.sql", "259_welfare_redeem_supplements.sql"} {
		migration, err := os.ReadFile("../../migrations/" + name)
		require.NoError(t, err)
		_, err = f.db.ExecContext(f.ctx, string(migration))
		require.NoError(t, err)
	}
}

func TestWelfareSupplementProjectionConcurrentDeparture(t *testing.T) {
	for _, winner := range []string{"departure", "supplement"} {
		t.Run(winner, func(t *testing.T) {
			f := newWelfareInviteFixture(t)
			installSupplementAdjustmentMigrations(t, f)
			original := f.award(t, "99331")
			_, err := f.redeemer(f.codes).Redeem(f.ctx, f.userID, original.Code)
			require.NoError(t, err)
			sub := f.currentWelfare(t)
			zero := 0.0
			_, err = f.subs.AdjustWelfareSubscription(f.ctx, sub.ID, &service.AdjustWelfareSubscriptionInput{MonthlyLimitUSD: &zero, ExpectedVersion: sub.DailyResetVersion, ExpectedGrantCount: sub.WelfareGrantCount, Reason: "clear initial reward"})
			require.NoError(t, err)
			code := supplementCode(t, f.welfareRedemptionFixture, f.campaign.ID, "add_quota", 20, 0)
			if winner == "departure" {
				barrier := &inviteRedemptionBarrier{RedeemCodeRepository: f.codes, afterUse: true, arrived: make(chan struct{}), release: make(chan struct{})}
				done := make(chan error, 1)
				go func() { _, err := f.redeemer(barrier).Redeem(f.ctx, f.userID, code.Code); done <- err }()
				select {
				case <-barrier.arrived:
				case <-f.ctx.Done():
					t.Fatal(f.ctx.Err())
				}
				leaveErr := f.leave("99331")
				close(barrier.release)
				require.NoError(t, leaveErr)
				require.NoError(t, <-done)
			} else {
				// Hold only the original reward code. Departure reaches this real row lock
				// while the separate manual code can finish and acquire its protected quota.
				blocker, err := f.db.BeginTx(f.ctx, nil)
				require.NoError(t, err)
				defer func() { _ = blocker.Rollback() }()
				_, err = blocker.ExecContext(f.ctx, `SELECT id FROM redeem_codes WHERE id=$1 FOR UPDATE`, original.ID)
				require.NoError(t, err)
				done := make(chan error, 1)
				go func() { done <- f.leave("99331") }()
				reached := false
				deadline := time.Now().Add(5 * time.Second)
				for time.Now().Before(deadline) {
					err = f.db.QueryRowContext(f.ctx, `SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE application_name=current_setting('application_name') AND pid<>pg_backend_pid() AND wait_event_type='Lock')`).Scan(&reached)
					if err != nil || reached {
						break
					}
					time.Sleep(10 * time.Millisecond)
				}
				require.NoError(t, err)
				require.True(t, reached, "departure reached the held original code")
				_, redeemErr := f.redeemer(f.codes).Redeem(f.ctx, f.userID, code.Code)
				require.NoError(t, blocker.Commit())
				require.NoError(t, redeemErr)
				require.NoError(t, <-done)
			}
			require.NoError(t, f.leave("99331"), "repeated departure cannot debit protection")
			got := f.currentWelfare(t)
			require.Equal(t, 20.0, *got.WelfareMonthlyLimitUsd)
			require.Equal(t, sub.WelfareGrantCount, got.WelfareGrantCount)
			var protected float64
			require.NoError(t, f.db.QueryRowContext(f.ctx, `SELECT protected_quota_usd FROM user_welfare_subscription_supplement_quota WHERE subscription_id=$1`, sub.ID).Scan(&protected))
			require.Equal(t, 20.0, protected)
		})
	}
}

func TestWelfareSupplementProjectionAdjustmentReceiptFailureRollsBack(t *testing.T) {
	f := newWelfareAdjustmentFixture(t)
	campaignID := prepareSupplementFixture(t, f)
	sub := seedWelfareAdjustmentSubscription(t, f, "PROJECTION-ROLLBACK")
	code := supplementCode(t, f, campaignID, "add_quota", 20, 0)
	_, err := f.redeemer(f.codes).Redeem(f.ctx, f.userID, code.Code)
	require.NoError(t, err)
	before, err := f.client.UserSubscription.Get(f.ctx, sub.ID)
	require.NoError(t, err)
	_, err = f.db.ExecContext(f.ctx, `CREATE FUNCTION reject_projection_adjustment() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'adjustment receipt unavailable'; END $$; CREATE TRIGGER reject_projection_adjustment BEFORE INSERT ON user_welfare_subscription_adjustments FOR EACH ROW EXECUTE FUNCTION reject_projection_adjustment()`)
	require.NoError(t, err)
	zero := 0.0
	_, err = f.subs.AdjustWelfareSubscription(f.ctx, sub.ID, &service.AdjustWelfareSubscriptionInput{MonthlyLimitUSD: &zero, ExpectedVersion: before.DailyResetVersion, ExpectedGrantCount: before.WelfareGrantCount, Reason: "must rollback protection"})
	require.ErrorContains(t, err, "adjustment receipt unavailable")
	after, err := f.client.UserSubscription.Get(f.ctx, sub.ID)
	require.NoError(t, err)
	require.Equal(t, *before.WelfareMonthlyLimitUsd, *after.WelfareMonthlyLimitUsd)
	require.Equal(t, before.DailyResetVersion, after.DailyResetVersion)
	var protected float64
	require.NoError(t, f.db.QueryRowContext(f.ctx, `SELECT protected_quota_usd FROM user_welfare_subscription_supplement_quota WHERE subscription_id=$1`, sub.ID).Scan(&protected))
	require.Equal(t, 20.0, protected)
}

func TestWelfareSupplementNewProjectionReceiptFailureRollsBack(t *testing.T) {
	f := newWelfareRedemptionFixture(t)
	campaignID := prepareSupplementFixture(t, f)
	original := f.reward(t, "NEW-PROJECTION-ROLLBACK")
	_, err := f.redeemer(f.codes).Redeem(f.ctx, f.userID, original.Code)
	require.NoError(t, err)
	before, err := f.client.UserSubscription.Query().Only(f.ctx)
	require.NoError(t, err)
	code := supplementCode(t, f, campaignID, "add_quota", 20, 0)
	_, err = f.db.ExecContext(f.ctx, `CREATE FUNCTION reject_new_projection_receipt() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'new projection receipt unavailable'; END $$; CREATE TRIGGER reject_new_projection_receipt BEFORE INSERT ON user_welfare_subscription_supplements FOR EACH ROW EXECUTE FUNCTION reject_new_projection_receipt()`)
	require.NoError(t, err)
	_, err = f.redeemer(f.codes).Redeem(f.ctx, f.userID, code.Code)
	require.ErrorContains(t, err, "new projection receipt unavailable")
	var projections, receipts int
	require.NoError(t, f.db.QueryRowContext(f.ctx, `SELECT count(*) FROM user_welfare_subscription_supplement_quota`).Scan(&projections))
	require.Zero(t, projections)
	require.NoError(t, f.db.QueryRowContext(f.ctx, `SELECT count(*) FROM user_welfare_subscription_supplements`).Scan(&receipts))
	require.Zero(t, receipts)
	unused, err := f.codes.GetByID(f.ctx, code.ID)
	require.NoError(t, err)
	require.Equal(t, service.StatusUnused, unused.Status)
	after, err := f.client.UserSubscription.Get(f.ctx, before.ID)
	require.NoError(t, err)
	require.Equal(t, *before.WelfareMonthlyLimitUsd, *after.WelfareMonthlyLimitUsd)
	require.Equal(t, before.DailyResetVersion, after.DailyResetVersion)
}
