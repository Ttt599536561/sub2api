//go:build integration

package service_test

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/ent/usersubscription"
	"github.com/Wei-Shaw/sub2api/internal/qqinvite"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

type welfareInviteBot struct{}

func (welfareInviteBot) CheckAccount(context.Context) error { return nil }
func (welfareInviteBot) GroupMembers(context.Context, string) ([]string, error) {
	return []string{"123456"}, nil
}
func (welfareInviteBot) SendGroup(context.Context, string, string, string) (string, error) {
	return "group-message", nil
}
func (welfareInviteBot) SendPrivate(context.Context, string, string) (string, error) {
	return "private-message", nil
}

type welfareInviteFixture struct {
	*welfareRedemptionFixture
	store    *qqinvite.Store
	runtime  *qqinvite.Runtime
	campaign qqinvite.Campaign
}

func newWelfareInviteFixture(t *testing.T) *welfareInviteFixture {
	t.Helper()
	f := newWelfareRedemptionFixture(t)
	// Replace the original redemption fixture's minimal provenance tables with
	// real invitation migrations, including the production redemption guard.
	_, err := f.db.ExecContext(f.ctx, `DROP TABLE qq_invite_rewards; DROP TABLE qq_invite_code_pool;
 ALTER TABLE redeem_codes ALTER COLUMN created_at SET DEFAULT now();`)
	require.NoError(t, err)
	for _, name := range []string{"253_qq_invite_rewards.sql", "258_qq_invite_reward_revocations.sql"} {
		migration, err := os.ReadFile("../../migrations/" + name)
		require.NoError(t, err)
		_, err = f.db.ExecContext(f.ctx, string(migration))
		require.NoError(t, err, name)
	}
	store := qqinvite.NewStore(f.db, []byte(strings.Repeat("k", 32)))
	_, err = store.SaveConfig(f.ctx, qqinvite.Config{Enabled: true, PublicBaseURL: "https://sub.example.test",
		OneBotURL: "http://napcat:3000", WebUIURL: "http://napcat:6099", BotQQ: "123456",
		OneBotToken: "test-only", WebUIToken: "test-only", EventSecret: "test-only"}, f.userID)
	require.NoError(t, err)
	c, err := store.SaveCampaign(f.ctx, qqinvite.Campaign{Name: "revoke integration", GroupIDs: []string{"100"},
		SubscriptionGroupID: f.group.ID, StartsAt: time.Now().Add(-time.Hour), EndsAt: time.Now().Add(time.Hour),
		WelcomeTemplate: "Welcome {invitee}", PendingTemplate: "Pending {invitee}",
		CongratulationTemplate: "Congratulations {inviter}", PrivateTemplate: "Code {code} {url}"}, f.userID)
	require.NoError(t, err)
	require.NoError(t, store.SaveBaseline(f.ctx, c.ID, map[string][]string{"100": {"123456"}}, f.userID))
	c.Enabled = true
	c, err = store.SaveCampaign(f.ctx, c, f.userID)
	require.NoError(t, err)
	runtime := qqinvite.NewRuntime(store, func(qqinvite.Config) (qqinvite.Bot, error) { return welfareInviteBot{}, nil })
	return &welfareInviteFixture{welfareRedemptionFixture: f, store: store, runtime: runtime, campaign: c}
}

func (f *welfareInviteFixture) award(t *testing.T, invitee string) *service.RedeemCode {
	t.Helper()
	_, err := f.store.GenerateCodes(f.ctx, f.campaign.ID, 1, f.userID)
	require.NoError(t, err)
	require.NoError(t, f.store.AcceptEvent(f.ctx, qqinvite.Event{Kind: "group_increase", SubType: "invite", SelfID: "123456",
		GroupID: "100", UserID: invitee, OperatorID: "44444", Time: time.Now().Unix(), EventID: fmt.Sprintf("campaign-%d-join-%s", f.campaign.ID, invitee)}))
	require.NoError(t, f.runtime.ProcessOnce(f.ctx))
	var codeID int64
	require.NoError(t, f.db.QueryRowContext(f.ctx, `SELECT p.redeem_code_id FROM qq_invite_code_pool p
 JOIN qq_invite_records r ON r.id=p.record_id WHERE r.campaign_id=$1 AND r.invitee_qq=$2`, f.campaign.ID, invitee).Scan(&codeID))
	code, err := f.codes.GetByID(f.ctx, codeID)
	require.NoError(t, err)
	return code
}

func (f *welfareInviteFixture) leave(invitee string) error {
	return f.store.AcceptEvent(f.ctx, qqinvite.Event{Kind: "group_decrease", SubType: "leave", SelfID: "123456",
		GroupID: "100", UserID: invitee, Time: time.Now().Unix()})
}

func (f *welfareInviteFixture) currentWelfare(t *testing.T) *dbent.UserSubscription {
	t.Helper()
	sub, err := f.client.UserSubscription.Query().Where(usersubscription.UserIDEQ(f.userID),
		usersubscription.EntitlementKindEQ(service.SubscriptionEntitlementWelfare), usersubscription.WelfareCycleClosedEQ(false)).Only(f.ctx)
	require.NoError(t, err)
	return sub
}

// Holding either before or after the real conditional UPDATE exercises both
// winners without replacing the service transaction or revocation with SQL.
type inviteRedemptionBarrier struct {
	service.RedeemCodeRepository
	afterUse bool
	arrived  chan struct{}
	release  chan struct{}
}

func (r *inviteRedemptionBarrier) Use(ctx context.Context, id, userID int64) error {
	if r.afterUse {
		if err := r.RedeemCodeRepository.Use(ctx, id, userID); err != nil {
			return err
		}
	}
	close(r.arrived)
	select {
	case <-r.release:
	case <-ctx.Done():
		return ctx.Err()
	}
	if r.afterUse {
		return nil
	}
	return r.RedeemCodeRepository.Use(ctx, id, userID)
}

func TestWelfareInviteLeaveWinsConcurrentRedemption(t *testing.T) {
	f := newWelfareInviteFixture(t)
	code := f.award(t, "33333")
	barrier := &inviteRedemptionBarrier{RedeemCodeRepository: f.codes, arrived: make(chan struct{}), release: make(chan struct{})}
	result := make(chan error, 1)
	go func() { _, err := f.redeemer(barrier).Redeem(f.ctx, f.userID, code.Code); result <- err }()
	select {
	case <-barrier.arrived:
	case <-f.ctx.Done():
		t.Fatal(f.ctx.Err())
	}
	departureErr := f.leave("33333")
	close(barrier.release)
	redemptionErr := <-result
	require.NoError(t, departureErr)
	require.ErrorIs(t, redemptionErr, service.ErrRedeemCodeCancelled)
	got, err := f.codes.GetByID(f.ctx, code.ID)
	require.NoError(t, err)
	require.Equal(t, "cancelled", got.Status)
	require.Nil(t, got.UsedBy)
	var grants, revocations int
	require.NoError(t, f.db.QueryRowContext(f.ctx, `SELECT count(*) FROM user_welfare_subscription_grants`).Scan(&grants))
	require.NoError(t, f.db.QueryRowContext(f.ctx, `SELECT count(*) FROM qq_invite_reward_revocations WHERE redeem_code_id=$1`, code.ID).Scan(&revocations))
	require.Zero(t, grants)
	require.Equal(t, 1, revocations)
}

func TestWelfareInviteRedeemWinsConcurrentLeavePreservesOtherRewardsAndPaidTerm(t *testing.T) {
	f := newWelfareInviteFixture(t)
	first := f.award(t, "33333")
	_, err := f.redeemer(f.codes).Redeem(f.ctx, f.userID, first.Code)
	require.NoError(t, err)
	welfare := f.currentWelfare(t)
	_, err = f.client.UserSubscription.UpdateOneID(welfare.ID).SetMonthlyUsageUsd(35).SetDailyUsageUsd(3).SetWeeklyUsageUsd(7).Save(f.ctx)
	require.NoError(t, err)
	now := time.Now().UTC().Truncate(time.Microsecond)
	paid, err := f.client.UserSubscription.Create().SetUserID(f.userID).SetGroupID(f.group.ID).
		SetStartsAt(now).SetExpiresAt(now.AddDate(0, 0, 60)).SetMonthlyUsageUsd(11).Save(f.ctx)
	require.NoError(t, err)
	second := f.award(t, "55555")
	barrier := &inviteRedemptionBarrier{RedeemCodeRepository: f.codes, afterUse: true, arrived: make(chan struct{}), release: make(chan struct{})}
	redemption := make(chan error, 1)
	go func() { _, err := f.redeemer(barrier).Redeem(f.ctx, f.userID, second.Code); redemption <- err }()
	select {
	case <-barrier.arrived:
	case <-f.ctx.Done():
		t.Fatal(f.ctx.Err())
	}
	departure := make(chan error, 1)
	go func() { departure <- f.leave("55555") }()
	close(barrier.release)
	require.NoError(t, <-redemption)
	require.NoError(t, <-departure)
	require.NoError(t, f.leave("55555"), "duplicate departure must not deduct twice")
	got := f.currentWelfare(t)
	require.Equal(t, welfare.ID, got.ID)
	require.Equal(t, 100.0, *got.WelfareMonthlyLimitUsd)
	require.Equal(t, 35.0, got.MonthlyUsageUsd)
	require.Equal(t, 3.0, got.DailyUsageUsd)
	require.Equal(t, 7.0, got.WeeklyUsageUsd)
	require.Equal(t, welfare.ExpiresAt, got.ExpiresAt)
	unchangedPaid, err := f.client.UserSubscription.Get(f.ctx, paid.ID)
	require.NoError(t, err)
	require.True(t, paid.ExpiresAt.Equal(unchangedPaid.ExpiresAt))
	require.Equal(t, paid.MonthlyUsageUsd, unchangedPaid.MonthlyUsageUsd)
	used, err := f.codes.GetByID(f.ctx, second.ID)
	require.NoError(t, err)
	require.Equal(t, service.StatusUsed, used.Status)
	require.Equal(t, f.userID, *used.UsedBy)
	var receipts, revocations int
	require.NoError(t, f.db.QueryRowContext(f.ctx, `SELECT count(*) FROM user_welfare_subscription_grants`).Scan(&receipts))
	require.NoError(t, f.db.QueryRowContext(f.ctx, `SELECT count(*) FROM qq_invite_reward_revocations WHERE redeem_code_id=$1 AND subscription_id=$2`, second.ID, got.ID).Scan(&revocations))
	require.Equal(t, 2, receipts, "original grant receipts remain auditable")
	require.Equal(t, 1, revocations)
}

func TestWelfareInviteOldCycleRevocationCannotDeductNewCycle(t *testing.T) {
	f := newWelfareInviteFixture(t)
	oldCode := f.award(t, "33333")
	_, err := f.redeemer(f.codes).Redeem(f.ctx, f.userID, oldCode.Code)
	require.NoError(t, err)
	old := f.currentWelfare(t)
	_, err = f.client.UserSubscription.UpdateOneID(old.ID).SetExpiresAt(time.Now().Add(-time.Second)).SetMonthlyUsageUsd(60).Save(f.ctx)
	require.NoError(t, err)
	newCode := f.award(t, "55555")
	_, err = f.redeemer(f.codes).Redeem(f.ctx, f.userID, newCode.Code)
	require.NoError(t, err)
	current := f.currentWelfare(t)
	require.NotEqual(t, old.ID, current.ID)
	require.NoError(t, f.leave("33333"))
	got := f.currentWelfare(t)
	require.Equal(t, current.ID, got.ID)
	require.Equal(t, 100.0, *got.WelfareMonthlyLimitUsd)
	require.Equal(t, current.ExpiresAt, got.ExpiresAt)
	require.Equal(t, current.MonthlyUsageUsd, got.MonthlyUsageUsd)
	closed, err := f.client.UserSubscription.Get(f.ctx, old.ID)
	require.NoError(t, err)
	require.True(t, closed.WelfareCycleClosed)
	require.Equal(t, 60.0, closed.MonthlyUsageUsd)
	var subscriptionID int64
	require.NoError(t, f.db.QueryRowContext(f.ctx, `SELECT subscription_id FROM qq_invite_reward_revocations WHERE redeem_code_id=$1`, oldCode.ID).Scan(&subscriptionID))
	require.Equal(t, old.ID, subscriptionID)
}

func TestWelfareInviteMultiCampaignLeaveCannotDeadlockSecondRedemption(t *testing.T) {
	f := newWelfareInviteFixture(t)
	first := f.award(t, "33333")
	_, err := f.redeemer(f.codes).Redeem(f.ctx, f.userID, first.Code)
	require.NoError(t, err)
	// A member may legitimately earn a new reward in a later campaign. Both
	// records are matched by one departure and both codes belong to one user.
	f.campaign.Enabled = false
	_, err = f.store.SaveCampaign(f.ctx, f.campaign, f.userID)
	require.NoError(t, err)
	next := f.campaign
	next.ID, next.Version = 0, 0
	next.Name = "next invitation campaign"
	next, err = f.store.SaveCampaign(f.ctx, next, f.userID)
	require.NoError(t, err)
	require.NoError(t, f.store.SaveBaseline(f.ctx, next.ID, map[string][]string{"100": {"123456"}}, f.userID))
	next.Enabled = true
	f.campaign, err = f.store.SaveCampaign(f.ctx, next, f.userID)
	require.NoError(t, err)
	second := f.award(t, "33333")
	require.NotEqual(t, first.ID, second.ID)
	barrier := &inviteRedemptionBarrier{RedeemCodeRepository: f.codes, afterUse: true,
		arrived: make(chan struct{}), release: make(chan struct{})}
	redemption := make(chan error, 1)
	go func() { _, err := f.redeemer(barrier).Redeem(f.ctx, f.userID, second.Code); redemption <- err }()
	select {
	case <-barrier.arrived:
	case <-f.ctx.Done():
		t.Fatal(f.ctx.Err())
	}
	departure := make(chan error, 1)
	go func() { departure <- f.leave("33333") }()
	// Wait until the departure reaches the second code's row lock. This is a
	// real PG wait, not a sleep or a mock of the redemption/revocation logic.
	var reachedWait bool
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		err = f.db.QueryRowContext(f.ctx, `SELECT EXISTS(SELECT 1 FROM pg_stat_activity
 WHERE application_name=current_setting('application_name') AND pid<>pg_backend_pid() AND wait_event_type='Lock')`).Scan(&reachedWait)
		if err != nil || reachedWait {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	close(barrier.release)
	redemptionErr, departureErr := <-redemption, <-departure
	require.NoError(t, err)
	require.True(t, reachedWait, "departure must reach the held second code before redemption continues")
	require.NoError(t, redemptionErr)
	require.NoError(t, departureErr)
	got := f.currentWelfare(t)
	require.Zero(t, *got.WelfareMonthlyLimitUsd)
	var revoked int
	require.NoError(t, f.db.QueryRowContext(f.ctx, `SELECT count(*) FROM qq_invite_reward_revocations WHERE status='applied'`).Scan(&revoked))
	require.Equal(t, 2, revoked)
}
