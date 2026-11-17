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

func TestSubscriptionSupplementUpgradePreservesUsedHistoryAndConvertsUnused(t *testing.T) {
	if dsn := os.Getenv("QQ_INVITE_TEST_DATABASE_URL"); dsn != "" {
		t.Setenv("QQ_IMAGE_TEST_DATABASE_URL", dsn)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	db := upstreamAC1B8F1TestDatabase(t, ctx)
	target := welfareSubscriptionUpgradeTarget(t, 319)
	for _, name := range []string{"259_welfare_redeem_supplements.sql", "260_welfare_group_lifecycle_guards.sql"} {
		contents, err := migrations.FS.ReadFile(name)
		require.NoError(t, err)
		target[name] = &fstest.MapFile{Data: contents}
	}
	require.NoError(t, applyMigrationsFS(ctx, db, target))
	var uid, gid, sid, campaign, usedCode int64
	require.NoError(t, db.QueryRowContext(ctx, `INSERT INTO users(email,password_hash) VALUES('generic-upgrade@test.invalid','fixture') RETURNING id`).Scan(&uid))
	require.NoError(t, db.QueryRowContext(ctx, `INSERT INTO groups(name,platform,subscription_type,monthly_limit_usd) VALUES('supplement','openai','subscription',100) RETURNING id`).Scan(&gid))
	require.NoError(t, db.QueryRowContext(ctx, `INSERT INTO user_subscriptions(user_id,group_id,starts_at,expires_at,entitlement_kind,welfare_monthly_limit_usd,welfare_grant_count,monthly_usage_usd)
 VALUES($1,$2,now()-interval '1 day',now()+interval '29 days','welfare',120,1,37) RETURNING id`, uid, gid).Scan(&sid))
	require.NoError(t, db.QueryRowContext(ctx, `INSERT INTO qq_invite_campaigns(payload) VALUES(jsonb_build_object('name','history','subscription_group_id',$1::bigint)) RETURNING id`, gid).Scan(&campaign))
	require.NoError(t, db.QueryRowContext(ctx, `INSERT INTO redeem_codes(code,type,value,status,used_by,used_at,group_id,subscription_effect,welfare_campaign_id,welfare_campaign_version,welfare_campaign_name,quota_amount_usd,validity_days)
 VALUES('USED-LEGACY','subscription',0,'used',$1,now(),$2,'add_quota',$3,1,'history',20,0) RETURNING id`, uid, gid, campaign).Scan(&usedCode))
	_, err := db.ExecContext(ctx, `INSERT INTO user_welfare_subscription_supplements(redeem_code_id,subscription_id,user_id,group_id,welfare_campaign_id,welfare_campaign_version,subscription_effect,quota_amount_usd,validity_days,before_monthly_limit_usd,after_monthly_limit_usd,monthly_usage_usd,before_expires_at,after_expires_at,before_version,after_version)
 SELECT $1,id,user_id,group_id,$2,1,'add_quota',20,0,100,120,37,expires_at,expires_at,0,1 FROM user_subscriptions WHERE id=$3`, usedCode, campaign, sid)
	require.NoError(t, err)
	_, err = db.ExecContext(ctx, `INSERT INTO user_welfare_subscription_supplement_quota(subscription_id,protected_quota_usd) VALUES($1,20)`, sid)
	require.NoError(t, err)
	_, err = db.ExecContext(ctx, `INSERT INTO redeem_codes(code,type,value,status,group_id,subscription_effect,welfare_campaign_id,welfare_campaign_version,welfare_campaign_name,quota_amount_usd,validity_days)
 VALUES('UNUSED-QUOTA','subscription',0,'unused',$1,'add_quota',$2,1,'history',10,0),
 ('UNUSED-DURATION','subscription',0,'unused',$1,'extend_duration',$2,1,'history',NULL,7),
 ('CANCELLED-LEGACY','subscription',0,'cancelled',$1,'add_quota',$2,1,'history',10,0)`, gid, campaign)
	require.NoError(t, err)
	tables := []string{"users", "groups", "user_subscriptions", "user_welfare_subscription_supplements", "user_welfare_subscription_supplement_quota", "qq_invite_campaigns"}
	before := welfareSubscriptionUpgradeTables(t, ctx, db, tables)
	codesBefore := upstreamAC1B8F1Snapshot(t, ctx, db, `SELECT jsonb_agg(to_jsonb(c)-'subscription_effect' ORDER BY id)::text FROM redeem_codes c`)
	usedBefore := upstreamAC1B8F1Snapshot(t, ctx, db, `SELECT jsonb_agg(to_jsonb(c) ORDER BY id)::text FROM redeem_codes c WHERE status<>'unused'`)
	oldHistory := upstreamAC1B8F1History(t, ctx, db)
	for _, name := range []string{"261_subscription_supplement_balances.sql", "262_subscription_supplement_redemption.sql", "263_grok_video_billing_tasks.sql"} {
		contents, err := migrations.FS.ReadFile(name)
		require.NoError(t, err)
		target[name] = &fstest.MapFile{Data: contents}
	}
	require.NoError(t, applyMigrationsFS(ctx, db, target))
	require.Equal(t, before, welfareSubscriptionUpgradeTables(t, ctx, db, tables))
	require.Equal(t, codesBefore, upstreamAC1B8F1Snapshot(t, ctx, db, `SELECT jsonb_agg(to_jsonb(c)-'subscription_effect' ORDER BY id)::text FROM redeem_codes c`))
	require.Equal(t, usedBefore, upstreamAC1B8F1Snapshot(t, ctx, db, `SELECT jsonb_agg(to_jsonb(c) ORDER BY id)::text FROM redeem_codes c WHERE status<>'unused'`))
	var effect string
	require.NoError(t, db.QueryRowContext(ctx, `SELECT subscription_effect FROM redeem_codes WHERE code='UNUSED-QUOTA'`).Scan(&effect))
	require.Equal(t, "add_balance", effect)
	require.NoError(t, db.QueryRowContext(ctx, `SELECT subscription_effect FROM redeem_codes WHERE code='UNUSED-DURATION'`).Scan(&effect))
	require.Equal(t, "extend_subscription", effect)
	var count int
	require.NoError(t, db.QueryRowContext(ctx, `SELECT (SELECT count(*) FROM user_subscription_supplement_balances)+(SELECT count(*) FROM user_subscription_supplements)+(SELECT count(*) FROM user_subscription_supplement_reservations)+(SELECT count(*) FROM grok_video_billing_tasks)`).Scan(&count))
	require.Zero(t, count)
	var genericCode int64
	require.NoError(t, db.QueryRowContext(ctx, `INSERT INTO redeem_codes(code,type,value,status,group_id,subscription_effect,quota_amount_usd,validity_days) VALUES('GENERIC-NO-CAMPAIGN','subscription',0,'unused',$1,'add_balance',10,0) RETURNING id`, gid).Scan(&genericCode))
	_, err = db.ExecContext(ctx, `INSERT INTO qq_invite_code_pool(campaign_id,redeem_code_id) VALUES($1,$2)`, campaign, genericCode)
	require.ErrorContains(t, err, "qq_invite_welfare_supplement_not_allowed")
	_, err = db.ExecContext(ctx, `UPDATE groups SET subscription_type='standard' WHERE id=$1`, gid)
	require.ErrorContains(t, err, "welfare_group_referenced")
	newHistory := upstreamAC1B8F1History(t, ctx, db)
	require.Len(t, newHistory, 324)
	for name, entry := range oldHistory {
		require.Equal(t, entry, newHistory[name])
	}
	require.NoError(t, applyMigrationsFS(ctx, db, target))
	require.Equal(t, newHistory, upstreamAC1B8F1History(t, ctx, db))
}
