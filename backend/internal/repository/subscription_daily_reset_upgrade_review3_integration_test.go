//go:build integration

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
	"github.com/stretchr/testify/require"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"
)

// Start from the upstream migration set, with existing customer data, rather
// than merely checking that the customized schema installs into an empty DB.
// All upstream SQL files are unchanged relative to upstream a3eb7ef302 (0.2.8).
func TestSubscriptionDailyResetUpgradeReview3_ExistingUpstreamDataAndMigrationHistory(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	container, err := tcpostgres.Run(ctx, selectDockerImage(ctx, postgresImageTag),
		tcpostgres.WithDatabase("upgrade_review3"), tcpostgres.WithUsername("postgres"),
		tcpostgres.WithPassword("postgres"), tcpostgres.BasicWaitStrategies())
	require.NoError(t, err)
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cleanupCancel()
		_ = container.Terminate(cleanupCtx)
	})
	dsn, err := container.ConnectionString(ctx, "sslmode=disable", "TimeZone=UTC")
	require.NoError(t, err)
	db, err := sql.Open("postgres", dsn)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	const customMigration = "235_subscription_daily_reset.sql"
	const welfareMigration = "239_welfare_center.sql"
	const subscriptionRewardsMigration = "240_welfare_subscription_rewards.sql"
	upstream := fstest.MapFS{}
	baseline := sha256.New()
	files, err := fs.Glob(migrations.FS, "*.sql")
	require.NoError(t, err)
	for _, name := range files {
		if name == customMigration || name == welfareMigration || name == subscriptionRewardsMigration {
			continue
		}
		contents, readErr := migrations.FS.ReadFile(name)
		require.NoError(t, readErr)
		upstream[name] = &fstest.MapFile{Data: contents}
		_, err = fmt.Fprintf(baseline, "%s\x00%s\x00", name, strings.TrimSpace(string(contents)))
		require.NoError(t, err)
	}
	// Pin the fixture to the audited upstream commit. Future upstream merges must
	// explicitly refresh this baseline instead of silently changing what we test.
	require.Len(t, upstream, 289)
	require.Equal(t, "6075250885f45555d7671005dc80db75c8848e9066a0cc3d291cd36a80c30947",
		fmt.Sprintf("%x", baseline.Sum(nil)), "upstream a3eb7ef302 migration fixture changed; re-audit the upgrade baseline")
	require.NoError(t, applyMigrationsFS(ctx, db, upstream))

	// These are old-schema writes: none mentions a customized field.
	var userID, groupID, subscriptionID int64
	require.NoError(t, db.QueryRowContext(ctx, `INSERT INTO users
		(email,password_hash,balance,status) VALUES ('upgrade-review3@example.com','existing-hash',123.45,'active') RETURNING id`).Scan(&userID))
	require.NoError(t, db.QueryRowContext(ctx, `INSERT INTO groups
		(name,status,subscription_type,daily_limit_usd,weekly_limit_usd,monthly_limit_usd,model_allowlist)
		VALUES ('upgrade-review3','active','subscription',100,1000,5000,'{"enabled":true,"models":["gpt-5.5"]}') RETURNING id`).Scan(&groupID))
	require.NoError(t, db.QueryRowContext(ctx, `INSERT INTO user_subscriptions
		(user_id,group_id,starts_at,expires_at,status,daily_window_start,weekly_window_start,monthly_window_start,
		daily_usage_usd,weekly_usage_usd,monthly_usage_usd)
		VALUES ($1,$2,now()-interval '1 day',now()+interval '30 days','active',date_trunc('day',now()),now(),now(),42.5,200.5,800.5)
		RETURNING id`, userID, groupID).Scan(&subscriptionID))
	_, err = db.ExecContext(ctx, `INSERT INTO api_keys (user_id,group_id,key,name,status)
		VALUES ($1,$2,'sk-upgrade-review3-existing-key','Existing key','active')`, userID, groupID)
	require.NoError(t, err)

	snapshot := func(query string) string {
		t.Helper()
		var value string
		require.NoError(t, db.QueryRowContext(ctx, query).Scan(&value))
		return value
	}
	queries := map[string]string{
		"users":             "SELECT jsonb_agg(to_jsonb(t) ORDER BY id)::text FROM users t",
		"groups":            "SELECT jsonb_agg(to_jsonb(t)-'allow_subscription_day_reset' ORDER BY id)::text FROM groups t",
		"subscriptions":     "SELECT jsonb_agg(to_jsonb(t)-ARRAY['auto_daily_reset_enabled','daily_reset_version','preserve_calendar_daily_reset'] ORDER BY id)::text FROM user_subscriptions t",
		"keys":              "SELECT jsonb_agg(to_jsonb(t) ORDER BY id)::text FROM api_keys t",
		"migration_history": "SELECT jsonb_agg(to_jsonb(t) ORDER BY filename)::text FROM schema_migrations t WHERE filename NOT IN ('235_subscription_daily_reset.sql', '239_welfare_center.sql', '240_welfare_subscription_rewards.sql')",
	}
	before := map[string]string{}
	for name, query := range queries {
		before[name] = snapshot(query)
	}

	require.NoError(t, ApplyMigrations(ctx, db))
	for name, query := range queries {
		require.JSONEq(t, before[name], snapshot(query), "%s must survive upgrade unchanged", name)
	}
	var allowed, automatic, calendar bool
	var version int64
	require.NoError(t, db.QueryRowContext(ctx, `SELECT g.allow_subscription_day_reset,
		s.auto_daily_reset_enabled,s.preserve_calendar_daily_reset,s.daily_reset_version
		FROM groups g JOIN user_subscriptions s ON s.group_id=g.id WHERE s.id=$1`, subscriptionID).
		Scan(&allowed, &automatic, &calendar, &version))
	require.False(t, allowed, "upgrading must not authorize paid resets")
	require.False(t, automatic, "upgrading must not opt customers into paid resets")
	require.False(t, calendar)
	require.Zero(t, version)
	require.Equal(t, "0", snapshot("SELECT count(*)::text FROM subscription_daily_reset_events"))
	var rewardsEnabled, hasLaunch bool
	require.NoError(t, db.QueryRowContext(ctx, `SELECT enabled,launch_at IS NOT NULL FROM welfare_programs WHERE id=1`).Scan(&rewardsEnabled, &hasLaunch))
	require.False(t, rewardsEnabled, "upgrading must not enable welfare awards")
	require.False(t, hasLaunch, "upgrading must not set the welfare spend boundary")
	require.Equal(t, "0", snapshot("SELECT count(*)::text FROM welfare_wallets"), "existing users must not receive synthetic welfare balances")
	require.Equal(t, "0", snapshot("SELECT count(*)::text FROM welfare_subscription_rewards"), "upgrading must not grant draws for historical purchases")

	// The numerical prefix is shared; both complete filenames must be recorded.
	for _, name := range []string{
		"235_group_model_allowlist.sql", customMigration,
		"239_channel_reasoning_effort_multipliers.sql", welfareMigration,
		"240_affiliate_ledger_operation_id.sql", subscriptionRewardsMigration,
	} {
		contents, readErr := migrations.FS.ReadFile(name)
		require.NoError(t, readErr)
		var checksum string
		require.NoError(t, db.QueryRowContext(ctx, "SELECT checksum FROM schema_migrations WHERE filename=$1", name).Scan(&checksum))
		require.Equal(t, fmt.Sprintf("%x", sha256.Sum256([]byte(strings.TrimSpace(string(contents))))), checksum)
	}

	// A second application/startup cannot overwrite user choices or rewrite history.
	_, err = db.ExecContext(ctx, "UPDATE groups SET allow_subscription_day_reset=true WHERE id=$1", groupID)
	require.NoError(t, err)
	_, err = db.ExecContext(ctx, "UPDATE user_subscriptions SET auto_daily_reset_enabled=true WHERE id=$1", subscriptionID)
	require.NoError(t, err)
	historyAfterUpgrade := snapshot("SELECT jsonb_agg(to_jsonb(t) ORDER BY filename)::text FROM schema_migrations t")
	require.NoError(t, ApplyMigrations(ctx, db))
	require.JSONEq(t, historyAfterUpgrade, snapshot("SELECT jsonb_agg(to_jsonb(t) ORDER BY filename)::text FROM schema_migrations t"))
	require.NoError(t, db.QueryRowContext(ctx, `SELECT g.allow_subscription_day_reset,s.auto_daily_reset_enabled
		FROM groups g JOIN user_subscriptions s ON s.group_id=g.id WHERE s.id=$1`, subscriptionID).Scan(&allowed, &automatic))
	require.True(t, allowed)
	require.True(t, automatic)
}
