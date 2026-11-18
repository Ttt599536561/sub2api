package repository

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
)

// The subscription lock is always acquired before its supplement projection.
// Allocation, quota usage, credit use, receipt and request dedup commit together.
func allocateUsageBillingSubscription(ctx context.Context, tx *sql.Tx, cmd *service.UsageBillingCommand, result *service.UsageBillingApplyResult) error {
	legacy := func() error {
		result.SubscriptionBaseCost = cmd.SubscriptionCost
		return incrementUsageBillingSubscription(ctx, tx, *cmd.SubscriptionID, cmd.SubscriptionCost)
	}
	if cmd.SubscriptionTermStartsAt == nil || cmd.SubscriptionTermStartsAt.IsZero() {
		return legacy()
	}
	var startsAt time.Time
	err := tx.QueryRowContext(ctx, `SELECT starts_at FROM user_subscriptions WHERE id=$1 AND deleted_at IS NULL FOR UPDATE`, *cmd.SubscriptionID).Scan(&startsAt)
	if errors.Is(err, sql.ErrNoRows) {
		return service.ErrSubscriptionNotFound
	}
	if err != nil {
		return err
	}
	if !startsAt.Equal(*cmd.SubscriptionTermStartsAt) {
		return allocateClosedSubscriptionSupplement(ctx, tx, cmd, result)
	}
	var beforeUsed string
	err = tx.QueryRowContext(ctx, `SELECT used_usd::text FROM user_subscription_supplement_balances WHERE subscription_id=$1 AND term_starts_at=$2 FOR UPDATE`, *cmd.SubscriptionID, startsAt).Scan(&beforeUsed)
	if errors.Is(err, sql.ErrNoRows) {
		return legacy()
	}
	if err != nil {
		return err
	}
	// All monetary arithmetic is NUMERIC. A single supplement covers the most
	// restrictive base window; it is never separately charged for three windows.
	return tx.QueryRowContext(ctx, `WITH limits AS (
 SELECT s.*,
 CASE WHEN s.entitlement_kind='welfare' THEN s.welfare_daily_limit_usd ELSE g.daily_limit_usd END AS daily_limit,
 CASE WHEN s.entitlement_kind='welfare' THEN s.welfare_weekly_limit_usd ELSE g.weekly_limit_usd END AS weekly_limit,
 CASE WHEN s.entitlement_kind='welfare' THEN s.welfare_monthly_limit_usd ELSE COALESCE(s.paid_monthly_limit_usd,g.monthly_limit_usd) END AS monthly_limit
 FROM user_subscriptions s JOIN groups g ON g.id=s.group_id AND g.deleted_at IS NULL WHERE s.id=$1
), allocation AS (
 SELECT LEAST($3::numeric,
 CASE WHEN daily_limit>0 THEN GREATEST(0,daily_limit-daily_usage_usd) ELSE $3::numeric END,
 CASE WHEN weekly_limit>0 THEN GREATEST(0,weekly_limit-weekly_usage_usd) ELSE $3::numeric END,
 CASE WHEN monthly_limit>0 OR entitlement_kind='welfare' OR paid_monthly_limit_usd IS NOT NULL THEN GREATEST(0,COALESCE(monthly_limit,0)-monthly_usage_usd) ELSE $3::numeric END) AS base_cost
 FROM limits
), base_update AS (
 UPDATE user_subscriptions s SET daily_usage_usd=s.daily_usage_usd+a.base_cost,
 weekly_usage_usd=s.weekly_usage_usd+a.base_cost,monthly_usage_usd=s.monthly_usage_usd+a.base_cost,updated_at=now()
 FROM allocation a WHERE s.id=$1 RETURNING a.base_cost
), supplement_update AS (
 UPDATE user_subscription_supplement_balances b SET used_usd=b.used_usd+$3::numeric-u.base_cost,updated_at=now()
 FROM base_update u WHERE b.subscription_id=$1 AND b.term_starts_at=$2 RETURNING b.used_usd,u.base_cost
)
INSERT INTO user_subscription_supplement_consumptions(request_id,api_key_id,subscription_id,term_starts_at,base_cost_usd,supplement_cost_usd,before_used_usd,after_used_usd)
SELECT $4,$5,$1,$2,base_cost,$3::numeric-base_cost,$6::numeric,used_usd FROM supplement_update
RETURNING base_cost_usd,supplement_cost_usd`, *cmd.SubscriptionID, startsAt, cmd.SubscriptionCost, cmd.RequestID, cmd.APIKeyID, beforeUsed).
		Scan(&result.SubscriptionBaseCost, &result.SubscriptionSupplementCost)
}

func allocateClosedSubscriptionSupplement(ctx context.Context, tx *sql.Tx, cmd *service.UsageBillingCommand, result *service.UsageBillingApplyResult) error {
	var beforeUsed string
	err := tx.QueryRowContext(ctx, `SELECT used_usd::text FROM user_subscription_supplement_balances WHERE subscription_id=$1 AND term_starts_at=$2 AND closed_at IS NOT NULL FOR UPDATE`, *cmd.SubscriptionID, *cmd.SubscriptionTermStartsAt).Scan(&beforeUsed)
	if errors.Is(err, sql.ErrNoRows) {
		// An imported/pre-upgrade old term may have no retained window snapshot.
		// Record that old term's delivered cost as overuse instead of charging
		// any part of the newly renewed entitlement.
		_, err = tx.ExecContext(ctx, `INSERT INTO user_subscription_supplement_balances(subscription_id,term_starts_at,closed_at,closed_base_remaining_usd) VALUES($1,$2,clock_timestamp(),0) ON CONFLICT(subscription_id,term_starts_at) DO NOTHING`, *cmd.SubscriptionID, *cmd.SubscriptionTermStartsAt)
		if err != nil {
			return err
		}
		err = tx.QueryRowContext(ctx, `SELECT used_usd::text FROM user_subscription_supplement_balances WHERE subscription_id=$1 AND term_starts_at=$2 FOR UPDATE`, *cmd.SubscriptionID, *cmd.SubscriptionTermStartsAt).Scan(&beforeUsed)
	}
	if err != nil {
		return err
	}
	return tx.QueryRowContext(ctx, `WITH allocation AS (
 SELECT LEAST($3::numeric,COALESCE(closed_base_remaining_usd,$3::numeric)) AS base_cost
 FROM user_subscription_supplement_balances WHERE subscription_id=$1 AND term_starts_at=$2
), settled AS (
 UPDATE user_subscription_supplement_balances b
 SET used_usd=b.used_usd+$3::numeric-a.base_cost,
 closed_base_remaining_usd=b.closed_base_remaining_usd-a.base_cost,updated_at=now()
 FROM allocation a WHERE b.subscription_id=$1 AND b.term_starts_at=$2 RETURNING b.used_usd,a.base_cost
)
INSERT INTO user_subscription_supplement_consumptions(request_id,api_key_id,subscription_id,term_starts_at,base_cost_usd,supplement_cost_usd,before_used_usd,after_used_usd)
SELECT $4,$5,$1,$2,base_cost,$3::numeric-base_cost,$6::numeric,used_usd FROM settled
RETURNING base_cost_usd,supplement_cost_usd`, *cmd.SubscriptionID, *cmd.SubscriptionTermStartsAt, cmd.SubscriptionCost, cmd.RequestID, cmd.APIKeyID, beforeUsed).
		Scan(&result.SubscriptionBaseCost, &result.SubscriptionSupplementCost)
}

func completeSubscriptionSupplementHold(ctx context.Context, tx *sql.Tx, cmd *service.UsageBillingCommand) error {
	if cmd.SubscriptionReservationID == "" || cmd.SubscriptionID == nil || cmd.SubscriptionTermStartsAt == nil {
		return nil
	}
	_, err := tx.ExecContext(ctx, `UPDATE user_subscription_supplement_reservations SET released_at=COALESCE(released_at,clock_timestamp()) WHERE id=$1 AND subscription_id=$2 AND term_starts_at=$3 AND user_id=$4`, cmd.SubscriptionReservationID, *cmd.SubscriptionID, *cmd.SubscriptionTermStartsAt, cmd.UserID)
	return err
}
