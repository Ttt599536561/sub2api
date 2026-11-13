package qqinvite

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

// SetSubscriptionInvalidator installs the post-commit cache invalidator. Failed
// callbacks remain in a durable queue, including when reward intake is disabled.
func (s *Store) SetSubscriptionInvalidator(fn func(userID, groupID int64) error) {
	s.invalidatorMu.Lock()
	defer s.invalidatorMu.Unlock()
	s.invalidateSubscription = fn
}

func (s *Store) acceptDeparture(ctx context.Context, tx *sql.Tx, ev Event) error {
	if _, err := tx.ExecContext(ctx, `INSERT INTO qq_invite_departures(self_id,group_id,invitee_qq,departed_at) VALUES($1,$2,$3,to_timestamp($4)) ON CONFLICT(self_id,group_id,invitee_qq) DO UPDATE SET departed_at=GREATEST(qq_invite_departures.departed_at,EXCLUDED.departed_at)`, ev.SelfID, ev.GroupID, ev.UserID, ev.Time); err != nil {
		return err
	}
	rows, err := tx.QueryContext(ctx, `SELECT id FROM qq_invite_records WHERE self_id=$1 AND group_id=$2 AND invitee_qq=$3 AND event_time<=to_timestamp($4) AND status NOT IN ('revoked','baseline') ORDER BY id FOR UPDATE`, ev.SelfID, ev.GroupID, ev.UserID, ev.Time)
	if err != nil {
		return err
	}
	var ids []int64
	for rows.Next() {
		var id int64
		if err = rows.Scan(&id); err != nil {
			_ = rows.Close()
			return err
		}
		ids = append(ids, id)
	}
	err = rows.Err()
	_ = rows.Close()
	if err != nil {
		return err
	}
	// A single departure may revoke rewards from several historical campaigns.
	// Acquire *all* codes before the first user lock: otherwise a redemption of
	// code B can hold B while waiting on the user held by our reversal of code A.
	rows, err = tx.QueryContext(ctx, `SELECT c.id FROM qq_invite_records r JOIN qq_invite_rewards w ON w.record_id=r.id JOIN qq_invite_code_pool p ON p.id=w.pool_id JOIN redeem_codes c ON c.id=p.redeem_code_id WHERE r.self_id=$1 AND r.group_id=$2 AND r.invitee_qq=$3 AND r.event_time<=to_timestamp($4) AND r.status NOT IN ('revoked','baseline') ORDER BY c.id FOR UPDATE OF c`, ev.SelfID, ev.GroupID, ev.UserID, ev.Time)
	if err != nil {
		return err
	}
	for rows.Next() {
		var codeID int64
		if err = rows.Scan(&codeID); err != nil {
			_ = rows.Close()
			return err
		}
	}
	err = rows.Err()
	_ = rows.Close()
	if err != nil {
		return err
	}
	for _, id := range ids {
		if err = s.revokeRecord(ctx, tx, id, time.Unix(ev.Time, 0)); err != nil {
			return err
		}
	}
	return nil
}

// Caller holds the record row. Lock order matches redemption from the code
// onward: code -> user NO KEY UPDATE -> subscription. No receipt means no debit.
func (s *Store) revokeRecord(ctx context.Context, tx *sql.Tx, id int64, departedAt time.Time) error {
	var group, inviter string
	if err := tx.QueryRowContext(ctx, `SELECT group_id,inviter_qq FROM qq_invite_records WHERE id=$1`, id).Scan(&group, &inviter); err != nil {
		return err
	}
	var codeID int64
	var codeStatus string
	var usedBy, codeGroup sql.NullInt64
	err := tx.QueryRowContext(ctx, `SELECT c.id,c.status,c.used_by,c.group_id FROM qq_invite_rewards w JOIN qq_invite_code_pool p ON p.id=w.pool_id JOIN redeem_codes c ON c.id=p.redeem_code_id WHERE w.record_id=$1 FOR UPDATE OF c`, id).Scan(&codeID, &codeStatus, &usedBy, &codeGroup)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	reason, status, private := "departed_before_reward", "applied", "被邀请人 {invitee} 已退出群聊，本次月卡资格已取消。"
	var receiptID sql.NullInt64
	if codeID > 0 {
		reason = "departed_unused"
		private = "被邀请人 {invitee} 已退出群聊，兑换码 {code} 未兑换，现已永久作废。"
		if codeStatus == "used" {
			reason = "departed_reclaimed"
			private = "被邀请人 {invitee} 已退出群聊，兑换码 {code} 已兑换，本次对应福利额度已收回。"
			if usedBy.Valid {
				var userID int64
				err = tx.QueryRowContext(ctx, `SELECT id FROM users WHERE id=$1 FOR NO KEY UPDATE`, usedBy.Int64).Scan(&userID)
				if err != nil && !errors.Is(err, sql.ErrNoRows) {
					return err
				}
			}
			var matches bool
			err = tx.QueryRowContext(ctx, `SELECT g.subscription_id,s.entitlement_kind='welfare' AND s.user_id=$2 AND s.group_id=$3 AND g.monthly_limit_usd IS NOT NULL FROM user_welfare_subscription_grants g JOIN user_subscriptions s ON s.id=g.subscription_id WHERE g.redeem_code_id=$1 FOR UPDATE OF s`, codeID, usedBy, codeGroup).Scan(&receiptID, &matches)
			if err != nil && !errors.Is(err, sql.ErrNoRows) {
				return err
			}
			if errors.Is(err, sql.ErrNoRows) || !matches {
				receiptID = sql.NullInt64{}
				status, reason = "manual_review", "departure_receipt_missing"
				private = "被邀请人 {invitee} 已退出群聊，兑换码 {code} 本次资格已取消；历史兑换凭据无法自动核对，额度待管理员人工核对，尚未自动扣回。"
			}
		} else if codeStatus != "unused" {
			status, reason = "manual_review", "departure_code_state_unknown"
			private = "被邀请人 {invitee} 已退出群聊，兑换码 {code} 本次资格已取消；兑换状态待管理员人工核对。"
		}
	}
	var revocationID int64
	var codeArg any
	if codeID > 0 {
		codeArg = codeID
	}
	err = tx.QueryRowContext(ctx, `INSERT INTO qq_invite_reward_revocations(record_id,redeem_code_id,subscription_id,user_id,group_id,departed_at,status,reason) VALUES($1,$2,$3,$4,$5,$6,$7,$8) RETURNING id`, id, codeArg, receiptID, usedBy, codeGroup, departedAt, status, reason).Scan(&revocationID)
	if err != nil {
		return err
	}
	if receiptID.Valid {
		// Every arithmetic operation stays NUMERIC in PostgreSQL. Preserve usage,
		// windows, expiry, status, grant count, and the identity of the original term.
		_, err = tx.ExecContext(ctx, `UPDATE qq_invite_reward_revocations v SET daily_limit_usd=g.daily_limit_usd,weekly_limit_usd=g.weekly_limit_usd,monthly_limit_usd=g.monthly_limit_usd,before_daily_limit_usd=s.welfare_daily_limit_usd,before_weekly_limit_usd=s.welfare_weekly_limit_usd,before_monthly_limit_usd=s.welfare_monthly_limit_usd,after_daily_limit_usd=CASE WHEN s.welfare_daily_limit_usd IS NULL THEN
 CASE WHEN EXISTS(SELECT 1 FROM user_welfare_subscription_grants remaining WHERE remaining.subscription_id=s.id AND remaining.daily_limit_usd IS NULL AND NOT EXISTS(SELECT 1 FROM qq_invite_reward_revocations rv WHERE rv.redeem_code_id=remaining.redeem_code_id AND rv.status='applied')) THEN NULL
 ELSE COALESCE((SELECT sum(remaining.daily_limit_usd) FROM user_welfare_subscription_grants remaining WHERE remaining.subscription_id=s.id AND NOT EXISTS(SELECT 1 FROM qq_invite_reward_revocations rv WHERE rv.redeem_code_id=remaining.redeem_code_id AND rv.status='applied')),0) END
 ELSE GREATEST(0,s.welfare_daily_limit_usd-COALESCE(g.daily_limit_usd,0)) END,after_weekly_limit_usd=CASE WHEN s.welfare_weekly_limit_usd IS NULL THEN
 CASE WHEN EXISTS(SELECT 1 FROM user_welfare_subscription_grants remaining WHERE remaining.subscription_id=s.id AND remaining.weekly_limit_usd IS NULL AND NOT EXISTS(SELECT 1 FROM qq_invite_reward_revocations rv WHERE rv.redeem_code_id=remaining.redeem_code_id AND rv.status='applied')) THEN NULL
 ELSE COALESCE((SELECT sum(remaining.weekly_limit_usd) FROM user_welfare_subscription_grants remaining WHERE remaining.subscription_id=s.id AND NOT EXISTS(SELECT 1 FROM qq_invite_reward_revocations rv WHERE rv.redeem_code_id=remaining.redeem_code_id AND rv.status='applied')),0) END
 ELSE GREATEST(0,s.welfare_weekly_limit_usd-COALESCE(g.weekly_limit_usd,0)) END,after_monthly_limit_usd=GREATEST(0,s.welfare_monthly_limit_usd-g.monthly_limit_usd) FROM user_welfare_subscription_grants g JOIN user_subscriptions s ON s.id=g.subscription_id WHERE v.id=$1 AND g.redeem_code_id=v.redeem_code_id`, revocationID)
		if err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx, `UPDATE user_subscriptions s SET welfare_daily_limit_usd=v.after_daily_limit_usd,welfare_weekly_limit_usd=v.after_weekly_limit_usd,welfare_monthly_limit_usd=v.after_monthly_limit_usd,daily_reset_version=s.daily_reset_version+1,updated_at=now() FROM qq_invite_reward_revocations v WHERE v.id=$1 AND s.id=v.subscription_id`, revocationID)
		if err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO qq_invite_subscription_invalidations(revocation_id,user_id,group_id) VALUES($1,$2,$3)`, revocationID, usedBy, codeGroup); err != nil {
			return err
		}
	}
	if codeStatus == "unused" {
		if _, err = tx.ExecContext(ctx, `UPDATE redeem_codes SET status='cancelled' WHERE id=$1`, codeID); err != nil {
			return err
		}
	}
	recordReason := reason
	if status == "manual_review" {
		recordReason = "revocation_manual_review"
	}
	if _, err = tx.ExecContext(ctx, `UPDATE qq_invite_records SET status='revoked',reason=$2,lease_token=NULL,lease_until=NULL,updated_at=now() WHERE id=$1`, id, recordReason); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE qq_invite_outbox SET status='cancelled',lease_token=NULL,lease_until=NULL WHERE record_id=$1 AND kind NOT IN ('revoke_private','revoke_group') AND status<>'sent'`, id); err != nil {
		return err
	}
	if err = enqueue(ctx, tx, id, "revoke_group", group, "", "{invitee}已退出群聊，已取消本次月卡资格"); err != nil {
		return err
	}
	if inviter != "" {
		return enqueue(ctx, tx, id, "revoke_private", inviter, "", private)
	}
	return nil
}

func (s *Store) invalidateOnce(ctx context.Context) error {
	s.invalidatorMu.RLock()
	fn := s.invalidateSubscription
	s.invalidatorMu.RUnlock()
	if fn == nil {
		return nil
	}
	var id, userID, groupID int64
	lease := token()
	err := s.scanWrite(ctx, `UPDATE qq_invite_subscription_invalidations SET lease_token=$1,lease_until=now()+interval '60 seconds',attempts=attempts+1 WHERE revocation_id=(SELECT revocation_id FROM qq_invite_subscription_invalidations WHERE next_attempt_at<=now() AND (lease_until IS NULL OR lease_until<now()) ORDER BY revocation_id FOR UPDATE SKIP LOCKED LIMIT 1) RETURNING revocation_id,user_id,group_id`, []any{lease}, &id, &userID, &groupID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	if err = fn(userID, groupID); err != nil {
		// Keep errors generic: external cache clients can include connection secrets.
		return s.execWrite(ctx, `UPDATE qq_invite_subscription_invalidations SET lease_token=NULL,lease_until=NULL,last_error='cache_invalidation_failed',next_attempt_at=now()+LEAST(256,power(2,LEAST(attempts,8))) * interval '1 second' WHERE revocation_id=$1 AND lease_token=$2`, id, lease)
	}
	return s.execWrite(ctx, `DELETE FROM qq_invite_subscription_invalidations WHERE revocation_id=$1 AND lease_token=$2`, id, lease)
}
