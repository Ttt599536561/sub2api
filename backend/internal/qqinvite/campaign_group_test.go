package qqinvite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/url"
	"os"
	"reflect"
	"testing"
	"time"
)

func draftCampaignFixture(t *testing.T, s *Store) Campaign {
	t.Helper()
	c, err := s.SaveCampaign(context.Background(), Campaign{
		Name: "Draft", GroupIDs: []string{"100"}, SubscriptionGroupID: 1,
		StartsAt: time.Now().UTC().Add(-time.Hour), EndsAt: time.Now().UTC().Add(time.Hour),
		WelcomeTemplate: "Welcome {invitee}", PendingTemplate: "Pending {invitee}",
		CongratulationTemplate: "Congratulations {inviter}", PrivateTemplate: "Code {code} {url}",
	}, 1)
	must(t, err)
	_, err = s.db.Exec(`INSERT INTO groups(id,name) VALUES(2,'new monthly') ON CONFLICT DO NOTHING`)
	must(t, err)
	return c
}

func TestDraftCampaignCanChangeSubscriptionGroup(t *testing.T) {
	for _, baseline := range []bool{false, true} {
		t.Run(map[bool]string{false: "without_baseline", true: "with_baseline"}[baseline], func(t *testing.T) {
			s := testStore(t)
			configure(t, s) // A globally enabled integration does not enable this draft.
			ctx := context.Background()
			c := draftCampaignFixture(t, s)
			if baseline {
				must(t, s.SaveBaseline(ctx, c.ID, map[string][]string{"100": {"99999"}}, 1))
			}
			before, err := s.GetCampaign(ctx, c.ID)
			must(t, err)
			var baselineBefore sql.NullTime
			must(t, s.db.QueryRow(`SELECT baseline_at FROM qq_invite_campaigns WHERE id=$1`, c.ID).Scan(&baselineBefore))
			c.SubscriptionGroupID = 2
			c.PlanName = "untrusted client label"
			saved, err := s.SaveCampaign(ctx, c, 1)
			must(t, err)
			if saved.Version != before.Version+1 || saved.SubscriptionGroupID != 2 || saved.PlanName != "new monthly" || saved.BaselineReady != baseline || !saved.CreatedAt.Equal(before.CreatedAt) {
				t.Fatalf("unexpected saved campaign: %+v", saved)
			}
			persisted, err := s.GetCampaign(ctx, c.ID)
			must(t, err)
			var baselineAfter sql.NullTime
			must(t, s.db.QueryRow(`SELECT baseline_at FROM qq_invite_campaigns WHERE id=$1`, c.ID).Scan(&baselineAfter))
			if baselineBefore.Valid != baselineAfter.Valid || !baselineBefore.Time.Equal(baselineAfter.Time) {
				t.Fatalf("baseline timestamp changed: before=%v after=%v", baselineBefore, baselineAfter)
			}
			if !reflect.DeepEqual(persisted, saved) {
				t.Fatalf("persisted=%+v saved=%+v", persisted, saved)
			}
			var payload []byte
			must(t, s.db.QueryRow(`SELECT payload FROM qq_invite_campaign_versions WHERE campaign_id=$1 AND version=$2`, c.ID, saved.Version).Scan(&payload))
			var version Campaign
			must(t, json.Unmarshal(payload, &version))
			// JSON decoding and lib/pq may use different Location objects for the
			// same database timestamp; compare after normalizing the time zone.
			version.CreatedAt = version.CreatedAt.UTC()
			expectedVersion := saved
			expectedVersion.CreatedAt = expectedVersion.CreatedAt.UTC()
			if !reflect.DeepEqual(version, expectedVersion) {
				t.Fatalf("version snapshot=%+v saved=%+v", version, saved)
			}
			// Subsequent edits use the returned version; a stale original remains a conflict.
			saved.Name = "Renamed draft"
			_, err = s.SaveCampaign(ctx, saved, 1)
			must(t, err)
			if _, err = s.SaveCampaign(ctx, c, 1); !errors.Is(err, ErrConflict) {
				t.Fatalf("stale save: %v", err)
			}
		})
	}
}

func TestDraftCampaignGroupChangeInvalidatesChangedQQGroupBaseline(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	c := draftCampaignFixture(t, s)
	must(t, s.SaveBaseline(ctx, c.ID, map[string][]string{"100": {"99999"}}, 1))
	c.SubscriptionGroupID = 2
	c.GroupIDs = []string{"200"}
	saved, err := s.SaveCampaign(ctx, c, 1)
	must(t, err)
	if saved.BaselineReady {
		t.Fatal("changed QQ groups retained baseline")
	}
}

func insertCampaignRecord(t *testing.T, s *Store, c Campaign) int64 {
	t.Helper()
	var id int64
	must(t, s.db.QueryRow(`INSERT INTO qq_invite_records(campaign_id,campaign_version,snapshot,self_id,group_id,invitee_qq,status,event_time) VALUES($1,$2,'{}','12345','100','33333','rejected',now()) RETURNING id`, c.ID, c.Version).Scan(&id))
	return id
}

func TestCampaignSubscriptionGroupLockedByUsage(t *testing.T) {
	for _, reason := range []string{"enable_in_same_save", "currently_enabled", "enabled_then_paused", "stock_available", "stock_expired", "stock_used", "stock_assigned", "record_rejected", "join_only"} {
		t.Run(reason, func(t *testing.T) {
			s := testStore(t)
			ctx := context.Background()
			c := draftCampaignFixture(t, s)
			var err error
			switch reason {
			case "enable_in_same_save":
				must(t, s.SaveBaseline(ctx, c.ID, map[string][]string{"100": {}}, 1))
				c.Enabled = true
			case "currently_enabled", "enabled_then_paused":
				must(t, s.SaveBaseline(ctx, c.ID, map[string][]string{"100": {}}, 1))
				c.Enabled = true
				c, err = s.SaveCampaign(ctx, c, 1)
				must(t, err)
				c.Enabled = false
				if reason == "enabled_then_paused" {
					c, err = s.SaveCampaign(ctx, c, 1)
					must(t, err)
				}
			case "record_rejected":
				insertCampaignRecord(t, s, c)
			case "join_only":
				_, err = s.db.Exec(`INSERT INTO qq_invite_joins(campaign_id,group_id,qq) VALUES($1,'100','33333')`, c.ID)
				must(t, err)
			default:
				var codeID int64
				must(t, s.db.QueryRow(`INSERT INTO redeem_codes(code,type,status,group_id,expires_at,used_by) VALUES('old-code','subscription',CASE WHEN $1='stock_used' THEN 'used' ELSE 'unused' END,1,CASE WHEN $1='stock_expired' THEN now()-interval '1 day' END,CASE WHEN $1='stock_used' THEN 42 END) RETURNING id`, reason).Scan(&codeID))
				var recordID *int64
				if reason == "stock_assigned" {
					id := insertCampaignRecord(t, s, c)
					recordID = &id
				}
				_, err = s.db.Exec(`INSERT INTO qq_invite_code_pool(campaign_id,redeem_code_id,record_id) VALUES($1,$2,$3)`, c.ID, codeID, recordID)
				must(t, err)
			}
			before, err := s.GetCampaign(ctx, c.ID)
			must(t, err)
			c.SubscriptionGroupID = 2
			_, err = s.SaveCampaign(ctx, c, 1)
			if !errors.Is(err, ErrCampaignGroupLocked) {
				t.Fatalf("expected dedicated group lock error, got %v", err)
			}
			after, err := s.GetCampaign(ctx, c.ID)
			must(t, err)
			if !reflect.DeepEqual(before, after) {
				t.Fatalf("rejected save changed campaign: before=%+v after=%+v", before, after)
			}
			c.Version--
			if _, err = s.SaveCampaign(ctx, c, 1); !errors.Is(err, ErrConflict) {
				t.Fatalf("stale version must take precedence over group lock: %v", err)
			}
			// Existing usage must not freeze ordinary edits that keep the same group.
			before.Name = "Allowed name edit"
			_, err = s.SaveCampaign(ctx, before, 1)
			must(t, err)
		})
	}
}

func TestDraftCampaignChangeIgnoresOtherCampaignUsage(t *testing.T) {
	s := testStore(t)
	other := campaignFixture(t, s)
	_, err := s.GenerateCodes(context.Background(), other.ID, 1, 1)
	must(t, err)
	insertCampaignRecord(t, s, other)
	_, err = s.db.Exec(`INSERT INTO qq_invite_joins(campaign_id,group_id,qq) VALUES($1,'100','33333')`, other.ID)
	must(t, err)
	c := draftCampaignFixture(t, s)
	c.SubscriptionGroupID = 2
	_, err = s.SaveCampaign(context.Background(), c, 1)
	must(t, err)
}

// Wait for a real database lock waiter, rather than assuming a goroutine has
// reached a particular statement after an arbitrary delay.
func waitCampaignLockWaiter(t *testing.T, ctx context.Context, db *sql.DB, blocker1, blocker2, exclude int) int {
	t.Helper()
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		var pid int
		err := db.QueryRowContext(ctx, `SELECT pid FROM pg_stat_activity WHERE wait_event_type='Lock' AND pid<>$3 AND ($1=ANY(pg_blocking_pids(pid)) OR $2=ANY(pg_blocking_pids(pid))) ORDER BY pid LIMIT 1`, blocker1, blocker2, exclude).Scan(&pid)
		if err == nil {
			return pid
		}
		if !errors.Is(err, sql.ErrNoRows) {
			t.Fatal(err)
		}
		select {
		case <-ctx.Done():
			t.Fatal("timed out waiting for campaign lock waiter", ctx.Err())
		case <-ticker.C:
		}
	}
}

func TestCampaignGroupChangeConcurrentImport(t *testing.T) {
	for _, isolation := range []string{"read committed", "repeatable read"} {
		t.Run(isolation, func(t *testing.T) {
			if os.Getenv("QQ_INVITE_TEST_DATABASE_URL") == "" {
				t.Skip("QQ_INVITE_TEST_DATABASE_URL required")
			}
			// Every newly opened connection gets the selected server default. Writes
			// must still use beginWrite's explicit Read Committed isolation.
			t.Setenv("QQ_INVITE_TEST_DATABASE_URL", os.Getenv("QQ_INVITE_TEST_DATABASE_URL")+"&default_transaction_isolation="+url.QueryEscape(isolation))
			s := testStore(t)
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			c := draftCampaignFixture(t, s)
			_, err := s.db.ExecContext(ctx, `INSERT INTO redeem_codes(code,type,group_id) VALUES('old-code','subscription',1)`)
			must(t, err)
			preview, err := s.PreviewCodes(ctx, c.ID, []string{"old-code"})
			must(t, err)
			if preview.ValidCount != 1 {
				t.Fatal("initial preview rejected old-group code")
			}
			campaignBarrier, err := s.db.BeginTx(ctx, nil)
			must(t, err)
			defer func() { _ = campaignBarrier.Rollback() }()
			_, err = campaignBarrier.ExecContext(ctx, `SELECT id FROM qq_invite_campaigns WHERE id=$1 FOR UPDATE`, c.ID)
			must(t, err)
			var campaignPID int
			must(t, campaignBarrier.QueryRowContext(ctx, `SELECT pg_backend_pid()`).Scan(&campaignPID))
			codeBarrier, err := s.db.BeginTx(ctx, nil)
			must(t, err)
			defer func() { _ = codeBarrier.Rollback() }()
			_, err = codeBarrier.ExecContext(ctx, `SELECT id FROM redeem_codes WHERE code='old-code' FOR UPDATE`)
			must(t, err)
			var codePID int
			must(t, codeBarrier.QueryRowContext(ctx, `SELECT pg_backend_pid()`).Scan(&codePID))
			c.SubscriptionGroupID = 2
			saved := make(chan error, 1)
			go func() { _, err := s.SaveCampaign(ctx, c, 1); saved <- err }()
			savePID := waitCampaignLockWaiter(t, ctx, s.db, campaignPID, campaignPID, 0)
			imported := make(chan error, 1)
			go func() { _, err := s.ImportCodes(ctx, c.ID, []string{"old-code"}, 1); imported <- err }()
			waitCampaignLockWaiter(t, ctx, s.db, savePID, codePID, savePID)
			must(t, campaignBarrier.Commit())
			saveErr := <-saved
			must(t, codeBarrier.Commit())
			importErr := <-imported
			must(t, saveErr)
			if !errors.Is(importErr, ErrInvalid) {
				t.Fatalf("stale preview/import must revalidate against the committed group: %v", importErr)
			}
			var stock int
			must(t, s.db.QueryRowContext(ctx, `SELECT count(*) FROM qq_invite_code_pool WHERE campaign_id=$1`, c.ID).Scan(&stock))
			if stock != 0 {
				t.Fatalf("old-group stock imported after group change: %d", stock)
			}
		})
	}
}

func TestCampaignGroupChangeWaitsForImport(t *testing.T) {
	for _, isolation := range []string{"read committed", "repeatable read"} {
		t.Run(isolation, func(t *testing.T) {
			if os.Getenv("QQ_INVITE_TEST_DATABASE_URL") == "" {
				t.Skip("QQ_INVITE_TEST_DATABASE_URL required")
			}
			t.Setenv("QQ_INVITE_TEST_DATABASE_URL", os.Getenv("QQ_INVITE_TEST_DATABASE_URL")+"&default_transaction_isolation="+url.QueryEscape(isolation))
			s := testStore(t)
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			c := draftCampaignFixture(t, s)
			_, err := s.db.ExecContext(ctx, `INSERT INTO redeem_codes(code,type,group_id) VALUES('old-code','subscription',1)`)
			must(t, err)
			codeBarrier, err := s.db.BeginTx(ctx, nil)
			must(t, err)
			defer func() { _ = codeBarrier.Rollback() }()
			_, err = codeBarrier.ExecContext(ctx, `SELECT id FROM redeem_codes WHERE code='old-code' FOR UPDATE`)
			must(t, err)
			var codePID int
			must(t, codeBarrier.QueryRowContext(ctx, `SELECT pg_backend_pid()`).Scan(&codePID))
			imported := make(chan error, 1)
			go func() { _, err := s.ImportCodes(ctx, c.ID, []string{"old-code"}, 1); imported <- err }()
			importPID := waitCampaignLockWaiter(t, ctx, s.db, codePID, codePID, 0)
			c.SubscriptionGroupID = 2
			saved := make(chan error, 1)
			go func() { _, err := s.SaveCampaign(ctx, c, 1); saved <- err }()
			waitCampaignLockWaiter(t, ctx, s.db, importPID, importPID, 0)
			must(t, codeBarrier.Commit())
			must(t, <-imported)
			if err = <-saved; !errors.Is(err, ErrCampaignGroupLocked) {
				t.Fatalf("group change must observe imported stock after waiting: %v", err)
			}
			current, err := s.GetCampaign(ctx, c.ID)
			must(t, err)
			if current.SubscriptionGroupID != 1 || current.Version != c.Version {
				t.Fatalf("rejected group change altered campaign: %+v", current)
			}
			var stock, mismatched int
			must(t, s.db.QueryRowContext(ctx, `SELECT count(*),count(*) FILTER(WHERE r.group_id<>$2) FROM qq_invite_code_pool p JOIN redeem_codes r ON r.id=p.redeem_code_id WHERE p.campaign_id=$1`, c.ID, current.SubscriptionGroupID).Scan(&stock, &mismatched))
			if stock != 1 || mismatched != 0 {
				t.Fatalf("stock=%d mismatched=%d", stock, mismatched)
			}
		})
	}
}
