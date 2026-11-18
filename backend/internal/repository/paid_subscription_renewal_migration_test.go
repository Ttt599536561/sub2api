package repository

import (
	"context"
	"os"
	"testing"
	"testing/fstest"
	"time"

	"github.com/Wei-Shaw/sub2api/migrations"
	"github.com/stretchr/testify/require"
)

func TestPaidSubscriptionRenewalMigrationPreservesExistingTerms(t *testing.T) {
	if dsn := os.Getenv("QQ_INVITE_TEST_DATABASE_URL"); dsn != "" {
		t.Setenv("QQ_IMAGE_TEST_DATABASE_URL", dsn)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	db := upstreamAC1B8F1TestDatabase(t, ctx)
	target := welfareSubscriptionUpgradeTarget(t, 319)
	for _, name := range []string{"259_welfare_redeem_supplements.sql", "260_welfare_group_lifecycle_guards.sql", "261_subscription_supplement_balances.sql", "262_subscription_supplement_redemption.sql", "263_grok_video_billing_tasks.sql"} {
		contents, err := migrations.FS.ReadFile(name)
		require.NoError(t, err)
		target[name] = &fstest.MapFile{Data: contents}
	}
	require.NoError(t, applyMigrationsFS(ctx, db, target))
	var userID, groupID int64
	require.NoError(t, db.QueryRowContext(ctx, `INSERT INTO users(email,password_hash) VALUES('monthly-upgrade@test.invalid','fixture') RETURNING id`).Scan(&userID))
	require.NoError(t, db.QueryRowContext(ctx, `INSERT INTO groups(name,platform,subscription_type,monthly_limit_usd) VALUES('paid renewal migration','openai','subscription',1120) RETURNING id`).Scan(&groupID))
	_, err := db.ExecContext(ctx, `INSERT INTO user_subscriptions(user_id,group_id,entitlement_kind,starts_at,expires_at,welfare_monthly_limit_usd,welfare_grant_count,monthly_usage_usd)
 VALUES($1,$2,'standard',now()-interval '4 days',now()+interval '26 days',NULL,0,820),
 ($1,$2,'welfare',now()-interval '9 days',now()+interval '21 days',100,1,37)`, userID, groupID)
	require.NoError(t, err)
	const originalTerms = `SELECT jsonb_agg(to_jsonb(s)-'paid_monthly_limit_usd'-'paid_monthly_legacy_limit_usd'-'paid_monthly_legacy_expires_at' ORDER BY id)::text FROM user_subscriptions s`
	before := upstreamAC1B8F1Snapshot(t, ctx, db, originalTerms)
	oldHistory := upstreamAC1B8F1History(t, ctx, db)
	const migrationName = "264_paid_subscription_monthly_renewals.sql"
	contents, err := migrations.FS.ReadFile(migrationName)
	require.NoError(t, err)
	target[migrationName] = &fstest.MapFile{Data: contents}
	require.NoError(t, applyMigrationsFS(ctx, db, target))
	require.Equal(t, before, upstreamAC1B8F1Snapshot(t, ctx, db, originalTerms))
	var unmarked, receipts int
	require.NoError(t, db.QueryRowContext(ctx, `SELECT (SELECT count(*) FROM user_subscriptions WHERE paid_monthly_limit_usd IS NULL), (SELECT count(*) FROM paid_subscription_monthly_renewals)`).Scan(&unmarked, &receipts))
	require.Equal(t, 2, unmarked)
	require.Zero(t, receipts)
	// A signed legacy duration adjustment can reduce the rollover horizon below
	// the term start while leaving freshly purchased days and quota available.
	_, err = db.ExecContext(ctx, `UPDATE user_subscriptions SET paid_monthly_limit_usd=40000000000,paid_monthly_legacy_limit_usd=20000000000,
 paid_monthly_legacy_expires_at=starts_at-interval '1 day' WHERE entitlement_kind='standard'`)
	require.NoError(t, err)
	after := upstreamAC1B8F1History(t, ctx, db)
	require.Len(t, after, len(oldHistory)+1)
	for name, entry := range oldHistory {
		require.Equal(t, entry, after[name])
	}
	require.NoError(t, applyMigrationsFS(ctx, db, target))
	require.Equal(t, after, upstreamAC1B8F1History(t, ctx, db))
}
