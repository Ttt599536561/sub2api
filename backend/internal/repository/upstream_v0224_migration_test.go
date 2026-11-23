package repository

import (
	"context"
	"crypto/sha256"
	"fmt"
	"io/fs"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/Wei-Shaw/sub2api/migrations"
	"github.com/stretchr/testify/require"
)

func TestUpstreamV0224MigrationFromR7(t *testing.T) {
	target := welfareSubscriptionUpgradeTarget(t, 319)
	for _, name := range []string{"259_welfare_redeem_supplements.sql", "260_welfare_group_lifecycle_guards.sql", "261_subscription_supplement_balances.sql", "262_subscription_supplement_redemption.sql", "263_grok_video_billing_tasks.sql", "264_paid_subscription_monthly_renewals.sql"} {
		content, err := migrations.FS.ReadFile(name)
		require.NoError(t, err)
		target[name] = &fstest.MapFile{Data: content}
	}
	hash := sha256.New()
	names, err := fs.Glob(target, "*.sql")
	require.NoError(t, err)
	for _, name := range names {
		_, err := fmt.Fprintf(hash, "%s\x00%s\x00", name, strings.TrimSpace(string(target[name].Data)))
		require.NoError(t, err)
	}
	// Independent fingerprint from git archive of published r7 application 2a9189b34.
	require.Len(t, names, 325)
	require.Equal(t, "669647b388d2034be1595d595632dbf5bd4eadc0b67488082bac55668ebd8de5", fmt.Sprintf("%x", hash.Sum(nil)))
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	db := upstreamAC1B8F1TestDatabase(t, ctx)
	require.NoError(t, applyMigrationsFS(ctx, db, target))
	_, err = db.ExecContext(ctx, "INSERT INTO groups(name,platform,force_openai_ultrafast) VALUES ('r7-existing','openai',true)")
	require.NoError(t, err)
	const snapshot = "SELECT jsonb_agg(to_jsonb(g) ORDER BY id)::text FROM groups g"
	before := upstreamAC1B8F1Snapshot(t, ctx, db, snapshot)
	history := upstreamAC1B8F1History(t, ctx, db)
	for _, name := range []string{"251_group_force_openai_ultrafast.sql", "251_group_usage_rollup_nonblocking_build.sql", "252_channel_pricing_ultrafast_multiplier.sql"} {
		content, err := migrations.FS.ReadFile(name)
		require.NoError(t, err)
		target[name] = &fstest.MapFile{Data: content}
	}
	require.NoError(t, applyMigrationsFS(ctx, db, target))
	require.Equal(t, before, upstreamAC1B8F1Snapshot(t, ctx, db, snapshot))
	after := upstreamAC1B8F1History(t, ctx, db)
	require.Len(t, after, 328)
	for name, row := range history {
		require.Equal(t, row, after[name])
	}
	require.NoError(t, applyMigrationsFS(ctx, db, target))
	require.Equal(t, after, upstreamAC1B8F1History(t, ctx, db))
	// Upgrade an existing r1 rule: the old price is unknown, never inferred.
	var ruleID, channelID int64
	require.NoError(t, db.QueryRowContext(ctx, "INSERT INTO channels(name) VALUES ('r1-test') RETURNING id").Scan(&channelID))
	require.NoError(t, db.QueryRowContext(ctx, "INSERT INTO channel_account_stats_pricing_rules(channel_id) VALUES ($1) RETURNING id", channelID).Scan(&ruleID))
	_, err = db.ExecContext(ctx, `INSERT INTO channel_account_stats_model_pricing(rule_id, models, input_price) VALUES ($1, '["test"]', 0.125)`, ruleID)
	require.NoError(t, err)
	const imagePriceMigration = "265_account_stats_image_input_price.sql"
	content, err := migrations.FS.ReadFile(imagePriceMigration)
	require.NoError(t, err)
	target[imagePriceMigration] = &fstest.MapFile{Data: content}
	require.NoError(t, applyMigrationsFS(ctx, db, target))
	var inherited bool
	require.NoError(t, db.QueryRowContext(ctx, "SELECT image_input_price IS NULL AND input_price = 0.125 FROM channel_account_stats_model_pricing WHERE rule_id = $1", ruleID).Scan(&inherited))
	require.True(t, inherited)
	updated := upstreamAC1B8F1History(t, ctx, db)
	require.Len(t, updated, 329)
	for name, row := range after {
		require.Equal(t, row, updated[name])
	}
	require.NoError(t, applyMigrationsFS(ctx, db, target))
	require.Equal(t, updated, upstreamAC1B8F1History(t, ctx, db))
}
