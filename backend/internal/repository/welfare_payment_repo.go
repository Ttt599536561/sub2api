package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/internal/payment"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/shopspring/decimal"
)

type welfarePaymentRepository struct{}

func NewWelfarePaymentRepository() service.WelfarePaymentRepository {
	return &welfarePaymentRepository{}
}

// AccrueSubscriptionPurchase shares the fulfillment transaction. The order is
// locked first, then the user and wallet, matching welfare and usage lock order.
func (*welfarePaymentRepository) AccrueSubscriptionPurchase(ctx context.Context, orderID int64) error {
	client, err := welfarePaymentTxClient(ctx)
	if err != nil {
		return err
	}
	order, err := welfarePaymentLockedOrder(ctx, client, orderID)
	if err != nil {
		return err
	}
	reward, err := welfarePaymentReward(ctx, client, orderID)
	if err != nil {
		return err
	}
	if reward != nil {
		return reward.validateOrder(order)
	}
	if order.OrderType != payment.OrderTypeSubscription || order.Status != service.OrderStatusCompleted ||
		order.PaidAt == nil || service.PaymentOrderCurrency(order.PaymentOrder) != "CNY" {
		return nil
	}
	draws := order.paid.Div(decimal.NewFromInt(50)).Floor().IntPart()
	if draws <= 0 {
		return nil
	}
	if !order.amount.IsPositive() {
		return errors.New("invalid welfare subscription order amount")
	}
	var eligible bool
	if err := welfarePaymentScan(ctx, client, `SELECT EXISTS(SELECT 1 FROM welfare_programs
		WHERE id=1 AND enabled AND launch_at IS NOT NULL AND launch_at<=statement_timestamp() AND launch_at<=$1)`,
		[]any{*order.PaidAt}, &eligible); err != nil {
		return err
	}
	if !eligible {
		return nil
	}
	if err := welfarePaymentLockUser(ctx, client, order.UserID); err != nil {
		return err
	}
	var walletID int64
	if err := welfarePaymentScan(ctx, client, `SELECT user_id FROM welfare_wallets WHERE user_id=$1 FOR UPDATE`, []any{order.UserID}, &walletID); err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	// Check the program again after potentially waiting for the user lock. No
	// program lock precedes the user/wallet locks used by check-in and redemption.
	result, err := client.ExecContext(ctx, `INSERT INTO welfare_subscription_rewards
		(order_id,user_id,order_amount,paid_amount_cny,paid_at,granted_draws)
		SELECT $1,$2,$3::numeric(20,2),$4::numeric(20,2),$5,$6 FROM welfare_programs
		WHERE id=1 AND enabled AND launch_at IS NOT NULL AND launch_at<=statement_timestamp() AND launch_at<=$5`,
		order.ID, order.UserID, order.amount.String(), order.paid.String(), *order.PaidAt, draws)
	if err != nil {
		return err
	}
	inserted, err := result.RowsAffected()
	if err != nil || inserted == 0 {
		return err
	}
	_, err = client.ExecContext(ctx, `INSERT INTO welfare_wallets(user_id,subscription_draws,wallet_version) VALUES($1,$2,1)
		ON CONFLICT(user_id) DO UPDATE SET subscription_draws=welfare_wallets.subscription_draws+EXCLUDED.subscription_draws,
		wallet_version=welfare_wallets.wallet_version+1,updated_at=NOW()`, order.UserID, draws)
	return err
}

// ReverseSubscriptionPurchase records the confirmed refund once. Previously
// spent draws and earned rewards stay intact; a reduced entitlement is debt.
func (*welfarePaymentRepository) ReverseSubscriptionPurchase(ctx context.Context, orderID int64) error {
	client, err := welfarePaymentTxClient(ctx)
	if err != nil {
		return err
	}
	order, err := welfarePaymentLockedOrder(ctx, client, orderID)
	if err != nil {
		return err
	}
	if order.Status != service.OrderStatusRefunded && order.Status != service.OrderStatusPartiallyRefunded {
		return nil
	}
	reward, err := welfarePaymentReward(ctx, client, orderID)
	if err != nil || reward == nil {
		return err
	}
	if err := reward.validateOrder(order); err != nil {
		return err
	}
	// CNY uses the same one-cent tolerance as payment refund validation. The
	// gateway helper preserves its full-refund and proportional rounding rules.
	if !order.refund.IsPositive() || order.RefundAmount-order.Amount > 0.01 {
		return errors.New("invalid welfare subscription refund amount")
	}
	refunded := decimal.NewFromFloat(service.PaymentGatewayRefundAmount(order.Amount, order.PayAmount, order.RefundAmount, "CNY"))
	if refunded.IsNegative() || refunded.GreaterThan(reward.paid) {
		return errors.New("welfare subscription refund exceeds original payment")
	}
	retained := reward.paid.Sub(refunded).Div(decimal.NewFromInt(50)).Floor().IntPart()
	reversed := reward.granted - retained
	reason := ""
	if order.RefundReason != nil {
		reason = strings.TrimSpace(*order.RefundReason)
	}
	if reason == "" {
		reason = fmt.Sprintf("Confirmed refund of subscription payment order %d", order.ID)
	}
	if reward.processedAt.Valid {
		if reward.refundAmount.String != order.refund.StringFixed(2) || reward.refundStatus.String != order.Status ||
			reward.reason.String != reason || !reward.refunded.Equal(refunded) || reward.reversed != reversed {
			return errors.New("welfare subscription refund idempotency conflict")
		}
		return nil
	}
	if err := welfarePaymentLockRefundUser(ctx, client, order.UserID); err != nil {
		return err
	}
	var walletDraws int64
	if err := welfarePaymentScan(ctx, client, `SELECT subscription_draws FROM welfare_wallets WHERE user_id=$1 FOR UPDATE`, []any{order.UserID}, &walletDraws); err != nil {
		return err
	}
	if walletDraws < reversed {
		return errors.New("welfare subscription wallet is below the refund entitlement")
	}
	_, err = client.ExecContext(ctx, `UPDATE welfare_subscription_rewards SET reversed_draws=$2,refunded_amount_cny=$3::numeric(20,2),
		refund_amount=$4::numeric(20,2),refund_status=$5,refund_reason=$6,refund_processed_at=NOW(),updated_at=NOW() WHERE order_id=$1`,
		order.ID, reversed, refunded.String(), order.refund.String(), order.Status, reason)
	if err != nil {
		return err
	}
	if reversed == 0 {
		return nil
	}
	_, err = client.ExecContext(ctx, `UPDATE welfare_wallets SET subscription_draws=subscription_draws-$2,
		wallet_version=wallet_version+1,updated_at=NOW() WHERE user_id=$1`, order.UserID, reversed)
	return err
}

type welfarePaymentOrderSnapshot struct {
	*dbent.PaymentOrder
	amount, paid, refund decimal.Decimal
}

type welfareSubscriptionReward struct {
	userID                             int64
	amount, paid                       decimal.Decimal
	paidAt                             time.Time
	granted, reversed                  int64
	refunded                           decimal.Decimal
	refundAmount, refundStatus, reason sql.NullString
	processedAt                        sql.NullTime
}

func welfarePaymentTxClient(ctx context.Context) (*dbent.Client, error) {
	tx := dbent.TxFromContext(ctx)
	if tx == nil {
		return nil, errors.New("welfare subscription accounting requires an Ent transaction")
	}
	return tx.Client(), nil
}

// Ent exposes QueryContext on the transaction client, but not QueryRowContext.
func welfarePaymentScan(ctx context.Context, client *dbent.Client, query string, args []any, dest ...any) error {
	rows, err := client.QueryContext(ctx, query, args...)
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
	return rows.Scan(dest...)
}

func welfarePaymentLockedOrder(ctx context.Context, client *dbent.Client, orderID int64) (*welfarePaymentOrderSnapshot, error) {
	order := &welfarePaymentOrderSnapshot{PaymentOrder: &dbent.PaymentOrder{}}
	var amount, paid, refund string
	var paidAt sql.NullTime
	var reason sql.NullString
	var snapshot []byte
	err := welfarePaymentScan(ctx, client, `SELECT id,user_id,order_type,status,amount::text,pay_amount::text,
		paid_at,provider_snapshot,refund_amount::text,refund_reason FROM payment_orders WHERE id=$1 FOR UPDATE`,
		[]any{orderID}, &order.ID, &order.UserID, &order.OrderType, &order.Status, &amount, &paid,
		&paidAt, &snapshot, &refund, &reason)
	if err != nil {
		return nil, fmt.Errorf("lock welfare payment order %d: %w", orderID, err)
	}
	if paidAt.Valid {
		order.PaidAt = &paidAt.Time
	}
	if reason.Valid {
		order.RefundReason = &reason.String
	}
	if len(snapshot) > 0 {
		if err := json.Unmarshal(snapshot, &order.ProviderSnapshot); err != nil {
			return nil, fmt.Errorf("decode welfare payment currency snapshot: %w", err)
		}
	}
	if order.amount, err = decimal.NewFromString(amount); err != nil {
		return nil, err
	}
	if order.paid, err = decimal.NewFromString(paid); err != nil {
		return nil, err
	}
	if order.refund, err = decimal.NewFromString(refund); err != nil {
		return nil, err
	}
	order.Amount, order.PayAmount, order.RefundAmount = order.amount.InexactFloat64(), order.paid.InexactFloat64(), order.refund.InexactFloat64()
	return order, nil
}

func welfarePaymentReward(ctx context.Context, client *dbent.Client, orderID int64) (*welfareSubscriptionReward, error) {
	reward := &welfareSubscriptionReward{}
	var amount, paid, refunded string
	err := welfarePaymentScan(ctx, client, `SELECT user_id,order_amount::text,paid_amount_cny::text,paid_at,granted_draws,
		reversed_draws,refunded_amount_cny::text,refund_amount::text,refund_status,refund_reason,refund_processed_at
		FROM welfare_subscription_rewards WHERE order_id=$1`, []any{orderID}, &reward.userID, &amount, &paid, &reward.paidAt,
		&reward.granted, &reward.reversed, &refunded, &reward.refundAmount, &reward.refundStatus, &reward.reason, &reward.processedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if reward.amount, err = decimal.NewFromString(amount); err != nil {
		return nil, err
	}
	if reward.paid, err = decimal.NewFromString(paid); err != nil {
		return nil, err
	}
	if reward.refunded, err = decimal.NewFromString(refunded); err != nil {
		return nil, err
	}
	return reward, nil
}

func (reward *welfareSubscriptionReward) validateOrder(order *welfarePaymentOrderSnapshot) error {
	if reward.userID != order.UserID || !reward.amount.Equal(order.amount) || !reward.paid.Equal(order.paid) ||
		order.PaidAt == nil || !reward.paidAt.Equal(*order.PaidAt) || order.OrderType != payment.OrderTypeSubscription ||
		service.PaymentOrderCurrency(order.PaymentOrder) != "CNY" {
		return errors.New("welfare subscription reward snapshot conflict")
	}
	return nil
}

func welfarePaymentLockUser(ctx context.Context, client *dbent.Client, userID int64) error {
	var lockedID int64
	return welfarePaymentScan(ctx, client, `SELECT id FROM users WHERE id=$1 AND deleted_at IS NULL FOR UPDATE`, []any{userID}, &lockedID)
}

// Confirmed refunds must settle even if the user was soft-deleted after the
// purchase or while the gateway refund was pending. Accrual still requires a
// live user through welfarePaymentLockUser.
func welfarePaymentLockRefundUser(ctx context.Context, client *dbent.Client, userID int64) error {
	var lockedID int64
	return welfarePaymentScan(ctx, client, `SELECT id FROM users WHERE id=$1 FOR UPDATE`, []any{userID}, &lockedID)
}
