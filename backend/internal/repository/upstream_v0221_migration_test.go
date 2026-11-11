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
	"github.com/lib/pq"
	"github.com/stretchr/testify/require"
)

var upstreamV0221Migrations = []string{
	"249_add_usage_logs_api_key_cost_index_notx.sql", "250_openai_cache_write_ttl.sql",
}

var upstreamV0221PricingTables = []string{
	"channel_model_pricing", "channel_pricing_intervals",
	"channel_account_stats_model_pricing", "channel_account_stats_pricing_intervals",
}

func upstreamV0221Baseline(t *testing.T) fstest.MapFS {
	t.Helper()
	baseline := qqInviteMigrationTarget(t)
	names, err := fs.Glob(baseline, "*.sql")
	require.NoError(t, err)
	hash := sha256.New()
	for _, name := range names {
		_, err = fmt.Fprintf(hash, "%s\x00%s\x00", name, strings.TrimSpace(string(baseline[name].Data)))
		require.NoError(t, err)
	}
	// Independently computed from git archive of the published 0.2.20-r3
	// application commit 33961e3b, never from the merged working tree.
	require.Len(t, baseline, 312)
	require.Equal(t, "a31b0ae8895bda659a25bc8fe5673056b43630f4edadbfe230770029ecb9556d", fmt.Sprintf("%x", hash.Sum(nil)))
	return baseline
}

func upstreamV0221Target(t *testing.T) fstest.MapFS {
	t.Helper()
	target := upstreamV0221Baseline(t)
	for _, name := range upstreamV0221Migrations {
		contents, err := migrations.FS.ReadFile(name)
		require.NoError(t, err)
		target[name] = &fstest.MapFile{Data: contents}
	}
	require.Len(t, target, 314)
	return target
}

func TestUpstreamV0221MigrationBaseline(t *testing.T) {
	upstreamV0221Baseline(t)
}

func upstreamV0221AssertHistory(t *testing.T, ctx context.Context, db *sql.DB) map[string]upstreamAC1B8F1HistoryEntry {
	t.Helper()
	history := upstreamAC1B8F1History(t, ctx, db)
	target := upstreamV0221Target(t)
	require.Len(t, history, len(target))
	for name, file := range target {
		require.Contains(t, history, name, "full filenames with reused numeric prefixes must coexist")
		require.Equal(t, fmt.Sprintf("%x", sha256.Sum256([]byte(strings.TrimSpace(string(file.Data))))), history[name].Checksum, name)
	}
	return history
}

func upstreamV0221Snapshot(t *testing.T, ctx context.Context, db *sql.DB) map[string]string {
	t.Helper()
	// Snapshot every existing data table, including populated QQ, invitation,
	// welfare, MCP and pricing rows. Only the six additive columns are omitted;
	// migration history is checked separately, including original applied_at.
	rows, err := db.QueryContext(ctx, `SELECT table_name FROM information_schema.tables
		WHERE table_schema='public' AND table_type='BASE TABLE' AND table_name <> 'schema_migrations' ORDER BY table_name`)
	require.NoError(t, err)
	var tables []string
	for rows.Next() {
		var table string
		require.NoError(t, rows.Scan(&table))
		tables = append(tables, table)
	}
	require.NoError(t, rows.Err())
	require.NoError(t, rows.Close())
	snapshot := map[string]string{}
	for _, table := range tables {
		row := "to_jsonb(t)"
		if table == "usage_logs" {
			row += "-'cache_creation_30m_tokens'-'cache_creation_12h_tokens'"
		}
		for _, pricingTable := range upstreamV0221PricingTables {
			if table == pricingTable {
				row += "-'cache_write_12h_price'"
			}
		}
		snapshot[table] = upstreamAC1B8F1Snapshot(t, ctx, db,
			"SELECT COALESCE(jsonb_agg("+row+" ORDER BY ("+row+")::text),'[]'::jsonb)::text FROM "+pq.QuoteIdentifier(table)+" t")
	}
	snapshot["existing columns"] = upstreamAC1B8F1Snapshot(t, ctx, db, `SELECT jsonb_agg(to_jsonb(t) ORDER BY table_name,ordinal_position)::text
		FROM information_schema.columns t WHERE table_schema='public' AND NOT
		((table_name='usage_logs' AND column_name IN ('cache_creation_30m_tokens','cache_creation_12h_tokens')) OR
		(table_name IN ('channel_model_pricing','channel_pricing_intervals','channel_account_stats_model_pricing','channel_account_stats_pricing_intervals')
		 AND column_name='cache_write_12h_price'))`)
	snapshot["existing indexes"] = upstreamAC1B8F1Snapshot(t, ctx, db, `SELECT jsonb_agg(to_jsonb(t) ORDER BY indexname)::text
		FROM pg_indexes t WHERE schemaname='public' AND indexname <> 'idx_usage_logs_api_key_created_cost'`)
	snapshot["existing constraints"] = upstreamAC1B8F1Snapshot(t, ctx, db, `SELECT jsonb_agg(to_jsonb(t) ORDER BY relation,name)::text FROM
		(SELECT conrelid::regclass::text AS relation,conname AS name,pg_get_constraintdef(oid) AS definition
		FROM pg_constraint WHERE connamespace='public'::regnamespace AND NOT
		(contype='n' AND conrelid='public.usage_logs'::regclass AND conkey && ARRAY(
		 SELECT attnum FROM pg_attribute WHERE attrelid='public.usage_logs'::regclass
		 AND attname IN ('cache_creation_30m_tokens','cache_creation_12h_tokens')))) t`)
	// PG18 also catalogs the two new NOT NULL constraints; their nullability
	// is asserted separately along with the columns on both PG16 and PG18.
	snapshot["existing triggers"] = upstreamAC1B8F1Snapshot(t, ctx, db, `SELECT jsonb_agg(to_jsonb(t) ORDER BY relation,name)::text FROM
		(SELECT tgrelid::regclass::text AS relation,tgname AS name,pg_get_triggerdef(oid) AS definition
		FROM pg_trigger WHERE NOT tgisinternal AND tgrelid IN (SELECT oid FROM pg_class WHERE relnamespace='public'::regnamespace)) t`)
	return snapshot
}

func upstreamV0221AssertSchema(t *testing.T, ctx context.Context, db *sql.DB) {
	t.Helper()
	for _, column := range []string{"cache_creation_30m_tokens", "cache_creation_12h_tokens"} {
		var nullable, defaultValue, dataType string
		require.NoError(t, db.QueryRowContext(ctx, `SELECT is_nullable,column_default,data_type FROM information_schema.columns
			WHERE table_schema='public' AND table_name='usage_logs' AND column_name=$1`, column).Scan(&nullable, &defaultValue, &dataType))
		require.Equal(t, "NO", nullable)
		require.Equal(t, "0", defaultValue)
		require.Equal(t, "integer", dataType)
	}
	for _, table := range upstreamV0221PricingTables {
		var nullable, dataType string
		var defaultValue sql.NullString
		var precision, scale int
		require.NoError(t, db.QueryRowContext(ctx, `SELECT is_nullable,column_default,data_type,numeric_precision,numeric_scale
			FROM information_schema.columns WHERE table_schema='public' AND table_name=$1 AND column_name='cache_write_12h_price'`, table).
			Scan(&nullable, &defaultValue, &dataType, &precision, &scale))
		require.Equal(t, "YES", nullable, table)
		require.False(t, defaultValue.Valid, table)
		require.Equal(t, "numeric", dataType, table)
		require.Equal(t, 20, precision, table)
		require.Equal(t, 12, scale, table)
	}
	var valid, ready bool
	var relation, method, definition string
	require.NoError(t, db.QueryRowContext(ctx, `SELECT i.indisvalid,i.indisready,i.indrelid::regclass::text,a.amname,pg_get_indexdef(i.indexrelid)
		FROM pg_index i JOIN pg_class c ON c.oid=i.indexrelid JOIN pg_am a ON a.oid=c.relam
		WHERE i.indexrelid='public.idx_usage_logs_api_key_created_cost'::regclass`).Scan(&valid, &ready, &relation, &method, &definition))
	require.True(t, valid)
	require.True(t, ready)
	require.Equal(t, "usage_logs", relation)
	require.Equal(t, "btree", method)
	require.Contains(t, definition, "(api_key_id, created_at) INCLUDE (actual_cost)")
}

func upstreamV0221AssertSnapshot(t *testing.T, ctx context.Context, db *sql.DB, before map[string]string) {
	t.Helper()
	after := upstreamV0221Snapshot(t, ctx, db)
	require.Len(t, after, len(before))
	for name, rows := range before {
		require.Contains(t, after, name)
		// Hash the canonical ordered JSON to keep failures readable even for
		// the full information_schema snapshot (several megabytes).
		require.Equal(t, sha256.Sum256([]byte(rows)), sha256.Sum256([]byte(after[name])), "preserve %s", name)
	}
}

func upstreamV0221Seed(t *testing.T, ctx context.Context, db *sql.DB) (int64, int64) {
	t.Helper()
	userID := welfareLotteryV4Seed(t, ctx, db, true, "2026-09-01T04:05:06.123456Z")
	upstreamV0219SeedQQ(t, ctx, db, userID)
	var groupID, codeID, accountID, keyID, channelID, pricingID, ruleID, statsPricingID int64
	require.NoError(t, db.QueryRowContext(ctx, `INSERT INTO groups(name,platform,subscription_type,force_openai_ultrafast,image_count_billing,i18n)
		VALUES ('v0221-preserved-group','openai','subscription',true,true,'{"zh":{"name":"保留分组"}}') RETURNING id`).Scan(&groupID))
	require.NoError(t, db.QueryRowContext(ctx, `INSERT INTO redeem_codes(code,type,value,status,group_id,validity_days)
		VALUES ('V0221-PRESERVED-MONTH','subscription',0,'unused',$1,30) RETURNING id`, groupID).Scan(&codeID))
	require.NoError(t, db.QueryRowContext(ctx, `INSERT INTO accounts(name,platform,type,credentials)
		VALUES ('v0221-account','openai','apikey','{"api_key":"preserved-test-key"}') RETURNING id`).Scan(&accountID))
	require.NoError(t, db.QueryRowContext(ctx, `INSERT INTO api_keys(user_id,key,name,group_id)
		VALUES ($1,'sk-v0221-test-key','v0221-key',$2) RETURNING id`, userID, groupID).Scan(&keyID))
	_, err := db.ExecContext(ctx, `INSERT INTO usage_logs(user_id,api_key_id,account_id,model,input_tokens,cache_creation_tokens,
		cache_creation_5m_tokens,cache_creation_1h_tokens,actual_cost,total_cost)
		VALUES ($1,$2,$3,'gpt-preserved',1234,555,222,333,0.1234567890,0.2345678901)`, userID, keyID, accountID)
	require.NoError(t, err)
	require.NoError(t, db.QueryRowContext(ctx, `INSERT INTO channels(name) VALUES ('v0221-channel') RETURNING id`).Scan(&channelID))
	require.NoError(t, db.QueryRowContext(ctx, `INSERT INTO channel_model_pricing(channel_id,models,cache_write_price,cache_write_1h_price)
		VALUES ($1,'["gpt-preserved"]',0.000001234567,0.000009876543) RETURNING id`, channelID).Scan(&pricingID))
	_, err = db.ExecContext(ctx, `INSERT INTO channel_pricing_intervals(pricing_id,min_tokens,cache_write_price)
		VALUES ($1,0,0.000002345678)`, pricingID)
	require.NoError(t, err)
	require.NoError(t, db.QueryRowContext(ctx, `INSERT INTO channel_account_stats_pricing_rules(channel_id,name)
		VALUES ($1,'v0221-stats-rule') RETURNING id`, channelID).Scan(&ruleID))
	require.NoError(t, db.QueryRowContext(ctx, `INSERT INTO channel_account_stats_model_pricing(rule_id,models,cache_write_price)
		VALUES ($1,'["gpt-preserved"]',0.0000034567) RETURNING id`, ruleID).Scan(&statsPricingID))
	_, err = db.ExecContext(ctx, `INSERT INTO channel_account_stats_pricing_intervals(pricing_id,min_tokens,cache_write_price)
		VALUES ($1,0,0.000004567890)`, statsPricingID)
	require.NoError(t, err)
	for _, statement := range []string{
		`INSERT INTO scoped_admin_api_keys(id,name,key_prefix,key_hash,created_by,policy,allowed_ips)
		 VALUES ('v0221-admin','preserved scoped key','test-prefix','irreversible-test-hash',$1,'{"mode":"allowlist"}','["127.0.0.1"]')`,
		`INSERT INTO mcp_oauth_grants(id,client_id,client_name,user_id,token_version,resource,scope,read_only,created_at,expires_at)
		 VALUES ('v0221-grant','test-client','Preserved MCP approval',$1,3,'https://example.test/mcp','admin',true,now(),now()+interval '1 day')`,
		`INSERT INTO qq_invite_config_versions(payload,secrets,actor_id)
		 VALUES ('{"enabled":true,"self_id":"123456"}','\x010203',$1)`,
	} {
		_, err = db.ExecContext(ctx, statement, userID)
		require.NoError(t, err)
	}
	_, err = db.ExecContext(ctx, `INSERT INTO mcp_oauth_codes(code_hash,grant_id,client_id,redirect_uri,resource,code_challenge,expires_at)
		VALUES ('irreversible-code-hash','v0221-grant','test-client','https://example.test/callback','https://example.test/mcp','pkce-challenge',now()+interval '5 minutes');
		INSERT INTO mcp_oauth_credentials(key_hash,grant_id,kind,expires_at) VALUES
		('irreversible-access-hash','v0221-grant','access',now()+interval '1 hour'),
		('irreversible-refresh-hash','v0221-grant','refresh',now()+interval '1 day')`)
	require.NoError(t, err)
	var campaignID, recordID, poolID int64
	require.NoError(t, db.QueryRowContext(ctx, `INSERT INTO qq_invite_campaigns(payload,baseline_at)
		VALUES ('{"enabled":true,"name":"保留邀请活动"}',now()) RETURNING id`).Scan(&campaignID))
	_, err = db.ExecContext(ctx, `INSERT INTO qq_invite_campaign_versions(campaign_id,version,payload,actor_id)
		VALUES ($1,1,'{"enabled":true}',$2)`, campaignID, userID)
	require.NoError(t, err)
	require.NoError(t, db.QueryRowContext(ctx, `INSERT INTO qq_invite_records(campaign_id,campaign_version,snapshot,self_id,group_id,invitee_qq,inviter_qq,qq_level,status,event_time)
		VALUES ($1,1,'{"reward_days":30}','123456','100','33333','44444',15,'rewarded',now()) RETURNING id`, campaignID).Scan(&recordID))
	require.NoError(t, db.QueryRowContext(ctx, `INSERT INTO qq_invite_code_pool(campaign_id,redeem_code_id,record_id)
		VALUES ($1,$2,$3) RETURNING id`, campaignID, codeID, recordID).Scan(&poolID))
	_, err = db.ExecContext(ctx, `INSERT INTO qq_invite_rewards(record_id,pool_id) VALUES ($1,$2)`, recordID, poolID)
	require.NoError(t, err)
	for _, statement := range []string{
		`INSERT INTO qq_invite_baseline(campaign_id,group_id,qq) VALUES ($1,'100','55555')`,
		`INSERT INTO qq_invite_joins(campaign_id,group_id,qq) VALUES ($1,'100','33333')`,
	} {
		_, err = db.ExecContext(ctx, statement, campaignID)
		require.NoError(t, err)
	}
	_, err = db.ExecContext(ctx, `INSERT INTO qq_invite_events(event_key,payload) VALUES ('v0221-invite','{"notice_type":"group_increase"}')`)
	require.NoError(t, err)
	_, err = db.ExecContext(ctx, `INSERT INTO qq_invite_outbox(record_id,kind,destination,template,status,attempts,last_error)
		VALUES ($1,'reward','44444','保留待投递消息','pending',2,'preserved retry')`, recordID)
	require.NoError(t, err)
	_, err = db.ExecContext(ctx, `INSERT INTO qq_invite_audit(actor_id,action,entity_id,detail)
		VALUES ($1,'import',$2,'{"imported":1}')`, userID, campaignID)
	require.NoError(t, err)
	// Keep a separate unallocated code so the existing redemption guard can be
	// exercised after upgrading without altering the populated reward fixture.
	var unallocatedCodeID int64
	require.NoError(t, db.QueryRowContext(ctx, `INSERT INTO redeem_codes(code,type,value,status,group_id,validity_days)
		VALUES ('V0221-GUARD-MONTH','subscription',0,'unused',$1,30) RETURNING id`, groupID).Scan(&unallocatedCodeID))
	return userID, unallocatedCodeID
}

func TestUpstreamV0221MigrationUpgradePreservesState(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	db := upstreamAC1B8F1TestDatabase(t, ctx)
	require.NoError(t, applyMigrationsFS(ctx, db, upstreamV0221Baseline(t)))
	beforeHistory := qqInviteMigrationAssertHistory(t, ctx, db)
	userID, codeID := upstreamV0221Seed(t, ctx, db)
	before := upstreamV0221Snapshot(t, ctx, db)
	require.NoError(t, applyMigrationsFS(ctx, db, upstreamV0221Target(t)))
	afterHistory := upstreamV0221AssertHistory(t, ctx, db)
	for name, entry := range beforeHistory {
		require.Equal(t, entry, afterHistory[name], "preserve released checksum and applied_at: %s", name)
	}
	upstreamV0221AssertSchema(t, ctx, db)
	var oldUsageDefaults bool
	require.NoError(t, db.QueryRowContext(ctx, `SELECT cache_creation_30m_tokens=0 AND cache_creation_12h_tokens=0 FROM usage_logs`).Scan(&oldUsageDefaults))
	require.True(t, oldUsageDefaults, "old Anthropic cache tokens must not be reclassified as OpenAI TTL tokens")
	for _, table := range upstreamV0221PricingTables {
		var fallback bool
		require.NoError(t, db.QueryRowContext(ctx, "SELECT cache_write_12h_price IS NULL AND cache_write_price IS NOT NULL FROM "+pq.QuoteIdentifier(table)).Scan(&fallback))
		require.True(t, fallback, "preserve generic pricing fallback: %s", table)
	}
	for range 2 {
		require.NoError(t, applyMigrationsFS(ctx, db, upstreamV0221Target(t)))
		require.Equal(t, afterHistory, upstreamAC1B8F1History(t, ctx, db))
		upstreamV0221AssertSnapshot(t, ctx, db, before)
	}
	_, err := db.ExecContext(ctx, `UPDATE usage_logs SET cache_creation_30m_tokens=17,cache_creation_12h_tokens=29`)
	require.NoError(t, err)
	for _, table := range upstreamV0221PricingTables {
		_, err = db.ExecContext(ctx, "UPDATE "+pq.QuoteIdentifier(table)+" SET cache_write_12h_price=0.000012345678")
		require.NoError(t, err)
	}
	require.NoError(t, applyMigrationsFS(ctx, db, upstreamV0221Target(t)))
	require.Equal(t, afterHistory, upstreamAC1B8F1History(t, ctx, db))
	upstreamV0221AssertSnapshot(t, ctx, db, before)
	var ttl30m, ttl12h int
	require.NoError(t, db.QueryRowContext(ctx, `SELECT cache_creation_30m_tokens,cache_creation_12h_tokens FROM usage_logs`).Scan(&ttl30m, &ttl12h))
	require.Equal(t, 17, ttl30m)
	require.Equal(t, 29, ttl12h)
	for _, table := range upstreamV0221PricingTables {
		var price string
		require.NoError(t, db.QueryRowContext(ctx, "SELECT cache_write_12h_price::text FROM "+pq.QuoteIdentifier(table)).Scan(&price))
		require.Equal(t, "0.000012345678", price, "preserve subsequent 12h pricing choice: %s", table)
	}
	qqInviteMigrationAssertGuard(t, ctx, db, userID, codeID)
}

func TestUpstreamV0221MigrationFreshInstall(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	db := upstreamAC1B8F1TestDatabase(t, ctx)
	require.NoError(t, applyMigrationsFS(ctx, db, upstreamV0221Target(t)))
	history := upstreamV0221AssertHistory(t, ctx, db)
	upstreamV0221AssertSchema(t, ctx, db)
	qqInviteMigrationAssertEmptyAndProtected(t, ctx, db)
	upstreamV0220AssertEmptyOAuth(t, ctx, db)
	welfareLotteryV4AssertSchema(t, ctx, db)
	before := upstreamV0221Snapshot(t, ctx, db)
	require.NoError(t, applyMigrationsFS(ctx, db, upstreamV0221Target(t)))
	require.Equal(t, history, upstreamAC1B8F1History(t, ctx, db))
	upstreamV0221AssertSnapshot(t, ctx, db, before)
}
