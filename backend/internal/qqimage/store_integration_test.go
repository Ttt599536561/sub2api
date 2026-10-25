package qqimage

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	_ "github.com/lib/pq"
)

func testStore(t *testing.T) *Store {
	t.Helper()
	dsn := os.Getenv("QQ_IMAGE_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("QQ_IMAGE_TEST_DATABASE_URL is required for real PostgreSQL tests")
	}
	if !strings.Contains(dsn, "127.0.0.1") && !strings.Contains(dsn, "localhost") {
		t.Fatal("only explicit loopback test PostgreSQL is allowed")
	}
	db, e := sql.Open("postgres", dsn)
	if e != nil {
		t.Fatal(e)
	}
	db.SetMaxOpenConns(20)
	schema := "qq_test_" + token(8)
	if _, e = db.Exec(`CREATE SCHEMA ` + schema); e != nil {
		t.Fatal(e)
	}
	scoped, e := sql.Open("postgres", dsn+"&search_path="+schema)
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { scoped.Close(); db.Exec(`DROP SCHEMA ` + schema + ` CASCADE`); db.Close() })
	_, e = scoped.Exec(`CREATE TABLE users(id BIGSERIAL PRIMARY KEY,email TEXT NOT NULL,password_hash TEXT NOT NULL DEFAULT '',status TEXT NOT NULL DEFAULT 'active',deleted_at TIMESTAMPTZ,balance NUMERIC(20,8) NOT NULL DEFAULT 0,frozen_balance NUMERIC(20,8) NOT NULL DEFAULT 0,updated_at TIMESTAMPTZ NOT NULL DEFAULT now())`)
	if e != nil {
		t.Fatal(e)
	}
	b, e := os.ReadFile(filepath.Join("..", "..", "migrations", "248_qq_image_generation.sql"))
	if e != nil {
		t.Fatal(e)
	}
	if _, e = scoped.Exec(string(b)); e != nil {
		t.Fatal(e)
	}
	b, e = os.ReadFile(filepath.Join("..", "..", "migrations", "249_qq_image_group_discovery.sql"))
	if e != nil {
		t.Fatal(e)
	}
	if _, e = scoped.Exec(string(b)); e != nil {
		t.Fatal(e)
	}
	b, e = os.ReadFile(filepath.Join("..", "..", "migrations", "250_qq_reply_templates.sql"))
	if e != nil {
		t.Fatal(e)
	}
	if _, e = scoped.Exec(string(b)); e != nil {
		t.Fatal(e)
	}
	s := NewStore(scoped, []byte(strings.Repeat("k", 32)))
	c := DefaultConfig()
	c.Enabled = true
	c.AppID = "app"
	c.APIURL = "https://images.example.com/v1"
	c.APIKey = "upstream-secret"
	c.BotSecret = "qq-secret"
	c.Model = "image-model"
	c.Size = "1024x1024"
	c.Price = "1"
	c.AllowedGroups = []string{"group", "group2"}
	c.PublicBaseURL = "https://sub.example.com"
	c.UserDailyAmount = "20"
	if _, e = s.SaveConfig(context.Background(), c, 1); e != nil {
		t.Fatal(e)
	}
	return s
}
func fixtureUser(t *testing.T, s *Store, email, balance string) int64 {
	t.Helper()
	var id int64
	if e := s.db.QueryRow(`INSERT INTO users(email,balance) VALUES($1,$2) RETURNING id`, email, balance).Scan(&id); e != nil {
		t.Fatal(e)
	}
	if e := s.MarkEmailVerified(context.Background(), id, email); e != nil {
		t.Fatal(e)
	}
	if e := s.MarkRecentAuthorization(context.Background(), id, "session"); e != nil {
		t.Fatal(e)
	}
	return id
}
func fixtureIdentity(subject, group string) Identity {
	return Identity{Platform: "qq", AppID: "app", SubjectType: "group_member", SubjectID: subject, ScopeType: "group", ScopeID: group, GroupID: group}
}
func fixtureGrant(t *testing.T, s *Store, user int64, i Identity) *Grant {
	t.Helper()
	ctx := context.Background()
	r, e := s.CreateBindingRequest(ctx, i)
	if e != nil {
		t.Fatal(e)
	}
	a, e := s.AuthorizeBinding(ctx, r.ID, user, "session")
	if e != nil {
		t.Fatal(e)
	}
	g, e := s.ConfirmBinding(ctx, i, a.ConfirmationCode)
	if e != nil {
		t.Fatal(e)
	}
	return g
}
func checkWallet(t *testing.T, s *Store, u int64, b, f string) {
	t.Helper()
	var balance, frozen string
	if e := s.db.QueryRow(`SELECT balance::text,frozen_balance::text FROM users WHERE id=$1`, u).Scan(&balance, &frozen); e != nil {
		t.Fatal(e)
	}
	if balance != b || frozen != f {
		t.Fatalf("wallet=(%s,%s) want=(%s,%s)", balance, frozen, b, f)
	}
}

func TestBindingCandidateAndWrongIdentity(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	u := fixtureUser(t, s, "one@example.com", "3")
	other := fixtureUser(t, s, "two@example.com", "3")
	i := fixtureIdentity("one", "group")
	r, e := s.CreateBindingRequest(ctx, i)
	if e != nil {
		t.Fatal(e)
	}
	a, e := s.AuthorizeBinding(ctx, r.ID, u, "session")
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.AuthorizeBinding(ctx, r.ID, other, "session"); !errors.Is(e, ErrConflict) {
		t.Fatalf("candidate overwritten: %v", e)
	}
	if _, e = s.ConfirmBinding(ctx, i, "BADCODE"); e == nil {
		t.Fatal("random code accepted")
	}
	rr, _ := s.GetBindingRequest(ctx, r.ID)
	if rr.Status != "await_qq" {
		t.Fatal("random code invalidated candidate")
	}
	if _, e = s.ConfirmBinding(ctx, fixtureIdentity("attacker", "group"), a.ConfirmationCode); !errors.Is(e, ErrForbidden) {
		t.Fatal(e)
	}
	if _, e = s.ConfirmBinding(ctx, i, a.ConfirmationCode); e == nil {
		t.Fatal("compromised candidate resurrected")
	}
}

func TestOrderReserveAndTerminalConcurrency(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	u := fixtureUser(t, s, "one@example.com", "1")
	a := fixtureIdentity("one", "group")
	b := fixtureIdentity("one", "group2")
	fixtureGrant(t, s, u, a)
	fixtureGrant(t, s, u, b)
	var wg sync.WaitGroup
	results := make(chan *Order, 2)
	for n, i := range []Identity{a, b} {
		wg.Add(1)
		go func(n int, i Identity) {
			defer wg.Done()
			o, e := s.CreateOrder(ctx, i, string(rune('a'+n)), "cat")
			if e == nil {
				results <- o
			}
		}(n, i)
	}
	wg.Wait()
	close(results)
	var order *Order
	count := 0
	for o := range results {
		order = o
		count++
	}
	if count != 1 {
		t.Fatalf("reserved %d orders", count)
	}
	checkWallet(t, s, u, "0.00000000", "1.00000000")
	claimed, e := s.ClaimOrder(ctx, "test", time.Minute)
	if e != nil {
		t.Fatal(e)
	}
	wg.Add(2)
	go func() {
		defer wg.Done()
		s.CompleteGeneration(ctx, order.ID, claimed.LeaseToken, Artifact{Data: []byte("image"), MIME: "image/png", Width: 1, Height: 1})
	}()
	go func() { defer wg.Done(); s.FailGeneration(ctx, order.ID, claimed.LeaseToken, "failed") }()
	wg.Wait()
	o, e := s.GetOrder(ctx, order.ID, u)
	if e != nil {
		t.Fatal(e)
	}
	if o.FundsStatus == "captured" {
		checkWallet(t, s, u, "0.00000000", "0.00000000")
		for range 2 {
			wg.Add(1)
			go func() { defer wg.Done(); s.RefundOrder(ctx, order.ID, 99, "compensate") }()
		}
		wg.Wait()
		checkWallet(t, s, u, "1.00000000", "0.00000000")
	} else if o.FundsStatus == "released" {
		checkWallet(t, s, u, "1.00000000", "0.00000000")
	} else {
		t.Fatalf("bad final %s", o.FundsStatus)
	}
}

func TestOrderDeadlineRecoveryAndOriginalOwner(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	u := fixtureUser(t, s, "one@example.com", "2")
	i := fixtureIdentity("one", "group")
	g := fixtureGrant(t, s, u, i)
	o, e := s.CreateOrder(ctx, i, "msg", "cat")
	if e != nil {
		t.Fatal(e)
	}
	c, e := s.ClaimOrder(ctx, "worker", time.Millisecond)
	if e != nil {
		t.Fatal(e)
	}
	s.db.Exec(`UPDATE qq_image_orders SET deadline=clock_timestamp()-interval '1 second' WHERE id=$1`, o.ID)
	if _, e = s.CompleteGeneration(ctx, o.ID, c.LeaseToken, Artifact{Data: []byte("image"), MIME: "image/png", Width: 1, Height: 1}); !errors.Is(e, ErrExpired) {
		t.Fatalf("late capture: %v", e)
	}
	if e = s.RecoverExpired(ctx); e != nil {
		t.Fatal(e)
	}
	checkWallet(t, s, u, "2.00000000", "0.00000000")
	if e = s.RevokeGrant(ctx, g.ID, u, "user"); e != nil {
		t.Fatal(e)
	}
	v := fixtureUser(t, s, "two@example.com", "2")
	fixtureGrant(t, s, v, i)
	if _, e = s.GetOrder(ctx, o.ID, v); !errors.Is(e, ErrNotFound) {
		t.Fatal("owner transferred")
	}
	if _, e = s.GetOrderForIdentity(ctx, i, o.ID); !errors.Is(e, ErrForbidden) {
		t.Fatalf("old grant access restored: %v", e)
	}
}

func TestConfigImmutableEncryptedAndMissingKey(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	first, e := s.GetConfig(ctx)
	if e != nil {
		t.Fatal(e)
	}
	if first.APIKey != "" || first.BotSecret != "" || !first.APIKeyConfigured {
		t.Fatal("secret leaked or flag absent")
	}
	c := *first
	c.Price = "2"
	second, e := s.SaveConfig(ctx, c, 1)
	if e != nil {
		t.Fatal(e)
	}
	old, e := s.RuntimeConfig(ctx, first.ID)
	if e != nil {
		t.Fatal(e)
	}
	if old.Price != "1.00000000" || old.APIKey != "upstream-secret" {
		t.Fatal("immutable config changed")
	}
	next, e := s.RuntimeConfig(ctx, second.ID)
	if e != nil || next.APIKey != old.APIKey {
		t.Fatal("blank secret did not retain")
	}
	var encrypted []byte
	if e = s.db.QueryRow(`SELECT api_key FROM qq_image_configs WHERE id=$1`, first.ID).Scan(&encrypted); e != nil {
		t.Fatal(e)
	}
	if strings.Contains(string(encrypted), "upstream-secret") {
		t.Fatal("secret plaintext at rest")
	}
	missing := NewStore(s.db, nil)
	if _, e = missing.SaveConfig(ctx, c, 1); !errors.Is(e, ErrEncryptionUnavailable) {
		t.Fatal(e)
	}
}

func TestOrderGroupLimitAndRestartNeverRedispatch(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	c, _ := s.GetConfig(ctx)
	c.GroupConcurrency = 1
	if _, e := s.SaveConfig(ctx, *c, 1); e != nil {
		t.Fatal(e)
	}
	for n := 0; n < 2; n++ {
		u := fixtureUser(t, s, string(rune('a'+n))+"@example.com", "5")
		i := fixtureIdentity(string(rune('a'+n)), "group")
		fixtureGrant(t, s, u, i)
		if _, e := s.CreateOrder(ctx, i, "event", "cat"); e != nil {
			t.Fatal(e)
		}
	}
	first, e := s.ClaimOrder(ctx, "first", time.Minute)
	if e != nil || first == nil {
		t.Fatalf("claim: %v", e)
	}
	second, e := s.ClaimOrder(ctx, "second", time.Minute)
	if e != nil || second != nil {
		t.Fatalf("group concurrency bypass: %+v %v", second, e)
	}
	if _, e = s.db.Exec(`UPDATE qq_image_orders SET lease_until=clock_timestamp()-interval '1 second' WHERE id=$1`, first.ID); e != nil {
		t.Fatal(e)
	}
	if e = s.RecoverExpired(ctx); e != nil {
		t.Fatal(e)
	}
	o, _ := s.GetOrder(ctx, first.ID, 0)
	if o.Status != "reconciling" {
		t.Fatal("stale dispatch not reconciled")
	}
	next, e := s.ClaimOrder(ctx, "restart", time.Minute)
	if e != nil || next != nil {
		t.Fatal("unknown task was redispatched or cap bypassed")
	}
}

func TestOrderSharedAPIDebtAndExclusiveHold(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	u := fixtureUser(t, s, "one@example.com", "1")
	i := fixtureIdentity("one", "group")
	fixtureGrant(t, s, u, i)
	// API charging intentionally keeps its established permissive UPDATE and can overdraw B.
	var wg sync.WaitGroup
	var o *Order
	var orderErr, errorAPI error
	wg.Add(2)
	go func() { defer wg.Done(); o, orderErr = s.CreateOrder(ctx, i, "event", "cat") }()
	go func() { defer wg.Done(); _, errorAPI = s.db.Exec(`UPDATE users SET balance=balance-1 WHERE id=$1`, u) }()
	wg.Wait()
	if errorAPI != nil {
		t.Fatal(errorAPI)
	}
	if orderErr == nil {
		checkWallet(t, s, u, "-1.00000000", "1.00000000")
		claim, e := s.ClaimOrder(ctx, "worker", time.Minute)
		if e != nil {
			t.Fatal(e)
		}
		if _, e = s.CompleteGeneration(ctx, o.ID, claim.LeaseToken, Artifact{Data: []byte("image"), MIME: "image/png", Width: 1, Height: 1}); e != nil {
			t.Fatal(e)
		}
		checkWallet(t, s, u, "-1.00000000", "0.00000000")
	} else {
		if !errors.Is(orderErr, ErrInsufficientBalance) {
			t.Fatal(orderErr)
		}
		checkWallet(t, s, u, "0.00000000", "0.00000000")
	}
}

func TestOrderCaptureRechecksDeadlineAfterWalletWait(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	u := fixtureUser(t, s, "one@example.com", "2")
	i := fixtureIdentity("one", "group")
	fixtureGrant(t, s, u, i)
	o, _ := s.CreateOrder(ctx, i, "event", "cat")
	claim, _ := s.ClaimOrder(ctx, "worker", time.Minute)
	tx, e := s.db.Begin()
	if e != nil {
		t.Fatal(e)
	}
	defer tx.Rollback()
	if _, e = tx.Exec(`SELECT id FROM users WHERE id=$1 FOR UPDATE`, u); e != nil {
		t.Fatal(e)
	}
	if _, e = s.db.Exec(`UPDATE qq_image_orders SET deadline=clock_timestamp()+interval '150 milliseconds' WHERE id=$1`, o.ID); e != nil {
		t.Fatal(e)
	}
	done := make(chan error, 1)
	go func() {
		_, e := s.CompleteGeneration(ctx, o.ID, claim.LeaseToken, Artifact{Data: []byte("image"), MIME: "image/png", Width: 1, Height: 1})
		done <- e
	}()
	time.Sleep(300 * time.Millisecond)
	if e = tx.Commit(); e != nil {
		t.Fatal(e)
	}
	if e = <-done; !errors.Is(e, ErrExpired) {
		t.Fatalf("capture after waiting beyond hard deadline: %v", e)
	}
	checkWallet(t, s, u, "1.00000000", "1.00000000")
}

func TestBindingRecentSessionAndCurrentEmail(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	u := fixtureUser(t, s, "one@example.com", "2")
	i := fixtureIdentity("one", "group")
	r, _ := s.CreateBindingRequest(ctx, i)
	if _, e := s.AuthorizeBinding(ctx, r.ID, u, "other-session"); !errors.Is(e, ErrForbidden) {
		t.Fatal("session freshness bypass")
	}
	s.db.Exec(`UPDATE users SET email='changed@example.com' WHERE id=$1`, u)
	ok, e := s.EmailVerified(ctx, u)
	if e != nil || ok {
		t.Fatal("old email proof survives change")
	}
	if _, e = s.AuthorizeBinding(ctx, r.ID, u, "session"); !errors.Is(e, ErrForbidden) {
		t.Fatal("changed email authorized")
	}
}

func TestOrderMediaTokenScopeRevocationAndOutbox(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	u := fixtureUser(t, s, "one@example.com", "2")
	i := fixtureIdentity("one", "group")
	g := fixtureGrant(t, s, u, i)
	o, e := s.CreateOrderFromEvent(ctx, i, "event-id", "message-id", time.Now(), "cat")
	if e != nil {
		t.Fatal(e)
	}
	claim, _ := s.ClaimOrder(ctx, "worker", time.Minute)
	if _, e = s.CompleteGeneration(ctx, o.ID, claim.LeaseToken, Artifact{Data: []byte("image"), MIME: "image/png", Width: 1, Height: 1}); e != nil {
		t.Fatal(e)
	}
	d, e := s.ClaimDelivery(ctx, "sender", time.Minute)
	if e != nil || d == nil {
		t.Fatalf("delivery: %v", e)
	}
	if d.ReplyMessageID != "message-id" || d.ReplySequence != 2 {
		t.Fatal("QQ message context lost")
	}
	key, e := s.CreateMediaGrant(ctx, d.ID, d.LeaseToken)
	if e != nil {
		t.Fatal(e)
	}
	for range 3 {
		if _, e = s.ReadMedia(ctx, key); e != nil {
			t.Fatal(e)
		}
	}
	if _, e = s.ReadMedia(ctx, key); !errors.Is(e, ErrExpired) {
		t.Fatal("media grant unlimited")
	}
	key, e = s.CreateMediaGrant(ctx, d.ID, d.LeaseToken)
	if e != nil {
		t.Fatal(e)
	}
	if e = s.RevokeGrant(ctx, g.ID, u, "user"); e != nil {
		t.Fatal(e)
	}
	if _, e = s.ReadMedia(ctx, key); !errors.Is(e, ErrForbidden) {
		t.Fatal("revoked grant can fetch media")
	}
	if _, e = s.GetArtifact(ctx, o.ID, u); e != nil {
		t.Fatal("owner lost private artifact")
	}
	if _, e = s.GetArtifact(ctx, o.ID, u+1); !errors.Is(e, ErrNotFound) {
		t.Fatal("cross-owner artifact leak")
	}
	items, e := s.PendingBalanceInvalidations(ctx, 100)
	if e != nil || len(items) != 2 {
		t.Fatalf("outbox: %v %+v", e, items)
	}
	for _, x := range items {
		if e = s.AckBalanceInvalidation(ctx, x.ID); e != nil {
			t.Fatal(e)
		}
	}
	items, _ = s.PendingBalanceInvalidations(ctx, 100)
	if len(items) != 0 {
		t.Fatal("outbox acknowledgement failed")
	}
}

func TestOrderRemovedGroupBlocksMediaButDisabledDoesNot(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	u := fixtureUser(t, s, "one@example.com", "2")
	i := fixtureIdentity("one", "group")
	fixtureGrant(t, s, u, i)
	o, _ := s.CreateOrder(ctx, i, "event", "cat")
	claim, _ := s.ClaimOrder(ctx, "worker", time.Minute)
	s.CompleteGeneration(ctx, o.ID, claim.LeaseToken, Artifact{Data: []byte("image"), MIME: "image/png", Width: 1, Height: 1})
	c, _ := s.GetConfig(ctx)
	c.Enabled = false
	s.SaveConfig(ctx, *c, 1)
	d, e := s.ClaimDelivery(ctx, "worker", time.Minute)
	if e != nil || d == nil {
		t.Fatal("disabled new entry blocks existing delivery")
	}
	key, e := s.CreateMediaGrant(ctx, d.ID, d.LeaseToken)
	if e != nil {
		t.Fatal(e)
	}
	c, _ = s.GetConfig(ctx)
	c.AllowedGroups = []string{"group2"}
	s.SaveConfig(ctx, *c, 1)
	if _, e = s.ReadMedia(ctx, key); !errors.Is(e, ErrForbidden) {
		t.Fatal("removed group retains media permission")
	}
}

func TestOrderArtifactPersistenceBeforeCaptureAndRestart(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	u := fixtureUser(t, s, "one@example.com", "2")
	i := fixtureIdentity("one", "group")
	fixtureGrant(t, s, u, i)
	o, _ := s.CreateOrder(ctx, i, "event", "cat")
	claim, _ := s.ClaimOrder(ctx, "worker", time.Minute)
	s.db.Exec(`UPDATE users SET status='disabled' WHERE id=$1`, u)
	if _, e := s.CompleteGeneration(ctx, o.ID, claim.LeaseToken, Artifact{Data: []byte("image"), MIME: "image/png", Width: 1, Height: 1}); !errors.Is(e, ErrForbidden) {
		t.Fatal(e)
	}
	var stored bool
	if e := s.db.QueryRow(`SELECT artifact IS NOT NULL FROM qq_image_orders WHERE id=$1`, o.ID).Scan(&stored); e != nil || !stored {
		t.Fatalf("private artifact lost when capture blocked: stored=%v err=%v", stored, e)
	}
	if _, e := s.GetArtifact(ctx, o.ID, u); e == nil {
		t.Fatal("unsettled artifact published")
	}
	s.db.Exec(`UPDATE users SET status='active' WHERE id=$1`, u)
	if e := s.RecoverExpired(ctx); e != nil {
		t.Fatal(e)
	}
	o, _ = s.GetOrder(ctx, o.ID, u)
	if o.FundsStatus != "captured" || o.WebsiteStatus != "available" {
		t.Fatalf("artifact did not recover: %+v", o)
	}
	checkWallet(t, s, u, "1.00000000", "0.00000000")
}

func TestOrderRefundIntentRetriesAfterWalletError(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	u := fixtureUser(t, s, "one@example.com", "2")
	i := fixtureIdentity("one", "group")
	fixtureGrant(t, s, u, i)
	o, _ := s.CreateOrder(ctx, i, "event", "cat")
	claim, _ := s.ClaimOrder(ctx, "worker", time.Minute)
	if _, e := s.CompleteGeneration(ctx, o.ID, claim.LeaseToken, Artifact{Data: []byte("image"), MIME: "image/png", Width: 1, Height: 1}); e != nil {
		t.Fatal(e)
	}
	if _, e := s.db.Exec(`CREATE FUNCTION fail_refund() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.balance>OLD.balance THEN RAISE EXCEPTION 'simulated wallet outage'; END IF; RETURN NEW; END $$; CREATE TRIGGER reject_refund BEFORE UPDATE ON users FOR EACH ROW EXECUTE FUNCTION fail_refund()`); e != nil {
		t.Fatal(e)
	}
	if _, e := s.RefundOrder(ctx, o.ID, 99, "storage compensation"); e == nil {
		t.Fatal("refund falsely reported success")
	}
	var state string
	if e := s.db.QueryRow(`SELECT status FROM qq_image_refund_intents WHERE order_id=$1`, o.ID).Scan(&state); e != nil || state != "pending" {
		t.Fatalf("durable refund intent missing: state=%s err=%v", state, e)
	}
	checkWallet(t, s, u, "1.00000000", "0.00000000")
	s.db.Exec(`DROP TRIGGER reject_refund ON users`)
	if e := s.RecoverExpired(ctx); e != nil {
		t.Fatal(e)
	}
	checkWallet(t, s, u, "2.00000000", "0.00000000")
	o, _ = s.GetOrder(ctx, o.ID, u)
	if o.FundsStatus != "refunded" {
		t.Fatal("pending refund not recovered")
	}
}

func TestOrderDailyLimitIdempotenceAndHoldIsolation(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	c, _ := s.GetConfig(ctx)
	c.UserDailyLimit = 1
	s.SaveConfig(ctx, *c, 1)
	u := fixtureUser(t, s, "one@example.com", "3")
	i := fixtureIdentity("one", "group")
	fixtureGrant(t, s, u, i)
	s.db.Exec(`UPDATE users SET frozen_balance=9 WHERE id=$1`, u)
	o, e := s.CreateOrder(ctx, i, "event", "cat")
	if e != nil {
		t.Fatal(e)
	}
	same, e := s.CreateOrder(ctx, i, "event", "cat")
	if e != nil || same.ID != o.ID {
		t.Fatal("exact replay changed order")
	}
	if _, e = s.CreateOrder(ctx, i, "event", "dog"); !errors.Is(e, ErrConflict) {
		t.Fatal("idempotency fingerprint ignored")
	}
	claim, _ := s.ClaimOrder(ctx, "worker", time.Minute)
	if _, e = s.CompleteGeneration(ctx, o.ID, claim.LeaseToken, Artifact{Data: []byte("image"), MIME: "image/png", Width: 1, Height: 1}); e != nil {
		t.Fatal(e)
	}
	checkWallet(t, s, u, "2.00000000", "9.00000000")
	var wg sync.WaitGroup
	for range 4 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, e := s.RefundOrder(ctx, o.ID, 99, "compensation"); e != nil {
				t.Error(e)
			}
		}()
	}
	wg.Wait()
	checkWallet(t, s, u, "3.00000000", "9.00000000")
	if _, e = s.CreateOrder(ctx, i, "event2", "cat"); !errors.Is(e, ErrLimit) {
		t.Fatalf("refunded success bypassed daily quota: %v", e)
	}
	var count int
	s.db.QueryRow(`SELECT count(*) FROM qq_image_ledger WHERE order_id=$1 AND operation='refund'`, o.ID).Scan(&count)
	if count != 1 {
		t.Fatal("double refund ledger")
	}
}

func TestBindingRevisionCancellationAndAdminActor(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	u := fixtureUser(t, s, "one@example.com", "2")
	i := fixtureIdentity("one", "group")
	old, _ := s.CreateBindingRequest(ctx, i)
	code, _ := s.AuthorizeBinding(ctx, old.ID, u, "session")
	fresh, _ := s.CreateBindingRequest(ctx, i)
	if _, e := s.ConfirmBinding(ctx, i, code.ConfirmationCode); e == nil {
		t.Fatal("old candidate revision revived")
	}
	a, _ := s.AuthorizeBinding(ctx, fresh.ID, u, "session")
	g, e := s.ConfirmBinding(ctx, i, a.ConfirmationCode)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.ConfirmBinding(ctx, i, a.ConfirmationCode); e == nil {
		t.Fatal("confirmation code replayed")
	}
	if e = s.RevokeGrantAsAdmin(ctx, g.ID, 99, "reviewed revocation"); e != nil {
		t.Fatal(e)
	}
	var actor int64
	if e = s.db.QueryRow(`SELECT actor_id FROM qq_image_audit WHERE object_id=$1 AND action='binding.revoke'`, g.ID).Scan(&actor); e != nil || actor != 99 {
		t.Fatalf("admin audit attribution lost: %d %v", actor, e)
	}
}

func TestOrderQueuedCancelDispatchRace(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	u := fixtureUser(t, s, "one@example.com", "2")
	i := fixtureIdentity("one", "group")
	fixtureGrant(t, s, u, i)
	o, _ := s.CreateOrder(ctx, i, "event", "cat")
	var wg sync.WaitGroup
	var claimed *Order
	var ce, errorClaim error
	wg.Add(2)
	go func() { defer wg.Done(); _, ce = s.CancelOrder(ctx, o.ID, u) }()
	go func() { defer wg.Done(); claimed, errorClaim = s.ClaimOrder(ctx, "worker", time.Minute) }()
	wg.Wait()
	if errorClaim != nil {
		t.Fatal(errorClaim)
	}
	o, _ = s.GetOrder(ctx, o.ID, u)
	if o.Status == "cancelled" {
		if ce != nil || claimed != nil {
			t.Fatal("cancelled task dispatched")
		}
		checkWallet(t, s, u, "2.00000000", "0.00000000")
	} else {
		if o.Status != "dispatching" || !errors.Is(ce, ErrConflict) || claimed == nil {
			t.Fatal("invalid dispatch/cancel outcome")
		}
		checkWallet(t, s, u, "1.00000000", "1.00000000")
	}
}

func TestOrderLateArtifactIsFreeAndOriginalOwnerOnly(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	u := fixtureUser(t, s, "one@example.com", "2")
	i := fixtureIdentity("one", "group")
	fixtureGrant(t, s, u, i)
	o, _ := s.CreateOrder(ctx, i, "event", "cat")
	claim, _ := s.ClaimOrder(ctx, "worker", time.Minute)
	s.db.Exec(`UPDATE qq_image_orders SET deadline=clock_timestamp()-interval '1 second' WHERE id=$1`, o.ID)
	if _, e := s.CompleteGeneration(ctx, o.ID, claim.LeaseToken, Artifact{Data: []byte("image"), MIME: "image/png", Width: 1, Height: 1}); !errors.Is(e, ErrExpired) {
		t.Fatal(e)
	}
	if e := s.RecoverExpired(ctx); e != nil {
		t.Fatal(e)
	}
	o, _ = s.GetOrder(ctx, o.ID, u)
	if o.FundsStatus != "released" || o.WebsiteStatus != "available" {
		t.Fatalf("late result incorrect: %+v", o)
	}
	checkWallet(t, s, u, "2.00000000", "0.00000000")
	if _, e := s.GetArtifact(ctx, o.ID, u); e != nil {
		t.Fatal(e)
	}
	d, e := s.ClaimDelivery(ctx, "worker", time.Minute)
	if e != nil || d != nil {
		t.Fatal("free late artifact auto-published to QQ")
	}
}

func TestOrderDeliveryUnknownAndEventReplay(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	ok, e := s.ClaimEvent(ctx, "app/group/event")
	if e != nil || !ok {
		t.Fatal(e)
	}
	ok, e = s.ClaimEvent(ctx, "app/group/event")
	if e != nil || ok {
		t.Fatal("event was reclaimed")
	}
	u := fixtureUser(t, s, "one@example.com", "2")
	i := fixtureIdentity("one", "group")
	fixtureGrant(t, s, u, i)
	o, _ := s.CreateOrder(ctx, i, "event", "cat")
	claim, _ := s.ClaimOrder(ctx, "worker", time.Minute)
	s.CompleteGeneration(ctx, o.ID, claim.LeaseToken, Artifact{Data: []byte("image"), MIME: "image/png", Width: 1, Height: 1})
	d, e := s.ClaimDelivery(ctx, "sender", time.Minute)
	if e != nil {
		t.Fatal(e)
	}
	s.db.Exec(`UPDATE qq_image_deliveries SET lease_until=clock_timestamp()-interval '1 second' WHERE id=$1`, d.ID)
	if e = s.RecoverExpired(ctx); e != nil {
		t.Fatal(e)
	}
	again, e := s.ClaimDelivery(ctx, "restart", time.Minute)
	if e != nil || again != nil {
		t.Fatal("unknown QQ receipt resent")
	}
	o, _ = s.GetOrder(ctx, o.ID, u)
	if o.DeliveryStatus != "unknown" || o.FundsStatus != "captured" {
		t.Fatal("delivery uncertainty changed funds")
	}
}

func TestOrderEmailChangeWhileWaitingForWalletRejectsAdmission(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	u := fixtureUser(t, s, "one@example.com", "2")
	i := fixtureIdentity("one", "group")
	fixtureGrant(t, s, u, i)
	tx, e := s.db.Begin()
	if e != nil {
		t.Fatal(e)
	}
	defer tx.Rollback()
	if _, e = tx.Exec(`SELECT id FROM users WHERE id=$1 FOR UPDATE`, u); e != nil {
		t.Fatal(e)
	}
	done := make(chan error, 1)
	go func() { _, e := s.CreateOrder(ctx, i, "event", "cat"); done <- e }()
	waitWalletBlocked(t, s)
	if _, e = tx.Exec(`UPDATE users SET email='changed@example.com' WHERE id=$1`, u); e != nil {
		t.Fatal(e)
	}
	tx.Commit()
	if e = <-done; !errors.Is(e, ErrForbidden) {
		t.Fatalf("email proof became stale while waiting but order admitted: %v", e)
	}
	checkWallet(t, s, u, "2.00000000", "0.00000000")
}

func TestConfigWrongEncryptionKeyCannotEnableRetainedSecrets(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	c, _ := s.GetConfig(ctx)
	wrong := NewStore(s.db, []byte(strings.Repeat("x", 32)))
	if _, e := wrong.SaveConfig(ctx, *c, 1); !errors.Is(e, ErrEncryptionUnavailable) {
		t.Fatalf("enabled inaccessible old credentials: %v", e)
	}
}

func waitWalletBlocked(t *testing.T, s *Store) {
	t.Helper()
	until := time.Now().Add(5 * time.Second)
	for time.Now().Before(until) {
		var n int
		if e := s.db.QueryRow(`SELECT count(*) FROM pg_stat_activity WHERE datname=current_database() AND wait_event_type='Lock' AND query LIKE '%users%'`).Scan(&n); e != nil {
			t.Fatal(e)
		}
		if n > 0 {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("wallet operation did not reach locked row")
}

func TestConfigRejectsFullGenerationURL(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	c, _ := s.GetConfig(ctx)
	c.APIURL = "https://images.example.com/v1/images/generations"
	if _, e := s.SaveConfig(ctx, *c, 1); !errors.Is(e, ErrInvalid) {
		t.Fatal("accepted full endpoint as base URL")
	}
}

func TestOrderMediaAboveQQSizeFallsBack(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	u := fixtureUser(t, s, "one@example.com", "2")
	i := fixtureIdentity("one", "group")
	fixtureGrant(t, s, u, i)
	o, _ := s.CreateOrder(ctx, i, "event", "cat")
	claim, _ := s.ClaimOrder(ctx, "worker", time.Minute)
	s.CompleteGeneration(ctx, o.ID, claim.LeaseToken, Artifact{Data: make([]byte, 5*1024*1024+1), MIME: "image/png", Width: 1, Height: 1})
	d, e := s.ClaimDelivery(ctx, "worker", time.Minute)
	if e != nil || d == nil {
		t.Fatal(e)
	}
	if d.ArtifactBytes != 5*1024*1024+1 {
		t.Fatal("artifact bytes not provided")
	}
	if _, e = s.CreateMediaGrant(ctx, d.ID, d.LeaseToken); !errors.Is(e, ErrInvalid) {
		t.Fatal("QQ oversized media grant minted")
	}
	if _, e = s.GetArtifact(ctx, o.ID, u); e != nil {
		t.Fatal("oversized QQ image lost website access")
	}
}

func TestOrderSiteOnlyDeliveryCompletes(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	u := fixtureUser(t, s, "one@example.com", "2")
	i := fixtureIdentity("one", "group")
	fixtureGrant(t, s, u, i)
	o, _ := s.CreateOrder(ctx, i, "event", "cat")
	claim, _ := s.ClaimOrder(ctx, "worker", time.Minute)
	s.CompleteGeneration(ctx, o.ID, claim.LeaseToken, Artifact{Data: []byte("image"), MIME: "image/png", Width: 1, Height: 1})
	d, _ := s.ClaimDelivery(ctx, "worker", time.Minute)
	if e := s.CompleteDelivery(ctx, d.ID, d.LeaseToken, "site_only", "platform_rejected"); e != nil {
		t.Fatal(e)
	}
	o, _ = s.GetOrder(ctx, o.ID, u)
	if o.DeliveryStatus != "site_only" || o.FundsStatus != "captured" {
		t.Fatal("site-only receipt not recorded")
	}
}

func TestOrderEventPayloadConflict(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	ok, e := s.ClaimEventPayload(ctx, "app/group/event", digest("original"))
	if e != nil || !ok {
		t.Fatal(e)
	}
	ok, e = s.ClaimEventPayload(ctx, "app/group/event", digest("original"))
	if e != nil || ok {
		t.Fatal("exact event replayed")
	}
	if _, e = s.ClaimEventPayload(ctx, "app/group/event", digest("other")); !errors.Is(e, ErrConflict) {
		t.Fatal("conflicting event payload reused")
	}
}

func TestConfigExplicitClearRevokesOldVersions(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	old, _ := s.GetConfig(ctx)
	next := *old
	next.ClearAPIKey = true
	cleared, e := s.SaveConfig(ctx, next, 99)
	if e != nil {
		t.Fatal(e)
	}
	if cleared.Enabled || cleared.APIKeyConfigured || cleared.ClearAPIKey {
		t.Fatal("clear did not disable and redact")
	}
	if _, e = s.RuntimeConfig(ctx, old.ID); !errors.Is(e, ErrEncryptionUnavailable) {
		t.Fatal("old revoked API key still usable")
	}
	next = *cleared
	next.APIKey = "replacement"
	next.Enabled = true
	restored, e := s.SaveConfig(ctx, next, 99)
	if e != nil {
		t.Fatal(e)
	}
	r, e := s.RuntimeConfig(ctx, restored.ID)
	if e != nil || r.APIKey != "replacement" {
		t.Fatal("new credentials unavailable")
	}
	next = *restored
	next.ClearBotSecret = true
	latest, e := s.SaveConfig(ctx, next, 99)
	if e != nil {
		t.Fatal(e)
	}
	if latest.Enabled || latest.BotSecretConfigured {
		t.Fatal("bot clear did not disable")
	}
	r, e = s.RuntimeConfig(ctx, restored.ID)
	if e != nil || r.BotSecret != "" || r.APIKey != "replacement" {
		t.Fatal("bot revocation changed provider key or left bot active")
	}
}

func TestOrderBotCredentialRevocationInvalidatesMedia(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	u := fixtureUser(t, s, "one@example.com", "2")
	i := fixtureIdentity("one", "group")
	fixtureGrant(t, s, u, i)
	o, _ := s.CreateOrder(ctx, i, "event", "cat")
	claim, _ := s.ClaimOrder(ctx, "worker", time.Minute)
	s.CompleteGeneration(ctx, o.ID, claim.LeaseToken, Artifact{Data: []byte("image"), MIME: "image/png", Width: 1, Height: 1})
	d, _ := s.ClaimDelivery(ctx, "worker", time.Minute)
	key, _ := s.CreateMediaGrant(ctx, d.ID, d.LeaseToken)
	c, _ := s.GetConfig(ctx)
	c.ClearBotSecret = true
	if _, e := s.SaveConfig(ctx, *c, 99); e != nil {
		t.Fatal(e)
	}
	if _, e := s.ReadMedia(ctx, key); !errors.Is(e, ErrForbidden) {
		t.Fatal("media token survived bot credential revocation")
	}
}

func TestOrderMissingOrWrongEncryptionKeyCannotReserve(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	u := fixtureUser(t, s, "one@example.com", "2")
	i := fixtureIdentity("one", "group")
	fixtureGrant(t, s, u, i)
	for _, key := range [][]byte{nil, []byte(strings.Repeat("x", 32))} {
		without := NewStore(s.db, key)
		if _, e := without.CreateOrder(ctx, i, "event", "cat"); !errors.Is(e, ErrEncryptionUnavailable) {
			t.Fatalf("unavailable credentials allowed reserve: %v", e)
		}
		checkWallet(t, s, u, "2.00000000", "0.00000000")
	}
}

func TestOrderResultArrivingAfterReleaseDoesNotCharge(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	u := fixtureUser(t, s, "one@example.com", "2")
	i := fixtureIdentity("one", "group")
	fixtureGrant(t, s, u, i)
	o, _ := s.CreateOrder(ctx, i, "event", "cat")
	claim, _ := s.ClaimOrder(ctx, "worker", time.Minute)
	s.db.Exec(`UPDATE qq_image_orders SET deadline=clock_timestamp()-interval '1 second' WHERE id=$1`, o.ID)
	if e := s.RecoverExpired(ctx); e != nil {
		t.Fatal(e)
	}
	late, e := s.CompleteGeneration(ctx, o.ID, claim.LeaseToken, Artifact{Data: []byte("image"), MIME: "image/png", Width: 1, Height: 1})
	if e != nil {
		t.Fatal(e)
	}
	if late.FundsStatus != "released" || late.WebsiteStatus != "available" {
		t.Fatal("late result charged or lost")
	}
	checkWallet(t, s, u, "2.00000000", "0.00000000")
}

func TestOrderDispatchDeadlineAfterWalletWait(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	u := fixtureUser(t, s, "one@example.com", "2")
	i := fixtureIdentity("one", "group")
	fixtureGrant(t, s, u, i)
	o, _ := s.CreateOrder(ctx, i, "event", "cat")
	tx, e := s.db.Begin()
	if e != nil {
		t.Fatal(e)
	}
	defer tx.Rollback()
	tx.Exec(`SELECT id FROM users WHERE id=$1 FOR UPDATE`, u)
	s.db.Exec(`UPDATE qq_image_orders SET queue_deadline=clock_timestamp()+interval '150 milliseconds' WHERE id=$1`, o.ID)
	type result struct {
		o *Order
		e error
	}
	done := make(chan result, 1)
	go func() { o, e := s.ClaimOrder(ctx, "worker", time.Minute); done <- result{o, e} }()
	waitWalletBlocked(t, s)
	time.Sleep(200 * time.Millisecond)
	tx.Commit()
	r := <-done
	if r.e != nil || r.o != nil {
		t.Fatalf("expired task dispatched after wallet wait: %+v %v", r.o, r.e)
	}
	checkWallet(t, s, u, "2.00000000", "0.00000000")
}

func TestOrderProcessEventAtomicRollbackAndReplay(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	u := fixtureUser(t, s, "one@example.com", "2")
	i := fixtureIdentity("one", "group")
	fixtureGrant(t, s, u, i)
	boom := errors.New("temporary database failure")
	_, _, _, e := s.ProcessEvent(ctx, "event", digest("payload"), func(inner context.Context) (string, string, error) {
		o, e := s.CreateOrder(inner, i, "event", "cat")
		if e != nil {
			return "", "", e
		}
		if _, e = s.GetOrder(inner, o.ID, u); e != nil {
			return "", "", e
		}
		return "", "", boom
	})
	if !errors.Is(e, boom) {
		t.Fatalf("callback error not propagated: %v", e)
	}
	checkWallet(t, s, u, "2.00000000", "0.00000000")
	calls := 0
	create := func(inner context.Context) (string, string, error) {
		calls++
		o, e := s.CreateOrder(inner, i, "event", "cat")
		if e != nil {
			return "", "", e
		}
		read, e := s.GetOrder(inner, o.ID, u)
		if e != nil {
			return "", "", e
		}
		return read.ID, "manual", nil
	}
	reply, manual, fresh, e := s.ProcessEvent(ctx, "event", digest("payload"), create)
	if e != nil || !fresh || reply == "" || manual != "manual" {
		t.Fatalf("event processing: %s %s %v %v", reply, manual, fresh, e)
	}
	same, _, fresh, e := s.ProcessEvent(ctx, "event", digest("payload"), create)
	if e != nil || fresh || same != reply || calls != 1 {
		t.Fatal("committed event executed twice")
	}
	if _, _, _, e = s.ProcessEvent(ctx, "event", digest("other"), create); !errors.Is(e, ErrConflict) {
		t.Fatal("conflicting event accepted")
	}
	checkWallet(t, s, u, "1.00000000", "1.00000000")
}

func TestOrderEmailChangeAfterDispatchKeepsOriginalCharge(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	u := fixtureUser(t, s, "one@example.com", "2")
	i := fixtureIdentity("one", "group")
	fixtureGrant(t, s, u, i)
	o, _ := s.CreateOrder(ctx, i, "event", "cat")
	claim, _ := s.ClaimOrder(ctx, "worker", time.Minute)
	s.db.Exec(`UPDATE users SET email='changed@example.com' WHERE id=$1`, u)
	if _, e := s.CompleteGeneration(ctx, o.ID, claim.LeaseToken, Artifact{Data: []byte("image"), MIME: "image/png", Width: 1, Height: 1}); e != nil {
		t.Fatal(e)
	}
	checkWallet(t, s, u, "1.00000000", "0.00000000")
	if _, e := s.GetArtifact(ctx, o.ID, u); e != nil {
		t.Fatal(e)
	}
}

func TestConfigImageHostsExactPublicNames(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	for _, host := range []string{"*.cdn.example.com", "https://cdn.example.com", "cdn.example.com:443", "127.0.0.1", "localhost", "foo..example.com", "foo.example.com/path"} {
		c, _ := s.GetConfig(ctx)
		c.ImageHosts = []string{host}
		if _, e := s.SaveConfig(ctx, *c, 1); !errors.Is(e, ErrInvalid) {
			t.Fatalf("invalid image host accepted %q", host)
		}
	}
	c, _ := s.GetConfig(ctx)
	c.ImageHosts = []string{"CDN.Example.com"}
	saved, e := s.SaveConfig(ctx, *c, 1)
	if e != nil || len(saved.ImageHosts) != 1 || saved.ImageHosts[0] != "cdn.example.com" {
		t.Fatalf("host not normalized: %+v %v", saved, e)
	}
}

func TestOrderReconciliationAttentionOnceAndArtifactExpiry(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	u := fixtureUser(t, s, "one@example.com", "2")
	i := fixtureIdentity("one", "group")
	fixtureGrant(t, s, u, i)
	o, _ := s.CreateOrder(ctx, i, "event", "cat")
	claim, _ := s.ClaimOrder(ctx, "worker", time.Minute)
	s.MarkReconciling(ctx, o.ID, claim.LeaseToken, "uncertain")
	s.db.Exec(`UPDATE qq_image_audit SET created_at=clock_timestamp()-interval '31 minutes' WHERE object_id=$1 AND action='order.dispatch'`, o.ID)
	for range 2 {
		if e := s.RecoverExpired(ctx); e != nil {
			t.Fatal(e)
		}
	}
	var n int
	if e := s.db.QueryRow(`SELECT count(*) FROM qq_image_audit WHERE object_id=$1 AND action='order.reconcile_attention'`, o.ID).Scan(&n); e != nil || n != 1 {
		t.Fatalf("missing or duplicate attention record: %d %v", n, e)
	}
	s.CompleteGeneration(ctx, o.ID, claim.LeaseToken, Artifact{Data: []byte("image"), MIME: "image/png", Width: 1, Height: 1})
	s.db.Exec(`UPDATE qq_image_orders SET artifact_expires_at=clock_timestamp()-interval '1 second' WHERE id=$1`, o.ID)
	if e := s.RecoverExpired(ctx); e != nil {
		t.Fatal(e)
	}
	var removed bool
	s.db.QueryRow(`SELECT artifact IS NULL FROM qq_image_orders WHERE id=$1`, o.ID).Scan(&removed)
	if !removed {
		t.Fatal("expired private artifact remains")
	}
	if _, e := s.GetArtifact(ctx, o.ID, u); !errors.Is(e, ErrNotFound) {
		t.Fatal("expired artifact accessible")
	}
}
