package qqinvite

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestDepartureCancelsUnusedRewardAndCannotResurrect(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	c := campaignFixture(t, s)
	configure(t, s)
	_, err := s.GenerateCodes(ctx, c.ID, 1, 1)
	must(t, err)
	joined := Event{SelfID: "123456", GroupID: "100", UserID: "33333", OperatorID: "44444", SubType: "invite", Kind: "group_increase", Time: time.Now().Unix()}
	must(t, s.AcceptEvent(ctx, joined))
	bot := &fakeBot{}
	rt := NewRuntime(s, func(Config) (Bot, error) { return bot, nil })
	must(t, rt.ProcessOnce(ctx))
	left := joined
	left.Kind = "group_decrease"
	left.SubType = "leave"
	must(t, s.AcceptEvent(ctx, left))
	must(t, s.AcceptEvent(ctx, left))
	var status, codeStatus string
	var recordID, rewardID int64
	must(t, s.db.QueryRow(`SELECT r.id,r.status,w.id,c.status FROM qq_invite_records r JOIN qq_invite_rewards w ON w.record_id=r.id JOIN qq_invite_code_pool p ON p.id=w.pool_id JOIN redeem_codes c ON c.id=p.redeem_code_id`).Scan(&recordID, &status, &rewardID, &codeStatus))
	if status != "revoked" || codeStatus != "cancelled" {
		t.Fatalf("status=%s code=%s", status, codeStatus)
	}
	for _, err := range []error{s.RecheckRecord(ctx, recordID, 1), s.ReviewRecord(ctx, recordID, "44444", "review again", 1), s.RedeliverReward(ctx, rewardID, 1)} {
		if !errors.Is(err, ErrConflict) {
			t.Fatalf("resurrection: %v", err)
		}
	}
	must(t, s.AcceptEvent(ctx, Event{SelfID: "123456", UserID: "44444", Kind: "private_claim", Time: time.Now().Unix()}))
	if _, err := s.db.Exec(`UPDATE redeem_codes SET status='unused'`); err == nil {
		t.Fatal("cancelled code resurrected")
	}
	for i := 0; i < 6; i++ {
		must(t, rt.DeliverOnce(ctx))
	}
	if len(bot.groups) != 1 || !strings.Contains(bot.groups[0], "已退出群聊") || len(bot.private) != 1 || !strings.Contains(bot.private[0], "作废") {
		t.Fatalf("wrong notifications groups=%v private=%v", bot.groups, bot.private)
	}
}

func departureFixture(t *testing.T) (*Store, *Runtime, Event, int64) {
	t.Helper()
	s := testStore(t)
	ctx := context.Background()
	c := campaignFixture(t, s)
	configure(t, s)
	_, err := s.GenerateCodes(ctx, c.ID, 1, 1)
	must(t, err)
	join := Event{SelfID: "123456", GroupID: "100", UserID: "33333", OperatorID: "44444", Kind: "group_increase", SubType: "invite", Time: time.Now().Unix()}
	must(t, s.AcceptEvent(ctx, join))
	rt := NewRuntime(s, func(Config) (Bot, error) { return &fakeBot{}, nil })
	must(t, rt.ProcessOnce(ctx))
	var codeID int64
	must(t, s.db.QueryRow(`SELECT redeem_code_id FROM qq_invite_code_pool WHERE record_id IS NOT NULL`).Scan(&codeID))
	join.Kind = "group_decrease"
	join.SubType = "leave"
	return s, rt, join, codeID
}

func TestDepartureUsedReceiptKeepsUsageTermAndRetriesInvalidation(t *testing.T) {
	s, rt, left, codeID := departureFixture(t)
	ctx := context.Background()
	_, err := s.db.Exec(`INSERT INTO users(id) VALUES(7);INSERT INTO user_subscriptions(user_id,group_id,entitlement_kind,welfare_daily_limit_usd,welfare_weekly_limit_usd,welfare_monthly_limit_usd,welfare_grant_count,daily_usage_usd,weekly_usage_usd,monthly_usage_usd) VALUES(7,1,'welfare',20,80,150,2,19,79,140)`)
	must(t, err)
	_, err = s.db.Exec(`UPDATE redeem_codes SET status='used',used_by=7,used_at=now() WHERE id=$1`, codeID)
	must(t, err)
	_, err = s.db.Exec(`INSERT INTO user_welfare_subscription_grants VALUES($1,1,10,40,100,now())`, codeID)
	must(t, err)
	var before string
	must(t, s.db.QueryRow(`SELECT (to_jsonb(s)-'welfare_daily_limit_usd'-'welfare_weekly_limit_usd'-'welfare_monthly_limit_usd'-'daily_reset_version'-'updated_at')::text FROM user_subscriptions s WHERE id=1`).Scan(&before))
	calls := 0
	s.SetSubscriptionInvalidator(func(userID, groupID int64) error {
		calls++
		if userID != 7 || groupID != 1 {
			t.Errorf("wrong cache identity %d/%d", userID, groupID)
		}
		if calls == 1 {
			return errors.New("cache unavailable")
		}
		return nil
	})
	must(t, s.AcceptEvent(ctx, left))
	left.EventID = "duplicate-reencoded"
	must(t, s.AcceptEvent(ctx, left))
	var after string
	must(t, s.db.QueryRow(`SELECT (to_jsonb(s)-'welfare_daily_limit_usd'-'welfare_weekly_limit_usd'-'welfare_monthly_limit_usd'-'daily_reset_version'-'updated_at')::text FROM user_subscriptions s WHERE id=1`).Scan(&after))
	if before != after {
		t.Fatalf("term/usage changed: %s -> %s", before, after)
	}
	var correct bool
	must(t, s.db.QueryRow(`SELECT welfare_daily_limit_usd=10 AND welfare_weekly_limit_usd=40 AND welfare_monthly_limit_usd=50 AND daily_reset_version=1 FROM user_subscriptions WHERE id=1`).Scan(&correct))
	if !correct {
		t.Fatal("wrong quota subtraction")
	}
	must(t, rt.DeliverOnce(ctx))
	var n int
	must(t, s.db.QueryRow(`SELECT count(*) FROM qq_invite_subscription_invalidations`).Scan(&n))
	if n != 1 || calls != 1 {
		t.Fatalf("lost invalidation %d calls=%d", n, calls)
	}
	_, err = s.db.Exec(`UPDATE qq_invite_subscription_invalidations SET next_attempt_at=now()`)
	must(t, err)
	must(t, rt.DeliverOnce(ctx))
	must(t, s.db.QueryRow(`SELECT count(*) FROM qq_invite_subscription_invalidations`).Scan(&n))
	if n != 0 || calls != 2 {
		t.Fatalf("retry failed %d calls=%d", n, calls)
	}
	var codeStatus string
	must(t, s.db.QueryRow(`SELECT status FROM redeem_codes WHERE id=$1`, codeID).Scan(&codeStatus))
	if codeStatus != "used" {
		t.Fatal(codeStatus)
	}
}

func TestDepartureWrongIdentityAndEarlierTimeDoNotRevoke(t *testing.T) {
	s, _, left, _ := departureFixture(t)
	ctx := context.Background()
	wrong := left
	wrong.GroupID = "200"
	must(t, s.AcceptEvent(ctx, wrong))
	wrong = left
	wrong.UserID = "77777"
	must(t, s.AcceptEvent(ctx, wrong))
	wrong = left
	wrong.Time--
	must(t, s.AcceptEvent(ctx, wrong))
	wrong = left
	wrong.SelfID = "99999"
	if !errors.Is(s.AcceptEvent(ctx, wrong), ErrInvalid) {
		t.Fatal("wrong bot accepted")
	}
	var status string
	must(t, s.db.QueryRow(`SELECT status FROM qq_invite_records`).Scan(&status))
	if status != "awarded" {
		t.Fatal(status)
	}
	must(t, s.AcceptEvent(ctx, left))
}

func TestDepartureProcessesWhileDisabledAndWithoutCurrentCampaign(t *testing.T) {
	s, _, left, _ := departureFixture(t)
	ctx := context.Background()
	cfg, err := s.RuntimeConfig(ctx)
	must(t, err)
	cfg.Enabled = false
	_, err = s.SaveConfig(ctx, cfg, 1)
	must(t, err)
	var campaignID int64
	must(t, s.db.QueryRow(`SELECT id FROM qq_invite_campaigns`).Scan(&campaignID))
	c, err := s.GetCampaign(ctx, campaignID)
	must(t, err)
	c.Enabled = false
	_, err = s.SaveCampaign(ctx, c, 1)
	must(t, err)
	bot := &fakeBot{}
	rt := NewRuntime(s, func(Config) (Bot, error) { return bot, nil })
	must(t, s.AcceptEvent(ctx, left))
	for i := 0; i < 4; i++ {
		must(t, rt.DeliverOnce(ctx))
	}
	if len(bot.private) != 1 || len(bot.groups) != 1 {
		t.Fatalf("disabled revocations missing: %#v", bot)
	}
}

func TestDepartureBeforeJoinPreventsLateAward(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	c := campaignFixture(t, s)
	configure(t, s)
	_, err := s.GenerateCodes(ctx, c.ID, 1, 1)
	must(t, err)
	ev := Event{SelfID: "123456", GroupID: "100", UserID: "33333", OperatorID: "44444", Kind: "group_decrease", SubType: "kick", Time: time.Now().Unix()}
	must(t, s.AcceptEvent(ctx, ev))
	ev.Kind = "group_increase"
	ev.SubType = "invite"
	must(t, s.AcceptEvent(ctx, ev))
	rt := NewRuntime(s, func(Config) (Bot, error) { return &fakeBot{}, nil })
	must(t, rt.ProcessOnce(ctx))
	var status string
	must(t, s.db.QueryRow(`SELECT status FROM qq_invite_records`).Scan(&status))
	if status != "revoked" {
		t.Fatal(status)
	}
	var n int
	must(t, s.db.QueryRow(`SELECT count(*) FROM qq_invite_rewards`).Scan(&n))
	if n != 0 {
		t.Fatal("late callback awarded")
	}
	ev.Time++
	must(t, s.AcceptEvent(ctx, ev))
	must(t, rt.ProcessOnce(ctx))
	must(t, s.db.QueryRow(`SELECT count(*) FROM qq_invite_rewards`).Scan(&n))
	if n != 0 {
		t.Fatal("rejoin resurrected campaign qualification")
	}
}

func TestDepartureMissingReceiptRequiresManualReview(t *testing.T) {
	s, _, left, codeID := departureFixture(t)
	ctx := context.Background()
	_, err := s.db.Exec(`INSERT INTO users VALUES(7);INSERT INTO user_subscriptions(user_id,group_id,entitlement_kind,welfare_monthly_limit_usd) VALUES(7,1,'standard',900)`)
	must(t, err)
	_, err = s.db.Exec(`UPDATE redeem_codes SET status='used',used_by=7,used_at=now() WHERE id=$1`, codeID)
	must(t, err)
	must(t, s.AcceptEvent(ctx, left))
	var status, reason string
	must(t, s.db.QueryRow(`SELECT status,reason FROM qq_invite_reward_revocations`).Scan(&status, &reason))
	if status != "manual_review" || reason != "departure_receipt_missing" {
		t.Fatalf("%s/%s", status, reason)
	}
	var untouched bool
	must(t, s.db.QueryRow(`SELECT welfare_monthly_limit_usd=900 AND daily_reset_version=0 FROM user_subscriptions`).Scan(&untouched))
	if !untouched {
		t.Fatal("unproven subscription changed")
	}
	bot := &fakeBot{}
	rt := NewRuntime(s, func(Config) (Bot, error) { return bot, nil })
	for i := 0; i < 4; i++ {
		must(t, rt.DeliverOnce(ctx))
	}
	if len(bot.private) != 1 || !strings.Contains(bot.private[0], "尚未自动扣回") {
		t.Fatalf("false notification %v", bot.private)
	}
}

func TestDepartureUnlimitedReceiptRestoresRemainingFiniteLimits(t *testing.T) {
	s, _, left, codeID := departureFixture(t)
	ctx := context.Background()
	_, err := s.db.Exec(`INSERT INTO users VALUES(7);INSERT INTO user_subscriptions(user_id,group_id,entitlement_kind,welfare_daily_limit_usd,welfare_weekly_limit_usd,welfare_monthly_limit_usd,welfare_grant_count) VALUES(7,1,'welfare',NULL,NULL,50,2)`)
	must(t, err)
	_, err = s.db.Exec(`UPDATE redeem_codes SET status='used',used_by=7,used_at=now() WHERE id=$1`, codeID)
	must(t, err)
	_, err = s.db.Exec(`INSERT INTO user_welfare_subscription_grants VALUES($1,1,NULL,NULL,100,now())`, codeID)
	must(t, err)
	_, err = s.db.Exec(`INSERT INTO redeem_codes(code,type,status,used_by,used_at,group_id) VALUES('OTHER','subscription','used',7,now(),1);INSERT INTO user_welfare_subscription_grants SELECT id,1,10,40,100,now() FROM redeem_codes WHERE code='OTHER'`)
	must(t, err)
	must(t, s.AcceptEvent(ctx, left))
	var correct bool
	must(t, s.db.QueryRow(`SELECT COALESCE(welfare_daily_limit_usd=10 AND welfare_weekly_limit_usd=40 AND welfare_monthly_limit_usd=0,false) FROM user_subscriptions`).Scan(&correct))
	if !correct {
		t.Fatal("unlimited grant revoked without restoring remaining finite grants")
	}
}

func TestDepartureOtherGroupLateJoinCannotRevokeOriginalReward(t *testing.T) {
	s, rt, left, _ := departureFixture(t)
	ctx := context.Background()
	left.GroupID = "200"
	must(t, s.AcceptEvent(ctx, left))
	left.Kind = "group_increase"
	left.SubType = "invite"
	must(t, s.AcceptEvent(ctx, left))
	must(t, rt.ProcessOnce(ctx))
	var status string
	must(t, s.db.QueryRow(`SELECT status FROM qq_invite_records`).Scan(&status))
	if status != "awarded" {
		t.Fatalf("other group delayed join revoked original reward: %s", status)
	}
}

type heldPrivateBot struct {
	fakeBot
	entered chan struct{}
	release chan struct{}
}

func (b *heldPrivateBot) SendPrivate(ctx context.Context, _ string, text string) (string, error) {
	close(b.entered)
	select {
	case <-b.release:
		return "in-flight-private", nil
	case <-ctx.Done():
		return "", ctx.Err()
	}
}

func TestDepartureConcurrentSendClaimRecoveryCompletesWithoutResurrection(t *testing.T) {
	s, _, left, _ := departureFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	_, err := s.db.Exec(`UPDATE qq_invite_outbox SET status='sent' WHERE kind='welcome'`)
	must(t, err)
	bot := &heldPrivateBot{entered: make(chan struct{}), release: make(chan struct{})}
	rt := NewRuntime(s, func(Config) (Bot, error) { return bot, nil })
	sendDone := make(chan error, 1)
	go func() { sendDone <- rt.DeliverOnce(ctx) }()
	select {
	case <-bot.entered:
	case <-ctx.Done():
		t.Fatal("private send did not start")
	}
	// The record lock belongs to the sending transaction. Deliberately expire
	// its lease and race an independent worker, a claim, and a leave callback.
	_, err = s.db.ExecContext(ctx, `UPDATE qq_invite_outbox SET lease_until=now()-interval '1 minute' WHERE kind='private'`)
	must(t, err)
	claimDone := make(chan error, 1)
	go func() {
		claimDone <- s.AcceptEvent(ctx, Event{SelfID: "123456", UserID: "44444", Kind: "private_claim", Time: time.Now().Unix()})
	}()
	deadline := time.Now().Add(3 * time.Second)
	for {
		var blocked int
		must(t, s.db.QueryRowContext(ctx, `SELECT count(*) FROM pg_stat_activity WHERE datname=current_database() AND pid<>pg_backend_pid() AND wait_event_type='Lock' AND query LIKE '%qq_invite%'`).Scan(&blocked))
		if blocked > 0 {
			break
		}
		if time.Now().After(deadline) {
			close(bot.release)
			t.Fatal("claim failed to reach lock barrier")
		}
		time.Sleep(10 * time.Millisecond)
	}
	recoverDone := make(chan error, 1)
	go func() { recoverDone <- rt.recoverExpiredSends(ctx) }()
	secondDone := make(chan error, 1)
	go func() {
		secondDone <- NewRuntime(NewStore(s.db, s.key), func(Config) (Bot, error) { return &fakeBot{}, nil }).DeliverOnce(ctx)
	}()
	leaveDone := make(chan error, 1)
	go func() { leaveDone <- s.AcceptEvent(ctx, left) }()
	close(bot.release)
	for _, done := range []chan error{sendDone, claimDone, recoverDone, secondDone, leaveDone} {
		select {
		case err := <-done:
			must(t, err)
		case <-ctx.Done():
			t.Fatal("send/claim/recovery/leave deadlocked")
		}
	}
	var status string
	must(t, s.db.QueryRow(`SELECT status FROM qq_invite_records`).Scan(&status))
	if status != "revoked" {
		t.Fatal(status)
	}
	var liveRewards int
	must(t, s.db.QueryRow(`SELECT count(*) FROM qq_invite_outbox WHERE kind NOT IN ('revoke_private','revoke_group') AND status NOT IN ('sent','cancelled')`).Scan(&liveRewards))
	if liveRewards != 0 {
		t.Fatal("in-flight completion recreated reward notifications")
	}
}

func TestDepartureClaimAfterDisabledOnlyReplaysCancellation(t *testing.T) {
	s, rt, left, _ := departureFixture(t)
	ctx := context.Background()
	must(t, s.AcceptEvent(ctx, left))
	for i := 0; i < 3; i++ {
		must(t, rt.DeliverOnce(ctx))
	}
	_, err := s.db.Exec(`UPDATE qq_invite_outbox SET status='unknown' WHERE kind='revoke_private'`)
	must(t, err)
	cfg, err := s.RuntimeConfig(ctx)
	must(t, err)
	cfg.Enabled = false
	_, err = s.SaveConfig(ctx, cfg, 1)
	must(t, err)
	must(t, s.AcceptEvent(ctx, Event{SelfID: "123456", UserID: "44444", Kind: "private_claim", Time: time.Now().Unix()}))
	var rewardStatus, cancelStatus string
	must(t, s.db.QueryRow(`SELECT (SELECT status FROM qq_invite_outbox WHERE kind='private'),(SELECT status FROM qq_invite_outbox WHERE kind='revoke_private')`).Scan(&rewardStatus, &cancelStatus))
	if rewardStatus != "cancelled" || cancelStatus != "pending" {
		t.Fatalf("claim reward=%s cancel=%s", rewardStatus, cancelStatus)
	}
	bot := &fakeBot{}
	rt = NewRuntime(s, func(Config) (Bot, error) { return bot, nil })
	must(t, rt.DeliverOnce(ctx))
	if len(bot.private) != 1 || !strings.Contains(bot.private[0], "作废") {
		t.Fatalf("wrong retry %v", bot.private)
	}
}
