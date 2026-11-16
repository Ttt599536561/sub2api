package qqinvite

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"strings"
	"testing"
	"time"
)

func guardTestStore(t *testing.T) *Store {
	t.Helper()
	s := testStore(t)
	_, err := s.db.Exec(`ALTER TABLE user_subscriptions ADD COLUMN deleted_at TIMESTAMPTZ;
		CREATE TABLE user_welfare_subscription_supplements(id BIGSERIAL PRIMARY KEY,group_id BIGINT NOT NULL);
		CREATE TABLE user_welfare_subscription_adjustments(id BIGSERIAL PRIMARY KEY,subscription_id BIGINT NOT NULL);`)
	must(t, err)
	b, err := os.ReadFile("../../migrations/260_welfare_group_lifecycle_guards.sql")
	must(t, err)
	_, err = s.db.Exec(string(b))
	must(t, err)
	return s
}

func TestManualWelfareSupplementCannotEnterQQPool(t *testing.T) {
	for _, effect := range []string{"add_quota", "extend_duration"} {
		t.Run(effect, func(t *testing.T) {
			s := testStore(t)
			c := draftCampaignFixture(t, s)
			_, err := s.db.Exec(`INSERT INTO redeem_codes(code,type,group_id,validity_days,subscription_effect,welfare_campaign_id,welfare_campaign_version,quota_amount_usd) VALUES('supplement','subscription',1,30,$1,$2,$3,10)`, effect, c.ID, c.Version)
			must(t, err)
			preview, err := s.PreviewCodes(context.Background(), c.ID, []string{"supplement"})
			must(t, err)
			if preview.InvalidCount != 1 || preview.Items[0].Reason != "welfare_supplement_not_allowed" {
				t.Fatalf("supplement preview=%+v", preview)
			}
			_, err = s.ImportCodes(context.Background(), c.ID, []string{"supplement"}, 1)
			if !errors.Is(err, ErrInvalid) {
				t.Fatalf("supplement import=%v", err)
			}
			var count int
			must(t, s.db.QueryRow(`SELECT count(*) FROM qq_invite_code_pool`).Scan(&count))
			if count != 0 {
				t.Fatalf("supplement entered QQ pool: %d", count)
			}
		})
	}
}

func TestManualWelfareSupplementLocksDraftCampaignGroup(t *testing.T) {
	s := testStore(t)
	c := draftCampaignFixture(t, s)
	_, err := s.db.Exec(`INSERT INTO redeem_codes(code,type,group_id,subscription_effect,welfare_campaign_id,welfare_campaign_version,quota_amount_usd) VALUES('supplement','subscription',1,'add_quota',$1,$2,10)`, c.ID, c.Version)
	must(t, err)
	c.SubscriptionGroupID = 2
	_, err = s.SaveCampaign(context.Background(), c, 1)
	if !errors.Is(err, ErrCampaignGroupLocked) {
		t.Fatalf("expected campaign group lock, got %v", err)
	}
}

func TestRejectedSupplementImportDoesNotDeadlockRedemption(t *testing.T) {
	s := guardTestStore(t)
	c := draftCampaignFixture(t, s)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_, err := s.db.ExecContext(ctx, `INSERT INTO redeem_codes(code,type,group_id,subscription_effect,welfare_campaign_id,welfare_campaign_version) VALUES('supplement','subscription',1,'extend_duration',$1,$2)`, c.ID, c.Version)
	must(t, err)
	redemption, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	must(t, err)
	defer func() { _ = redemption.Rollback() }()
	_, err = redemption.ExecContext(ctx, `SELECT id FROM redeem_codes WHERE code='supplement' FOR UPDATE`)
	must(t, err)
	var pid int
	must(t, redemption.QueryRowContext(ctx, `SELECT pg_backend_pid()`).Scan(&pid))
	imported := make(chan error, 1)
	go func() { _, err := s.ImportCodes(ctx, c.ID, []string{"supplement"}, 1); imported <- err }()
	waitCampaignLockWaiter(t, ctx, s.db, pid, pid, 0)
	// A redeeming code holder must still be able to obtain campaign then group.
	_, err = redemption.ExecContext(ctx, `SELECT id FROM qq_invite_campaigns WHERE id=$1 FOR SHARE`, c.ID)
	must(t, err)
	_, err = redemption.ExecContext(ctx, `SELECT id FROM groups WHERE id=1 FOR SHARE`)
	must(t, err)
	must(t, redemption.Commit())
	if err = <-imported; !errors.Is(err, ErrInvalid) {
		t.Fatalf("supplement import: %v", err)
	}
}

func TestWelfareGroupLifecycleRejectsHistoricalReferences(t *testing.T) {
	for _, kind := range []string{"campaign", "campaign_version", "supplement", "welfare", "deleted_welfare", "receipt"} {
		t.Run(kind, func(t *testing.T) {
			s := guardTestStore(t)
			var err error
			switch kind {
			case "campaign":
				draftCampaignFixture(t, s)
			case "campaign_version":
				c := draftCampaignFixture(t, s)
				c.SubscriptionGroupID = 2
				_, err = s.SaveCampaign(context.Background(), c, 1)
			case "supplement":
				_, err = s.db.Exec(`INSERT INTO redeem_codes(code,type,group_id,subscription_effect) VALUES('supplement','subscription',1,'add_quota')`)
			case "welfare", "deleted_welfare":
				_, err = s.db.Exec(`INSERT INTO user_subscriptions(user_id,group_id,entitlement_kind,deleted_at) VALUES(1,1,'welfare',CASE WHEN $1='deleted_welfare' THEN now() END)`, kind)
			case "receipt":
				_, err = s.db.Exec(`INSERT INTO user_welfare_subscription_supplements(group_id) VALUES(1)`)
			}
			must(t, err)
			for _, query := range []string{`UPDATE groups SET deleted_at=now() WHERE id=1`, `UPDATE groups SET subscription_type='standard' WHERE id=1`, `DELETE FROM groups WHERE id=1`} {
				_, err = s.db.Exec(query)
				if err == nil || !strings.Contains(err.Error(), "welfare_group_referenced") {
					t.Fatalf("%s: expected referenced error, got %v", query, err)
				}
			}
			_, err = s.db.Exec(`UPDATE groups SET name='renamed',monthly_limit_usd=123 WHERE id=1`)
			must(t, err)
		})
	}
}

func TestWelfareGroupLifecycleAllowsUnreferencedGroup(t *testing.T) {
	s := guardTestStore(t)
	_, err := s.db.Exec(`UPDATE groups SET subscription_type='standard' WHERE id=1; DELETE FROM groups WHERE id=1`)
	must(t, err)
}

func TestWelfareGroupLifecycleRejectsStaleIsolationButAllowsRename(t *testing.T) {
	s := guardTestStore(t)
	ctx := context.Background()
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelRepeatableRead})
	must(t, err)
	defer func() { _ = tx.Rollback() }()
	var id int64
	must(t, tx.QueryRowContext(ctx, `SELECT id FROM groups WHERE id=1`).Scan(&id))
	draftCampaignFixture(t, s)
	_, err = tx.ExecContext(ctx, `UPDATE groups SET name='rename at repeatable read' WHERE id=1`)
	must(t, err)
	_, err = tx.ExecContext(ctx, `UPDATE groups SET deleted_at=now() WHERE id=1`)
	if err == nil || !strings.Contains(err.Error(), "welfare_group_write_requires_read_committed") {
		t.Fatalf("stale destructive transaction: %v", err)
	}
}

func TestQQPoolDatabaseGuardRejectsManualSupplement(t *testing.T) {
	s := guardTestStore(t)
	c := draftCampaignFixture(t, s)
	var codeID int64
	must(t, s.db.QueryRow(`INSERT INTO redeem_codes(code,type,group_id,subscription_effect) VALUES('supplement','subscription',1,'extend_duration') RETURNING id`).Scan(&codeID))
	_, err := s.db.Exec(`INSERT INTO qq_invite_code_pool(campaign_id,redeem_code_id) VALUES($1,$2)`, c.ID, codeID)
	if err == nil || !strings.Contains(err.Error(), "qq_invite_welfare_supplement_not_allowed") {
		t.Fatalf("direct pool insert: %v", err)
	}
	_, err = s.db.Exec(`INSERT INTO redeem_codes(code,type,group_id) VALUES('legacy','subscription',1)`)
	must(t, err)
	_, err = s.ImportCodes(context.Background(), c.ID, []string{"legacy"}, 1)
	must(t, err)
	_, err = s.db.Exec(`UPDATE redeem_codes SET subscription_effect='extend_duration' WHERE code='legacy'`)
	if err == nil || !strings.Contains(err.Error(), "qq_invite_welfare_supplement_not_allowed") {
		t.Fatalf("pool metadata mutation: %v", err)
	}
}

func TestWelfareGroupLifecycleConcurrentBinding(t *testing.T) {
	for _, first := range []string{"binding", "deletion"} {
		t.Run(first, func(t *testing.T) {
			s := guardTestStore(t)
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
			must(t, err)
			defer func() { _ = tx.Rollback() }()
			var pid int
			must(t, tx.QueryRowContext(ctx, `SELECT pg_backend_pid()`).Scan(&pid))
			bindSQL := `INSERT INTO qq_invite_campaigns(payload) VALUES('{"subscription_group_id":1}')`
			deleteSQL := `UPDATE groups SET deleted_at=now() WHERE id=1`
			firstSQL, secondSQL := bindSQL, deleteSQL
			if first == "deletion" {
				firstSQL, secondSQL = deleteSQL, bindSQL
			}
			_, err = tx.ExecContext(ctx, firstSQL)
			must(t, err)
			done := make(chan error, 1)
			go func() { _, err := s.db.ExecContext(ctx, secondSQL); done <- err }()
			waitCampaignLockWaiter(t, ctx, s.db, pid, pid, 0)
			must(t, tx.Commit())
			err = <-done
			if err == nil {
				t.Fatal("concurrent mutation accepted an invalid reference")
			}
			if first == "binding" && !strings.Contains(err.Error(), "welfare_group_referenced") {
				t.Fatal(err)
			}
			if first == "deletion" && !strings.Contains(err.Error(), "welfare_group_unavailable") {
				t.Fatal(err)
			}
		})
	}
}
