package repository

import (
	"context"
	"fmt"
	"testing"
	"testing/fstest"
	"time"

	"github.com/Wei-Shaw/sub2api/migrations"
	"github.com/stretchr/testify/require"
)

// Full published r4 upgrade, independent of Ent's fresh-schema fixtures. The
// helper creates a disposable loopback database and never migrates the supplied DB.
func TestWelfareRedeemUpgradePreservesLegacyCodesAndEntitlements(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	db := upstreamAC1B8F1TestDatabase(t, ctx)
	target := welfareSubscriptionUpgradeTarget(t, 319)
	require.NoError(t, applyMigrationsFS(ctx, db, target))
	var userID, groupID, campaignID int64
	require.NoError(t, db.QueryRowContext(ctx, `INSERT INTO users(email,password_hash,balance) VALUES('supplement-upgrade@test.invalid','fixture',12.34) RETURNING id`).Scan(&userID))
	require.NoError(t, db.QueryRowContext(ctx, `INSERT INTO groups(name,platform,subscription_type,monthly_limit_usd) VALUES('legacy invitation','openai','subscription',100) RETURNING id`).Scan(&groupID))
	payload := fmt.Sprintf(`{"enabled":false,"subscription_group_id":%d,"name":"legacy campaign"}`, groupID)
	require.NoError(t, db.QueryRowContext(ctx, `INSERT INTO qq_invite_campaigns(payload) VALUES($1::jsonb) RETURNING id`, payload).Scan(&campaignID))
	_, err := db.ExecContext(ctx, `INSERT INTO qq_invite_campaign_versions(campaign_id,version,payload,actor_id) VALUES($1,1,$2::jsonb,0)`, campaignID, payload)
	require.NoError(t, err)
	_, err = db.ExecContext(ctx, `INSERT INTO user_subscriptions(user_id,group_id,starts_at,expires_at,monthly_usage_usd) VALUES($1,$2,now()-interval '2 days',now()+interval '45 days',7.5)`, userID, groupID)
	require.NoError(t, err)
	var welfareID int64
	require.NoError(t, db.QueryRowContext(ctx, `INSERT INTO user_subscriptions(user_id,group_id,starts_at,expires_at,entitlement_kind,welfare_monthly_limit_usd,welfare_grant_count,monthly_usage_usd,daily_reset_version) VALUES($1,$2,now()-interval '2 days',now()+interval '28 days','welfare',100,1,42.5,3) RETURNING id`, userID, groupID).Scan(&welfareID))
	_, err = db.ExecContext(ctx, `INSERT INTO redeem_codes(code,type,value,status,group_id,validity_days) VALUES('UPGRADE-ORDINARY','subscription',0,'unused',$1,30),('UPGRADE-BALANCE','balance',9,'unused',NULL,30)`, groupID)
	require.NoError(t, err)
	var rewardCodeID, recordID, poolID int64
	require.NoError(t, db.QueryRowContext(ctx, `INSERT INTO redeem_codes(code,type,value,status,used_by,used_at,group_id,validity_days) VALUES('UPGRADE-QQ','subscription',0,'used',$1,now(),$2,30) RETURNING id`, userID, groupID).Scan(&rewardCodeID))
	require.NoError(t, db.QueryRowContext(ctx, `INSERT INTO qq_invite_records(campaign_id,campaign_version,snapshot,self_id,group_id,invitee_qq,inviter_qq,status,event_time) VALUES($1,1,$2::jsonb,'10000','20000','30000','40000','awarded',now()) RETURNING id`, campaignID, payload).Scan(&recordID))
	require.NoError(t, db.QueryRowContext(ctx, `INSERT INTO qq_invite_code_pool(campaign_id,redeem_code_id,record_id) VALUES($1,$2,$3) RETURNING id`, campaignID, rewardCodeID, recordID).Scan(&poolID))
	_, err = db.ExecContext(ctx, `INSERT INTO qq_invite_rewards(record_id,pool_id) VALUES($1,$2)`, recordID, poolID)
	require.NoError(t, err)
	_, err = db.ExecContext(ctx, `INSERT INTO user_welfare_subscription_grants(redeem_code_id,subscription_id,monthly_limit_usd) VALUES($1,$2,100)`, rewardCodeID, welfareID)
	require.NoError(t, err)
	tables := []string{"users", "groups", "user_subscriptions", "qq_invite_campaigns", "qq_invite_campaign_versions", "qq_invite_records", "qq_invite_code_pool", "qq_invite_rewards", "user_welfare_subscription_grants"}
	before := welfareSubscriptionUpgradeTables(t, ctx, db, tables)
	legacyCodes := upstreamAC1B8F1Snapshot(t, ctx, db, `SELECT COALESCE(jsonb_agg(to_jsonb(r) ORDER BY id),'[]'::jsonb)::text FROM redeem_codes r`)
	oldHistory := upstreamAC1B8F1History(t, ctx, db)
	for _, name := range []string{"259_welfare_redeem_supplements.sql", "260_welfare_group_lifecycle_guards.sql"} {
		contents, readErr := migrations.FS.ReadFile(name)
		require.NoError(t, readErr)
		target[name] = &fstest.MapFile{Data: contents}
	}
	require.Len(t, target, 321)
	require.NoError(t, applyMigrationsFS(ctx, db, target))
	require.Equal(t, before, welfareSubscriptionUpgradeTables(t, ctx, db, tables), "upgrade preserves existing balances, dates, usage and invitation provenance")
	currentCodes := upstreamAC1B8F1Snapshot(t, ctx, db, `SELECT COALESCE(jsonb_agg(to_jsonb(r)-'subscription_effect'-'welfare_campaign_id'-'welfare_campaign_version'-'welfare_campaign_name'-'quota_amount_usd' ORDER BY id),'[]'::jsonb)::text FROM redeem_codes r`)
	require.Equal(t, legacyCodes, currentCodes)
	var count int
	require.NoError(t, db.QueryRowContext(ctx, `SELECT count(*) FROM redeem_codes WHERE subscription_effect='' AND welfare_campaign_id IS NULL AND welfare_campaign_version IS NULL AND quota_amount_usd IS NULL`).Scan(&count))
	require.Equal(t, 3, count, "all legacy codes retain provenance-based behavior")
	require.NoError(t, db.QueryRowContext(ctx, `SELECT count(*) FROM user_welfare_subscription_supplements`).Scan(&count))
	require.Zero(t, count, "upgrade must not manufacture manual supplement receipts")
	require.NoError(t, db.QueryRowContext(ctx, `SELECT count(*) FROM user_welfare_subscription_supplement_quota`).Scan(&count))
	require.Zero(t, count, "upgrade must not manufacture protected manual quota")
	newHistory := upstreamAC1B8F1History(t, ctx, db)
	require.Len(t, newHistory, 321)
	for name, entry := range oldHistory {
		require.Equal(t, entry, newHistory[name], "published migration unchanged: %s", name)
	}
	for range 2 {
		require.NoError(t, applyMigrationsFS(ctx, db, target))
		require.Equal(t, newHistory, upstreamAC1B8F1History(t, ctx, db))
		require.Equal(t, before, welfareSubscriptionUpgradeTables(t, ctx, db, tables))
	}
}
