package qqinvite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	_ "github.com/lib/pq"
	"os"
	"strings"
	"sync"
	"testing"
	"time"
)

func testStore(t *testing.T) *Store {
	t.Helper()
	dsn := os.Getenv("QQ_INVITE_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("QQ_INVITE_TEST_DATABASE_URL required")
	}
	if !strings.Contains(dsn, "127.0.0.1") {
		t.Fatal("loopback required")
	}
	db, e := sql.Open("postgres", dsn)
	if e != nil {
		t.Fatal(e)
	}
	schema := fmt.Sprintf("invite_%d", time.Now().UnixNano())
	if _, e = db.Exec("CREATE SCHEMA " + schema); e != nil {
		t.Fatal(e)
	}
	scoped, e := sql.Open("postgres", dsn+"&search_path="+schema)
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() {
		if e := scoped.Close(); e != nil {
			t.Error(e)
		}
		if _, e := db.Exec("DROP SCHEMA " + schema + " CASCADE"); e != nil {
			t.Error(e)
		}
		if e := db.Close(); e != nil {
			t.Error(e)
		}
	})
	_, e = scoped.Exec(`CREATE TABLE groups(id BIGSERIAL PRIMARY KEY,name TEXT,subscription_type TEXT DEFAULT 'subscription',status TEXT DEFAULT 'active',deleted_at TIMESTAMPTZ,daily_limit_usd NUMERIC,weekly_limit_usd NUMERIC,monthly_limit_usd NUMERIC DEFAULT 100); INSERT INTO groups(name) VALUES('monthly'); CREATE TABLE redeem_codes(id BIGSERIAL PRIMARY KEY,code VARCHAR(32) UNIQUE NOT NULL,type VARCHAR(20) NOT NULL DEFAULT 'balance',value NUMERIC(20,8) NOT NULL DEFAULT 0,status VARCHAR(20) NOT NULL DEFAULT 'unused',used_by BIGINT,used_at TIMESTAMPTZ,notes TEXT,created_at TIMESTAMPTZ NOT NULL DEFAULT now(),expires_at TIMESTAMPTZ,group_id BIGINT REFERENCES groups(id),validity_days INT NOT NULL DEFAULT 30)`)
	if e != nil {
		t.Fatal(e)
	}
	b, e := os.ReadFile("../../migrations/253_qq_invite_rewards.sql")
	if e != nil {
		t.Fatal(e)
	}
	if _, e = scoped.Exec(string(b)); e != nil {
		t.Fatal(e)
	}
	return NewStore(scoped, []byte(strings.Repeat("k", 32)))
}

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

func TestConcurrentCrossGroupAndLastCode(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	c := campaignFixture(t, s)
	configure(t, s)
	if _, e := s.GenerateCodes(ctx, c.ID, 1, 1); e != nil {
		t.Fatal(e)
	}
	var wg sync.WaitGroup
	errs := make(chan error, 40)
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			group := "100"
			if i%2 == 0 {
				group = "200"
			}
			user := "33333"
			if i%3 == 0 {
				user = "55555"
			}
			errs <- s.AcceptEvent(ctx, Event{SelfID: "123456", GroupID: group, UserID: user, OperatorID: "44444", SubType: "invite", Kind: "group_increase", Time: time.Now().Unix()})
		}(i)
	}
	wg.Wait()
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			r := NewRuntime(s, func(Config) (Bot, error) { return &fakeBot{}, nil })
			errs <- r.ProcessOnce(ctx)
		}()
	}
	wg.Wait()
	close(errs)
	for e := range errs {
		if e != nil {
			t.Fatal(e)
		}
	}
	var rewards, records int
	must(t, s.db.QueryRow(`SELECT count(*) FROM qq_invite_rewards`).Scan(&rewards))
	must(t, s.db.QueryRow(`SELECT count(*) FROM qq_invite_records`).Scan(&records))
	if rewards != 1 || records != 2 {
		t.Fatalf("rewards=%d records=%d", rewards, records)
	}
	if _, e := s.GenerateCodes(ctx, c.ID, 1, 1); e != nil {
		t.Fatal(e)
	}
	r := NewRuntime(s, func(Config) (Bot, error) { return &fakeBot{}, nil })
	if e := r.ProcessOnce(ctx); e != nil {
		t.Fatal(e)
	}
	must(t, s.db.QueryRow(`SELECT count(*) FROM qq_invite_rewards`).Scan(&rewards))
	if rewards != 2 {
		t.Fatalf("restocked rewards=%d", rewards)
	}
}

func TestUnknownDeliveryAndRestartKeepOriginalCode(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	c := campaignFixture(t, s)
	configure(t, s)
	if _, e := s.GenerateCodes(ctx, c.ID, 2, 1); e != nil {
		t.Fatal(e)
	}
	b := &fakeBot{privateErr: ErrDeliveryUnknown}
	r := NewRuntime(s, func(Config) (Bot, error) { return b, nil })
	if e := s.AcceptEvent(ctx, Event{SelfID: "123456", GroupID: "100", UserID: "33333", OperatorID: "44444", SubType: "invite", Kind: "group_increase", Time: time.Now().Unix()}); e != nil {
		t.Fatal(e)
	}
	if e := r.ProcessOnce(ctx); e != nil {
		t.Fatal(e)
	}
	for i := 0; i < 3; i++ {
		if e := r.DeliverOnce(ctx); e != nil {
			t.Fatal(e)
		}
	}
	if len(b.private) != 1 || len(b.groups) != 2 {
		t.Fatalf("private=%d groups=%d", len(b.private), len(b.groups))
	}
	var status string
	must(t, s.db.QueryRow(`SELECT status FROM qq_invite_outbox WHERE kind='private'`).Scan(&status))
	if status != "unknown" {
		t.Fatal(status)
	}
	rows, e := s.ListRecords(ctx, c.ID, "", 1, 20)
	if e != nil {
		t.Fatal(e)
	}
	if e = s.RedeliverReward(ctx, *rows.Items[0].RewardID, 1); e != nil {
		t.Fatal(e)
	}
	b.privateErr = nil
	r = NewRuntime(NewStore(s.db, s.key), func(Config) (Bot, error) { return b, nil })
	if e = r.DeliverOnce(ctx); e != nil {
		t.Fatal(e)
	}
	if e = r.DeliverOnce(ctx); e != nil {
		t.Fatal(e)
	}
	if len(b.private) != 2 || b.private[0] != b.private[1] || len(b.groups) != 3 {
		t.Fatalf("retry changed code or missing notification: %#v %#v", b.private, b.groups)
	}
	var n int
	must(t, s.db.QueryRow(`SELECT count(*) FROM qq_invite_rewards`).Scan(&n))
	if n != 1 {
		t.Fatal(n)
	}
	if _, e = s.db.Exec(`UPDATE qq_invite_outbox SET status='sending',lease_until=now()-interval '1 minute',lease_token='old' WHERE kind='private'`); e != nil {
		t.Fatal(e)
	}
	if e = r.DeliverOnce(ctx); e != nil {
		t.Fatal(e)
	}
	if len(b.private) != 2 {
		t.Fatal("expired sending lease automatically resent an uncertain delivery")
	}
	if e = s.RedeliverReward(ctx, *rows.Items[0].RewardID, 1); e != nil {
		t.Fatal(e)
	}
	if e = r.DeliverOnce(ctx); e != nil {
		t.Fatal(e)
	}
	if len(b.private) != 3 || b.private[1] != b.private[2] {
		t.Fatal("explicit lease recovery changed original code")
	}
}

func TestAllocationRollbackHasNoLostCode(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	c := campaignFixture(t, s)
	configure(t, s)
	if _, e := s.GenerateCodes(ctx, c.ID, 1, 1); e != nil {
		t.Fatal(e)
	}
	r := NewRuntime(s, func(Config) (Bot, error) { return &fakeBot{}, nil })
	must(t, s.AcceptEvent(ctx, Event{SelfID: "123456", GroupID: "100", UserID: "33333", OperatorID: "44444", SubType: "invite", Kind: "group_increase", Time: time.Now().Unix()}))
	_, e := s.db.Exec(`CREATE FUNCTION fail_outbox() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.kind='private' THEN RAISE EXCEPTION 'test fault'; END IF; RETURN NEW; END $$; CREATE TRIGGER test_fault BEFORE INSERT ON qq_invite_outbox FOR EACH ROW EXECUTE FUNCTION fail_outbox()`)
	if e != nil {
		t.Fatal(e)
	}
	if e = r.ProcessOnce(ctx); e == nil {
		t.Fatal("fault not propagated")
	}
	var rewards, assigned int
	must(t, s.db.QueryRow(`SELECT count(*) FROM qq_invite_rewards`).Scan(&rewards))
	must(t, s.db.QueryRow(`SELECT count(*) FROM qq_invite_code_pool WHERE record_id IS NOT NULL`).Scan(&assigned))
	if rewards != 0 || assigned != 0 {
		t.Fatalf("partial commit rewards=%d assigned=%d", rewards, assigned)
	}
	if _, e := s.db.Exec(`DROP TRIGGER test_fault ON qq_invite_outbox; UPDATE qq_invite_records SET lease_until=now()-interval '1 minute'`); e != nil {
		t.Fatal(e)
	}
	if e = r.ProcessOnce(ctx); e != nil {
		t.Fatal(e)
	}
	must(t, s.db.QueryRow(`SELECT count(*) FROM qq_invite_rewards`).Scan(&rewards))
	if rewards != 1 {
		t.Fatal("restart failed")
	}
}

func TestImportValidationAndAllocatedCodeFrozen(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	c := campaignFixture(t, s)
	_, e := s.db.Exec(`INSERT INTO redeem_codes(code,type,group_id,validity_days,status) VALUES('MONTHLY-CODE-00001','subscription',1,30,'unused'),('YEARLY-CODE-000002','subscription',1,365,'unused'),('USED-CODE-0000003','subscription',1,30,'used'),('BALANCE-CODE-0004','balance',1,30,'unused')`)
	if e != nil {
		t.Fatal(e)
	}
	p, e := s.PreviewCodes(ctx, c.ID, []string{"MONTHLY-CODE-00001", "YEARLY-CODE-000002", "USED-CODE-0000003", "BALANCE-CODE-0004", "missing"})
	if e != nil || p.ValidCount != 1 || p.InvalidCount != 4 {
		t.Fatalf("preview=%+v %v", p, e)
	}
	if _, e = s.ImportCodes(ctx, c.ID, []string{"MONTHLY-CODE-00001", "YEARLY-CODE-000002"}, 1); e == nil {
		t.Fatal("partial invalid import accepted")
	}
	var n int
	must(t, s.db.QueryRow(`SELECT count(*) FROM qq_invite_code_pool`).Scan(&n))
	if n != 0 {
		t.Fatal("partial import")
	}
	if _, e = s.ImportCodes(ctx, c.ID, []string{"MONTHLY-CODE-00001"}, 1); e != nil {
		t.Fatal(e)
	}
	if _, e = s.ImportCodes(ctx, c.ID, []string{"MONTHLY-CODE-00001"}, 1); e == nil {
		t.Fatal("duplicate import")
	}
}

func TestPendingNoticeCancelledWhenAwarded(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	c := campaignFixture(t, s)
	c.PendingTemplate = "pending"
	c, e := s.SaveCampaign(ctx, c, 1)
	if e != nil {
		t.Fatal(e)
	}
	configure(t, s)
	r := NewRuntime(s, func(Config) (Bot, error) { return &fakeBot{}, nil })
	must(t, s.AcceptEvent(ctx, Event{SelfID: "123456", GroupID: "100", UserID: "33333", OperatorID: "44444", SubType: "approve", Kind: "group_increase", Time: time.Now().Unix()}))
	must(t, r.ProcessOnce(ctx))
	records, e := s.ListRecords(ctx, c.ID, "", 1, 20)
	if e != nil {
		t.Fatal(e)
	}
	if e = s.ReviewRecord(ctx, records.Items[0].ID, "44444", "evidence verified", 1); e != nil {
		t.Fatal(e)
	}
	if _, e := s.GenerateCodes(ctx, c.ID, 1, 1); e != nil {
		t.Fatal(e)
	}
	must(t, r.ProcessOnce(ctx))
	var state string
	must(t, s.db.QueryRow(`SELECT status FROM qq_invite_outbox WHERE kind='pending_notice'`).Scan(&state))
	if state != "cancelled" {
		t.Fatalf("stale pending notice remains %s", state)
	}
}

func TestExpiringInventoryAndExpiredDelivery(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	c := campaignFixture(t, s)
	configure(t, s)
	_, e := s.db.Exec(`INSERT INTO redeem_codes(code,type,group_id,expires_at) VALUES('FUTURE-CODE-00001','subscription',1,now()+interval '1 day'),('EXPIRED-CODE-0002','subscription',1,now()-interval '1 day')`)
	if e != nil {
		t.Fatal(e)
	}
	p, e := s.PreviewCodes(ctx, c.ID, []string{"FUTURE-CODE-00001", "EXPIRED-CODE-0002"})
	if e != nil || p.ValidCount != 1 || p.Items[1].Reason != "expired" {
		t.Fatalf("preview %+v %v", p, e)
	}
	if _, e = s.ImportCodes(ctx, c.ID, []string{"FUTURE-CODE-00001"}, 1); e != nil {
		t.Fatal(e)
	}
	must(t, s.AcceptEvent(ctx, Event{SelfID: "123456", GroupID: "100", UserID: "33333", OperatorID: "44444", SubType: "invite", Kind: "group_increase", Time: time.Now().Unix()}))
	b := &fakeBot{}
	r := NewRuntime(s, func(Config) (Bot, error) { return b, nil })
	if e = r.ProcessOnce(ctx); e != nil {
		t.Fatal(e)
	}
	// Simulate the passage of time without permitting production metadata edits.
	if _, e = s.db.Exec(`ALTER TABLE redeem_codes DISABLE TRIGGER qq_invite_redeem_code_guard; UPDATE redeem_codes SET expires_at=now()-interval '1 day' WHERE code='FUTURE-CODE-00001'; ALTER TABLE redeem_codes ENABLE TRIGGER qq_invite_redeem_code_guard`); e != nil {
		t.Fatal(e)
	}
	for i := 0; i < 3; i++ {
		if e = r.DeliverOnce(ctx); e != nil {
			t.Fatal(e)
		}
	}
	if len(b.private) != 0 {
		t.Fatal("expired code sent")
	}
	var status, reason string
	must(t, s.db.QueryRow(`SELECT status,last_error FROM qq_invite_outbox WHERE kind='private'`).Scan(&status, &reason))
	if status != "failed" || reason != "code_expired" {
		t.Fatalf("%s %s", status, reason)
	}
}

func TestPausedCampaignLateEvidenceUsesOriginalVersion(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	c := campaignFixture(t, s)
	configure(t, s)
	// Transport timestamps are seconds. Arrange stable version boundaries without sleeping.
	if _, e := s.db.Exec(`UPDATE qq_invite_campaign_versions SET created_at=now()-interval '2 minutes' WHERE campaign_id=$1`, c.ID); e != nil {
		t.Fatal(e)
	}
	c.Enabled = false
	c.WelcomeTemplate = "changed after pause"
	if _, e := s.SaveCampaign(ctx, c, 1); e != nil {
		t.Fatal(e)
	}
	ev := Event{SelfID: "123456", GroupID: "100", UserID: "33333", OperatorID: "44444", SubType: "invite", Kind: "group_increase", Time: time.Now().Add(-time.Minute).Unix()}
	if e := s.AcceptEvent(ctx, ev); e != nil {
		t.Fatal(e)
	}
	ev.UserID = "55555"
	ev.Time = time.Now().Unix()
	if e := s.AcceptEvent(ctx, ev); e != nil {
		t.Fatal(e)
	}
	b := &fakeBot{accountErr: errors.New("network failure")}
	r := NewRuntime(s, func(Config) (Bot, error) { return b, nil })
	if e := r.ProcessOnce(ctx); e != nil {
		t.Fatal(e)
	}
	records, e := s.ListRecords(ctx, c.ID, "", 1, 20)
	if e != nil || len(records.Items) != 1 || records.Items[0].Status != "received" || records.Items[0].Reason != "bot_unavailable" || records.Items[0].QQLevel != nil || records.Items[0].RewardID != nil {
		t.Fatalf("records=%+v %v", records, e)
	}
	var template string
	if e = s.db.QueryRow(`SELECT template FROM qq_invite_outbox WHERE kind='welcome'`).Scan(&template); e != nil || template != "Welcome {invitee}" {
		t.Fatalf("template=%q %v", template, e)
	}
	b.accountErr = nil
	if _, e = s.GenerateCodes(ctx, c.ID, 1, 1); e != nil {
		t.Fatal(e)
	}
	if e = s.RecheckRecord(ctx, records.Items[0].ID, 1); e != nil {
		t.Fatal(e)
	}
	if e = r.ProcessOnce(ctx); e != nil {
		t.Fatal(e)
	}
	records, e = s.ListRecords(ctx, c.ID, "", 1, 20)
	if e != nil || records.Items[0].Status != "awarded" {
		t.Fatalf("late completion %+v %v", records, e)
	}
}

func TestRejoinDoesNotWelcomeOrAwardAgain(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	c := campaignFixture(t, s)
	configure(t, s)
	if _, e := s.GenerateCodes(ctx, c.ID, 2, 1); e != nil {
		t.Fatal(e)
	}
	r := NewRuntime(s, func(Config) (Bot, error) { return &fakeBot{}, nil })
	ev := Event{SelfID: "123456", GroupID: "100", UserID: "33333", OperatorID: "44444", SubType: "invite", Kind: "group_increase", Time: time.Now().Unix()}
	if e := s.AcceptEvent(ctx, ev); e != nil {
		t.Fatal(e)
	}
	if e := r.ProcessOnce(ctx); e != nil {
		t.Fatal(e)
	}
	ev.EventID = "rejoin-event"
	ev.OperatorID = "77777"
	if e := s.AcceptEvent(ctx, ev); e != nil {
		t.Fatal(e)
	}
	if e := r.ProcessOnce(ctx); e != nil {
		t.Fatal(e)
	}
	var rewards, welcome int
	var inviter string
	must(t, s.db.QueryRow(`SELECT count(*) FROM qq_invite_rewards`).Scan(&rewards))
	must(t, s.db.QueryRow(`SELECT count(*) FROM qq_invite_outbox WHERE kind='welcome'`).Scan(&welcome))
	must(t, s.db.QueryRow(`SELECT inviter_qq FROM qq_invite_records`).Scan(&inviter))
	if rewards != 1 || welcome != 1 || inviter != "44444" {
		t.Fatalf("reward=%d welcome=%d inviter=%s", rewards, welcome, inviter)
	}
}

func TestCampaignOverlapBaselineAndSecretPreservation(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	c := campaignFixture(t, s)
	other := c
	other.ID = 0
	other.Version = 0
	other.Enabled = false
	other.Name = "second"
	var e error
	other, e = s.SaveCampaign(ctx, other, 1)
	if e != nil {
		t.Fatal(e)
	}
	other.Enabled = true
	if _, e = s.SaveCampaign(ctx, other, 1); !errors.Is(e, ErrInvalid) {
		t.Fatalf("enabled before baseline %v", e)
	}
	if e = s.SaveBaseline(ctx, other.ID, map[string][]string{"100": {}, "200": {}}, 1); e != nil {
		t.Fatal(e)
	}
	if _, e = s.SaveCampaign(ctx, other, 1); !errors.Is(e, ErrConflict) {
		t.Fatalf("overlap allowed %v", e)
	}
	configure(t, s)
	cfg, e := s.GetConfig(ctx)
	if e != nil {
		t.Fatal(e)
	}
	cfg.Enabled = false
	if _, e = s.SaveConfig(ctx, cfg, 1); e != nil {
		t.Fatal(e)
	}
	raw, e := s.RuntimeConfig(ctx)
	if e != nil || raw.EventSecret != "secret" || raw.OneBotToken != "secret" {
		t.Fatalf("blank write lost secret %v", e)
	}
}

func TestImportCompetesWithWebsiteRedemption(t *testing.T) {
	for i := 0; i < 8; i++ {
		t.Run(fmt.Sprint(i), func(t *testing.T) {
			s := testStore(t)
			ctx := context.Background()
			c := campaignFixture(t, s)
			if _, e := s.db.Exec(`INSERT INTO redeem_codes(code,type,group_id) VALUES('RACE-MONTHLY-CODE','subscription',1)`); e != nil {
				t.Fatal(e)
			}
			start := make(chan struct{})
			errs := make(chan error, 2)
			go func() { <-start; _, e := s.ImportCodes(ctx, c.ID, []string{"RACE-MONTHLY-CODE"}, 1); errs <- e }()
			go func() {
				<-start
				_, e := s.db.Exec(`UPDATE redeem_codes SET status='used',used_by=123,used_at=now() WHERE code='RACE-MONTHLY-CODE'`)
				errs <- e
			}()
			close(start)
			e1, e2 := <-errs, <-errs
			if e1 == nil && e2 == nil {
				t.Fatal("both import and website redemption succeeded")
			}
			var n int
			if e := s.db.QueryRow(`SELECT count(*) FROM qq_invite_code_pool p JOIN redeem_codes c ON c.id=p.redeem_code_id WHERE c.status='used' AND p.record_id IS NULL`).Scan(&n); e != nil || n != 0 {
				t.Fatalf("used unassigned inventory n=%d %v", n, e)
			}
		})
	}
}

func TestPlanNameSnapshotAndDisabledSubscriptionStock(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	c := campaignFixture(t, s)
	configure(t, s)
	if renderTemplate("{plan_name}", workRecord{campaign: c}, 11, "", "") != "monthly" {
		t.Fatal("plan_name must be subscription name, not campaign name")
	}
	if _, e := s.GenerateCodes(ctx, c.ID, 1, 1); e != nil {
		t.Fatal(e)
	}
	if _, e := s.db.Exec(`UPDATE groups SET status='inactive' WHERE id=1`); e != nil {
		t.Fatal(e)
	}
	if _, e := s.GenerateCodes(ctx, c.ID, 1, 1); !errors.Is(e, ErrInvalid) {
		t.Fatalf("inactive subscription generation accepted %v", e)
	}
	if e := s.AcceptEvent(ctx, Event{SelfID: "123456", GroupID: "100", UserID: "33333", OperatorID: "44444", SubType: "invite", Kind: "group_increase", Time: time.Now().Unix()}); e != nil {
		t.Fatal(e)
	}
	r := NewRuntime(s, func(Config) (Bot, error) { return &fakeBot{}, nil })
	if e := r.ProcessOnce(ctx); e != nil {
		t.Fatal(e)
	}
	records, e := s.ListRecords(ctx, c.ID, "", 1, 20)
	if e != nil || records.Items[0].Status != "queued_inventory" {
		t.Fatalf("inactive subscription awarded %+v %v", records, e)
	}
}

func TestPrivateFailureQueuesClaimNoticeAndRetryLimit(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	c := campaignFixture(t, s)
	configure(t, s)
	if _, e := s.GenerateCodes(ctx, c.ID, 1, 1); e != nil {
		t.Fatal(e)
	}
	b := &fakeBot{privateErr: errors.New("explicit send rejection")}
	r := NewRuntime(s, func(Config) (Bot, error) { return b, nil })
	if e := s.AcceptEvent(ctx, Event{SelfID: "123456", GroupID: "100", UserID: "33333", OperatorID: "44444", SubType: "invite", Kind: "group_increase", Time: time.Now().Unix()}); e != nil {
		t.Fatal(e)
	}
	if e := r.ProcessOnce(ctx); e != nil {
		t.Fatal(e)
	}
	for i := 0; i < 3; i++ {
		if e := r.DeliverOnce(ctx); e != nil {
			t.Fatal(e)
		}
	}
	var mention, template, status string
	var notices int
	if e := s.db.QueryRow(`SELECT mention,template,status FROM qq_invite_outbox WHERE kind='delivery_pending'`).Scan(&mention, &template, &status); e != nil {
		t.Fatalf("missing failed-delivery claim notice: %v", e)
	}
	if mention != "44444" || !strings.Contains(template, "领取奖励") || status != "sent" {
		t.Fatalf("notice %s %s %s", mention, template, status)
	}
	for i := 0; i < 7; i++ {
		if _, e := s.db.Exec(`UPDATE qq_invite_outbox SET next_attempt_at=now() WHERE kind='private'`); e != nil {
			t.Fatal(e)
		}
		if e := r.DeliverOnce(ctx); e != nil {
			t.Fatal(e)
		}
	}
	if e := s.db.QueryRow(`SELECT status FROM qq_invite_outbox WHERE kind='private'`).Scan(&status); e != nil || status != "blocked" {
		t.Fatalf("retry limit status=%s %v", status, e)
	}
	must(t, s.db.QueryRow(`SELECT count(*) FROM qq_invite_outbox WHERE kind='delivery_pending'`).Scan(&notices))
	if notices != 1 || len(b.private) != 8 {
		t.Fatalf("notices=%d attempts=%d", notices, len(b.private))
	}
	records, e := s.ListRecords(ctx, c.ID, "", 1, 20)
	if e != nil {
		t.Fatal(e)
	}
	if e := s.RedeliverReward(ctx, *records.Items[0].RewardID, 1); e != nil {
		t.Fatal(e)
	}
	b.privateErr = nil
	if e := r.DeliverOnce(ctx); e != nil {
		t.Fatal(e)
	}
	if e := r.DeliverOnce(ctx); e != nil {
		t.Fatal(e)
	}
	if len(b.private) != 9 || b.private[0] != b.private[8] {
		t.Fatal("manual retry did not keep original code")
	}
}

func TestUnknownOutcomeNeverAutomaticallyResends(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	c := campaignFixture(t, s)
	configure(t, s)
	if _, e := s.GenerateCodes(ctx, c.ID, 1, 1); e != nil {
		t.Fatal(e)
	}
	b := &fakeBot{privateErr: ErrDeliveryUnknown}
	r := NewRuntime(s, func(Config) (Bot, error) { return b, nil })
	must(t, s.AcceptEvent(ctx, Event{SelfID: "123456", GroupID: "100", UserID: "33333", OperatorID: "44444", SubType: "invite", Kind: "group_increase", Time: time.Now().Unix()}))
	must(t, r.ProcessOnce(ctx))
	must(t, r.DeliverOnce(ctx))
	must(t, r.DeliverOnce(ctx))
	for i := 0; i < 3; i++ {
		if _, e := s.db.Exec(`UPDATE qq_invite_outbox SET next_attempt_at=now() WHERE kind='private'`); e != nil {
			t.Fatal(e)
		}
		if e := r.DeliverOnce(ctx); e != nil {
			t.Fatal(e)
		}
	}
	if len(b.private) != 1 {
		t.Fatalf("unknown automatically resent %d times", len(b.private))
	}
	var status string
	must(t, s.db.QueryRow(`SELECT status FROM qq_invite_outbox WHERE kind='private'`).Scan(&status))
	if status != "unknown" {
		t.Fatal(status)
	}
}

func TestOwnBotJoinAndBaselineMembership(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	c := campaignFixture(t, s)
	configure(t, s)
	if e := s.AcceptEvent(ctx, Event{SelfID: "123456", GroupID: "100", UserID: "123456", OperatorID: "44444", SubType: "invite", Kind: "group_increase", Time: time.Now().Unix()}); e != nil {
		t.Fatal(e)
	}
	records, e := s.ListRecords(ctx, c.ID, "", 1, 20)
	if e != nil || len(records.Items) != 0 {
		t.Fatal("bot's own join created reward eligibility")
	}
	c.Enabled = false
	c, e = s.SaveCampaign(ctx, c, 1)
	if e != nil {
		t.Fatal(e)
	}
	r := NewRuntime(s, func(Config) (Bot, error) { return &fakeBot{}, nil })
	if e = r.SyncBaseline(ctx, c.ID, 1); !errors.Is(e, ErrInvalid) {
		t.Fatalf("baseline without bot membership accepted: %v", e)
	}
}

func TestTemplateAndRedeemOriginValidation(t *testing.T) {
	c := Campaign{PrivateTemplate: "{code} {url}"}
	if got := renderTemplate("{url}", workRecord{campaign: c}, 0, "", "https://sub.example.com/"); got != "https://sub.example.com/redeem" {
		t.Fatalf("redemption URL=%s", got)
	}
	for level, want := range map[int]string{0: "未采集", 10: "10"} {
		if got := renderTemplate("{level}", workRecord{campaign: c}, level, "", ""); got != want {
			t.Fatalf("level %d rendered as %q, want %q", level, got, want)
		}
	}
	for _, v := range []string{"{unknown}", "{{code}}", "unbalanced{", "{code}"} {
		if checkTemplate(v, false) {
			t.Fatalf("public template accepted %q", v)
		}
	}
	if !checkTemplate(strings.Repeat("中", 2000), false) {
		t.Fatal("2000 UTF16 Chinese template rejected")
	}
	if checkTemplate(strings.Repeat("😀", 1001), false) {
		t.Fatal("more than 2000 UTF16 units accepted")
	}
	s := testStore(t)
	ctx := context.Background()
	_, e := s.SaveConfig(ctx, Config{Enabled: true, PublicBaseURL: "https://sub.example.com/path", OneBotURL: "http://napcat:3000", WebUIURL: "http://napcat:6099", BotQQ: "123456", OneBotToken: "token", WebUIToken: "token", EventSecret: "token"}, 1)
	if !errors.Is(e, ErrInvalid) {
		t.Fatalf("non-origin URL accepted %v", e)
	}
}

func TestMigrationCanBeAppliedTwiceWithoutChangingLedger(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	c := campaignFixture(t, s)
	configure(t, s)
	if _, e := s.GenerateCodes(ctx, c.ID, 1, 1); e != nil {
		t.Fatal(e)
	}
	b, e := os.ReadFile("../../migrations/253_qq_invite_rewards.sql")
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.db.Exec(string(b)); e != nil {
		t.Fatalf("second migration application: %v", e)
	}
	list, e := s.ListCodes(ctx, c.ID, 1, 20)
	if e != nil || list.Total != 1 || list.Available != 1 {
		t.Fatalf("ledger changed %+v %v", list, e)
	}
	cfg, e := s.RuntimeConfig(ctx)
	if e != nil || cfg.OneBotToken != "secret" {
		t.Fatalf("config changed %v", e)
	}
	if _, e = s.db.Exec(`UPDATE redeem_codes SET status='used',used_by=1,used_at=now()`); e == nil {
		t.Fatal("guard missing after reapply")
	}
}

func TestPrivateClaimImmediatelyResumesOwnOriginalCode(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	c := campaignFixture(t, s)
	configure(t, s)
	if _, e := s.GenerateCodes(ctx, c.ID, 1, 1); e != nil {
		t.Fatal(e)
	}
	b := &fakeBot{privateErr: ErrDeliveryUnknown}
	r := NewRuntime(s, func(Config) (Bot, error) { return b, nil })
	must(t, s.AcceptEvent(ctx, Event{SelfID: "123456", GroupID: "100", UserID: "33333", OperatorID: "44444", SubType: "invite", Kind: "group_increase", Time: time.Now().Unix()}))
	must(t, r.ProcessOnce(ctx))
	must(t, r.DeliverOnce(ctx))
	must(t, r.DeliverOnce(ctx))
	claim := Event{SelfID: "123456", UserID: "55555", Kind: "private_claim", Time: time.Now().Unix(), EventID: "other-claim"}
	if e := s.AcceptEvent(ctx, claim); e != nil {
		t.Fatal(e)
	}
	var status string
	must(t, s.db.QueryRow(`SELECT status FROM qq_invite_outbox WHERE kind='private'`).Scan(&status))
	if status != "unknown" {
		t.Fatal("other QQ claimed reward")
	}
	claim.UserID = "44444"
	claim.EventID = "own-claim"
	if e := s.AcceptEvent(ctx, claim); e != nil {
		t.Fatal(e)
	}
	must(t, s.db.QueryRow(`SELECT status FROM qq_invite_outbox WHERE kind='private'`).Scan(&status))
	if status != "pending" {
		t.Fatalf("immediate claim ignored: %s", status)
	}
	b.privateErr = nil
	if e := r.DeliverOnce(ctx); e != nil {
		t.Fatal(e)
	}
	var pending string
	if e := s.db.QueryRow(`SELECT status FROM qq_invite_outbox WHERE kind='delivery_pending'`).Scan(&pending); e != nil || pending != "cancelled" {
		t.Fatalf("pending notice not cancelled %s %v", pending, e)
	}
	if len(b.private) != 2 || b.private[0] != b.private[1] {
		t.Fatal("claim changed original code")
	}
}

func TestConfigEncryptedAndVersioned(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	c := Config{OneBotURL: "http://napcat:3000", WebUIURL: "http://napcat:6099", BotQQ: "123456", OneBotToken: "onebot-secret", WebUIToken: "webui-secret", EventSecret: "event-secret"}
	saved, e := s.SaveConfig(ctx, c, 1)
	if e != nil {
		t.Fatal(e)
	}
	if saved.OneBotToken != "" || !saved.OneBotTokenConfigured {
		t.Fatalf("unsafe config %+v", saved)
	}
	var data string
	if e = s.db.QueryRow("SELECT payload::text FROM qq_invite_config_versions").Scan(&data); e != nil {
		t.Fatal(e)
	}
	if strings.Contains(data, "onebot-secret") {
		t.Fatal("plaintext secret")
	}
	if _, e = s.SaveConfig(ctx, c, 1); e == nil {
		t.Fatal("stale config accepted")
	}
	runtime, e := s.RuntimeConfig(ctx)
	if e != nil || runtime.OneBotToken != "onebot-secret" {
		t.Fatalf("decrypt: %v", e)
	}
}

func campaignFixture(t *testing.T, s *Store) Campaign {
	t.Helper()
	c, e := s.SaveCampaign(context.Background(), Campaign{Name: "Invite", GroupIDs: []string{"100", "200"}, SubscriptionGroupID: 1, StartsAt: time.Now().Add(-time.Hour), EndsAt: time.Now().Add(time.Hour), WelcomeTemplate: "Welcome {invitee}", PendingTemplate: "Pending {invitee}", CongratulationTemplate: "Congratulations {inviter}", PrivateTemplate: "Code {code} {url}"}, 1)
	if e != nil {
		t.Fatal(e)
	}
	if e = s.SaveBaseline(context.Background(), c.ID, map[string][]string{"100": {"99999"}, "200": {}}, 1); e != nil {
		t.Fatal(e)
	}
	c.Enabled = true
	c, e = s.SaveCampaign(context.Background(), c, 1)
	if e != nil {
		t.Fatal(e)
	}
	return c
}

func TestInventoryGuardsAndAtomicAllocation(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	c := campaignFixture(t, s)
	if _, e := s.GenerateCodes(ctx, c.ID, 1, 1); e != nil {
		t.Fatal(e)
	}
	if _, e := s.db.Exec("UPDATE redeem_codes SET status='used',used_by=2"); e == nil {
		t.Fatal("unallocated code redeemable")
	}
	if _, e := s.db.Exec("UPDATE redeem_codes SET validity_days=300"); e == nil {
		t.Fatal("pool metadata mutable")
	}
	if _, e := s.db.Exec("DELETE FROM redeem_codes"); e == nil {
		t.Fatal("pool code deletable")
	}
	bot := &fakeBot{}
	r := NewRuntime(s, func(Config) (Bot, error) { return bot, nil })
	configure(t, s)
	for _, g := range []string{"100", "200"} {
		if e := s.AcceptEvent(ctx, Event{SelfID: "123456", GroupID: g, UserID: "33333", OperatorID: "44444", SubType: "invite", Time: time.Now().Unix(), Kind: "group_increase"}); e != nil {
			t.Fatal(e)
		}
	}
	if e := r.ProcessOnce(ctx); e != nil {
		t.Fatal(e)
	}
	if e := r.ProcessOnce(ctx); e != nil {
		t.Fatal(e)
	}
	var n int
	if e := s.db.QueryRow("SELECT count(*) FROM qq_invite_rewards").Scan(&n); e != nil || n != 1 {
		t.Fatalf("rewards=%d err=%v", n, e)
	}
	var level sql.NullInt64
	must(t, s.db.QueryRow("SELECT qq_level FROM qq_invite_records").Scan(&level))
	if level.Valid {
		t.Fatalf("new reward recorded a QQ level: %d", level.Int64)
	}
	if _, e := s.db.Exec("UPDATE redeem_codes SET status='used',used_by=2,used_at=now()"); e != nil {
		t.Fatalf("allocated redemption rejected: %v", e)
	}
}

type fakeBot struct {
	accountErr error
	privateErr error
	private    []string
	groups     []string
}

func (b *fakeBot) CheckAccount(context.Context) error { return b.accountErr }
func (b *fakeBot) GetLevel(context.Context, string) (int, error) {
	panic("invitation rewards must not query QQ level")
}
func (b *fakeBot) GroupMembers(context.Context, string) ([]string, error) {
	return []string{"99999"}, nil
}
func (b *fakeBot) SendGroup(_ context.Context, _ string, text string, _ string) (string, error) {
	b.groups = append(b.groups, text)
	return "group-id", nil
}
func (b *fakeBot) SendPrivate(_ context.Context, _ string, text string) (string, error) {
	b.private = append(b.private, text)
	return "private-id", b.privateErr
}
func configure(t *testing.T, s *Store) {
	t.Helper()
	_, e := s.SaveConfig(context.Background(), Config{Enabled: true, PublicBaseURL: "https://sub.example.com", OneBotURL: "http://napcat:3000", WebUIURL: "http://napcat:6099", BotQQ: "123456", OneBotToken: "secret", WebUIToken: "secret", EventSecret: "secret"}, 1)
	if e != nil {
		t.Fatal(e)
	}
}

func TestUnknownAttributionBaselineAndStockQueue(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	c := campaignFixture(t, s)
	configure(t, s)
	b := &fakeBot{}
	r := NewRuntime(s, func(Config) (Bot, error) { return b, nil })
	for _, ev := range []Event{{UserID: "11111", OperatorID: "44444", SubType: "approve"}, {UserID: "22222", OperatorID: "44444", SubType: "invite"}, {UserID: "99999", OperatorID: "44444", SubType: "invite"}} {
		ev.Kind = "group_increase"
		ev.SelfID = "123456"
		ev.GroupID = "100"
		ev.Time = time.Now().Unix()
		if e := s.AcceptEvent(ctx, ev); e != nil {
			t.Fatal(e)
		}
		if e := r.ProcessOnce(ctx); e != nil {
			t.Fatal(e)
		}
	}
	rows, e := s.ListRecords(ctx, c.ID, "", 1, 20)
	if e != nil {
		t.Fatal(e)
	}
	states := map[string]string{}
	var reviewID int64
	for _, v := range rows.Items {
		states[v.InviteeQQ] = v.Status
		if v.QQLevel != nil {
			t.Fatalf("new invitation recorded a QQ level: %+v", v)
		}
		if v.InviteeQQ == "11111" {
			reviewID = v.ID
		}
	}
	if states["11111"] != "pending_attribution" || states["22222"] != "queued_inventory" || states["99999"] != "baseline" {
		t.Fatalf("states=%v", states)
	}
	if e := s.ReviewRecord(ctx, reviewID, "44444", "Verified invitation with group administrator", 1); e != nil {
		t.Fatal(e)
	}
	if e := r.ProcessOnce(ctx); e != nil {
		t.Fatal(e)
	}
	if _, e := s.GenerateCodes(ctx, c.ID, 1, 1); e != nil {
		t.Fatal(e)
	}
	if e := r.ProcessOnce(ctx); e != nil {
		t.Fatal(e)
	}
	var n int
	must(t, s.db.QueryRow("SELECT count(*) FROM qq_invite_rewards").Scan(&n))
	if n != 1 {
		t.Fatalf("queued reward not filled %d", n)
	}
}

func TestLegacyLevelRecordsFollowCurrentPolicy(t *testing.T) {
	for _, tc := range []struct {
		status string
		level  int
		reason string
	}{
		{status: "pending_level", reason: "qq_level_unavailable"},
		{status: "ineligible", level: 10, reason: "qq_level_must_exceed_10"},
	} {
		t.Run(tc.status, func(t *testing.T) {
			s := testStore(t)
			ctx := context.Background()
			c := campaignFixture(t, s)
			configure(t, s)
			if _, e := s.GenerateCodes(ctx, c.ID, 1, 1); e != nil {
				t.Fatal(e)
			}
			must(t, s.AcceptEvent(ctx, Event{SelfID: "123456", GroupID: "100", UserID: "33333", OperatorID: "44444", SubType: "invite", Kind: "group_increase", Time: time.Now().Unix()}))
			var id int64
			must(t, s.db.QueryRow(`UPDATE qq_invite_records SET status=$1,qq_level=NULLIF($2,0),reason=$3 RETURNING id`, tc.status, tc.level, tc.reason).Scan(&id))
			r := NewRuntime(s, func(Config) (Bot, error) { return &fakeBot{}, nil })
			must(t, r.ProcessOnce(ctx))
			if tc.status == "ineligible" {
				records, e := s.ListRecords(ctx, c.ID, "", 1, 20)
				if e != nil || len(records.Items) != 1 || records.Items[0].Status != "ineligible" || records.Items[0].RewardID != nil {
					t.Fatalf("legacy rejection changed without explicit recheck: %+v %v", records, e)
				}
				must(t, s.RecheckRecord(ctx, id, 1))
				must(t, r.ProcessOnce(ctx))
			}
			records, e := s.ListRecords(ctx, c.ID, "", 1, 20)
			if e != nil || len(records.Items) != 1 || records.Items[0].Status != "awarded" || records.Items[0].RewardID == nil {
				t.Fatalf("legacy record not awarded under current policy: %+v %v", records, e)
			}
			level := records.Items[0].QQLevel
			if (tc.level == 0 && level != nil) || (tc.level != 0 && (level == nil || *level != tc.level)) {
				t.Fatalf("legacy recorded level changed: %+v", records.Items[0])
			}
			if e := s.RecheckRecord(ctx, id, 1); !errors.Is(e, ErrConflict) {
				t.Fatalf("awarded record can be rechecked: %v", e)
			}
			must(t, r.ProcessOnce(ctx))
			var rewards int
			must(t, s.db.QueryRow(`SELECT count(*) FROM qq_invite_rewards`).Scan(&rewards))
			if rewards != 1 {
				t.Fatalf("legacy record awarded %d times", rewards)
			}
		})
	}
}
