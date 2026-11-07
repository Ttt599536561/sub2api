package repository

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"fmt"
	"io/fs"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/Wei-Shaw/sub2api/migrations"
	"github.com/lib/pq"
	"github.com/stretchr/testify/require"
)

const upstreamV0220Migration = "249_mcp_oauth.sql"

// Independently calculated from the published 0.2.19-r2 application commit
// 1e3ca55416d9117258fc906bd1891bc0c64c8644, using the runner's trimmed SQL.
func upstreamV0220Baseline(t *testing.T) fstest.MapFS {
	t.Helper()
	baseline := upstreamV0219Target(t)
	names, err := fs.Glob(baseline, "*.sql")
	require.NoError(t, err)
	hash := sha256.New()
	for _, name := range names {
		_, err = fmt.Fprintf(hash, "%s\x00%s\x00", name, strings.TrimSpace(string(baseline[name].Data)))
		require.NoError(t, err)
	}
	require.Len(t, baseline, 310)
	require.Equal(t, "84414a6eee2465e6213ccb56ac7bf67aba7af7df4013fc1623493b8ef591eb5f", fmt.Sprintf("%x", hash.Sum(nil)))
	return baseline
}

func TestUpstreamV0220MigrationBaseline(t *testing.T) {
	upstreamV0220Baseline(t)
}

func upstreamV0220Snapshot(t *testing.T, ctx context.Context, db *sql.DB) map[string]string {
	t.Helper()
	snapshot := upstreamV0219Snapshot(t, ctx, db)
	for _, table := range []string{"groups", "scoped_admin_api_keys"} {
		snapshot[table] = upstreamAC1B8F1Snapshot(t, ctx, db,
			"SELECT COALESCE(jsonb_agg(to_jsonb(t) ORDER BY to_jsonb(t)::text),'[]'::jsonb)::text FROM "+pq.QuoteIdentifier(table)+" t")
	}
	// No existing column, index or constraint may change during this additive
	// upgrade, including the QQ/affiliate/welfare schema and funds protections.
	snapshot["existing columns"] = upstreamAC1B8F1Snapshot(t, ctx, db, `SELECT jsonb_agg(to_jsonb(t) ORDER BY table_name,ordinal_position)::text
		FROM information_schema.columns t WHERE table_schema='public' AND table_name NOT LIKE 'mcp_oauth_%'`)
	snapshot["existing indexes"] = upstreamAC1B8F1Snapshot(t, ctx, db, `SELECT jsonb_agg(to_jsonb(t) ORDER BY indexname)::text
		FROM pg_indexes t WHERE schemaname='public' AND tablename NOT LIKE 'mcp_oauth_%'`)
	snapshot["existing constraints"] = upstreamAC1B8F1Snapshot(t, ctx, db, `SELECT jsonb_agg(to_jsonb(t) ORDER BY relation,name)::text FROM
		(SELECT conrelid::regclass::text AS relation,conname AS name,pg_get_constraintdef(oid) AS definition
		FROM pg_constraint WHERE connamespace='public'::regnamespace AND conrelid::regclass::text NOT LIKE 'mcp_oauth_%') t`)
	return snapshot
}

func upstreamV0220AssertHistory(t *testing.T, ctx context.Context, db *sql.DB) map[string]upstreamAC1B8F1HistoryEntry {
	t.Helper()
	history := upstreamAC1B8F1History(t, ctx, db)
	require.Len(t, history, 311)
	for _, name := range []string{upstreamV0220Migration, "249_qq_image_group_discovery.sql"} {
		contents, err := migrations.FS.ReadFile(name)
		require.NoError(t, err)
		require.Contains(t, history, name, "both 249 filenames must coexist")
		require.Equal(t, fmt.Sprintf("%x", sha256.Sum256([]byte(strings.TrimSpace(string(contents))))), history[name].Checksum)
	}
	return history
}

func upstreamV0220AssertEmptyOAuth(t *testing.T, ctx context.Context, db *sql.DB) {
	t.Helper()
	for _, table := range []string{"mcp_oauth_grants", "mcp_oauth_codes", "mcp_oauth_credentials"} {
		var count int
		require.NoError(t, db.QueryRowContext(ctx, "SELECT count(*) FROM "+pq.QuoteIdentifier(table)).Scan(&count))
		require.Zero(t, count, "migration must not manufacture management authorizations")
	}
}

func TestUpstreamV0220MigrationUpgradePreservesState(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	db := upstreamAC1B8F1TestDatabase(t, ctx)
	require.NoError(t, applyMigrationsFS(ctx, db, upstreamV0220Baseline(t)))
	beforeHistory := upstreamAC1B8F1History(t, ctx, db)
	require.Len(t, beforeHistory, 310)
	userID := welfareLotteryV4Seed(t, ctx, db, true, "2026-09-01T04:05:06.123456Z")
	upstreamV0219SeedQQ(t, ctx, db, userID)
	_, err := db.ExecContext(ctx, `INSERT INTO scoped_admin_api_keys(id,name,key_prefix,key_hash,created_by,policy,allowed_ips)
		VALUES ('v0220-key','preserved scoped key','test-prefix','irreversible-test-hash',$1,'{"mode":"allowlist"}','["127.0.0.1"]')`, userID)
	require.NoError(t, err)
	_, err = db.ExecContext(ctx, `INSERT INTO groups
		(name,platform,allow_image_generation,force_openai_ultrafast,image_count_billing,i18n,allow_subscription_day_reset)
		VALUES ('v0220-group','openai',true,true,true,'{"zh":{"name":"保留分组"}}',true)`)
	require.NoError(t, err)
	before := upstreamV0220Snapshot(t, ctx, db)
	require.NoError(t, ApplyMigrations(ctx, db))
	afterHistory := upstreamV0220AssertHistory(t, ctx, db)
	for name, entry := range beforeHistory {
		require.Equal(t, entry, afterHistory[name], "released checksum and application timestamp: %s", name)
	}
	upstreamV0220AssertEmptyOAuth(t, ctx, db)
	for range 2 {
		require.NoError(t, ApplyMigrations(ctx, db))
		require.Equal(t, afterHistory, upstreamAC1B8F1History(t, ctx, db))
		for name, rows := range upstreamV0220Snapshot(t, ctx, db) {
			require.JSONEq(t, before[name], rows, "preserve %s across upgrade and repeated startup", name)
		}
	}
}

func TestUpstreamV0220MigrationFreshInstall(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	db := upstreamAC1B8F1TestDatabase(t, ctx)
	require.NoError(t, ApplyMigrations(ctx, db))
	history := upstreamV0220AssertHistory(t, ctx, db)
	upstreamV0220AssertEmptyOAuth(t, ctx, db)
	welfareLotteryV4AssertSchema(t, ctx, db)
	before := upstreamV0220Snapshot(t, ctx, db)
	require.NoError(t, ApplyMigrations(ctx, db))
	require.Equal(t, history, upstreamAC1B8F1History(t, ctx, db))
	for name, rows := range upstreamV0220Snapshot(t, ctx, db) {
		require.JSONEq(t, before[name], rows, name)
	}
}
