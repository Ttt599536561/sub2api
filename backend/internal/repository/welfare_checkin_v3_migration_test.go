package repository

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/Wei-Shaw/sub2api/migrations"
	"github.com/lib/pq"
	"github.com/stretchr/testify/require"
)

const welfareCheckinV3Migration = "251_welfare_checkin_rules_v3.sql"

// Preserve the released 306 -> 307 migration target when later rules ship.
func welfareCheckinV3Target(t *testing.T) fstest.MapFS {
	t.Helper()
	target := upstreamAC1B8F1Target(t)
	contents, err := migrations.FS.ReadFile(welfareCheckinV3Migration)
	require.NoError(t, err)
	target[welfareCheckinV3Migration] = &fstest.MapFile{Data: contents}
	require.Len(t, target, 307)
	return target
}

func TestWelfareCheckinV3MigrationFile(t *testing.T) {
	_, err := migrations.FS.ReadFile(welfareCheckinV3Migration)
	require.NoError(t, err)
}

// Compare the whole program row; only the explicit v2 -> v3 metadata change
// is permitted. In particular, enabling, launch and audit timestamps survive.
func welfareCheckinV3ExpectedPrograms(t *testing.T, before string) string {
	t.Helper()
	var programs []map[string]any
	require.NoError(t, json.Unmarshal([]byte(before), &programs))
	for _, program := range programs {
		require.Equal(t, "v2", program["rules_version"])
		program["rules_version"] = "v3"
	}
	after, err := json.Marshal(programs)
	require.NoError(t, err)
	return string(after)
}

func welfareCheckinV3AssertSchema(t *testing.T, ctx context.Context, db *sql.DB) {
	t.Helper()
	var version, defaultValue, definition string
	require.NoError(t, db.QueryRowContext(ctx, `SELECT rules_version FROM welfare_programs WHERE id=1`).Scan(&version))
	require.Equal(t, "v3", version)
	require.NoError(t, db.QueryRowContext(ctx, `SELECT column_default FROM information_schema.columns
		WHERE table_schema='public' AND table_name='welfare_programs' AND column_name='rules_version'`).Scan(&defaultValue))
	require.Equal(t, "'v3'::text", defaultValue)
	require.NoError(t, db.QueryRowContext(ctx, `SELECT pg_get_constraintdef(oid) FROM pg_constraint
		WHERE conrelid='welfare_programs'::regclass AND conname='welfare_programs_rules_version_check'`).Scan(&definition))
	require.Contains(t, definition, "rules_version = 'v3'::text")
	for _, rejected := range []string{"v2", "v4", "unknown"} {
		_, err := db.ExecContext(ctx, `UPDATE welfare_programs SET rules_version=$1 WHERE id=1`, rejected)
		var pgErr *pq.Error
		require.ErrorAs(t, err, &pgErr)
		require.Equal(t, pq.ErrorCode("23514"), pgErr.Code)
		require.Equal(t, "welfare_programs_rules_version_check", pgErr.Constraint)
	}
	_, err := db.ExecContext(ctx, `UPDATE welfare_programs SET rules_version=DEFAULT WHERE id=1`)
	require.NoError(t, err, "the new default must satisfy the new constraint")
}

func welfareCheckinV3AssertHistory(t *testing.T, ctx context.Context, db *sql.DB) map[string]upstreamAC1B8F1HistoryEntry {
	t.Helper()
	history := upstreamAC1B8F1History(t, ctx, db)
	require.Len(t, history, 307)
	require.Contains(t, history, welfareCheckinV3Migration)
	contents, err := migrations.FS.ReadFile(welfareCheckinV3Migration)
	require.NoError(t, err)
	require.Equal(t, fmt.Sprintf("%x", sha256.Sum256([]byte(strings.TrimSpace(string(contents))))), history[welfareCheckinV3Migration].Checksum)
	return history
}

func TestWelfareCheckinV3MigrationUpgradePreservesState(t *testing.T) {
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
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
			defer cancel()
			db := upstreamAC1B8F1TestDatabase(t, ctx)
			require.NoError(t, applyMigrationsFS(ctx, db, upstreamAC1B8F1Target(t)))
			beforeHistory := upstreamAC1B8F1History(t, ctx, db)
			require.Len(t, beforeHistory, 306)
			require.NotContains(t, beforeHistory, welfareCheckinV3Migration)
			var userID int64
			require.NoError(t, db.QueryRowContext(ctx, `INSERT INTO users
				(email,password_hash,balance,frozen_balance,status) VALUES
				('welfare-v3-upgrade@example.com','preserved-hash',123.45678901,4.25000000,'active') RETURNING id`).Scan(&userID))
			_, err := db.ExecContext(ctx, `UPDATE welfare_programs SET enabled=$1,launch_at=$2,
				created_at='2026-08-01T01:02:03.123456Z',updated_at='2026-10-01T07:08:09.123456Z' WHERE id=1`, tc.enabled, tc.launch)
			require.NoError(t, err)
			const operationID = "55e58a36-439d-4507-97b9-945f7fdbbeb1"
			const oldResult = `{"operation_id":"55e58a36-439d-4507-97b9-945f7fdbbeb1","status":"success","reward_amount":"3.75","base_reward_amount":"1.25","streak_reward_amount":"2.50","overview":{"rules_version":2,"welfare_balance":"271.25","cycle_day":19,"total_checkin_days":49}}`
			statements := []struct {
				query string
				args  []any
			}{
				{`INSERT INTO welfare_wallets(user_id,balance_cents,total_earned_cents,total_redeemed_cents,
					total_checkin_days,cycle_id,cycle_day,last_checkin_date,daily_low_count,eligible_spend,
					draws_used,subscription_draws,wallet_version,welfare_balance_version)
					VALUES($1,27125,32125,5000,49,2,19,'2026-10-25',3,175.12345678,2,4,77,5)`, []any{userID}},
				{`INSERT INTO welfare_operations(id,user_id,type,idempotency_key,request_fingerprint,result,private_audit)
					VALUES($1,$2,'checkin','2026-10-25','checkin:2026-10-25',$3,
					'{"rules_version":2,"daily_cents":125,"streak_cents":250,"daily_low_count":3}')`, []any{operationID, userID, oldResult}},
				{`INSERT INTO welfare_checkins(user_id,business_date,operation_id,daily_cents,streak_cents,cycle_id,cycle_day)
					VALUES($1,'2026-10-25',$2,125,250,2,19)`, []any{userID, operationID}},
				{`INSERT INTO welfare_ledger(user_id,operation_id,type,amount_cents,balance_after_cents,business_date)
					VALUES($1,$2,'daily',125,26875,'2026-10-25'),($1,$2,'streak',250,27125,'2026-10-25')`, []any{userID, operationID}},
				{`INSERT INTO welfare_spend_events(user_id,source_type,source_id,action,amount,ticket_delta)
					VALUES($1,'usage','preserved-usage','debit',175.12345678,3)`, []any{userID}},
				{`INSERT INTO welfare_balance_outbox(user_id,version,attempts,last_error)
					VALUES($1,5,1,'preserved retry')`, []any{userID}},
			}
			for _, statement := range statements {
				_, err = db.ExecContext(ctx, statement.query, statement.args...)
				require.NoError(t, err)
			}
			queries := map[string]string{}
			for _, table := range []string{"users", "welfare_programs", "welfare_wallets", "welfare_operations", "welfare_checkins",
				"welfare_ledger", "welfare_spend_events", "welfare_balance_outbox", "welfare_subscription_rewards"} {
				queries[table] = "SELECT COALESCE(jsonb_agg(to_jsonb(t) ORDER BY to_jsonb(t)::text),'[]'::jsonb)::text FROM " + pq.QuoteIdentifier(table) + " t"
			}
			before := map[string]string{}
			for name, query := range queries {
				before[name] = upstreamAC1B8F1Snapshot(t, ctx, db, query)
			}
			before["welfare_programs"] = welfareCheckinV3ExpectedPrograms(t, before["welfare_programs"])
			assertPreserved := func() {
				t.Helper()
				for name, query := range queries {
					require.JSONEq(t, before[name], upstreamAC1B8F1Snapshot(t, ctx, db, query), name)
				}
			}
			require.NoError(t, applyMigrationsFS(ctx, db, welfareCheckinV3Target(t)))
			assertPreserved()
			welfareCheckinV3AssertSchema(t, ctx, db)
			history := welfareCheckinV3AssertHistory(t, ctx, db)
			for name, entry := range beforeHistory {
				require.Equal(t, entry, history[name], "old checksum and application time: %s", name)
			}
			repo := NewWelfareRepository(db)
			settings, err := repo.GetSettings(ctx)
			require.NoError(t, err)
			require.Equal(t, service.WelfareRulesVersion, settings.RulesVersion)
			state, err := repo.State(ctx, userID)
			require.NoError(t, err)
			require.Equal(t, service.WelfareRulesVersion, state.Program.RulesVersion)
			var expected service.WelfareOperation
			require.NoError(t, json.Unmarshal([]byte(oldResult), &expected))
			replayed, err := repo.Mutate(ctx, userID, "checkin", "2026-10-25", "checkin:2026-10-25", func(*service.WelfareState) (*service.WelfareMutation, error) {
				t.Error("a historical v2 result must be replayed without applying v3 rewards")
				return nil, fmt.Errorf("unexpected mutation")
			})
			require.NoError(t, err)
			require.Equal(t, expected, *replayed)
			require.NoError(t, applyMigrationsFS(ctx, db, welfareCheckinV3Target(t)))
			require.Equal(t, history, upstreamAC1B8F1History(t, ctx, db), "repeated startup preserves migration history")
			// Direct SQL re-entry must also be harmless, even without the runner's history check.
			contents, err := migrations.FS.ReadFile(welfareCheckinV3Migration)
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
			welfareCheckinV3AssertSchema(t, ctx, db)
			assertPreserved()
			require.Equal(t, history, upstreamAC1B8F1History(t, ctx, db))
		})
	}
}

func TestWelfareCheckinV3MigrationFreshInstall(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	db := upstreamAC1B8F1TestDatabase(t, ctx)
	require.NoError(t, applyMigrationsFS(ctx, db, welfareCheckinV3Target(t)))
	welfareCheckinV3AssertSchema(t, ctx, db)
	history := welfareCheckinV3AssertHistory(t, ctx, db)
	var enabled bool
	var launch sql.NullTime
	require.NoError(t, db.QueryRowContext(ctx, `SELECT enabled,launch_at FROM welfare_programs WHERE id=1`).Scan(&enabled, &launch))
	require.False(t, enabled)
	require.False(t, launch.Valid)
	for _, table := range []string{"welfare_wallets", "welfare_operations", "welfare_checkins", "welfare_ledger"} {
		var count int
		require.NoError(t, db.QueryRowContext(ctx, "SELECT count(*) FROM "+pq.QuoteIdentifier(table)).Scan(&count))
		require.Zero(t, count, table)
	}
	require.NoError(t, applyMigrationsFS(ctx, db, welfareCheckinV3Target(t)))
	require.Equal(t, history, upstreamAC1B8F1History(t, ctx, db))
	welfareCheckinV3AssertSchema(t, ctx, db)
}

func TestWelfareCheckinV3MigrationConcurrentPendingUpgrade(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	db := upstreamAC1B8F1TestDatabase(t, ctx)
	require.NoError(t, applyMigrationsFS(ctx, db, upstreamAC1B8F1Target(t)))
	beforeHistory := upstreamAC1B8F1History(t, ctx, db)
	require.Len(t, beforeHistory, 306)
	require.NotContains(t, beforeHistory, welfareCheckinV3Migration)

	// Both startup attempts begin against the released schema, with 251 pending.
	// The harness's already-migrated database cannot exercise this race.
	const instances = 2
	ready := make(chan struct{}, instances)
	start := make(chan struct{})
	results := make(chan error, instances)
	for range instances {
		go func() {
			ready <- struct{}{}
			<-start
			results <- applyMigrationsFS(ctx, db, welfareCheckinV3Target(t))
		}()
	}
	for range instances {
		<-ready
	}
	close(start)
	for range instances {
		require.NoError(t, <-results)
	}

	history := welfareCheckinV3AssertHistory(t, ctx, db)
	for name, entry := range beforeHistory {
		require.Equal(t, entry, history[name], "old checksum and application time: %s", name)
	}
	var recorded int
	require.NoError(t, db.QueryRowContext(ctx, `SELECT count(*) FROM schema_migrations WHERE filename=$1`, welfareCheckinV3Migration).Scan(&recorded))
	require.Equal(t, 1, recorded, "concurrent startup must record 251 exactly once")
	welfareCheckinV3AssertSchema(t, ctx, db)
}

func TestWelfareCheckinV3MigrationRecordFailureRollsBackAndRetries(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	db := upstreamAC1B8F1TestDatabase(t, ctx)
	require.NoError(t, applyMigrationsFS(ctx, db, upstreamAC1B8F1Target(t)))
	beforeHistory := upstreamAC1B8F1History(t, ctx, db)
	require.Len(t, beforeHistory, 306)
	require.NotContains(t, beforeHistory, welfareCheckinV3Migration)
	_, err := db.ExecContext(ctx, `UPDATE welfare_programs SET enabled=true,
		launch_at='2026-09-01T04:05:06.123456Z',created_at='2026-08-01T01:02:03.123456Z',
		updated_at='2026-10-01T07:08:09.123456Z' WHERE id=1`)
	require.NoError(t, err)
	queries := map[string]string{
		"program": `SELECT jsonb_agg(to_jsonb(t) ORDER BY id)::text FROM welfare_programs t`,
		"default": `SELECT column_default FROM information_schema.columns
			WHERE table_schema='public' AND table_name='welfare_programs' AND column_name='rules_version'`,
		"constraint": `SELECT pg_get_constraintdef(oid) FROM pg_constraint
			WHERE conrelid='welfare_programs'::regclass AND conname='welfare_programs_rules_version_check'`,
	}
	before := map[string]string{}
	for name, query := range queries {
		before[name] = upstreamAC1B8F1Snapshot(t, ctx, db, query)
	}
	require.Equal(t, "'v2'::text", before["default"])
	require.Contains(t, before["constraint"], "rules_version = 'v2'::text")

	// Fail after all of 251's DDL and data changes, at the history insert.
	// This trigger exists only in this test's randomly named database.
	_, err = db.ExecContext(ctx, `CREATE FUNCTION reject_welfare_v3_migration_record() RETURNS trigger
		LANGUAGE plpgsql AS $$
		BEGIN
			IF NEW.filename = '251_welfare_checkin_rules_v3.sql' THEN
				RAISE EXCEPTION 'injected welfare v3 migration record failure' USING ERRCODE = 'P0001';
			END IF;
			RETURN NEW;
		END;
		$$;
		CREATE TRIGGER reject_welfare_v3_migration_record BEFORE INSERT ON schema_migrations
		FOR EACH ROW EXECUTE FUNCTION reject_welfare_v3_migration_record()`)
	require.NoError(t, err)
	err = applyMigrationsFS(ctx, db, welfareCheckinV3Target(t))
	require.ErrorContains(t, err, "record migration "+welfareCheckinV3Migration)
	require.ErrorContains(t, err, "injected welfare v3 migration record failure")
	var pgErr *pq.Error
	require.ErrorAs(t, err, &pgErr)
	require.Equal(t, pq.ErrorCode("P0001"), pgErr.Code)
	for name, query := range queries {
		require.Equal(t, before[name], upstreamAC1B8F1Snapshot(t, ctx, db, query), name+" must roll back with the migration history")
	}
	afterFailureHistory := upstreamAC1B8F1History(t, ctx, db)
	require.Equal(t, beforeHistory, afterFailureHistory)
	require.NotContains(t, afterFailureHistory, welfareCheckinV3Migration)

	_, err = db.ExecContext(ctx, `DROP TRIGGER reject_welfare_v3_migration_record ON schema_migrations;
		DROP FUNCTION reject_welfare_v3_migration_record()`)
	require.NoError(t, err)
	require.NoError(t, applyMigrationsFS(ctx, db, welfareCheckinV3Target(t)))
	history := welfareCheckinV3AssertHistory(t, ctx, db)
	for name, entry := range beforeHistory {
		require.Equal(t, entry, history[name], "retry preserves old checksum and application time: %s", name)
	}
	welfareCheckinV3AssertSchema(t, ctx, db)
	require.JSONEq(t, welfareCheckinV3ExpectedPrograms(t, before["program"]), upstreamAC1B8F1Snapshot(t, ctx, db, queries["program"]))
}
