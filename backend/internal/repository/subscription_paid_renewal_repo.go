package repository

import (
	"context"
	"fmt"
	"time"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/internal/service"
)

func (r *userSubscriptionRepository) GrantPaidMonthlyRenewal(ctx context.Context, sub *service.UserSubscription, grant service.PaidMonthlyRenewalGrant) error {
	tx := dbent.TxFromContext(ctx)
	if tx == nil {
		return fmt.Errorf("paid monthly renewal requires a payment transaction")
	}
	if !grant.AfterExpiresAt.Equal(sub.ExpiresAt.AddDate(0, 0, grant.ValidityDays)) {
		return fmt.Errorf("paid monthly renewal exceeds the subscription expiry limit")
	}
	_, err := tx.ExecContext(ctx, `INSERT INTO paid_subscription_monthly_renewals
 (payment_order_id,subscription_id,user_id,group_id,term_starts_at,amount_usd,validity_days,before_expires_at,after_expires_at)
 VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)`, grant.PaymentOrderID, sub.ID, sub.UserID, sub.GroupID,
		sub.StartsAt, grant.AmountUSD, grant.ValidityDays, sub.ExpiresAt, grant.AfterExpiresAt)
	if err != nil {
		return err
	}
	result, err := tx.ExecContext(ctx, `UPDATE user_subscriptions SET
 paid_monthly_limit_usd=COALESCE(paid_monthly_limit_usd,$3::numeric)+$4::numeric,
 paid_monthly_legacy_limit_usd=COALESCE(paid_monthly_legacy_limit_usd,$3::numeric),
 paid_monthly_legacy_expires_at=COALESCE(paid_monthly_legacy_expires_at,expires_at),
 daily_reset_version=daily_reset_version+1,updated_at=now()
 WHERE id=$1 AND starts_at=$2 AND status='active' AND entitlement_kind='standard' AND deleted_at IS NULL`,
		sub.ID, sub.StartsAt, grant.BaseMonthlyLimitUSD, grant.AmountUSD)
	if err != nil {
		return err
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if rows != 1 {
		return service.ErrSubscriptionNotFound
	}
	return nil
}

func (r *userSubscriptionRepository) AdvancePaidMonthlyWindow(ctx context.Context, sub *service.UserSubscription, start time.Time, allowanceUSD float64) error {
	client := clientFromContext(ctx, r.client)
	result, err := client.ExecContext(ctx, `UPDATE user_subscriptions SET
 monthly_window_start=$4,paid_monthly_limit_usd=paid_monthly_limit_usd+$5::numeric,
 daily_reset_version=daily_reset_version+1,updated_at=now()
 WHERE id=$1 AND starts_at=$2 AND monthly_window_start=$3 AND deleted_at IS NULL
 AND paid_monthly_limit_usd IS NOT NULL AND $4<paid_monthly_legacy_expires_at AND $4<expires_at`,
		sub.ID, sub.StartsAt, sub.MonthlyWindowStart, start, allowanceUSD)
	if err != nil {
		return err
	}
	rows, err := result.RowsAffected()
	return r.translateConditionalWindowReset(ctx, client, sub.ID, int(rows), err)
}
