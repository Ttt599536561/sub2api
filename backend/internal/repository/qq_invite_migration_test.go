package repository

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"fmt"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/Wei-Shaw/sub2api/migrations"
	"github.com/lib/pq"
	"github.com/stretchr/testify/require"
)

const qqInviteMigration = "253_qq_invite_rewards.sql"

var qqInviteMigrationTables = []string{
	"qq_invite_config_versions", "qq_invite_campaigns", "qq_invite_campaign_versions",
	"qq_invite_baseline", "qq_invite_events", "qq_invite_records", "qq_invite_joins",
	"qq_invite_code_pool", "qq_invite_rewards", "qq_invite_outbox", "qq_invite_audit",
}

func qqInviteMigrationTarget(t *testing.T) fstest.MapFS {
	t.Helper()
	target := upstreamV0220Target(t)
	contents, err := migrations.FS.ReadFile(qqInviteMigration)
	require.NoError(t, err)
	target[qqInviteMigration] = &fstest.MapFile{Data: contents}
	require.Len(t, target, 312)
	return target
}

func qqInviteMigrationSnapshot(t *testing.T, ctx context.Context, db *sql.DB) map[string]string {
	t.Helper()
	snapshot := upstreamV0220Snapshot(t, ctx, db)
	for _, table := range []string{"redeem_codes", "mcp_oauth_grants", "mcp_oauth_codes", "mcp_oauth_credentials"} {
		snapshot[table] = upstreamAC1B8F1Snapshot(t, ctx, db,
			"SELECT COALESCE(jsonb_agg(to_jsonb(t) ORDER BY to_jsonb(t)::text),'[]'::jsonb)::text FROM "+pq.QuoteIdentifier(table)+" t")
	}
	// The previous MCP OAuth schema is now part of the immutable baseline.
	// Only the eleven new invitation tables/indexes/FKs are additive here.
	snapshot["existing columns"] = upstreamAC1B8F1Snapshot(t, ctx, db, `SELECT jsonb_agg(to_jsonb(t) ORDER BY table_name,ordinal_position)::text
		FROM information_schema.columns t WHERE table_schema='public' AND table_name NOT LIKE 'qq_invite_%'`)
	snapshot["existing indexes"] = upstreamAC1B8F1Snapshot(t, ctx, db, `SELECT jsonb_agg(to_jsonb(t) ORDER BY indexname)::text
		FROM pg_indexes t WHERE schemaname='public' AND tablename NOT LIKE 'qq_invite_%'`)
	snapshot["existing constraints"] = upstreamAC1B8F1Snapshot(t, ctx, db, `SELECT jsonb_agg(to_jsonb(t) ORDER BY relation,name)::text FROM
		(SELECT conrelid::regclass::text AS relation,conname AS name,pg_get_constraintdef(oid) AS definition
		FROM pg_constraint WHERE connamespace='public'::regnamespace AND conrelid::regclass::text NOT LIKE 'qq_invite_%') t`)
	return snapshot
}

func qqInviteMigrationAssertHistory(t *testing.T, ctx context.Context, db *sql.DB) map[string]upstreamAC1B8F1HistoryEntry {
	t.Helper()
	history := upstreamAC1B8F1History(t, ctx, db)
	require.Len(t, history, 312)
	for _, name := range []string{qqInviteMigration, upstreamV0220Migration, "249_qq_image_group_discovery.sql"} {
		contents, err := migrations.FS.ReadFile(name)
		require.NoError(t, err)
		require.Contains(t, history, name)
		require.Equal(t, fmt.Sprintf("%x", sha256.Sum256([]byte(strings.TrimSpace(string(contents))))), history[name].Checksum)
	}
	return history
}

func qqInviteMigrationAssertEmptyAndProtected(t *testing.T, ctx context.Context, db *sql.DB) {
	t.Helper()
	for _, table := range qqInviteMigrationTables {
		var count int
		require.NoError(t, db.QueryRowContext(ctx, "SELECT count(*) FROM "+pq.QuoteIdentifier(table)).Scan(&count))
		require.Zero(t, count, "migration must not enable a campaign or manufacture rewards: %s", table)
	}
	var guardCount int
	require.NoError(t, db.QueryRowContext(ctx, `SELECT count(*) FROM pg_trigger WHERE tgrelid='public.redeem_codes'::regclass
		AND tgname='qq_invite_redeem_code_guard' AND tgenabled='O' AND NOT tgisinternal`).Scan(&guardCount))
	require.Equal(t, 1, guardCount)
	var restricted bool
	require.NoError(t, db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM pg_constraint WHERE conrelid='public.qq_invite_code_pool'::regclass
		AND confrelid='public.redeem_codes'::regclass AND contype='f' AND confdeltype='r')`).Scan(&restricted))
	require.True(t, restricted, "pool codes must retain ON DELETE RESTRICT")
}

func TestQQInviteMigrationUpgradePreservesState(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	db := upstreamAC1B8F1TestDatabase(t, ctx)
	require.NoError(t, applyMigrationsFS(ctx, db, upstreamV0220Target(t)))
	beforeHistory := upstreamV0220AssertHistory(t, ctx, db)
	userID := welfareLotteryV4Seed(t, ctx, db, true, "2026-09-01T04:05:06.123456Z")
	upstreamV0219SeedQQ(t, ctx, db, userID)
	var groupID, codeID int64
	require.NoError(t, db.QueryRowContext(ctx, `INSERT INTO groups(name,platform,subscription_type,image_count_billing,i18n)
		VALUES ('napcat-preserved-subscription','openai','subscription',true,'{"zh":{"name":"保留月卡"}}') RETURNING id`).Scan(&groupID))
	require.NoError(t, db.QueryRowContext(ctx, `INSERT INTO redeem_codes(code,type,value,status,group_id,validity_days)
		VALUES ('MIGRATION-PRESERVED-MONTH','subscription',0,'unused',$1,30) RETURNING id`, groupID).Scan(&codeID))
	for _, statement := range []string{
		`INSERT INTO scoped_admin_api_keys(id,name,key_prefix,key_hash,created_by,policy,allowed_ips)
		 VALUES ('napcat-preserved-key','preserved scoped key','test-prefix','irreversible-test-hash',$1,'{"mode":"allowlist"}','["127.0.0.1"]')`,
		`INSERT INTO mcp_oauth_grants(id,client_id,client_name,user_id,token_version,resource,scope,read_only,created_at,expires_at)
		 VALUES ('napcat-preserved-grant','test-client','Preserved MCP approval',$1,3,'https://example.test/mcp','admin',true,now(),now()+interval '1 day')`,
	} {
		_, err := db.ExecContext(ctx, statement, userID)
		require.NoError(t, err)
	}
	_, err := db.ExecContext(ctx, `INSERT INTO mcp_oauth_codes(code_hash,grant_id,client_id,redirect_uri,resource,code_challenge,expires_at)
		VALUES ('irreversible-code-hash','napcat-preserved-grant','test-client','https://example.test/callback','https://example.test/mcp','pkce-challenge',now()+interval '5 minutes');
		INSERT INTO mcp_oauth_credentials(key_hash,grant_id,kind,expires_at) VALUES
		('irreversible-access-hash','napcat-preserved-grant','access',now()+interval '1 hour'),
		('irreversible-refresh-hash','napcat-preserved-grant','refresh',now()+interval '1 day')`)
	require.NoError(t, err)
	before := qqInviteMigrationSnapshot(t, ctx, db)
	require.NoError(t, applyMigrationsFS(ctx, db, qqInviteMigrationTarget(t)))
	afterHistory := qqInviteMigrationAssertHistory(t, ctx, db)
	for name, entry := range beforeHistory {
		require.Equal(t, entry, afterHistory[name], "preserve released checksum and applied_at: %s", name)
	}
	qqInviteMigrationAssertEmptyAndProtected(t, ctx, db)
	for range 2 {
		require.NoError(t, applyMigrationsFS(ctx, db, qqInviteMigrationTarget(t)))
		require.Equal(t, afterHistory, upstreamAC1B8F1History(t, ctx, db))
		for name, rows := range qqInviteMigrationSnapshot(t, ctx, db) {
			require.JSONEq(t, before[name], rows, "preserve %s across NapCat upgrade/restart", name)
		}
	}
	qqInviteMigrationAssertGuard(t, ctx, db, userID, codeID)
}

func qqInviteMigrationAssertGuard(t *testing.T, ctx context.Context, db *sql.DB, userID, codeID int64) {
	t.Helper()
	var campaignID, recordID int64
	require.NoError(t, db.QueryRowContext(ctx, `INSERT INTO qq_invite_campaigns(payload) VALUES ('{"enabled":false}') RETURNING id`).Scan(&campaignID))
	_, err := db.ExecContext(ctx, `INSERT INTO qq_invite_campaign_versions(campaign_id,version,payload,actor_id) VALUES ($1,1,'{"enabled":false}',$2)`, campaignID, userID)
	require.NoError(t, err)
	_, err = db.ExecContext(ctx, `INSERT INTO qq_invite_code_pool(campaign_id,redeem_code_id) VALUES ($1,$2)`, campaignID, codeID)
	require.NoError(t, err)
	_, err = db.ExecContext(ctx, `UPDATE redeem_codes SET status='used',used_by=$2,used_at=now() WHERE id=$1`, codeID, userID)
	require.ErrorContains(t, err, "qq_invite_pool_unallocated")
	_, err = db.ExecContext(ctx, `UPDATE redeem_codes SET validity_days=365 WHERE id=$1`, codeID)
	require.ErrorContains(t, err, "qq_invite_pool_metadata_locked")
	var poolCodeConstraint string
	require.NoError(t, db.QueryRowContext(ctx, `SELECT conname FROM pg_constraint
		WHERE conrelid='public.qq_invite_code_pool'::regclass
		AND confrelid='public.redeem_codes'::regclass AND contype='f' AND confdeltype='r'`).Scan(&poolCodeConstraint))
	_, err = db.ExecContext(ctx, `DELETE FROM redeem_codes WHERE id=$1`, codeID)
	var foreignKeyError *pq.Error
	require.ErrorAs(t, err, &foreignKeyError)
	// PostgreSQL 16 reports this RESTRICT rejection as foreign_key_violation;
	// PostgreSQL 18 reports restrict_violation. Both must name our actual FK.
	require.Contains(t, []pq.ErrorCode{"23001", "23503"}, foreignKeyError.Code)
	require.Equal(t, poolCodeConstraint, foreignKeyError.Constraint)
	var preservedStatus string
	require.NoError(t, db.QueryRowContext(ctx, `SELECT status FROM redeem_codes WHERE id=$1`, codeID).Scan(&preservedStatus))
	require.Equal(t, "unused", preservedStatus, "failed deletion must retain the unused code")
	var poolReferences int
	require.NoError(t, db.QueryRowContext(ctx, `SELECT count(*) FROM qq_invite_code_pool
		WHERE campaign_id=$1 AND redeem_code_id=$2 AND record_id IS NULL`, campaignID, codeID).Scan(&poolReferences))
	require.Equal(t, 1, poolReferences, "failed deletion must retain the unallocated pool reference")
	require.NoError(t, db.QueryRowContext(ctx, `INSERT INTO qq_invite_records(campaign_id,campaign_version,snapshot,self_id,group_id,invitee_qq,inviter_qq,event_time)
		VALUES ($1,1,'{}','123456','100','33333','44444',now()) RETURNING id`, campaignID).Scan(&recordID))
	_, err = db.ExecContext(ctx, `UPDATE qq_invite_code_pool SET record_id=$2 WHERE redeem_code_id=$1`, codeID, recordID)
	require.NoError(t, err)
	_, err = db.ExecContext(ctx, `UPDATE redeem_codes SET status='used',used_by=$2,used_at=now() WHERE id=$1`, codeID, userID)
	require.NoError(t, err, "allocated code still uses the original website redemption transition")
}

func TestQQInviteMigrationFreshInstall(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	db := upstreamAC1B8F1TestDatabase(t, ctx)
	require.NoError(t, applyMigrationsFS(ctx, db, qqInviteMigrationTarget(t)))
	history := qqInviteMigrationAssertHistory(t, ctx, db)
	qqInviteMigrationAssertEmptyAndProtected(t, ctx, db)
	upstreamV0220AssertEmptyOAuth(t, ctx, db)
	welfareLotteryV4AssertSchema(t, ctx, db)
	before := qqInviteMigrationSnapshot(t, ctx, db)
	require.NoError(t, applyMigrationsFS(ctx, db, qqInviteMigrationTarget(t)))
	require.Equal(t, history, upstreamAC1B8F1History(t, ctx, db))
	for name, rows := range qqInviteMigrationSnapshot(t, ctx, db) {
		require.JSONEq(t, before[name], rows, name)
	}
}
