package repository

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"fmt"
	"os"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/Wei-Shaw/sub2api/migrations"
	"github.com/lib/pq"
	"github.com/stretchr/testify/require"
)

var welfareSubscriptionUpgradeFiles = []string{
	"254_welfare_subscription_entitlements.sql",
	"255_qq_welfare_subscription_backfill.sql",
	"256_qq_welfare_backfill_no_resets.sql",
	"257_welfare_subscription_adjustments.sql",
}

func welfareSubscriptionUpgradeTarget(t *testing.T, count int) fstest.MapFS {
	t.Helper()
	target := upstreamV0221Target(t) // Immutable published r1 baseline: 314 files.
	for _, name := range welfareSubscriptionUpgradeFiles[:count-314] {
		contents, err := migrations.FS.ReadFile(name)
		require.NoError(t, err)
		target[name] = &fstest.MapFile{Data: contents}
	}
	require.Len(t, target, count)
	return target
}

type welfareSubscriptionUpgradeRow struct {
	id     int64
	userID int64
	codes  []int64
}

// This fixture writes only to a newly created local test database. It never
// starts a QQ worker, contacts OneBot, sends messages, or redeems real codes.
func welfareSubscriptionUpgradeSeed(t *testing.T, ctx context.Context, db *sql.DB) map[string]welfareSubscriptionUpgradeRow {
	t.Helper()
	var groupID, campaignID int64
	require.NoError(t, db.QueryRowContext(ctx, `INSERT INTO groups
		(name,platform,subscription_type,daily_limit_usd,weekly_limit_usd,monthly_limit_usd)
		VALUES ('welfare-upgrade','openai','subscription',10,40,100) RETURNING id`).Scan(&groupID))
	require.NoError(t, db.QueryRowContext(ctx, `INSERT INTO qq_invite_campaigns(payload)
		VALUES ('{"enabled":false}') RETURNING id`).Scan(&campaignID))
	_, err := db.ExecContext(ctx, `INSERT INTO qq_invite_campaign_versions(campaign_id,version,payload,actor_id)
		VALUES ($1,1,'{"enabled":false}',0)`, campaignID)
	require.NoError(t, err)
	result := map[string]welfareSubscriptionUpgradeRow{}
	start := time.Now().UTC().Truncate(time.Microsecond).Add(-48 * time.Hour)
	for _, name := range []string{"pure", "auto-only", "paid", "ordinary-code", "reset-used", "standard"} {
		row := welfareSubscriptionUpgradeRow{}
		require.NoError(t, db.QueryRowContext(ctx, `INSERT INTO users(email,password_hash)
			VALUES ($1,'migration-fixture-only') RETURNING id`, name+"@welfare-upgrade.test").Scan(&row.userID))
		var notes []string
		if name != "standard" {
			for number := range 2 {
				var codeID, recordID, poolID int64
				require.NoError(t, db.QueryRowContext(ctx, `INSERT INTO redeem_codes
					(code,type,value,status,group_id,validity_days) VALUES ($1,'subscription',0,'unused',$2,30) RETURNING id`,
					fmt.Sprintf("UPGRADE-%s-%d", name, number), groupID).Scan(&codeID))
				require.NoError(t, db.QueryRowContext(ctx, `INSERT INTO qq_invite_records
					(campaign_id,campaign_version,snapshot,self_id,group_id,invitee_qq,inviter_qq,status,event_time)
					VALUES ($1,1,'{}','10000','20000',$2,'30000','awarded',$3) RETURNING id`,
					campaignID, fmt.Sprint(codeID), start).Scan(&recordID))
				require.NoError(t, db.QueryRowContext(ctx, `INSERT INTO qq_invite_code_pool
					(campaign_id,redeem_code_id,record_id) VALUES ($1,$2,$3) RETURNING id`,
					campaignID, codeID, recordID).Scan(&poolID))
				_, err = db.ExecContext(ctx, `INSERT INTO qq_invite_rewards(record_id,pool_id) VALUES ($1,$2)`, recordID, poolID)
				require.NoError(t, err)
				_, err = db.ExecContext(ctx, `UPDATE redeem_codes SET status='used',used_by=$2,used_at=$3 WHERE id=$1`,
					codeID, row.userID, start.Add(time.Duration(number)*time.Hour))
				require.NoError(t, err)
				row.codes = append(row.codes, codeID)
				notes = append(notes, fmt.Sprintf("通过兑换码 #%d 兑换", codeID))
			}
		}
		require.NoError(t, db.QueryRowContext(ctx, `INSERT INTO user_subscriptions
			(user_id,group_id,starts_at,expires_at,status,daily_window_start,weekly_window_start,monthly_window_start,
			 daily_usage_usd,weekly_usage_usd,monthly_usage_usd,notes,auto_daily_reset_enabled,daily_reset_version)
			VALUES ($1,$2,$3,$3::timestamptz+interval '60 days','active',$3,$3,$3,
			 1.1234567890,12.2345678901,27.3456789012,$4,$5,7) RETURNING id`,
			row.userID, groupID, start, strings.Join(notes, "\n"), name == "auto-only" || name == "reset-used").Scan(&row.id))
		switch name {
		case "paid":
			_, err = db.ExecContext(ctx, `INSERT INTO payment_orders
				(user_id,amount,pay_amount,order_type,status,subscription_group_id,subscription_days,
				 expires_at,paid_at,completed_at,out_trade_no)
				VALUES ($1,10,70,'subscription','COMPLETED',$2,30,$3,$3,$3,'welfare-upgrade-paid')`, row.userID, groupID, start)
			require.NoError(t, err)
		case "ordinary-code":
			// Keep the subscription otherwise eligible: ordinary-code provenance
			// alone must exclude it even if historical notes omit that redemption.
			_, err = db.ExecContext(ctx, `INSERT INTO redeem_codes
				(code,type,value,status,group_id,validity_days,used_by,used_at)
				VALUES ('UPGRADE-ORDINARY','subscription',0,'used',$1,30,$2,$3)`, groupID, row.userID, start)
			require.NoError(t, err)
		case "reset-used":
			_, err = db.ExecContext(ctx, `INSERT INTO subscription_daily_reset_events
				(subscription_id,user_id,group_id,source,operation_id,request_fingerprint,before_version,after_version,
				 timezone,count_date,day_sequence,before_daily_usage_usd,daily_limit_usd,before_expires_at,after_expires_at,decided_at)
				VALUES ($1,$2,$3,'automatic','migration-reset','migration-reset',6,7,'UTC',$4::date,1,5,10,
				 $4::timestamptz+interval '61 days',$4::timestamptz+interval '60 days',$4)`, row.id, row.userID, groupID, start)
			require.NoError(t, err)
		}
		result[name] = row
	}
	return result
}

func welfareSubscriptionUpgradeRowSnapshot(t *testing.T, ctx context.Context, db *sql.DB, id int64, welfare bool) string {
	t.Helper()
	// Compare all pre-existing fields, except the narrowly intended welfare
	// changes. Standard rows must retain expiry/status/reset settings verbatim.
	expression := `to_jsonb(s)-'entitlement_kind'-'welfare_daily_limit_usd'-'welfare_weekly_limit_usd'
		-'welfare_monthly_limit_usd'-'welfare_grant_count'-'welfare_cycle_closed'`
	if welfare {
		expression += `-'expires_at'-'status'-'auto_daily_reset_enabled'-'updated_at'`
	}
	var result string
	require.NoError(t, db.QueryRowContext(ctx, "SELECT ("+expression+")::text FROM user_subscriptions s WHERE id=$1", id).Scan(&result))
	return result
}

func welfareSubscriptionUpgradeTables(t *testing.T, ctx context.Context, db *sql.DB, tables []string) map[string]string {
	t.Helper()
	result := map[string]string{}
	for _, table := range tables {
		result[table] = upstreamAC1B8F1Snapshot(t, ctx, db, `SELECT COALESCE(jsonb_agg(to_jsonb(s) ORDER BY to_jsonb(s)::text),'[]'::jsonb)::text FROM `+pq.QuoteIdentifier(table)+" s")
	}
	return result
}

func TestWelfareSubscriptionUpgradeMigrations(t *testing.T) {
	dsn := os.Getenv("QQ_INVITE_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("QQ_INVITE_TEST_DATABASE_URL is required for real PostgreSQL welfare upgrade tests")
	}
	// Reuse the existing harness's strict loopback URL validation, CREATEDB,
	// random database name and cleanup. The supplied database is never migrated.
	t.Setenv("QQ_IMAGE_TEST_DATABASE_URL", dsn)
	for _, initialCount := range []int{314, 317} {
		t.Run(fmt.Sprintf("%d_to_318", initialCount), func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
			defer cancel()
			db := upstreamAC1B8F1TestDatabase(t, ctx)
			require.NoError(t, applyMigrationsFS(ctx, db, upstreamV0221Target(t)))
			rows := welfareSubscriptionUpgradeSeed(t, ctx, db)
			originalHistory := upstreamAC1B8F1History(t, ctx, db)
			require.Len(t, originalHistory, 314)
			before := map[string]string{}
			for name, row := range rows {
				before[name] = welfareSubscriptionUpgradeRowSnapshot(t, ctx, db, row.id, name == "pure" || name == "auto-only")
			}
			preservedTables := []string{"users", "groups", "redeem_codes", "payment_orders", "qq_invite_campaigns",
				"qq_invite_campaign_versions", "qq_invite_records", "qq_invite_code_pool", "qq_invite_rewards", "subscription_daily_reset_events"}
			preserved := welfareSubscriptionUpgradeTables(t, ctx, db, preservedTables)
			if initialCount == 317 {
				require.NoError(t, applyMigrationsFS(ctx, db, welfareSubscriptionUpgradeTarget(t, 316)))
				var kind string
				require.NoError(t, db.QueryRowContext(ctx, `SELECT entitlement_kind FROM user_subscriptions WHERE id=$1`, rows["auto-only"].id).Scan(&kind))
				require.Equal(t, "standard", kind, "255 leaves auto-reset-enabled historical rows for the 256 follow-up")
				require.NoError(t, applyMigrationsFS(ctx, db, welfareSubscriptionUpgradeTarget(t, 317)))
				_, err := db.ExecContext(ctx, `UPDATE user_subscriptions SET welfare_monthly_limit_usd=0 WHERE id=$1`, rows["pure"].id)
				require.ErrorContains(t, err, "user_subscriptions_welfare_limits_check", "before 257, zero welfare total is forbidden")
			}
			baselineHistory := upstreamAC1B8F1History(t, ctx, db)
			require.Len(t, baselineHistory, initialCount)
			target := welfareSubscriptionUpgradeTarget(t, 318)
			require.NoError(t, applyMigrationsFS(ctx, db, target))
			afterHistory := upstreamAC1B8F1History(t, ctx, db)
			require.Len(t, afterHistory, 318)
			for name, entry := range originalHistory {
				require.Equal(t, entry, afterHistory[name], "preserve original r1 checksum and applied_at: %s", name)
			}
			for name, entry := range baselineHistory {
				require.Equal(t, entry, afterHistory[name], "preserve upgrade baseline checksum and applied_at: %s", name)
			}
			for name, file := range target {
				require.Equal(t, fmt.Sprintf("%x", sha256.Sum256([]byte(strings.TrimSpace(string(file.Data))))), afterHistory[name].Checksum, name)
			}
			for name, row := range rows {
				isWelfare := name == "pure" || name == "auto-only"
				require.JSONEq(t, before[name], welfareSubscriptionUpgradeRowSnapshot(t, ctx, db, row.id, isWelfare), "preserve %s consumption, identity, windows and standard term", name)
				var kind string
				var grants int
				require.NoError(t, db.QueryRowContext(ctx, `SELECT entitlement_kind,welfare_grant_count FROM user_subscriptions WHERE id=$1`, row.id).Scan(&kind, &grants))
				if !isWelfare {
					require.Equal(t, "standard", kind, name)
					require.Zero(t, grants, name)
					continue
				}
				require.Equal(t, "welfare", kind, name)
				require.Equal(t, 2, grants, name)
				var valid bool
				require.NoError(t, db.QueryRowContext(ctx, `SELECT welfare_daily_limit_usd=20 AND welfare_weekly_limit_usd=80
					AND welfare_monthly_limit_usd=200 AND expires_at=starts_at+interval '30 days'
					AND status='active' AND NOT auto_daily_reset_enabled AND NOT welfare_cycle_closed
					FROM user_subscriptions WHERE id=$1`, row.id).Scan(&valid))
				require.True(t, valid, name)
				var receipts int
				require.NoError(t, db.QueryRowContext(ctx, `SELECT count(*) FROM user_welfare_subscription_grants g
					JOIN redeem_codes c ON c.id=g.redeem_code_id WHERE g.subscription_id=$1 AND c.id=ANY($2)
					AND g.daily_limit_usd=10 AND g.weekly_limit_usd=40 AND g.monthly_limit_usd=100 AND g.granted_at=c.used_at`,
					row.id, pq.Array(row.codes)).Scan(&receipts))
				require.Equal(t, 2, receipts, name)
			}
			require.Equal(t, preserved, welfareSubscriptionUpgradeTables(t, ctx, db, preservedTables))
			var receiptCount int
			require.NoError(t, db.QueryRowContext(ctx, `SELECT count(*) FROM user_welfare_subscription_grants`).Scan(&receiptCount))
			require.Equal(t, 4, receiptCount, "no grants for ambiguous, paid, ordinary-code or reset-used rows")

			// Exercise the real 257 constraint and durable audit table together.
			// This is migration storage validation, not a real management action.
			row := rows["pure"]
			tx, err := db.BeginTx(ctx, nil)
			require.NoError(t, err)
			defer func() { _ = tx.Rollback() }()
			_, err = tx.ExecContext(ctx, `INSERT INTO user_welfare_subscription_adjustments
				(subscription_id,user_id,group_id,actor_id,actor_key_id,reason,before_monthly_limit_usd,after_monthly_limit_usd,
				 before_expires_at,after_expires_at,before_status,after_status,before_version,after_version,welfare_grant_count)
				SELECT id,user_id,group_id,0,'migration-test','zero-total migration storage check',welfare_monthly_limit_usd,0,
				 expires_at,expires_at,status,status,daily_reset_version,daily_reset_version+1,welfare_grant_count
				FROM user_subscriptions WHERE id=$1`, row.id)
			require.NoError(t, err)
			_, err = tx.ExecContext(ctx, `UPDATE user_subscriptions SET welfare_monthly_limit_usd=0,daily_reset_version=daily_reset_version+1 WHERE id=$1`, row.id)
			require.NoError(t, err)
			require.NoError(t, tx.Commit())
			var zeroAndAudited bool
			require.NoError(t, db.QueryRowContext(ctx, `SELECT s.welfare_monthly_limit_usd=0 AND s.monthly_usage_usd=27.3456789012
				AND a.before_monthly_limit_usd=200 AND a.after_monthly_limit_usd=0
				AND a.before_version=7 AND a.after_version=8 AND a.welfare_grant_count=2
				FROM user_subscriptions s JOIN user_welfare_subscription_adjustments a ON a.subscription_id=s.id WHERE s.id=$1`, row.id).Scan(&zeroAndAudited))
			require.True(t, zeroAndAudited)
			allTables := append(append([]string{}, preservedTables...), "user_subscriptions", "user_welfare_subscription_grants", "user_welfare_subscription_adjustments")
			after := welfareSubscriptionUpgradeTables(t, ctx, db, allTables)
			for range 2 {
				require.NoError(t, applyMigrationsFS(ctx, db, target))
				require.Equal(t, afterHistory, upstreamAC1B8F1History(t, ctx, db))
				require.Equal(t, after, welfareSubscriptionUpgradeTables(t, ctx, db, allTables), "restart preserves zero quota, audit, usage and all historical rows")
			}
		})
	}
}
