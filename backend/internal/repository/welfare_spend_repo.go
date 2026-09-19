package repository

import (
	"context"
	"database/sql"
	"errors"
	"math"
	"strconv"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/service"
)

// RequiresAtomicUsageBilling prevents legacy service paths from silently
// bypassing welfare accounting when this production repository is injected.
func (*userRepository) RequiresAtomicUsageBilling() bool { return true }

func welfareUsageSourceID(requestID string, apiKeyID int64) string {
	return requestID + ":" + strconv.FormatInt(apiKeyID, 10)
}

// RefundWelfareUsageBalance reverses a captured eligible charge in the caller's
// SQL transaction. It credits the account, appends an immutable correction and
// schedules reliable cache invalidation. Earned rewards and draws_used remain
// unchanged, so a reduced entitlement becomes ticket debt. There is no user API
// for this accounting capability. Every correction requires an active admin and
// a reason, which form part of the immutable idempotency identity. Callers must
// invoke this before taking user-row locks; it locks target and actor in ID order.
func RefundWelfareUsageBalance(ctx context.Context, tx *sql.Tx, userID int64, sourceType, debitSourceID, refundSourceID string, amount float64, actorID int64, reason string) (bool, error) {
	amount = service.QuantizeUsageBillingAmount(amount)
	reason = strings.TrimSpace(reason)
	if tx == nil || amount <= 0 || math.IsNaN(amount) || math.IsInf(amount, 0) || debitSourceID == "" || refundSourceID == "" || actorID <= 0 || reason == "" {
		return false, errors.New("invalid welfare refund")
	}
	// The actor FK also acquires a user-row lock when the fact is inserted.
	// Lock both users in a consistent order before the wallet, so reciprocal
	// corrections between admins cannot invert that otherwise implicit lock.
	rows, err := tx.QueryContext(ctx, `SELECT id FROM users WHERE id IN ($1,$2) ORDER BY id FOR UPDATE`, userID, actorID)
	if err != nil {
		return false, err
	}
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			_ = rows.Close()
			return false, err
		}
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return false, err
	}
	if err := rows.Close(); err != nil {
		return false, err
	}
	var activeAdmin bool
	if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM users WHERE id=$1 AND role=$2 AND status=$3 AND deleted_at IS NULL)`, actorID, service.RoleAdmin, service.StatusActive).Scan(&activeAdmin); err != nil {
		return false, err
	}
	if !activeAdmin {
		return false, errors.New("welfare refund requires an active admin actor")
	}
	var lockedID int64
	if err := tx.QueryRowContext(ctx, `SELECT id FROM users WHERE id=$1 AND deleted_at IS NULL FOR UPDATE`, userID).Scan(&lockedID); err != nil {
		return false, err
	}
	var matches bool
	err = tx.QueryRowContext(ctx, `SELECT user_id=$3 AND refund_source_id=$4 AND amount=-$5::numeric(20,8) AND actor_id=$6 AND reason=$7 FROM welfare_spend_events WHERE source_type=$1 AND source_id=$2 AND action='refund'`, sourceType, refundSourceID, userID, debitSourceID, amount, actorID, reason).Scan(&matches)
	if err == nil {
		if matches {
			return false, nil
		}
		return false, errors.New("welfare refund idempotency conflict")
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return false, err
	}
	var allowed bool
	err = tx.QueryRowContext(ctx, `SELECT amount+COALESCE((SELECT SUM(amount) FROM welfare_spend_events WHERE source_type=$1 AND refund_source_id=$2 AND action='refund'),0)>=$4::numeric(20,8)
 FROM welfare_spend_events WHERE source_type=$1 AND source_id=$2 AND action='debit' AND user_id=$3`, sourceType, debitSourceID, userID, amount).Scan(&allowed)
	if err != nil {
		return false, err
	}
	if !allowed {
		return false, errors.New("welfare refund exceeds original charge")
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO welfare_spend_events(user_id,source_type,source_id,action,amount,refund_source_id,actor_id,reason,ticket_delta)
 SELECT user_id,$2,$3,'refund',-$4::numeric(20,8),$5,
 $6,$7,
 floor((eligible_spend-$4::numeric(20,8))/50)::bigint-floor(eligible_spend/50)::bigint
 FROM welfare_wallets WHERE user_id=$1`, userID, sourceType, refundSourceID, amount, debitSourceID, actorID, reason)
	if err != nil {
		return false, err
	}
	var version int64
	err = tx.QueryRowContext(ctx, `UPDATE welfare_wallets SET eligible_spend=eligible_spend-$2::numeric(20,8),wallet_version=wallet_version+1,updated_at=NOW() WHERE user_id=$1 RETURNING wallet_version`, userID, amount).Scan(&version)
	if err != nil {
		return false, err
	}
	_, err = tx.ExecContext(ctx, `UPDATE users SET balance=balance+$2::numeric(20,8),updated_at=NOW() WHERE id=$1`, userID, amount)
	if err != nil {
		return false, err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO welfare_balance_outbox(user_id,version) VALUES($1,$2)`, userID, version)
	if err != nil {
		return false, err
	}
	return true, nil
}

// accrueWelfareSpend runs inside the existing debit transaction, after users has
// been locked. One short statement records an immutable fact and increments the
// wallet with the same NUMERIC(20,8) amount used to deduct the account balance.
func accrueWelfareSpend(ctx context.Context, tx *sql.Tx, userID int64, sourceType, sourceID string, amount float64) error {
	if amount <= 0 {
		return nil
	}
	_, err := tx.ExecContext(ctx, `WITH fact AS (
 INSERT INTO welfare_spend_events(user_id,source_type,source_id,action,amount,ticket_delta)
 SELECT $1,$2,$3,'debit',$4::numeric(20,8),
 floor((COALESCE((SELECT eligible_spend FROM welfare_wallets WHERE user_id=$1),0)+$4::numeric(20,8))/50)::bigint
 -floor(COALESCE((SELECT eligible_spend FROM welfare_wallets WHERE user_id=$1),0)/50)::bigint
 FROM welfare_programs WHERE id=1 AND enabled AND launch_at IS NOT NULL
 AND launch_at<=statement_timestamp() AND $4::numeric(20,8)>0
 ON CONFLICT(source_type,source_id,action) DO NOTHING RETURNING user_id,amount
 ) INSERT INTO welfare_wallets(user_id,eligible_spend,wallet_version)
 SELECT user_id,amount,1 FROM fact
 ON CONFLICT(user_id) DO UPDATE SET eligible_spend=welfare_wallets.eligible_spend+EXCLUDED.eligible_spend,
 wallet_version=welfare_wallets.wallet_version+1,updated_at=NOW()`, userID, sourceType, sourceID, amount)
	return err
}
