package repository

import (
	"context"
	"database/sql"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

var qqInviteRevocationMigrationTables = []string{
	"qq_invite_departures", "qq_invite_reward_revocations", "qq_invite_subscription_invalidations",
}

func TestQQInviteRevocationMigrationGuard(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	db := upstreamAC1B8F1TestDatabase(t, ctx)
	target := welfareSubscriptionUpgradeTarget(t, 319)
	require.NoError(t, applyMigrationsFS(ctx, db, target))
	var userID, groupID, campaignID int64
	require.NoError(t, db.QueryRowContext(ctx, `INSERT INTO users(email,password_hash)
		VALUES ('revocation-migration@test.invalid','fixture') RETURNING id`).Scan(&userID))
	require.NoError(t, db.QueryRowContext(ctx, `INSERT INTO groups(name,platform,subscription_type)
		VALUES ('revocation-migration','openai','subscription') RETURNING id`).Scan(&groupID))
	require.NoError(t, db.QueryRowContext(ctx, `INSERT INTO qq_invite_campaigns(payload)
		VALUES ('{"enabled":false}') RETURNING id`).Scan(&campaignID))
	_, err := db.ExecContext(ctx, `INSERT INTO qq_invite_campaign_versions(campaign_id,version,payload,actor_id)
		VALUES ($1,1,'{"enabled":false}',0)`, campaignID)
	require.NoError(t, err)

	newCode := func(name string, assigned bool) (codeID, recordID int64) {
		t.Helper()
		require.NoError(t, db.QueryRowContext(ctx, `INSERT INTO redeem_codes(code,type,value,status,group_id,validity_days)
			VALUES ($1,'subscription',0,'unused',$2,30) RETURNING id`, "REVOKE-"+name, groupID).Scan(&codeID))
		var reference any
		if assigned {
			require.NoError(t, db.QueryRowContext(ctx, `INSERT INTO qq_invite_records
				(campaign_id,campaign_version,snapshot,self_id,group_id,invitee_qq,inviter_qq,status,event_time)
				VALUES ($1,1,'{}','10000','20000',$2,'30000','awarded',now()) RETURNING id`, campaignID, fmt.Sprint(codeID)).Scan(&recordID))
			reference = recordID
		}
		_, insertErr := db.ExecContext(ctx, `INSERT INTO qq_invite_code_pool(campaign_id,redeem_code_id,record_id)
			VALUES ($1,$2,$3)`, campaignID, codeID, reference)
		require.NoError(t, insertErr)
		return codeID, recordID
	}
	newReceipt := func(codeID, recordID int64, status string) int64 {
		t.Helper()
		var receiptID int64
		require.NoError(t, db.QueryRowContext(ctx, `INSERT INTO qq_invite_reward_revocations
			(record_id,redeem_code_id,departed_at,status,reason) VALUES ($1,$2,now(),$3,'migration guard test') RETURNING id`,
			recordID, codeID, status).Scan(&receiptID))
		return receiptID
	}
	assertStatus := func(codeID int64, expected string) {
		t.Helper()
		var status string
		var usedBy sql.NullInt64
		var usedAt sql.NullTime
		require.NoError(t, db.QueryRowContext(ctx, `SELECT status,used_by,used_at FROM redeem_codes WHERE id=$1`, codeID).Scan(&status, &usedBy, &usedAt))
		require.Equal(t, expected, status)
		if expected != "used" {
			require.False(t, usedBy.Valid)
			require.False(t, usedAt.Valid)
		}
	}

	codeID, _ := newCode("UNALLOCATED", false)
	for _, status := range []string{"cancelled", "used"} {
		_, err = db.ExecContext(ctx, `UPDATE redeem_codes SET status=$2 WHERE id=$1`, codeID, status)
		require.ErrorContains(t, err, "qq_invite_pool_unallocated")
	}
	assertStatus(codeID, "unused")

	codeID, recordID := newCode("APPLIED", true)
	_, err = db.ExecContext(ctx, `UPDATE redeem_codes SET status='cancelled' WHERE id=$1`, codeID)
	require.ErrorContains(t, err, "qq_invite_pool_redemption_transition", "cancellation requires its committed receipt")
	_, err = db.ExecContext(ctx, `UPDATE redeem_codes SET validity_days=365 WHERE id=$1`, codeID)
	require.ErrorContains(t, err, "qq_invite_pool_metadata_locked")
	receiptID := newReceipt(codeID, recordID, "applied")
	_, err = db.ExecContext(ctx, `UPDATE redeem_codes SET status='cancelled',used_by=$2,used_at=now() WHERE id=$1`, codeID, userID)
	require.ErrorContains(t, err, "qq_invite_pool_redemption_transition", "cancellation cannot forge redemption provenance")
	_, err = db.ExecContext(ctx, `UPDATE redeem_codes SET status='cancelled' WHERE id=$1`, codeID)
	require.NoError(t, err)
	for _, statement := range []string{
		`UPDATE redeem_codes SET status='unused' WHERE id=$1`,
		`UPDATE redeem_codes SET status='used',used_by=$2,used_at=now() WHERE id=$1`,
	} {
		args := []any{codeID}
		if statement == `UPDATE redeem_codes SET status='used',used_by=$2,used_at=now() WHERE id=$1` {
			args = append(args, userID)
		}
		_, err = db.ExecContext(ctx, statement, args...)
		require.ErrorContains(t, err, "qq_invite_pool_redemption_transition", "cancelled rewards cannot return to circulation")
	}
	assertStatus(codeID, "cancelled")
	_, err = db.ExecContext(ctx, `INSERT INTO qq_invite_reward_revocations
		(record_id,redeem_code_id,departed_at,status,reason) VALUES ($1,$2,now(),'applied','duplicate')`, recordID, codeID)
	require.ErrorContains(t, err, "duplicate key", "one durable revocation per invitation/code")
	_, err = db.ExecContext(ctx, `INSERT INTO qq_invite_subscription_invalidations(revocation_id,user_id,group_id)
		VALUES ($1,$2,$3)`, receiptID, userID, groupID)
	require.NoError(t, err)
	_, err = db.ExecContext(ctx, `DELETE FROM qq_invite_reward_revocations WHERE id=$1`, receiptID)
	require.Error(t, err, "pending cache invalidation retains its receipt")

	codeID, recordID = newCode("REVIEW", true)
	newReceipt(codeID, recordID, "manual_review")
	_, err = db.ExecContext(ctx, `UPDATE redeem_codes SET status='cancelled' WHERE id=$1`, codeID)
	require.ErrorContains(t, err, "qq_invite_pool_redemption_transition", "uncertain provenance cannot authorize cancellation")
	_, err = db.ExecContext(ctx, `UPDATE redeem_codes SET status='used',used_by=$2,used_at=now() WHERE id=$1`, codeID, userID)
	require.ErrorContains(t, err, "qq_invite_pool_redemption_transition", "review-pending rewards cannot be redeemed")
	assertStatus(codeID, "unused")

	codeID, _ = newCode("NORMAL", true)
	_, err = db.ExecContext(ctx, `UPDATE redeem_codes SET status='used',used_by=$2,used_at=now() WHERE id=$1`, codeID, userID)
	require.NoError(t, err, "ordinary allocated reward redemption still works")
	_, err = db.ExecContext(ctx, `UPDATE redeem_codes SET status='cancelled' WHERE id=$1`, codeID)
	require.ErrorContains(t, err, "qq_invite_pool_redemption_transition", "used redemption history is immutable")
	assertStatus(codeID, "used")

	var ordinaryID int64
	require.NoError(t, db.QueryRowContext(ctx, `INSERT INTO redeem_codes(code,type,value,status)
		VALUES ('REVOKE-NONPOOL','balance',1,'unused') RETURNING id`).Scan(&ordinaryID))
	_, err = db.ExecContext(ctx, `UPDATE redeem_codes SET status='used',used_by=$2,used_at=now() WHERE id=$1`, ordinaryID, userID)
	require.NoError(t, err, "non-invitation redemption is unaffected")
	_, err = db.ExecContext(ctx, `INSERT INTO qq_invite_departures(self_id,group_id,invitee_qq,departed_at)
		VALUES ('10000','20000','40000',now())`)
	require.NoError(t, err)

	tables := append(append([]string{}, qqInviteRevocationMigrationTables...), "redeem_codes", "qq_invite_records", "qq_invite_code_pool")
	before := welfareSubscriptionUpgradeTables(t, ctx, db, tables)
	history := upstreamAC1B8F1History(t, ctx, db)
	for range 2 {
		require.NoError(t, applyMigrationsFS(ctx, db, target))
		require.Equal(t, history, upstreamAC1B8F1History(t, ctx, db))
		require.Equal(t, before, welfareSubscriptionUpgradeTables(t, ctx, db, tables), "restart preserves receipts, cancellation and retry state")
	}
}
