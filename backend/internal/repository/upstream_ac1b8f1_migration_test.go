package repository

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"fmt"
	"io/fs"
	"net"
	"net/url"
	"os"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/Wei-Shaw/sub2api/migrations"
	"github.com/lib/pq"
	"github.com/stretchr/testify/require"
)

const upstreamAC1B8F1Migration = "246_enable_gemini_image_generation_groups.sql"

// Pin the already released/customized source, including the other 246 and QQ
// migrations 248-250. Later migrations must never silently enter this fixture.
func upstreamAC1B8F1Baseline(t *testing.T) fstest.MapFS {
	t.Helper()
	const commit = "cda70a59b584932d3de120423112a9b4feb707e5"
	const expectedDigest = "3159e4b38a23577d1e9d0fa9a9773e35949c8c5be5d7e39be121c5335d4ad91b"
	names, err := fs.Glob(migrations.FS, "*.sql")
	require.NoError(t, err)
	fixture := fstest.MapFS{}
	digest := sha256.New()
	for _, name := range names {
		if name > "250_qq_reply_templates.sql" || name == upstreamAC1B8F1Migration {
			continue
		}
		contents, readErr := migrations.FS.ReadFile(name)
		require.NoError(t, readErr)
		fixture[name] = &fstest.MapFile{Data: contents}
		_, err = fmt.Fprintf(digest, "%s\x00%s\x00", name, strings.TrimSpace(string(contents)))
		require.NoError(t, err)
	}
	require.Len(t, fixture, 305, "preserve migration baseline %s", commit)
	require.Equal(t, expectedDigest, hex.EncodeToString(digest.Sum(nil)), "released SQL changed: %s", commit)
	return fixture
}

func TestUpstreamAC1B8F1MigrationBaseline(t *testing.T) {
	upstreamAC1B8F1Baseline(t)
}

// Only the explicit loopback URL used by the existing QQ CI fixture is accepted.
// Query parameters cannot redirect host/database or select an existing schema.
func upstreamAC1B8F1DatabaseURL(raw string) (*url.URL, error) {
	u, err := url.Parse(raw)
	if err != nil || u == nil || (u.Scheme != "postgres" && u.Scheme != "postgresql") || u.Fragment != "" || u.Path == "" || u.Path == "/" {
		return nil, fmt.Errorf("an explicit loopback PostgreSQL test URL is required")
	}
	ip := net.ParseIP(u.Hostname())
	if u.Hostname() != "localhost" && (ip == nil || !ip.IsLoopback()) {
		return nil, fmt.Errorf("only loopback test PostgreSQL is allowed")
	}
	query, err := url.ParseQuery(u.RawQuery)
	if err != nil {
		return nil, fmt.Errorf("invalid test PostgreSQL URL parameters")
	}
	for key, values := range query {
		switch key {
		case "sslmode", "connect_timeout", "application_name", "TimeZone", "timezone":
			if len(values) != 1 {
				return nil, fmt.Errorf("duplicate test PostgreSQL URL parameter")
			}
		default:
			return nil, fmt.Errorf("unsupported test PostgreSQL URL parameter")
		}
	}
	return u, nil
}

func TestUpstreamAC1B8F1MigrationDatabaseURL(t *testing.T) {
	for _, tc := range []struct {
		name string
		raw  string
		ok   bool
	}{
		{"IPv4", "postgres://test@127.0.0.1:5432/test?sslmode=disable", true},
		{"IPv6", "postgresql://test@[::1]:5432/test?sslmode=disable", true},
		{"localhost", "postgres://test@localhost/test", true},
		{"remote", "postgres://test@db.example.com/test", false},
		{"loopback in user", "postgres://127.0.0.1@db.example.com/test", false},
		{"host override", "postgres://test@127.0.0.1/test?host=db.example.com", false},
		{"database override", "postgres://test@127.0.0.1/test?dbname=other", false},
		{"schema override", "postgres://test@127.0.0.1/test?search_path=public", false},
		{"missing database", "postgres://test@127.0.0.1/", false},
		{"keyword DSN", "host=127.0.0.1 dbname=test", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := upstreamAC1B8F1DatabaseURL(tc.raw)
			if tc.ok {
				require.NoError(t, err)
			} else {
				require.Error(t, err)
			}
		})
	}
}

func upstreamAC1B8F1TestDatabase(t *testing.T, ctx context.Context) *sql.DB {
	t.Helper()
	dsn := os.Getenv("QQ_IMAGE_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("QQ_IMAGE_TEST_DATABASE_URL is required for real PostgreSQL migration tests")
	}
	u, err := upstreamAC1B8F1DatabaseURL(dsn)
	require.NoError(t, err)
	admin, err := sql.Open("postgres", u.String())
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, admin.Close()) })
	var serverAddress string
	require.NoError(t, admin.QueryRowContext(ctx, "SELECT host(inet_server_addr())").Scan(&serverAddress))
	require.True(t, net.ParseIP(serverAddress).IsLoopback(), "test PostgreSQL must resolve to loopback")

	var nonce [12]byte
	_, err = rand.Read(nonce[:])
	require.NoError(t, err)
	name := "upstream_ac1b8f1_" + hex.EncodeToString(nonce[:])
	_, err = admin.ExecContext(ctx, "CREATE DATABASE "+pq.QuoteIdentifier(name)+" TEMPLATE template0")
	require.NoError(t, err, "test role needs CREATEDB; the supplied database is never migrated")
	var db *sql.DB
	t.Cleanup(func() {
		if db != nil {
			if closeErr := db.Close(); closeErr != nil {
				t.Errorf("close migration database: %v", closeErr)
			}
		}
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		_, dropErr := admin.ExecContext(cleanupCtx, "DROP DATABASE "+pq.QuoteIdentifier(name)+" WITH (FORCE)")
		require.NoError(t, dropErr, "remove only this test's database %s", name)
	})
	u.Path = "/" + name
	u.RawPath = ""
	db, err = sql.Open("postgres", u.String())
	require.NoError(t, err)
	var actualName, schema string
	require.NoError(t, db.QueryRowContext(ctx, "SELECT current_database(), current_schema()").Scan(&actualName, &schema))
	require.Equal(t, name, actualName)
	require.Equal(t, "public", schema, "full migrations contain public-qualified indexes")
	t.Logf("isolated PostgreSQL migration database: %s", name)
	return db
}

type upstreamAC1B8F1HistoryEntry struct {
	Checksum  string
	AppliedAt time.Time
}

func upstreamAC1B8F1History(t *testing.T, ctx context.Context, db *sql.DB) map[string]upstreamAC1B8F1HistoryEntry {
	t.Helper()
	rows, err := db.QueryContext(ctx, "SELECT filename, checksum, applied_at FROM schema_migrations ORDER BY filename")
	require.NoError(t, err)
	defer func() { require.NoError(t, rows.Close()) }()
	history := map[string]upstreamAC1B8F1HistoryEntry{}
	for rows.Next() {
		var name string
		var entry upstreamAC1B8F1HistoryEntry
		require.NoError(t, rows.Scan(&name, &entry.Checksum, &entry.AppliedAt))
		history[name] = entry
	}
	require.NoError(t, rows.Err())
	return history
}

func upstreamAC1B8F1Snapshot(t *testing.T, ctx context.Context, db *sql.DB, query string) string {
	t.Helper()
	var result string
	require.NoError(t, db.QueryRowContext(ctx, query).Scan(&result))
	return result
}

func upstreamAC1B8F1AssertHistory(t *testing.T, history map[string]upstreamAC1B8F1HistoryEntry) {
	t.Helper()
	require.Len(t, history, 306)
	for _, name := range []string{
		upstreamAC1B8F1Migration, "246_group_force_openai_ultrafast.sql", "247_affiliate_promotion_hosts.sql",
		"248_qq_image_generation.sql", "249_qq_image_group_discovery.sql", "250_qq_reply_templates.sql",
	} {
		contents, err := migrations.FS.ReadFile(name)
		require.NoError(t, err)
		require.Contains(t, history, name, "full filenames sharing numerical prefixes must coexist")
		require.Equal(t, fmt.Sprintf("%x", sha256.Sum256([]byte(strings.TrimSpace(string(contents))))), history[name].Checksum, name)
	}
}

func TestUpstreamAC1B8F1MigrationUpgrade(t *testing.T) {
	baseline := upstreamAC1B8F1Baseline(t)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	db := upstreamAC1B8F1TestDatabase(t, ctx)
	require.NoError(t, applyMigrationsFS(ctx, db, baseline))
	beforeHistory := upstreamAC1B8F1History(t, ctx, db)
	require.Len(t, beforeHistory, 305)
	require.NotContains(t, beforeHistory, upstreamAC1B8F1Migration)

	for _, platform := range []string{"gemini", "antigravity", "composite", "openai"} {
		for _, enabled := range []bool{false, true} {
			_, err := db.ExecContext(ctx, `INSERT INTO groups
				(name,platform,allow_image_generation,force_openai_ultrafast,description,rate_multiplier,i18n,allow_subscription_day_reset)
				VALUES ($1,$2,$3,$4,'preserve customized group',1.2345,'{"zh":{"name":"保留分组"}}',true)`,
				fmt.Sprintf("upgrade-%s-%t", platform, enabled), platform, enabled, !enabled)
			require.NoError(t, err)
		}
	}
	var userID, configID int64
	require.NoError(t, db.QueryRowContext(ctx, `INSERT INTO users
		(email,password_hash,balance,frozen_balance,status) VALUES
		('ac1b8f1-upgrade@example.com','preserved-password-hash',123.45678901,4.25000000,'active') RETURNING id`).Scan(&userID))
	require.NoError(t, db.QueryRowContext(ctx, `INSERT INTO qq_image_configs(config,bot_secret,api_key)
		VALUES ('{"enabled":false,"model":"preserved-model"}','\x010203','\x040506') RETURNING id`).Scan(&configID))
	statements := []struct {
		query string
		args  []any
	}{
		{`INSERT INTO affiliate_promotion_hosts(hostname,inviter_id,revision) VALUES ('upgrade.example.com',$1,7)`, []any{userID}},
		{`INSERT INTO qq_image_grants(id,identity_key,identity,user_id,status)
			VALUES ('upgrade-grant','upgrade-identity','{"platform":"qq"}',$1,'active')`, []any{userID}},
		{`INSERT INTO qq_image_orders(id,user_id,grant_id,grant_revision,identity_key,event_key,request_hash,
			config_id,data,price,business_day,status,funds_status,deadline,queue_deadline)
			VALUES ('upgrade-order',$1,'upgrade-grant',1,'upgrade-identity','upgrade-event','request-hash',
			$2,'{"prompt":"preserved prompt"}',4.25,CURRENT_DATE,'queued','held',now()+interval '1 hour',now()+interval '5 minutes')`, []any{userID, configID}},
		{`INSERT INTO qq_image_holds(order_id,user_id,amount,status) VALUES ('upgrade-order',$1,4.25,'held')`, []any{userID}},
		{`INSERT INTO qq_image_ledger(order_id,user_id,operation,amount) VALUES ('upgrade-order',$1,'hold',4.25)`, []any{userID}},
		{`INSERT INTO qq_image_discoveries(id,config_id,app_id,status,code_hash,expires_at)
			VALUES ('upgrade-discovery',$1,'test-app','waiting','test-code-hash',now()+interval '5 minutes')`, []any{configID}},
		{`INSERT INTO qq_reply_template_revisions(revision,overrides,actor_id) VALUES (1,'{"help":"保留文案"}',$1)`, []any{userID}},
		{`UPDATE qq_reply_template_state SET active_revision=1 WHERE id=1`, nil},
	}
	for _, statement := range statements {
		_, err := db.ExecContext(ctx, statement.query, statement.args...)
		require.NoError(t, err)
	}

	queries := map[string]string{
		"users including balance and frozen funds": "SELECT jsonb_agg(to_jsonb(t) ORDER BY id)::text FROM users t",
		"groups including custom fields":           "SELECT jsonb_agg(to_jsonb(t)-'allow_image_generation' ORDER BY id)::text FROM groups t",
		"QQ columns": `SELECT jsonb_agg(to_jsonb(t) ORDER BY table_name,ordinal_position)::text
			FROM information_schema.columns t WHERE table_schema='public' AND table_name LIKE 'qq_%'`,
		"QQ indexes": `SELECT jsonb_agg(to_jsonb(t) ORDER BY indexname)::text
			FROM pg_indexes t WHERE schemaname='public' AND tablename LIKE 'qq_%'`,
		"QQ constraints": `SELECT jsonb_agg(to_jsonb(t) ORDER BY relation,name)::text FROM
			(SELECT conrelid::regclass::text AS relation,conname AS name,pg_get_constraintdef(oid) AS definition
			 FROM pg_constraint WHERE connamespace='public'::regnamespace AND conrelid::regclass::text LIKE 'qq_%') t`,
		"QQ triggers": `SELECT jsonb_agg(to_jsonb(t) ORDER BY name)::text FROM
			(SELECT tgname AS name,pg_get_triggerdef(oid) AS definition FROM pg_trigger
			 WHERE NOT tgisinternal AND tgrelid::regclass::text LIKE 'qq_%') t`,
	}
	for _, table := range []string{"affiliate_promotion_hosts", "qq_image_configs", "qq_image_grants", "qq_image_orders",
		"qq_image_holds", "qq_image_ledger", "qq_image_discoveries", "qq_reply_template_revisions", "qq_reply_template_state"} {
		queries[table] = "SELECT jsonb_agg(to_jsonb(t) ORDER BY to_jsonb(t)::text)::text FROM " + pq.QuoteIdentifier(table) + " t"
	}
	before := map[string]string{}
	for name, query := range queries {
		before[name] = upstreamAC1B8F1Snapshot(t, ctx, db, query)
	}

	require.NoError(t, ApplyMigrations(ctx, db))
	for _, platform := range []string{"gemini", "antigravity", "composite", "openai"} {
		for _, original := range []bool{false, true} {
			var enabled bool
			require.NoError(t, db.QueryRowContext(ctx, "SELECT allow_image_generation FROM groups WHERE name=$1",
				fmt.Sprintf("upgrade-%s-%t", platform, original)).Scan(&enabled))
			require.Equal(t, original || platform == "gemini" || platform == "antigravity", enabled, "%s originally %t", platform, original)
		}
	}
	for name, query := range queries {
		require.JSONEq(t, before[name], upstreamAC1B8F1Snapshot(t, ctx, db, query), name)
	}
	afterHistory := upstreamAC1B8F1History(t, ctx, db)
	upstreamAC1B8F1AssertHistory(t, afterHistory)
	for name, entry := range beforeHistory {
		require.Equal(t, entry, afterHistory[name], "old checksum and application time must not change: %s", name)
	}
	// Exactly one added filename follows from unchanged 305 entries plus the
	// asserted 306-entry history containing the new migration.
	_, err := db.ExecContext(ctx, "UPDATE groups SET allow_image_generation=false WHERE name='upgrade-gemini-false'")
	require.NoError(t, err)
	groupsAfterChoice := upstreamAC1B8F1Snapshot(t, ctx, db, "SELECT jsonb_agg(to_jsonb(t) ORDER BY id)::text FROM groups t")
	require.NoError(t, ApplyMigrations(ctx, db))
	require.Equal(t, afterHistory, upstreamAC1B8F1History(t, ctx, db), "repeated startup must not rewrite migration history")
	require.JSONEq(t, groupsAfterChoice, upstreamAC1B8F1Snapshot(t, ctx, db, "SELECT jsonb_agg(to_jsonb(t) ORDER BY id)::text FROM groups t"), "do not re-enable a choice made after the upgrade")
	for name, query := range queries {
		require.JSONEq(t, before[name], upstreamAC1B8F1Snapshot(t, ctx, db, query), name)
	}
}

func TestUpstreamAC1B8F1MigrationFreshInstall(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	db := upstreamAC1B8F1TestDatabase(t, ctx)
	require.NoError(t, ApplyMigrations(ctx, db))
	history := upstreamAC1B8F1History(t, ctx, db)
	upstreamAC1B8F1AssertHistory(t, history)
	for _, table := range []string{"affiliate_promotion_hosts", "qq_image_orders", "qq_image_ledger", "qq_image_discoveries", "qq_reply_template_revisions"} {
		var exists bool
		require.NoError(t, db.QueryRowContext(ctx, "SELECT to_regclass($1) IS NOT NULL", "public."+table).Scan(&exists))
		require.True(t, exists, table)
	}
	var customColumns int
	require.NoError(t, db.QueryRowContext(ctx, `SELECT count(*) FROM information_schema.columns WHERE table_schema='public'
		AND ((table_name='groups' AND column_name IN ('force_openai_ultrafast','allow_subscription_day_reset','i18n'))
		OR (table_name='users' AND column_name IN ('balance','frozen_balance')))`).Scan(&customColumns))
	require.Equal(t, 5, customColumns)
	require.NoError(t, ApplyMigrations(ctx, db))
	require.Equal(t, history, upstreamAC1B8F1History(t, ctx, db))
}
