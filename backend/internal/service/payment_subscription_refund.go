package service

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strconv"
	"time"

	"entgo.io/ent/dialect"
	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/ent/paymentauditlog"
	"github.com/Wei-Shaw/sub2api/internal/payment"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
)

func isPostgresMonthlySubscriptionOrder(client *dbent.Client, order *dbent.PaymentOrder) bool {
	return order != nil && order.OrderType == payment.OrderTypeSubscription &&
		order.SubscriptionDays != nil && *order.SubscriptionDays == 30 &&
		paymentAuditDialect(client) == dialect.Postgres
}

func (s *PaymentService) preparePaidMonthlyRenewalRefund(ctx context.Context, order *dbent.PaymentOrder, plan *RefundPlan) (bool, error) {
	if !isPostgresMonthlySubscriptionOrder(s.entClient, order) {
		return false, nil
	}
	rows, err := s.entClient.QueryContext(ctx,
		"SELECT payment_order_id FROM paid_subscription_monthly_renewals WHERE payment_order_id = $1", order.ID)
	if err != nil {
		return false, fmt.Errorf("look up paid monthly renewal for refund: %w", err)
	}
	found := rows.Next()
	if err := rows.Close(); err != nil {
		return false, fmt.Errorf("close paid monthly receipt lookup: %w", err)
	}
	if !found {
		return false, rows.Err()
	}
	if plan.RefundAmount < order.Amount {
		return false, infraerrors.BadRequest("PAID_RENEWAL_PARTIAL_DEDUCTION", "partial refund of a paid monthly renewal must leave subscription entitlement unchanged")
	}
	// Keep the original paid grant available until the gateway confirms the
	// refund. Finalization then removes the quota and days in one transaction.
	plan.DeductionType = payment.DeductionTypeSubscription
	return true, nil
}

func settlePaidMonthlyRenewalRefund(ctx context.Context, client *dbent.Client, plan *RefundPlan) (int, error) {
	if !isPostgresMonthlySubscriptionOrder(client, plan.Order) ||
		plan.RefundAmount < plan.Order.Amount || !refundPlanShouldDeduct(plan) {
		return 0, nil
	}
	// Lock/mark the receipt once and only affect its own term, including a
	// soft-deleted matching term that an administrator could restore later.
	rows, err := client.QueryContext(ctx, `
WITH receipt AS (
    UPDATE paid_subscription_monthly_renewals SET reversed_at = clock_timestamp()
    WHERE payment_order_id = $1 AND reversed_at IS NULL
    RETURNING subscription_id, user_id, group_id, term_starts_at, amount_usd, validity_days
), affected AS (
    UPDATE user_subscriptions s
    SET paid_monthly_limit_usd = GREATEST(0, s.paid_monthly_limit_usd - receipt.amount_usd),
        expires_at = CASE WHEN s.expires_at > now()
                          THEN GREATEST(s.starts_at, s.expires_at - make_interval(days => receipt.validity_days))
                          ELSE s.expires_at END,
        status = CASE WHEN s.expires_at > now()
                           AND s.expires_at - make_interval(days => receipt.validity_days) <= now()
                      THEN 'expired' ELSE s.status END,
        updated_at = now(),
        daily_reset_version = s.daily_reset_version + 1
    FROM receipt
    WHERE s.id = receipt.subscription_id AND s.user_id = receipt.user_id AND s.group_id = receipt.group_id
      AND s.starts_at = receipt.term_starts_at AND s.entitlement_kind = 'standard' AND s.paid_monthly_limit_usd IS NOT NULL
    RETURNING receipt.validity_days
)
SELECT validity_days FROM affected`, plan.OrderID)
	if err != nil {
		return 0, fmt.Errorf("reverse paid monthly renewal: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var days int
	for rows.Next() {
		if err := rows.Scan(&days); err != nil {
			return 0, fmt.Errorf("read reversed monthly renewal: %w", err)
		}
	}
	return days, rows.Err()
}

func (s *PaymentService) refundOrderMatchesSubscriptionTerm(ctx context.Context, order *dbent.PaymentOrder, sub *UserSubscription) bool {
	if order == nil || sub == nil || sub.StartsAt.IsZero() || sub.UserID != order.UserID ||
		order.SubscriptionGroupID == nil || sub.GroupID != *order.SubscriptionGroupID {
		return false
	}
	client := s.entClient
	if tx := dbent.TxFromContext(ctx); tx != nil {
		client = tx.Client()
	}
	if client != nil {
		audit, err := client.PaymentAuditLog.Query().Where(
			paymentauditlog.OrderIDEQ(strconv.FormatInt(order.ID, 10)),
			paymentauditlog.ActionEQ("SUBSCRIPTION_ASSIGNED"),
		).First(ctx)
		if err == nil {
			var assignment struct {
				SubscriptionID    int64     `json:"subscriptionID"`
				TermStartsAt      time.Time `json:"termStartsAt"`
				RecoveredFromNote bool      `json:"recoveredFromNote"`
			}
			if err := json.Unmarshal([]byte(audit.Detail), &assignment); err != nil {
				return false
			}
			if assignment.SubscriptionID > 0 && !assignment.TermStartsAt.IsZero() {
				return assignment.SubscriptionID == sub.ID && assignment.TermStartsAt.Equal(sub.StartsAt)
			}
			if !assignment.RecoveredFromNote {
				return !audit.CreatedAt.Before(sub.StartsAt)
			}
		} else if !dbent.IsNotFound(err) {
			return false
		}
	}
	// Legacy rows may only have timestamps. The recorded completion time is
	// closest to assignment, because first purchase can start after PaidAt.
	if order.CompletedAt != nil {
		return !order.CompletedAt.Before(sub.StartsAt)
	}
	return order.PaidAt != nil && !order.PaidAt.Before(sub.StartsAt)
}

func (s *PaymentService) withOriginalRefundSubscriptionTerm(ctx context.Context, plan *RefundPlan, isCompensation bool, operation func(context.Context) error) error {
	return s.subscriptionSvc.withSubscriptionUpdateTx(ctx, func(txCtx context.Context) error {
		sub, err := s.subscriptionSvc.userSubRepo.GetByIDForUpdate(txCtx, plan.SubscriptionID)
		if err != nil {
			return err
		}
		if isCompensation && plan.subscriptionTermStartsAt != nil && sub.StartsAt.After(*plan.subscriptionTermStartsAt) &&
			!sub.IsWelfare() && sub.UserID == plan.Order.UserID && plan.Order.SubscriptionGroupID != nil && sub.GroupID == *plan.Order.SubscriptionGroupID {
			// This exact old term was replaced while the provider was handling
			// the refund. Its deduction cannot apply to the new entitlement.
			plan.subscriptionCompensationSuperseded = true
			return nil
		}
		if !s.refundPlanMatchesSubscriptionTerm(txCtx, plan, sub) {
			return infraerrors.Conflict("SUBSCRIPTION_REFUND_TERM_CHANGED", "the original subscription term is no longer current")
		}
		return operation(txCtx)
	})
}

func (s *PaymentService) refundPlanMatchesSubscriptionTerm(ctx context.Context, plan *RefundPlan, sub *UserSubscription) bool {
	if plan.subscriptionTermStartsAt != nil && (sub == nil || !plan.subscriptionTermStartsAt.Equal(sub.StartsAt)) {
		return false
	}
	return s.refundOrderMatchesSubscriptionTerm(ctx, plan.Order, sub)
}

func (s *PaymentService) shortenRefundSubscription(ctx context.Context, plan *RefundPlan) error {
	err := s.withOriginalRefundSubscriptionTerm(ctx, plan, false, func(txCtx context.Context) error {
		_, err := s.subscriptionSvc.ExtendSubscription(txCtx, plan.SubscriptionID, -plan.SubDaysToDeduct)
		return err
	})
	if err == nil && dbent.TxFromContext(ctx) == nil && plan.Order.SubscriptionGroupID != nil {
		if cacheErr := s.subscriptionSvc.invalidateSubscriptionCaches(plan.Order.UserID, *plan.Order.SubscriptionGroupID); cacheErr != nil {
			slog.Warn("invalidate committed refund shortening cache", "orderID", plan.OrderID, "error", cacheErr)
		}
	}
	return err
}
