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

// Reproduce the deployed custom v0.2.7 migration history, including custom 239
// and 240 filenames, before adding the official v0.2.8 files with those prefixes.
func TestSubscriptionDailyResetUpgradeV028_PreservesDeployedCustomStateAndHistory(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	container, err := tcpostgres.Run(ctx, selectDockerImage(ctx, postgresImageTag),
		tcpostgres.WithDatabase("custom_upgrade_v028"), tcpostgres.WithUsername("postgres"),
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

	const moderationMigration = "238b_content_moderation_engine_meta.sql"
	const pricingMigration = "239_channel_reasoning_effort_multipliers.sql"
	const affiliateMigration = "240_affiliate_ledger_operation_id.sql"
	previous := fstest.MapFS{}
	baseline := sha256.New()
	files, err := fs.Glob(migrations.FS, "*.sql")
	require.NoError(t, err)
	for _, name := range files {
		if name == moderationMigration || name == pricingMigration || name == affiliateMigration {
			continue
		}
		contents, readErr := migrations.FS.ReadFile(name)
		require.NoError(t, readErr)
		previous[name] = &fstest.MapFile{Data: contents}
		_, err = fmt.Fprintf(baseline, "%s\x00%s\x00", name, strings.TrimSpace(string(contents)))
		require.NoError(t, err)
	}
	// Pin the actual deployed custom baseline, so an edited historical SQL file
	// cannot silently replace the fixture or its expected recorded checksum.
	require.Len(t, previous, 289)
	require.Equal(t, "47bd915b3a9b9baf8b145c432617b26082f138dcc34170ccfeabdd4079484648",
		fmt.Sprintf("%x", baseline.Sum(nil)), "custom 713d2852e migration fixture changed; re-audit the deployed baseline")
	require.NoError(t, applyMigrationsFS(ctx, db, previous))

	var userID, groupID, subscriptionID, orderID int64
	require.NoError(t, db.QueryRowContext(ctx, `INSERT INTO users
		(email,password_hash,balance,status) VALUES ('custom-upgrade-v028@example.com','existing-hash',123.45,'active')
		RETURNING id`).Scan(&userID))
	require.NoError(t, db.QueryRowContext(ctx, `INSERT INTO groups
		(name,status,subscription_type,daily_limit_usd,weekly_limit_usd,monthly_limit_usd,allow_subscription_day_reset,model_pricing)
		VALUES ('custom-upgrade-v028','active','subscription',100,1000,5000,true,
		'[{"model":"gpt-5.5","max_reasoning_effort_multiplier":1.75}]') RETURNING id`).Scan(&groupID))
	require.NoError(t, db.QueryRowContext(ctx, `INSERT INTO user_subscriptions
		(user_id,group_id,starts_at,expires_at,status,daily_window_start,weekly_window_start,monthly_window_start,
		daily_usage_usd,weekly_usage_usd,monthly_usage_usd,auto_daily_reset_enabled,daily_reset_version,preserve_calendar_daily_reset)
		VALUES ($1,$2,now()-interval '1 day',now()+interval '29 days','active',date_trunc('day',now()),now(),now(),
		12.5,200.5,800.5,true,1,true) RETURNING id`, userID, groupID).Scan(&subscriptionID))
	_, err = db.ExecContext(ctx, `INSERT INTO subscription_daily_reset_events
		(subscription_id,user_id,group_id,source,operation_id,request_fingerprint,before_version,after_version,
		timezone,count_date,day_sequence,before_daily_usage_usd,daily_limit_usd,before_expires_at,after_expires_at,decided_at)
		SELECT id,user_id,group_id,'automatic','existing-reset-operation','existing-reset-fingerprint',0,1,
		'Asia/Shanghai',(now() AT TIME ZONE 'Asia/Shanghai')::date,1,100,100,expires_at+interval '1 day',expires_at,now()
		FROM user_subscriptions WHERE id=$1`, subscriptionID)
	require.NoError(t, err)
	_, err = db.ExecContext(ctx, `INSERT INTO api_keys (user_id,group_id,key,name,status)
		VALUES ($1,$2,'sk-custom-upgrade-v028-existing-key','Existing key','active')`, userID, groupID)
	require.NoError(t, err)
	require.NoError(t, db.QueryRowContext(ctx, `INSERT INTO payment_orders
		(user_id,amount,pay_amount,order_type,status,paid_at,completed_at,expires_at,out_trade_no,provider_snapshot)
		VALUES($1,7,100,'subscription','COMPLETED',now(),now(),now()+interval '1 hour','custom-upgrade-v028-order',
		'{"currency":"CNY"}') RETURNING id`, userID).Scan(&orderID))
	_, err = db.ExecContext(ctx, `UPDATE welfare_programs SET enabled=true,launch_at=now()-interval '30 days' WHERE id=1`)
	require.NoError(t, err)
	_, err = db.ExecContext(ctx, `INSERT INTO welfare_wallets
		(user_id,balance_cents,total_earned_cents,total_redeemed_cents,total_checkin_days,cycle_id,cycle_day,
		last_checkin_date,eligible_spend,draws_used,subscription_draws,wallet_version,welfare_balance_version)
		VALUES($1,125,125,0,1,1,1,(now() AT TIME ZONE 'Asia/Shanghai')::date,75,0,2,4,0)`, userID)
	require.NoError(t, err)
	const operationID = "e79d387c-ec4d-4dd1-8f8d-02822d43c0a1"
	_, err = db.ExecContext(ctx, `INSERT INTO welfare_operations
		(id,user_id,type,idempotency_key,request_fingerprint,result,private_audit)
		VALUES($1,$2,'checkin','existing-checkin','existing-checkin-fingerprint',
		'{"balance_cents":125}', '{"daily_cents":125}')`, operationID, userID)
	require.NoError(t, err)
	_, err = db.ExecContext(ctx, `INSERT INTO welfare_checkins
		(user_id,business_date,operation_id,daily_cents,cycle_id,cycle_day)
		VALUES($1,(now() AT TIME ZONE 'Asia/Shanghai')::date,$2,125,1,1)`, userID, operationID)
	require.NoError(t, err)
	_, err = db.ExecContext(ctx, `INSERT INTO welfare_ledger
		(user_id,operation_id,type,amount_cents,balance_after_cents,business_date)
		VALUES($1,$2,'daily',125,125,(now() AT TIME ZONE 'Asia/Shanghai')::date)`, userID, operationID)
	require.NoError(t, err)
	_, err = db.ExecContext(ctx, `INSERT INTO welfare_spend_events
		(user_id,source_type,source_id,action,amount,ticket_delta) VALUES($1,'usage','existing-usage','debit',75,1)`, userID)
	require.NoError(t, err)
	_, err = db.ExecContext(ctx, `INSERT INTO welfare_subscription_rewards
		(order_id,user_id,order_amount,paid_amount_cny,paid_at,granted_draws)
		SELECT id,user_id,amount,pay_amount,paid_at,2 FROM payment_orders WHERE id=$1`, orderID)
	require.NoError(t, err)
	_, err = db.ExecContext(ctx, `INSERT INTO welfare_balance_outbox (user_id,version,attempts,last_error)
		VALUES($1,4,1,'previous delivery retry')`, userID)
	require.NoError(t, err)
	_, err = db.ExecContext(ctx, `INSERT INTO content_moderation_logs (request_id,user_id,group_id,action)
		VALUES('existing-moderation-request',$1,$2,'allow')`, userID, groupID)
	require.NoError(t, err)
	_, err = db.ExecContext(ctx, `INSERT INTO user_affiliate_ledger (user_id,action,amount,source_order_id)
		VALUES($1,'accrue',5,$2)`, userID, orderID)
	require.NoError(t, err)

	snapshot := func(query string) string {
		t.Helper()
		var value string
		require.NoError(t, db.QueryRowContext(ctx, query).Scan(&value))
		return value
	}
	queries := map[string]string{
		"users":                "SELECT jsonb_agg(to_jsonb(t) ORDER BY id)::text FROM users t",
		"groups":               "SELECT jsonb_agg(to_jsonb(t)-'model_pricing' ORDER BY id)::text FROM groups t",
		"subscriptions":        "SELECT jsonb_agg(to_jsonb(t) ORDER BY id)::text FROM user_subscriptions t",
		"reset_events":         "SELECT jsonb_agg(to_jsonb(t) ORDER BY id)::text FROM subscription_daily_reset_events t",
		"keys":                 "SELECT jsonb_agg(to_jsonb(t) ORDER BY id)::text FROM api_keys t",
		"orders":               "SELECT jsonb_agg(to_jsonb(t) ORDER BY id)::text FROM payment_orders t",
		"programs":             "SELECT jsonb_agg(to_jsonb(t) ORDER BY id)::text FROM welfare_programs t",
		"wallets":              "SELECT jsonb_agg(to_jsonb(t) ORDER BY user_id)::text FROM welfare_wallets t",
		"operations":           "SELECT jsonb_agg(to_jsonb(t) ORDER BY id)::text FROM welfare_operations t",
		"checkins":             "SELECT jsonb_agg(to_jsonb(t) ORDER BY id)::text FROM welfare_checkins t",
		"ledger":               "SELECT jsonb_agg(to_jsonb(t) ORDER BY id)::text FROM welfare_ledger t",
		"spend_events":         "SELECT jsonb_agg(to_jsonb(t) ORDER BY id)::text FROM welfare_spend_events t",
		"subscription_rewards": "SELECT jsonb_agg(to_jsonb(t) ORDER BY order_id)::text FROM welfare_subscription_rewards t",
		"outbox":               "SELECT jsonb_agg(to_jsonb(t) ORDER BY id)::text FROM welfare_balance_outbox t",
		"moderation":           "SELECT jsonb_agg(to_jsonb(t)-'engine_meta' ORDER BY id)::text FROM content_moderation_logs t",
		"affiliate_ledger":     "SELECT jsonb_agg(to_jsonb(t)-'operation_id' ORDER BY id)::text FROM user_affiliate_ledger t",
		"migration_history":    "SELECT jsonb_agg(to_jsonb(t) ORDER BY filename)::text FROM schema_migrations t WHERE filename NOT IN ('238b_content_moderation_engine_meta.sql','239_channel_reasoning_effort_multipliers.sql','240_affiliate_ledger_operation_id.sql')",
	}
	before := map[string]string{}
	for name, query := range queries {
		before[name] = snapshot(query)
	}

	require.NoError(t, ApplyMigrations(ctx, db))
	for name, query := range queries {
		require.JSONEq(t, before[name], snapshot(query), "%s must survive the deployed custom upgrade unchanged", name)
	}
	// The official pricing migration still consumes the old custom multiplier.
	require.JSONEq(t, `[{"model":"gpt-5.5","reasoning_effort_multipliers":{"max":1.75}}]`,
		snapshot("SELECT model_pricing::text FROM groups WHERE name='custom-upgrade-v028'"))
	require.Equal(t, "1", snapshot("SELECT count(*)::text FROM content_moderation_logs WHERE engine_meta IS NULL"))
	require.Equal(t, "1", snapshot("SELECT count(*)::text FROM user_affiliate_ledger WHERE operation_id IS NULL"))
	require.Equal(t, "292", snapshot("SELECT count(*)::text FROM schema_migrations"))
	for _, name := range files {
		contents, readErr := migrations.FS.ReadFile(name)
		require.NoError(t, readErr)
		var checksum string
		require.NoError(t, db.QueryRowContext(ctx, "SELECT checksum FROM schema_migrations WHERE filename=$1", name).Scan(&checksum))
		require.Equal(t, fmt.Sprintf("%x", sha256.Sum256([]byte(strings.TrimSpace(string(contents))))), checksum, "%s", name)
	}

	// A later startup preserves both the old history and all newly recorded rows.
	queries["groups"] = "SELECT jsonb_agg(to_jsonb(t) ORDER BY id)::text FROM groups t"
	queries["moderation"] = "SELECT jsonb_agg(to_jsonb(t) ORDER BY id)::text FROM content_moderation_logs t"
	queries["affiliate_ledger"] = "SELECT jsonb_agg(to_jsonb(t) ORDER BY id)::text FROM user_affiliate_ledger t"
	queries["migration_history"] = "SELECT jsonb_agg(to_jsonb(t) ORDER BY filename)::text FROM schema_migrations t"
	for name, query := range queries {
		before[name] = snapshot(query)
	}
	require.NoError(t, ApplyMigrations(ctx, db))
	for name, query := range queries {
		require.JSONEq(t, before[name], snapshot(query), "%s must survive the second startup unchanged", name)
	}
}
