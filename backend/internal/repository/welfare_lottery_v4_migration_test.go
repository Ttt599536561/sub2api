package repository

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/Wei-Shaw/sub2api/migrations"
	"github.com/lib/pq"
	"github.com/stretchr/testify/require"
)

const welfareLotteryV4Migration = "252_welfare_lottery_rules_v4.sql"

func TestWelfareLotteryV4MigrationFile(t *testing.T) {
	_, err := migrations.FS.ReadFile(welfareLotteryV4Migration)
	require.NoError(t, err)
}

func welfareLotteryV4Database(t *testing.T, released bool) (context.Context, *sql.DB) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	t.Cleanup(cancel)
	db := upstreamAC1B8F1TestDatabase(t, ctx)
	if released {
		require.NoError(t, applyMigrationsFS(ctx, db, welfareCheckinV3Target(t)))
		welfareCheckinV3AssertHistory(t, ctx, db)
		welfareCheckinV3AssertSchema(t, ctx, db)
	}
	return ctx, db
}

func welfareLotteryV4ExpectedPrograms(t *testing.T, before string) string {
	t.Helper()
	var programs []map[string]any
	require.NoError(t, json.Unmarshal([]byte(before), &programs))
	for _, program := range programs {
		require.Equal(t, "v3", program["rules_version"])
		program["rules_version"] = "v4"
	}
	after, err := json.Marshal(programs)
	require.NoError(t, err)
	return string(after)
}

func welfareLotteryV4Snapshot(t *testing.T, ctx context.Context, db *sql.DB) map[string]string {
	t.Helper()
	result := map[string]string{}
	for _, table := range []string{"users", "payment_orders", "welfare_programs", "welfare_wallets", "welfare_operations",
		"welfare_checkins", "welfare_ledger", "welfare_spend_events", "welfare_balance_outbox", "welfare_subscription_rewards"} {
		query := "SELECT COALESCE(jsonb_agg(to_jsonb(t) ORDER BY to_jsonb(t)::text),'[]'::jsonb)::text FROM " + pq.QuoteIdentifier(table) + " t"
		result[table] = upstreamAC1B8F1Snapshot(t, ctx, db, query)
	}
	return result
}

func welfareLotteryV4AssertPreserved(t *testing.T, ctx context.Context, db *sql.DB, expected map[string]string) {
	t.Helper()
	for table, after := range welfareLotteryV4Snapshot(t, ctx, db) {
		require.JSONEq(t, expected[table], after, table)
	}
}

func welfareLotteryV4AssertSchema(t *testing.T, ctx context.Context, db *sql.DB) {
	t.Helper()
	var version, defaultValue, definition string
	require.NoError(t, db.QueryRowContext(ctx, `SELECT rules_version FROM welfare_programs WHERE id=1`).Scan(&version))
	require.Equal(t, "v4", version)
	require.NoError(t, db.QueryRowContext(ctx, `SELECT column_default FROM information_schema.columns
		WHERE table_schema='public' AND table_name='welfare_programs' AND column_name='rules_version'`).Scan(&defaultValue))
	require.Equal(t, "'v4'::text", defaultValue)
	require.NoError(t, db.QueryRowContext(ctx, `SELECT pg_get_constraintdef(oid) FROM pg_constraint
		WHERE conrelid='welfare_programs'::regclass AND conname='welfare_programs_rules_version_check'`).Scan(&definition))
	require.Contains(t, definition, "rules_version = 'v4'::text")
	for _, rejected := range []string{"v2", "v3", "v5", "unknown"} {
		_, err := db.ExecContext(ctx, `UPDATE welfare_programs SET rules_version=$1 WHERE id=1`, rejected)
		var pgErr *pq.Error
		require.ErrorAs(t, err, &pgErr)
		require.Equal(t, pq.ErrorCode("23514"), pgErr.Code)
		require.Equal(t, "welfare_programs_rules_version_check", pgErr.Constraint)
	}
	_, err := db.ExecContext(ctx, `UPDATE welfare_programs SET rules_version=DEFAULT WHERE id=1`)
	require.NoError(t, err)
}

func welfareLotteryV4AssertHistory(t *testing.T, ctx context.Context, db *sql.DB, before map[string]upstreamAC1B8F1HistoryEntry) map[string]upstreamAC1B8F1HistoryEntry {
	t.Helper()
	history := upstreamAC1B8F1History(t, ctx, db)
	require.Len(t, history, 308)
	require.Contains(t, history, welfareLotteryV4Migration)
	contents, err := migrations.FS.ReadFile(welfareLotteryV4Migration)
	require.NoError(t, err)
	require.Equal(t, fmt.Sprintf("%x", sha256.Sum256([]byte(strings.TrimSpace(string(contents))))), history[welfareLotteryV4Migration].Checksum)
	for name, entry := range before {
		require.Equal(t, entry, history[name], "old checksum and application time: %s", name)
	}
	return history
}

const welfareLotteryV4OldDrawID = "a5d39858-7954-4bc5-93c1-cce78f968abc"
const welfareLotteryV4OldDraw = `{"operation_id":"a5d39858-7954-4bc5-93c1-cce78f968abc","status":"completed","reward_amount":"50.00","prize":{"id":"5000","name":"$50.00","amount":"50.00","probability":"0.1%"},"overview":{"rules_version":3,"welfare_balance":"271.25","draws_used":20,"wallet_version":77,"welfare_balance_version":5}}`

func welfareLotteryV4Seed(t *testing.T, ctx context.Context, db *sql.DB, enabled bool, launch any) int64 {
	t.Helper()
	var userID, orderID int64
	require.NoError(t, db.QueryRowContext(ctx, `INSERT INTO users(email,password_hash,balance,frozen_balance,status)
		VALUES('welfare-v4-upgrade@example.com','preserved-hash',123.45678901,4.25000000,'active') RETURNING id`).Scan(&userID))
	require.NoError(t, db.QueryRowContext(ctx, `INSERT INTO payment_orders
		(user_id,amount,pay_amount,order_type,status,paid_at,completed_at,expires_at,out_trade_no,provider_snapshot)
		VALUES($1,7,200,'subscription','COMPLETED','2026-10-01','2026-10-01','2026-10-02','welfare-v4-preserved-order','{"currency":"CNY"}') RETURNING id`, userID).Scan(&orderID))
	statements := []struct {
		query string
		args  []any
	}{
		{`UPDATE welfare_programs SET enabled=$1,launch_at=$2,created_at='2026-08-01T01:02:03.123456Z',updated_at='2026-10-01T07:08:09.123456Z' WHERE id=1`, []any{enabled, launch}},
		{`INSERT INTO welfare_wallets(user_id,balance_cents,total_earned_cents,total_redeemed_cents,total_checkin_days,
			cycle_id,cycle_day,last_checkin_date,daily_low_count,eligible_spend,draws_used,subscription_draws,wallet_version,welfare_balance_version)
			VALUES($1,27125,32125,5000,45,2,15,'2026-10-26',3,175.12345678,20,3,77,5)`, []any{userID}},
		{`INSERT INTO welfare_operations(id,user_id,type,idempotency_key,request_fingerprint,result,private_audit)
			VALUES($1,$2,'draw','old-high-prize','draw:v2',$3,'{"rules_version":"v2","amount_cents":5000,"uniform_roll":999}')`, []any{welfareLotteryV4OldDrawID, userID, welfareLotteryV4OldDraw}},
		{`INSERT INTO welfare_operations(id,user_id,type,idempotency_key,request_fingerprint,result,private_audit)
			VALUES('ee8729dd-62a0-4d4d-b795-5b07ded56607',$1,'checkin','2026-10-26','checkin:2026-10-26',
			'{"reward_amount":"0.10","base_reward_amount":"0.02","streak_reward_amount":"0.08","overview":{"rules_version":3}}','{"rules_version":"v3","low_after":3}')`, []any{userID}},
		{`INSERT INTO welfare_checkins(user_id,business_date,operation_id,daily_cents,streak_cents,cycle_id,cycle_day)
			VALUES($1,'2026-10-26','ee8729dd-62a0-4d4d-b795-5b07ded56607',2,8,2,15)`, []any{userID}},
		{`INSERT INTO welfare_ledger(user_id,operation_id,type,amount_cents,balance_after_cents,business_date)
			VALUES($1,$2,'draw',5000,27125,'2026-10-26'),($1,'ee8729dd-62a0-4d4d-b795-5b07ded56607','daily',2,22117,'2026-10-26'),
			($1,'ee8729dd-62a0-4d4d-b795-5b07ded56607','streak',8,22125,'2026-10-26')`, []any{userID, welfareLotteryV4OldDrawID}},
		{`INSERT INTO welfare_spend_events(user_id,source_type,source_id,action,amount,ticket_delta)
			VALUES($1,'usage','preserved-v4-usage','debit',175.12345678,3)`, []any{userID}},
		{`INSERT INTO welfare_balance_outbox(user_id,version,attempts,last_error) VALUES($1,5,1,'preserved retry')`, []any{userID}},
		{`INSERT INTO welfare_subscription_rewards(order_id,user_id,order_amount,paid_amount_cny,paid_at,granted_draws,
			reversed_draws,refunded_amount_cny,refund_amount,refund_status,refund_reason,refund_processed_at)
			VALUES($1,$2,7,200,'2026-10-01',4,1,50,1.75,'PARTIALLY_REFUNDED','preserved refund','2026-10-02')`, []any{orderID, userID}},
	}
	for _, statement := range statements {
		_, err := db.ExecContext(ctx, statement.query, statement.args...)
		require.NoError(t, err)
	}
	return userID
}

func TestWelfareLotteryV4MigrationUpgradePreservesState(t *testing.T) {
	for _, tc := range []struct {
		name    string
		enabled bool
		launch  any
	}{
		{"never enabled", false, nil},
		{"paused after launch", false, "2026-09-01T04:05:06.123456Z"},
		{"enabled", true, "2026-09-01T04:05:06.123456Z"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, db := welfareLotteryV4Database(t, true)
			userID := welfareLotteryV4Seed(t, ctx, db, tc.enabled, tc.launch)
			beforeHistory := upstreamAC1B8F1History(t, ctx, db)
			expected := welfareLotteryV4Snapshot(t, ctx, db)
			expected["welfare_programs"] = welfareLotteryV4ExpectedPrograms(t, expected["welfare_programs"])
			require.NoError(t, ApplyMigrations(ctx, db))
			welfareLotteryV4AssertSchema(t, ctx, db)
			history := welfareLotteryV4AssertHistory(t, ctx, db, beforeHistory)
			welfareLotteryV4AssertPreserved(t, ctx, db, expected)

			repo := NewWelfareRepository(db)
			state, err := repo.State(ctx, userID)
			require.NoError(t, err)
			require.Equal(t, service.WelfareRulesVersion, state.Program.RulesVersion)
			settings, err := repo.GetSettings(ctx)
			require.NoError(t, err)
			require.Equal(t, service.WelfareRulesVersion, settings.RulesVersion)
			svc := service.NewWelfareService(repo, service.WithWelfareRandom(func(int64) (int64, error) {
				t.Error("historical draw must not sample the v4 pool")
				return 0, fmt.Errorf("unexpected random sample")
			}))
			var saved service.WelfareOperation
			require.NoError(t, json.Unmarshal([]byte(welfareLotteryV4OldDraw), &saved))
			for range 2 {
				replayed, replayErr := svc.Draw(ctx, userID, "old-high-prize")
				require.NoError(t, replayErr)
				require.Equal(t, saved, *replayed)
			}
			stored, err := svc.Operation(ctx, userID, welfareLotteryV4OldDrawID)
			require.NoError(t, err)
			require.Equal(t, saved, *stored)
			require.NoError(t, ApplyMigrations(ctx, db))
			require.Equal(t, history, upstreamAC1B8F1History(t, ctx, db))
			// Exercise direct SQL re-entry as well as the startup history guard.
			contents, err := migrations.FS.ReadFile(welfareLotteryV4Migration)
			require.NoError(t, err)
			for range 2 {
				tx, beginErr := db.BeginTx(ctx, nil)
				require.NoError(t, beginErr)
				_, execErr := tx.ExecContext(ctx, string(contents))
				if execErr != nil {
					require.NoError(t, tx.Rollback())
					t.Fatal(execErr)
				}
				require.NoError(t, tx.Commit())
			}
			welfareLotteryV4AssertPreserved(t, ctx, db, expected)
			require.Equal(t, history, upstreamAC1B8F1History(t, ctx, db))
		})
	}
}

func TestWelfareLotteryV4MigrationFreshInstall(t *testing.T) {
	ctx, db := welfareLotteryV4Database(t, false)
	require.NoError(t, ApplyMigrations(ctx, db))
	welfareLotteryV4AssertSchema(t, ctx, db)
	history := welfareLotteryV4AssertHistory(t, ctx, db, nil)
	var enabled bool
	var launch sql.NullTime
	require.NoError(t, db.QueryRowContext(ctx, `SELECT enabled,launch_at FROM welfare_programs WHERE id=1`).Scan(&enabled, &launch))
	require.False(t, enabled)
	require.False(t, launch.Valid)
	snapshot := welfareLotteryV4Snapshot(t, ctx, db)
	for table, rows := range snapshot {
		if table != "welfare_programs" {
			require.JSONEq(t, "[]", rows, table)
		}
	}
	require.NoError(t, ApplyMigrations(ctx, db))
	require.Equal(t, history, upstreamAC1B8F1History(t, ctx, db))
	welfareLotteryV4AssertPreserved(t, ctx, db, snapshot)
}

func TestWelfareLotteryV4MigrationConcurrentPendingUpgrade(t *testing.T) {
	ctx, db := welfareLotteryV4Database(t, true)
	welfareLotteryV4Seed(t, ctx, db, true, "2026-09-01T04:05:06.123456Z")
	beforeHistory := upstreamAC1B8F1History(t, ctx, db)
	expected := welfareLotteryV4Snapshot(t, ctx, db)
	expected["welfare_programs"] = welfareLotteryV4ExpectedPrograms(t, expected["welfare_programs"])
	ready, start, results := make(chan struct{}, 2), make(chan struct{}), make(chan error, 2)
	for range 2 {
		go func() {
			ready <- struct{}{}
			<-start
			results <- ApplyMigrations(ctx, db)
		}()
	}
	for range 2 {
		<-ready
	}
	close(start)
	for range 2 {
		require.NoError(t, <-results)
	}
	welfareLotteryV4AssertSchema(t, ctx, db)
	welfareLotteryV4AssertHistory(t, ctx, db, beforeHistory)
	welfareLotteryV4AssertPreserved(t, ctx, db, expected)
	var recorded int
	require.NoError(t, db.QueryRowContext(ctx, `SELECT count(*) FROM schema_migrations WHERE filename=$1`, welfareLotteryV4Migration).Scan(&recorded))
	require.Equal(t, 1, recorded)
}

func TestWelfareLotteryV4MigrationRecordFailureRollsBackAndRetries(t *testing.T) {
	ctx, db := welfareLotteryV4Database(t, true)
	welfareLotteryV4Seed(t, ctx, db, true, "2026-09-01T04:05:06.123456Z")
	beforeHistory := upstreamAC1B8F1History(t, ctx, db)
	before := welfareLotteryV4Snapshot(t, ctx, db)
	_, err := db.ExecContext(ctx, `CREATE FUNCTION reject_welfare_v4_migration_record() RETURNS trigger
		LANGUAGE plpgsql AS $$ BEGIN
			IF NEW.filename = '252_welfare_lottery_rules_v4.sql' THEN
				RAISE EXCEPTION 'injected welfare v4 migration record failure' USING ERRCODE = 'P0001';
			END IF;
			RETURN NEW;
		END; $$;
		CREATE TRIGGER reject_welfare_v4_migration_record BEFORE INSERT ON schema_migrations
		FOR EACH ROW EXECUTE FUNCTION reject_welfare_v4_migration_record()`)
	require.NoError(t, err)
	err = ApplyMigrations(ctx, db)
	require.ErrorContains(t, err, "record migration "+welfareLotteryV4Migration)
	require.ErrorContains(t, err, "injected welfare v4 migration record failure")
	var pgErr *pq.Error
	require.ErrorAs(t, err, &pgErr)
	require.Equal(t, pq.ErrorCode("P0001"), pgErr.Code)
	welfareCheckinV3AssertSchema(t, ctx, db)
	welfareLotteryV4AssertPreserved(t, ctx, db, before)
	require.Equal(t, beforeHistory, upstreamAC1B8F1History(t, ctx, db))
	_, err = db.ExecContext(ctx, `DROP TRIGGER reject_welfare_v4_migration_record ON schema_migrations;
		DROP FUNCTION reject_welfare_v4_migration_record()`)
	require.NoError(t, err)
	require.NoError(t, ApplyMigrations(ctx, db))
	welfareLotteryV4AssertSchema(t, ctx, db)
	welfareLotteryV4AssertHistory(t, ctx, db, beforeHistory)
	before["welfare_programs"] = welfareLotteryV4ExpectedPrograms(t, before["welfare_programs"])
	welfareLotteryV4AssertPreserved(t, ctx, db, before)
}
