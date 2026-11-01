package repository

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
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

// The production 0.2.18-r2 image was built from this exact migration set.
// Its digest comes from 03eb8f3fc4a61729eb6a1525547a2f79b38b71f0, not from
// the merged tree, and uses the same sorted/trimmed SQL contract as the runner.
func upstreamV0219Baseline(t *testing.T) fstest.MapFS {
	t.Helper()
	const digest = "28bf8c95552e86e7a51dc940fff30a699670223129f6852f348b51f48ae0a34d"
	baseline := welfareLotteryV4Target(t)
	names, err := fs.Glob(baseline, "*.sql")
	require.NoError(t, err)
	hash := sha256.New()
	for _, name := range names {
		_, err = fmt.Fprintf(hash, "%s\x00%s\x00", name, strings.TrimSpace(string(baseline[name].Data)))
		require.NoError(t, err)
	}
	require.Len(t, baseline, 308)
	require.Equal(t, digest, hex.EncodeToString(hash.Sum(nil)), "preserve the exact released r2 SQL")
	return baseline
}

func TestUpstreamV0219MigrationBaseline(t *testing.T) {
	upstreamV0219Baseline(t)
}

func upstreamV0219AssertHistory(t *testing.T, ctx context.Context, db *sql.DB) map[string]upstreamAC1B8F1HistoryEntry {
	t.Helper()
	history := upstreamAC1B8F1History(t, ctx, db)
	require.Len(t, history, 310)
	for _, name := range []string{
		"247_affiliate_promotion_hosts.sql", "247_scoped_admin_api_keys.sql",
		"248_qq_image_generation.sql", "248_group_image_count_billing.sql",
		"249_qq_image_group_discovery.sql", "250_qq_reply_templates.sql",
		welfareCheckinV3Migration, welfareLotteryV4Migration,
	} {
		contents, err := migrations.FS.ReadFile(name)
		require.NoError(t, err)
		require.Contains(t, history, name, "duplicate numeric prefixes must coexist by complete filename")
		require.Equal(t, fmt.Sprintf("%x", sha256.Sum256([]byte(strings.TrimSpace(string(contents))))), history[name].Checksum, name)
	}
	return history
}

func upstreamV0219Snapshot(t *testing.T, ctx context.Context, db *sql.DB) map[string]string {
	t.Helper()
	snapshot := welfareLotteryV4Snapshot(t, ctx, db)
	for _, table := range []string{"affiliate_promotion_hosts", "qq_image_configs", "qq_image_grants", "qq_image_orders",
		"qq_image_holds", "qq_image_ledger", "qq_image_discoveries", "qq_reply_template_revisions", "qq_reply_template_state"} {
		query := "SELECT COALESCE(jsonb_agg(to_jsonb(t) ORDER BY to_jsonb(t)::text),'[]'::jsonb)::text FROM " + pq.QuoteIdentifier(table) + " t"
		snapshot[table] = upstreamAC1B8F1Snapshot(t, ctx, db, query)
	}
	// New defaulted group field is checked separately; all preexisting fields
	// (including customized ultrafast and image-generation choices) must survive.
	snapshot["groups"] = upstreamAC1B8F1Snapshot(t, ctx, db,
		"SELECT COALESCE(jsonb_agg(to_jsonb(t)-'image_count_billing' ORDER BY id),'[]'::jsonb)::text FROM groups t")
	return snapshot
}

func upstreamV0219AssertSnapshot(t *testing.T, ctx context.Context, db *sql.DB, before map[string]string) {
	t.Helper()
	for table, rows := range upstreamV0219Snapshot(t, ctx, db) {
		require.JSONEq(t, before[table], rows, "preserve %s across v0.2.19", table)
	}
}

func upstreamV0219SeedQQ(t *testing.T, ctx context.Context, db *sql.DB, userID int64) {
	t.Helper()
	var configID int64
	require.NoError(t, db.QueryRowContext(ctx, `INSERT INTO qq_image_configs(config,bot_secret,api_key)
		VALUES ('{"enabled":false,"model":"preserved-model"}','\x010203','\x040506') RETURNING id`).Scan(&configID))
	statements := []struct {
		query string
		args  []any
	}{
		{`INSERT INTO affiliate_promotion_hosts(hostname,inviter_id,revision) VALUES ('v0219.example.com',$1,7)`, []any{userID}},
		{`INSERT INTO qq_image_grants(id,identity_key,identity,user_id,status)
			VALUES ('v0219-grant','v0219-identity','{"platform":"qq"}',$1,'active')`, []any{userID}},
		{`INSERT INTO qq_image_orders(id,user_id,grant_id,grant_revision,identity_key,event_key,request_hash,
			config_id,data,price,business_day,status,funds_status,deadline,queue_deadline)
			VALUES ('v0219-order',$1,'v0219-grant',1,'v0219-identity','v0219-event','request-hash',
			$2,'{"prompt":"preserved prompt"}',4.25,CURRENT_DATE,'queued','held',now()+interval '1 hour',now()+interval '5 minutes')`, []any{userID, configID}},
		{`INSERT INTO qq_image_holds(order_id,user_id,amount,status) VALUES ('v0219-order',$1,4.25,'held')`, []any{userID}},
		{`INSERT INTO qq_image_ledger(order_id,user_id,operation,amount) VALUES ('v0219-order',$1,'hold',4.25)`, []any{userID}},
		{`INSERT INTO qq_image_discoveries(id,config_id,app_id,status,code_hash,expires_at)
			VALUES ('v0219-discovery',$1,'test-app','waiting','test-code-hash',now()+interval '5 minutes')`, []any{configID}},
		{`INSERT INTO qq_reply_template_revisions(revision,overrides,actor_id) VALUES (1,'{"help":"保留文案"}',$1)`, []any{userID}},
		{`UPDATE qq_reply_template_state SET active_revision=1 WHERE id=1`, nil},
	}
	for _, statement := range statements {
		_, err := db.ExecContext(ctx, statement.query, statement.args...)
		require.NoError(t, err)
	}
}

func TestUpstreamV0219MigrationUpgradePreservesState(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	db := upstreamAC1B8F1TestDatabase(t, ctx)
	require.NoError(t, applyMigrationsFS(ctx, db, upstreamV0219Baseline(t)))
	beforeHistory := upstreamAC1B8F1History(t, ctx, db)
	require.Len(t, beforeHistory, 308)
	userID := welfareLotteryV4Seed(t, ctx, db, true, "2026-09-01T04:05:06.123456Z")
	upstreamV0219SeedQQ(t, ctx, db, userID)
	for _, platform := range []string{"openai", "composite", "gemini", "antigravity"} {
		_, err := db.ExecContext(ctx, `INSERT INTO groups
			(name,platform,allow_image_generation,force_openai_ultrafast,description,rate_multiplier,i18n,allow_subscription_day_reset)
			VALUES ($1,$2,true,true,'preserve customized group',1.2345,'{"zh":{"name":"保留分组"}}',true)`, "v0219-"+platform, platform)
		require.NoError(t, err)
	}
	before := upstreamV0219Snapshot(t, ctx, db)
	require.NoError(t, ApplyMigrations(ctx, db))
	afterHistory := upstreamV0219AssertHistory(t, ctx, db)
	for name, entry := range beforeHistory {
		require.Equal(t, entry, afterHistory[name], "released checksum/application timestamp: %s", name)
	}
	upstreamV0219AssertSnapshot(t, ctx, db, before)
	welfareLotteryV4AssertSchema(t, ctx, db)
	var enabled int
	require.NoError(t, db.QueryRowContext(ctx, "SELECT count(*) FROM groups WHERE image_count_billing").Scan(&enabled))
	require.Zero(t, enabled, "upgrade must not silently opt existing groups into per-image billing")
	var keys int
	require.NoError(t, db.QueryRowContext(ctx, "SELECT count(*) FROM scoped_admin_api_keys").Scan(&keys))
	require.Zero(t, keys, "upgrade must not manufacture an administrative credential")
	_, err := db.ExecContext(ctx, `INSERT INTO scoped_admin_api_keys(id,name,key_prefix,key_hash,created_by,policy,allowed_ips)
		VALUES ('v0219-key','test key','test-prefix','irreversible-test-hash',$1,'{"mode":"allowlist"}','["127.0.0.1"]')`, userID)
	require.NoError(t, err)
	_, err = db.ExecContext(ctx, "UPDATE groups SET image_count_billing=true WHERE name='v0219-openai'")
	require.NoError(t, err)
	keySnapshot := upstreamAC1B8F1Snapshot(t, ctx, db, "SELECT jsonb_agg(to_jsonb(t) ORDER BY id)::text FROM scoped_admin_api_keys t")
	for range 2 {
		require.NoError(t, ApplyMigrations(ctx, db))
		require.Equal(t, afterHistory, upstreamAC1B8F1History(t, ctx, db))
		upstreamV0219AssertSnapshot(t, ctx, db, before)
		require.JSONEq(t, keySnapshot, upstreamAC1B8F1Snapshot(t, ctx, db, "SELECT jsonb_agg(to_jsonb(t) ORDER BY id)::text FROM scoped_admin_api_keys t"))
		var choice bool
		require.NoError(t, db.QueryRowContext(ctx, "SELECT image_count_billing FROM groups WHERE name='v0219-openai'").Scan(&choice))
		require.True(t, choice, "repeated startup preserves the administrator's subsequent billing choice")
	}
}

func TestUpstreamV0219MigrationFreshInstall(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	db := upstreamAC1B8F1TestDatabase(t, ctx)
	require.NoError(t, ApplyMigrations(ctx, db))
	history := upstreamV0219AssertHistory(t, ctx, db)
	welfareLotteryV4AssertSchema(t, ctx, db)
	var nullable, defaultValue string
	require.NoError(t, db.QueryRowContext(ctx, `SELECT is_nullable,column_default FROM information_schema.columns
		WHERE table_schema='public' AND table_name='groups' AND column_name='image_count_billing'`).Scan(&nullable, &defaultValue))
	require.Equal(t, "NO", nullable)
	require.Equal(t, "false", defaultValue)
	for _, table := range []string{"scoped_admin_api_keys", "qq_image_orders", "affiliate_promotion_hosts"} {
		var exists bool
		require.NoError(t, db.QueryRowContext(ctx, "SELECT to_regclass($1) IS NOT NULL", "public."+table).Scan(&exists))
		require.True(t, exists, table)
	}
	before := upstreamV0219Snapshot(t, ctx, db)
	require.NoError(t, ApplyMigrations(ctx, db))
	require.Equal(t, history, upstreamAC1B8F1History(t, ctx, db))
	upstreamV0219AssertSnapshot(t, ctx, db, before)
}
