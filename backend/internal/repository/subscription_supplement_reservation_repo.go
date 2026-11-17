package repository

import (
	"context"
	"database/sql"
	"errors"
	"math"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
)

type supplementSQLQueryer interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}

func supplementQueryOne(ctx context.Context, q supplementSQLQueryer, statement string, args []any, dest ...any) error {
	rows, err := q.QueryContext(ctx, statement, args...)
	if err != nil {
		return err
	}
	defer func() { _ = rows.Close() }()
	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return err
		}
		return sql.ErrNoRows
	}
	if err := rows.Scan(dest...); err != nil {
		return err
	}
	return rows.Err()
}

func (r *userSubscriptionRepository) ReserveSubscriptionSupplement(ctx context.Context, hold *service.SubscriptionSupplementReservation) error {
	if hold == nil || hold.ID == "" || hold.TTL <= 0 || hold.SubscriptionID <= 0 || hold.UserID <= 0 || hold.GroupID <= 0 || hold.TermStartsAt.IsZero() || hold.AmountUSD <= 0 || math.IsNaN(hold.AmountUSD) || math.IsInf(hold.AmountUSD, 0) {
		return service.ErrSubscriptionInvalid
	}
	tx, err := clientFromContext(ctx, r.client).BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	var start time.Time
	err = supplementQueryOne(ctx, tx, `SELECT starts_at FROM user_subscriptions WHERE id=$1 AND user_id=$2 AND group_id=$3 AND deleted_at IS NULL AND status='active' AND NOT welfare_cycle_closed AND starts_at<=clock_timestamp() AND expires_at>clock_timestamp() FOR UPDATE`, []any{hold.SubscriptionID, hold.UserID, hold.GroupID}, &start)
	if errors.Is(err, sql.ErrNoRows) {
		return service.ErrSubscriptionInvalid
	}
	if err != nil {
		return err
	}
	if !start.Equal(hold.TermStartsAt) {
		return service.ErrSubscriptionInvalid
	}
	var allowed bool
	err = supplementQueryOne(ctx, tx, `WITH pending AS (
 SELECT COALESCE(sum(amount_usd),0) AS amount FROM user_subscription_supplement_reservations WHERE subscription_id=$1 AND term_starts_at=$2 AND released_at IS NULL AND expires_at>clock_timestamp()
), limits AS (
 SELECT s.*,b.granted_usd,b.used_usd,p.amount AS pending,
 CASE WHEN s.entitlement_kind='welfare' THEN s.welfare_daily_limit_usd ELSE g.daily_limit_usd END AS daily_limit,
 CASE WHEN s.entitlement_kind='welfare' THEN s.welfare_weekly_limit_usd ELSE g.weekly_limit_usd END AS weekly_limit,
 CASE WHEN s.entitlement_kind='welfare' THEN s.welfare_monthly_limit_usd ELSE g.monthly_limit_usd END AS monthly_limit
 FROM user_subscriptions s JOIN groups g ON g.id=s.group_id AND g.deleted_at IS NULL AND g.status='active' AND g.subscription_type='subscription'
 JOIN user_subscription_supplement_balances b ON b.subscription_id=s.id AND b.term_starts_at=$2 AND b.closed_at IS NULL CROSS JOIN pending p WHERE s.id=$1
)
SELECT $3::numeric+pending<=LEAST($3::numeric+pending,
 CASE WHEN daily_limit>0 THEN GREATEST(0,daily_limit-daily_usage_usd) ELSE $3::numeric+pending END,
 CASE WHEN weekly_limit>0 THEN GREATEST(0,weekly_limit-weekly_usage_usd) ELSE $3::numeric+pending END,
 CASE WHEN monthly_limit>0 OR entitlement_kind='welfare' THEN GREATEST(0,COALESCE(monthly_limit,0)-monthly_usage_usd) ELSE $3::numeric+pending END)+GREATEST(0,granted_usd-used_usd)
FROM limits`, []any{hold.SubscriptionID, hold.TermStartsAt, hold.AmountUSD}, &allowed)
	if errors.Is(err, sql.ErrNoRows) {
		return service.ErrSubscriptionInvalid
	}
	if err != nil {
		return err
	}
	if !allowed {
		return service.ErrMonthlyLimitExceeded
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO user_subscription_supplement_reservations(id,subscription_id,term_starts_at,user_id,group_id,amount_usd,expires_at) VALUES($1,$2,$3,$4,$5,$6,clock_timestamp()+$7*interval '1 second')`, hold.ID, hold.SubscriptionID, hold.TermStartsAt, hold.UserID, hold.GroupID, hold.AmountUSD, hold.TTL.Seconds())
	if err != nil {
		return err
	}
	return tx.Commit()
}

func (r *userSubscriptionRepository) RenewSubscriptionSupplementReservation(ctx context.Context, id string, ttl time.Duration) (bool, error) {
	if id == "" || ttl <= 0 {
		return false, service.ErrSubscriptionInvalid
	}
	result, err := clientFromContext(ctx, r.client).ExecContext(ctx, `UPDATE user_subscription_supplement_reservations SET expires_at=GREATEST(expires_at,clock_timestamp()+$2*interval '1 second') WHERE id=$1 AND released_at IS NULL AND expires_at>clock_timestamp()`, id, ttl.Seconds())
	if err != nil {
		return false, err
	}
	n, err := result.RowsAffected()
	return n == 1, err
}

func (r *userSubscriptionRepository) ReleaseSubscriptionSupplementReservation(ctx context.Context, id string) error {
	if id == "" {
		return nil
	}
	_, err := clientFromContext(ctx, r.client).ExecContext(ctx, `UPDATE user_subscription_supplement_reservations SET released_at=COALESCE(released_at,clock_timestamp()) WHERE id=$1`, id)
	return err
}

func (r *userSubscriptionRepository) TransferSubscriptionSupplementReservation(ctx context.Context, id, taskKey string, ttl time.Duration) error {
	if id == "" || strings.TrimSpace(taskKey) == "" || ttl <= 0 {
		return service.ErrSubscriptionInvalid
	}
	result, err := clientFromContext(ctx, r.client).ExecContext(ctx, `UPDATE user_subscription_supplement_reservations SET external_task_key=$2,expires_at=GREATEST(expires_at,clock_timestamp()+$3*interval '1 second') WHERE id=$1 AND released_at IS NULL AND expires_at>clock_timestamp() AND (external_task_key IS NULL OR external_task_key=$2)`, id, taskKey, ttl.Seconds())
	if err != nil {
		return err
	}
	n, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if n != 1 {
		return service.ErrSubscriptionInvalid
	}
	return nil
}

var _ service.SubscriptionSupplementReservationRepository = (*userSubscriptionRepository)(nil)
