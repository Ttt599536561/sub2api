package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"strconv"
	"strings"
	"time"

	"entgo.io/ent/dialect/sql"
	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/ent/paymentauditlog"
	"github.com/Wei-Shaw/sub2api/ent/paymentorder"
	"github.com/Wei-Shaw/sub2api/ent/paymentproviderinstance"
	"github.com/Wei-Shaw/sub2api/ent/schema/mixins"
	"github.com/Wei-Shaw/sub2api/ent/usersubscription"
	"github.com/Wei-Shaw/sub2api/internal/payment"
	"github.com/Wei-Shaw/sub2api/internal/payment/provider"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/pkg/servertiming"
	"github.com/shopspring/decimal"
)

// --- Refund Flow ---

var createPaymentProviderFromInstance = provider.CreateProvider

// getOrderProviderInstance looks up the provider instance that processed this order.
// For legacy orders without provider_instance_id, it resolves only when the
// historical instance is uniquely identifiable from the stored order fields.
func (s *PaymentService) getOrderProviderInstance(ctx context.Context, o *dbent.PaymentOrder) (*dbent.PaymentProviderInstance, error) {
	if s == nil || s.entClient == nil || o == nil {
		return nil, nil
	}

	if snapshot := psOrderProviderSnapshot(o); snapshot != nil {
		return s.resolveSnapshotOrderProviderInstance(ctx, o, snapshot)
	}

	instIDStr := strings.TrimSpace(psStringValue(o.ProviderInstanceID))
	if instIDStr == "" {
		return s.resolveUniqueLegacyOrderProviderInstance(ctx, o)
	}

	instID, err := strconv.ParseInt(instIDStr, 10, 64)
	if err != nil {
		return nil, nil
	}
	return s.entClient.PaymentProviderInstance.Get(ctx, instID)
}

// getRefundOrderProviderInstance resolves the provider instance for refund paths.
// Refunds must be pinned to an explicit historical binding, so legacy
// "best-effort" provider guessing is intentionally not allowed here.
func (s *PaymentService) getRefundOrderProviderInstance(ctx context.Context, o *dbent.PaymentOrder) (*dbent.PaymentProviderInstance, error) {
	if s == nil || s.entClient == nil || o == nil {
		return nil, nil
	}

	if snapshot := psOrderProviderSnapshot(o); snapshot != nil {
		return s.resolveSnapshotOrderProviderInstance(ctx, o, snapshot)
	}

	instIDStr := strings.TrimSpace(psStringValue(o.ProviderInstanceID))
	if instIDStr == "" {
		return nil, nil
	}

	instID, err := strconv.ParseInt(instIDStr, 10, 64)
	if err != nil {
		return nil, fmt.Errorf("order %d refund provider instance id is invalid: %s", o.ID, instIDStr)
	}
	inst, err := s.entClient.PaymentProviderInstance.Get(ctx, instID)
	if err != nil {
		if dbent.IsNotFound(err) {
			return nil, fmt.Errorf("order %d refund provider instance %s is missing", o.ID, instIDStr)
		}
		return nil, err
	}
	return inst, nil
}

func (s *PaymentService) resolveUniqueLegacyOrderProviderInstance(ctx context.Context, o *dbent.PaymentOrder) (*dbent.PaymentProviderInstance, error) {
	paymentType := payment.GetBasePaymentType(strings.TrimSpace(o.PaymentType))
	providerKey := strings.TrimSpace(psStringValue(o.ProviderKey))
	if providerKey != "" {
		instances, err := s.entClient.PaymentProviderInstance.Query().
			Where(paymentproviderinstance.ProviderKeyEQ(providerKey)).
			All(ctx)
		if err != nil {
			return nil, err
		}
		matched := psFilterLegacyOrderProviderInstances(paymentType, instances)
		if len(matched) == 1 {
			return matched[0], nil
		}
		return nil, nil
	}

	if paymentType == "" {
		return nil, nil
	}

	instances, err := s.entClient.PaymentProviderInstance.Query().
		All(ctx)
	if err != nil {
		return nil, err
	}

	matched := psFilterLegacyOrderProviderInstances(paymentType, instances)
	if len(matched) == 1 {
		return matched[0], nil
	}
	return nil, nil
}

func psFilterLegacyOrderProviderInstances(orderPaymentType string, instances []*dbent.PaymentProviderInstance) []*dbent.PaymentProviderInstance {
	if len(instances) == 0 {
		return nil
	}
	if strings.TrimSpace(orderPaymentType) == "" {
		return instances
	}
	var matched []*dbent.PaymentProviderInstance
	for _, inst := range instances {
		if psLegacyOrderMatchesInstance(orderPaymentType, inst) {
			matched = append(matched, inst)
		}
	}
	return matched
}

func psLegacyOrderMatchesInstance(orderPaymentType string, inst *dbent.PaymentProviderInstance) bool {
	if inst == nil {
		return false
	}

	baseType := payment.GetBasePaymentType(strings.TrimSpace(orderPaymentType))
	instanceProviderKey := strings.TrimSpace(inst.ProviderKey)
	if baseType == "" {
		return false
	}

	if baseType == payment.TypeStripe {
		return instanceProviderKey == payment.TypeStripe
	}
	if instanceProviderKey == payment.TypeStripe {
		return false
	}
	if instanceProviderKey == baseType {
		return true
	}
	return payment.InstanceSupportsType(inst.SupportedTypes, baseType)
}

func (s *PaymentService) RequestRefund(ctx context.Context, oid, uid int64, reason string) error {
	o, err := s.validateRefundRequest(ctx, oid, uid)
	if err != nil {
		return err
	}
	u, err := s.userRepo.GetByID(ctx, o.UserID)
	if err != nil {
		return fmt.Errorf("get user: %w", err)
	}
	if u.Balance < o.Amount {
		return infraerrors.BadRequest("BALANCE_NOT_ENOUGH", "refund amount exceeds balance")
	}
	nr := strings.TrimSpace(reason)
	now := time.Now()
	by := fmt.Sprintf("%d", uid)
	c, err := s.entClient.PaymentOrder.Update().Where(paymentorder.IDEQ(oid), paymentorder.UserIDEQ(uid), paymentorder.StatusEQ(OrderStatusCompleted), paymentorder.OrderTypeEQ(payment.OrderTypeBalance)).SetStatus(OrderStatusRefundRequested).SetRefundRequestedAt(now).SetRefundRequestReason(nr).SetRefundRequestedBy(by).SetRefundAmount(o.Amount).Save(ctx)
	if err != nil {
		return fmt.Errorf("update: %w", err)
	}
	if c == 0 {
		return infraerrors.Conflict("CONFLICT", "order status changed")
	}
	s.writeAuditLog(ctx, oid, "REFUND_REQUESTED", fmt.Sprintf("user:%d", uid), map[string]any{"amount": o.Amount, "reason": nr})
	return nil
}

func (s *PaymentService) validateRefundRequest(ctx context.Context, oid, uid int64) (*dbent.PaymentOrder, error) {
	o, err := s.entClient.PaymentOrder.Get(ctx, oid)
	if err != nil {
		return nil, infraerrors.NotFound("NOT_FOUND", "order not found")
	}
	if o.UserID != uid {
		return nil, infraerrors.Forbidden("FORBIDDEN", "no permission")
	}
	if o.OrderType != payment.OrderTypeBalance {
		return nil, infraerrors.BadRequest("INVALID_ORDER_TYPE", "only balance orders can request refund")
	}
	if o.Status != OrderStatusCompleted {
		return nil, infraerrors.BadRequest("INVALID_STATUS", "only completed orders can request refund")
	}
	// Check provider instance allows user refund
	inst, err := s.getRefundOrderProviderInstance(ctx, o)
	if err != nil || inst == nil {
		return nil, infraerrors.Forbidden("USER_REFUND_DISABLED", "refund is not available for this order")
	}
	if !inst.AllowUserRefund {
		return nil, infraerrors.Forbidden("USER_REFUND_DISABLED", "user refund is not enabled for this provider")
	}
	return o, nil
}

func (s *PaymentService) PrepareRefund(ctx context.Context, oid int64, amt float64, reason string, force, deduct bool) (*RefundPlan, *RefundResult, error) {
	o, err := s.entClient.PaymentOrder.Get(ctx, oid)
	if err != nil {
		return nil, nil, infraerrors.NotFound("NOT_FOUND", "order not found")
	}
	// REFUND_PENDING is recovery-only; ExecuteRefund queries its persisted operation.
	ok := []string{OrderStatusCompleted, OrderStatusRefundRequested, OrderStatusRefundPending, OrderStatusRefundFailed}
	if !psSliceContains(ok, o.Status) {
		return nil, nil, infraerrors.BadRequest("INVALID_STATUS", "order status does not allow refund")
	}
	if o.Status == OrderStatusRefundPending {
		detail, err := s.latestRefundPendingDetail(ctx, oid)
		if err != nil {
			return nil, nil, err
		}
		// A pending refund already has a gateway identity. Retry its persisted
		// operation, never reinterpret a new amount or deduction choice as a
		// second refund. ExecuteRefund queries it (or settles confirmed success).
		plan := s.refundFinalizePlan(o)
		plan.DeductBalance = detail.shouldDeduct()
		plan.DeductionType = detail.DeductionType
		plan.BalanceToDeduct = 0
		plan.SubDaysToDeduct = 0
		return plan, nil, nil
	}
	// Check provider instance allows admin refund
	inst, instErr := s.getRefundOrderProviderInstance(ctx, o)
	if instErr != nil {
		slog.Warn("refund: provider instance lookup failed", "orderID", oid, "error", instErr)
		return nil, nil, infraerrors.InternalServer("PROVIDER_LOOKUP_FAILED", "failed to look up payment provider for this order")
	}
	if inst == nil {
		// Legacy order without provider_instance_id — block refund
		return nil, nil, infraerrors.Forbidden("REFUND_DISABLED", "refund is not available for this order")
	}
	if !inst.RefundEnabled {
		return nil, nil, infraerrors.Forbidden("REFUND_DISABLED", "refund is not enabled for this provider")
	}
	if math.IsNaN(amt) || math.IsInf(amt, 0) {
		return nil, nil, infraerrors.BadRequest("INVALID_AMOUNT", "invalid refund amount")
	}
	if amt <= 0 {
		amt = o.Amount
	}
	// RefundAmount is stored as numeric(20,2), independently of the gateway
	// currency. Reject precision loss before sending an irreversible refund.
	refundAmount := decimal.NewFromFloat(amt)
	if !refundAmount.Equal(refundAmount.Round(2)) {
		return nil, nil, infraerrors.BadRequest("INVALID_AMOUNT", "refund amount must not have more than 2 decimal places")
	}
	orderCurrency := PaymentOrderCurrency(o)
	if amt-o.Amount > paymentAmountToleranceForCurrency(orderCurrency) {
		return nil, nil, infraerrors.BadRequest("REFUND_AMOUNT_EXCEEDED", "refund amount exceeds recharge")
	}
	ga := calculateGatewayRefundAmount(o.Amount, o.PayAmount, amt, orderCurrency)
	rr := strings.TrimSpace(reason)
	if rr == "" && o.RefundRequestReason != nil {
		rr = *o.RefundRequestReason
	}
	if rr == "" {
		rr = fmt.Sprintf("refund order:%d", o.ID)
	}
	p := &RefundPlan{OrderID: oid, Order: o, RefundAmount: amt, GatewayAmount: ga, Reason: rr, Force: force, DeductBalance: deduct, DeductionType: payment.DeductionTypeNone}
	if held, err := s.heldRefundRetryPlan(ctx, o); err != nil {
		return nil, nil, err
	} else if held != nil {
		if err := validateHeldRefundRetry(p, held); err != nil {
			return nil, nil, err
		}
		return held, nil, nil
	}
	if deduct {
		if er := s.prepDeduct(ctx, o, p, force); er != nil {
			return nil, er, nil
		}
	}
	return p, nil, nil
}

func (s *PaymentService) prepDeduct(ctx context.Context, o *dbent.PaymentOrder, p *RefundPlan, force bool) *RefundResult {
	if o.OrderType == payment.OrderTypeSubscription {
		p.DeductionType = payment.DeductionTypeSubscription
		if o.SubscriptionGroupID != nil && o.SubscriptionDays != nil {
			p.SubDaysToDeduct = *o.SubscriptionDays
			sub, err := s.subscriptionSvc.GetActiveSubscription(ctx, o.UserID, *o.SubscriptionGroupID)
			if err == nil && sub != nil {
				p.SubscriptionID = sub.ID
			} else if !force {
				return &RefundResult{Success: false, Warning: "cannot find active subscription for deduction, use force", RequireForce: true}
			}
		}
		return nil
	}
	u, err := s.userRepo.GetByID(ctx, o.UserID)
	if err != nil {
		if !force {
			return &RefundResult{Success: false, Warning: "cannot fetch user balance, use force", RequireForce: true}
		}
		return nil
	}
	p.DeductionType = payment.DeductionTypeBalance
	if u.Balance < p.RefundAmount && !force {
		return &RefundResult{Success: false, Warning: "user balance is insufficient for deduction, use force", RequireForce: true}
	}
	p.BalanceToDeduct = math.Max(0, math.Min(p.RefundAmount, u.Balance))
	return nil
}

type availableBalanceDeductor interface {
	DeductAvailableBalance(ctx context.Context, id int64, amount float64) (float64, error)
}

func (s *PaymentService) deductAvailableBalance(ctx context.Context, userID int64, amount float64) (float64, error) {
	repo, ok := s.userRepo.(availableBalanceDeductor)
	if !ok {
		return 0, errors.New("user repository does not support available balance deduction")
	}
	return repo.DeductAvailableBalance(ctx, userID, amount)
}

func (s *PaymentService) ExecuteRefund(ctx context.Context, p *RefundPlan) (*RefundResult, error) {
	current, err := s.entClient.PaymentOrder.Get(ctx, p.OrderID)
	if err != nil {
		return nil, fmt.Errorf("load refund order: %w", err)
	}
	// A plan prepared for a pending operation carries recovery metadata, not a
	// new deduction. Keep it recovery-only even if another query has meanwhile
	// moved the order to REFUND_FAILED or a terminal refund state.
	if current.Status == OrderStatusRefundPending || (p.Order != nil && p.Order.Status == OrderStatusRefundPending) {
		return s.QueryAndFinalizeRefund(ctx, p.OrderID)
	}
	// Re-read the outstanding deduction before claiming the order. A prepared
	// plan may predate another attempt's failed compensation.
	if held, err := s.heldRefundRetryPlan(ctx, current); err != nil {
		return nil, err
	} else if held != nil {
		if err := validateHeldRefundRetry(p, held); err != nil {
			return nil, err
		}
		*p = *held
	} else if p.deductionHeld {
		return nil, infraerrors.Conflict("CONFLICT", "held refund deduction changed; prepare the refund again")
	}
	c, err := s.entClient.PaymentOrder.Update().Where(
		paymentorder.IDEQ(p.OrderID),
		paymentorder.StatusIn(OrderStatusCompleted, OrderStatusRefundRequested, OrderStatusRefundFailed),
		paymentorder.StatusEQ(current.Status),
		paymentorder.UpdatedAtEQ(current.UpdatedAt),
	).SetStatus(OrderStatusRefunding).Save(ctx)
	if err != nil {
		return nil, fmt.Errorf("lock: %w", err)
	}
	if c == 0 {
		return nil, infraerrors.Conflict("CONFLICT", "order status changed")
	}
	// Restore the state actually claimed, including a user refund request
	// accepted after this plan was prepared.
	p.Order = current
	if !p.deductionHeld && p.DeductionType == payment.DeductionTypeBalance && p.BalanceToDeduct > 0 {
		deducted, err := s.deductAvailableBalance(ctx, p.Order.UserID, p.BalanceToDeduct)
		if err != nil {
			s.restoreStatus(ctx, p)
			return nil, fmt.Errorf("deduction: %w", err)
		}
		p.BalanceToDeduct = deducted
		if deducted > 0 {
			s.invalidateRefundBalanceCaches(ctx, p.Order.UserID)
		}
	}
	if !p.deductionHeld && p.DeductionType == payment.DeductionTypeSubscription && p.SubDaysToDeduct > 0 && p.SubscriptionID > 0 {
		_, err := s.subscriptionSvc.ExtendSubscription(ctx, p.SubscriptionID, -p.SubDaysToDeduct)
		if err != nil {
			if errors.Is(err, ErrAdjustWouldExpire) {
				// Deduction would expire the subscription — revoke it entirely
				slog.Info("subscription deduction would expire, revoking", "orderID", p.OrderID, "subID", p.SubscriptionID, "days", p.SubDaysToDeduct)
				if revokeErr := s.revokeRefundSubscription(ctx, p); revokeErr != nil {
					s.restoreStatus(ctx, p)
					return nil, fmt.Errorf("revoke subscription: %w", revokeErr)
				}
			} else {
				// Other errors (DB failure, not found) — abort refund
				s.restoreStatus(ctx, p)
				return nil, fmt.Errorf("deduct subscription days: %w", err)
			}
		}
	}
	p.deductionHeld = true
	resp, err := s.gwRefund(ctx, p)
	if err != nil {
		return s.handleGwFail(ctx, p, err)
	}
	return s.finishRefund(ctx, p, resp)
}

func (s *PaymentService) gwRefund(ctx context.Context, p *RefundPlan) (*payment.RefundResponse, error) {
	if p.Order.PaymentTradeNo == "" {
		s.writeAuditLog(ctx, p.Order.ID, "REFUND_NO_TRADE_NO", "admin", map[string]any{"detail": "skipped"})
		return &payment.RefundResponse{Status: payment.ProviderStatusSuccess}, nil
	}

	// Use the exact provider instance that created this order, not a random one
	// from the registry. Each instance has its own merchant credentials.
	prov, err := s.getRefundProvider(ctx, p.Order)
	if err != nil {
		return nil, fmt.Errorf("get refund provider: %w", err)
	}
	if err := validateProviderSnapshotMetadata(p.Order, prov.ProviderKey(), providerMerchantIdentityMetadata(prov)); err != nil {
		s.writeAuditLog(ctx, p.Order.ID, "REFUND_PROVIDER_METADATA_MISMATCH", "admin", map[string]any{
			"detail": err.Error(),
		})
		return nil, err
	}
	finishProviderCall := servertiming.ObserveDependency(ctx, "payment")
	resp, err := prov.Refund(ctx, payment.RefundRequest{
		TradeNo: p.Order.PaymentTradeNo,
		OrderID: p.Order.OutTradeNo,
		Amount:  formatGatewayRefundAmount(p.GatewayAmount, p.Order),
		Reason:  p.Reason,
	})
	finishProviderCall()
	if err != nil {
		if resp != nil && strings.TrimSpace(resp.Status) == payment.ProviderStatusPending {
			return resp, nil
		}
		return nil, err
	}
	if err := validateRefundProviderResponse(resp); err != nil {
		return nil, err
	}
	return resp, nil
}

func formatGatewayRefundAmount(amount float64, order *dbent.PaymentOrder) string {
	return payment.FormatAmountForCurrency(amount, PaymentOrderCurrency(order))
}

func validateRefundProviderResponse(resp *payment.RefundResponse) error {
	if resp == nil {
		return fmt.Errorf("payment refund response missing")
	}
	status := strings.TrimSpace(resp.Status)
	switch status {
	case payment.ProviderStatusSuccess, payment.ProviderStatusRefunded, payment.ProviderStatusPending:
		return nil
	case payment.ProviderStatusFailed:
		return fmt.Errorf("payment refund failed: status %s", status)
	default:
		return fmt.Errorf("payment refund returned unknown status: %s", status)
	}
}

func (s *PaymentService) finishRefund(ctx context.Context, p *RefundPlan, resp *payment.RefundResponse) (*RefundResult, error) {
	if err := validateRefundProviderResponse(resp); err != nil {
		return s.handleGwFail(ctx, p, err)
	}
	switch strings.TrimSpace(resp.Status) {
	case payment.ProviderStatusSuccess, payment.ProviderStatusRefunded:
		result, err := s.markRefundOk(ctx, p)
		if err == nil {
			return result, nil
		}
		return s.recoverConfirmedRefund(ctx, p, resp, err, nil)
	case payment.ProviderStatusPending:
		return s.markRefundPending(ctx, p, resp)
	default:
		return s.handleGwFail(ctx, p, fmt.Errorf("payment refund returned unknown status: %s", strings.TrimSpace(resp.Status)))
	}
}

func (s *PaymentService) QueryAndFinalizeRefund(ctx context.Context, oid int64) (*RefundResult, error) {
	o, err := s.entClient.PaymentOrder.Get(ctx, oid)
	if err != nil {
		return nil, infraerrors.NotFound("NOT_FOUND", "order not found")
	}
	if o.Status == OrderStatusRefunded || o.Status == OrderStatusPartiallyRefunded {
		return &RefundResult{Success: true}, nil
	}
	if o.Status != OrderStatusRefundPending {
		return nil, infraerrors.BadRequest("INVALID_STATUS", "only refund pending orders can be finalized")
	}

	pendingDetail, err := s.latestRefundPendingDetail(ctx, oid)
	if err != nil {
		return nil, err
	}
	if pendingDetail.GatewayConfirmed {
		return s.finalizeConfirmedPendingRefund(ctx, o, pendingDetail, nil)
	}
	if pendingDetail.GatewayFailure != "" {
		// The gateway outcome is already known. Only local compensation and
		// its audit need retrying; do not send or query another gateway request.
		return s.finalizeRefundFailure(ctx, s.refundFailurePlan(o, pendingDetail), errors.New(pendingDetail.GatewayFailure), true)
	}

	prov, err := s.getRefundProvider(ctx, o)
	if err != nil {
		return nil, fmt.Errorf("get refund provider: %w", err)
	}
	queryProvider, ok := prov.(payment.RefundQueryProvider)
	if !ok {
		return nil, infraerrors.BadRequest("REFUND_QUERY_UNSUPPORTED", "this payment provider does not support refund status query; please verify manually")
	}

	finishProviderCall := servertiming.ObserveDependency(ctx, "payment")
	resp, err := queryProvider.QueryRefund(ctx, payment.RefundQueryRequest{
		TradeNo:     o.PaymentTradeNo,
		OrderID:     o.OutTradeNo,
		RefundID:    pendingDetail.RefundID,
		Amount:      formatGatewayRefundAmount(calculateGatewayRefundAmount(o.Amount, o.PayAmount, o.RefundAmount, PaymentOrderCurrency(o)), o),
		RequestedAt: pendingDetail.requestedAt,
	})
	finishProviderCall()
	if err != nil {
		return nil, fmt.Errorf("query refund: %w", err)
	}
	if err := validateRefundProviderResponse(resp); err != nil {
		return s.finalizeRefundFailed(ctx, s.refundFailurePlan(o, pendingDetail), err)
	}

	switch strings.TrimSpace(resp.Status) {
	case payment.ProviderStatusSuccess, payment.ProviderStatusRefunded:
		return s.finalizeConfirmedPendingRefund(ctx, o, pendingDetail, resp)
	case payment.ProviderStatusPending:
		s.writeAuditLog(ctx, oid, "REFUND_QUERY_PENDING", "admin", map[string]any{"refundID": resp.RefundID})
		return &RefundResult{Success: false, Warning: "gateway refund is still pending confirmation"}, nil
	default:
		return s.finalizeRefundFailed(ctx, s.refundFailurePlan(o, pendingDetail), fmt.Errorf("payment refund returned unknown status: %s", strings.TrimSpace(resp.Status)))
	}
}

func (s *PaymentService) refundFailurePlan(o *dbent.PaymentOrder, detail refundPendingAuditDetail) *RefundPlan {
	plan := s.refundFinalizePlan(o)
	if detail.DeductionHeld {
		detail.applyTo(plan)
	}
	return plan
}

func (s *PaymentService) finalizeConfirmedPendingRefund(ctx context.Context, o *dbent.PaymentOrder, pendingDetail refundPendingAuditDetail, resp *payment.RefundResponse) (*RefundResult, error) {
	plan := s.refundFinalizePlan(o)
	plan.DeductBalance = pendingDetail.shouldDeduct()
	if !plan.DeductBalance {
		plan.DeductionType = payment.DeductionTypeNone
		plan.BalanceToDeduct = 0
		plan.SubDaysToDeduct = 0
	} else if pendingDetail.DeductionHeld {
		pendingDetail.applyTo(plan)
	} else if !pendingDetail.DeductionRollbackOK {
		plan.DeductionType = pendingDetail.DeductionType
		plan.BalanceToDeduct = 0
		plan.SubDaysToDeduct = 0
	} else if o.OrderType == payment.OrderTypeSubscription {
		if early := s.prepDeduct(ctx, o, plan, true); early != nil {
			return early, nil
		}
	}
	result, err := s.finalizePendingRefundSuccess(ctx, plan)
	if err == nil || pendingDetail.GatewayConfirmed {
		return result, err
	}
	// This finalization transaction rolled back, including any new deduction.
	// Retain the original pending deduction state when saving gateway success.
	return s.recoverConfirmedRefund(ctx, plan, resp, err, &pendingDetail)
}

func (s *PaymentService) finalizePendingRefundSuccess(ctx context.Context, p *RefundPlan) (_ *RefundResult, err error) {
	tx, err := s.entClient.Tx(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin refund finalization: %w", err)
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()
	txCtx := dbent.NewTxContext(ctx, tx)

	claimed, err := tx.PaymentOrder.Update().
		Where(paymentorder.IDEQ(p.OrderID), paymentorder.StatusEQ(OrderStatusRefundPending), paymentorder.UpdatedAtEQ(p.Order.UpdatedAt)).
		SetStatus(OrderStatusRefunding).
		Save(txCtx)
	if err != nil {
		return nil, fmt.Errorf("claim pending refund: %w", err)
	}
	if claimed == 0 {
		return nil, infraerrors.Conflict("CONFLICT", "order status changed")
	}

	if err := s.applyRefundFinalDeduction(txCtx, p); err != nil {
		return nil, err
	}
	result, err := s.markRefundOkTx(txCtx, tx.Client(), p)
	if err != nil {
		return nil, err
	}
	if err = tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit refund finalization: %w", err)
	}
	if p.DeductionType == payment.DeductionTypeBalance && p.BalanceToDeduct > 0 {
		s.invalidateRefundBalanceCaches(ctx, p.Order.UserID)
	}
	if p.DeductionType == payment.DeductionTypeSubscription && p.SubscriptionID > 0 && p.Order.SubscriptionGroupID != nil {
		// Deduction invalidates inside this transaction. Readers can refill the
		// old committed entitlement before commit, so evict it again afterward.
		if cacheErr := s.subscriptionSvc.invalidateSubscriptionCaches(p.Order.UserID, *p.Order.SubscriptionGroupID); cacheErr != nil {
			slog.Warn("invalidate committed pending refund subscription cache", "subID", p.SubscriptionID, "error", cacheErr)
		}
	}
	return result, nil
}

func (s *PaymentService) refundFinalizePlan(o *dbent.PaymentOrder) *RefundPlan {
	refundAmount := o.RefundAmount
	reason := strings.TrimSpace(psStringValue(o.RefundReason))
	if reason == "" {
		reason = fmt.Sprintf("refund order:%d", o.ID)
	}
	return &RefundPlan{
		OrderID:       o.ID,
		Order:         o,
		RefundAmount:  refundAmount,
		GatewayAmount: calculateGatewayRefundAmount(o.Amount, o.PayAmount, refundAmount, PaymentOrderCurrency(o)),
		Reason:        reason,
		Force:         o.ForceRefund,
		DeductBalance: true,
		DeductionType: payment.DeductionTypeBalance,
		BalanceToDeduct: func() float64 {
			if o.OrderType == payment.OrderTypeBalance {
				return refundAmount
			}
			return 0
		}(),
	}
}

func (s *PaymentService) applyRefundFinalDeduction(ctx context.Context, p *RefundPlan) error {
	if p.deductionHeld {
		// 进入 pending 时扣减已保留，终态成功无需再扣。
		return nil
	}
	if p.DeductionType == payment.DeductionTypeBalance && p.BalanceToDeduct > 0 {
		deducted, err := s.deductAvailableBalance(ctx, p.Order.UserID, p.BalanceToDeduct)
		if err != nil {
			return fmt.Errorf("deduction: %w", err)
		}
		p.BalanceToDeduct = deducted
	}
	if p.DeductionType == payment.DeductionTypeSubscription && p.SubDaysToDeduct > 0 && p.SubscriptionID > 0 {
		if _, err := s.subscriptionSvc.ExtendSubscription(ctx, p.SubscriptionID, -p.SubDaysToDeduct); err != nil {
			if errors.Is(err, ErrAdjustWouldExpire) {
				if revokeErr := s.subscriptionSvc.RevokeSubscription(ctx, p.SubscriptionID); revokeErr != nil {
					return fmt.Errorf("revoke subscription: %w", revokeErr)
				}
			} else {
				return fmt.Errorf("deduct subscription days: %w", err)
			}
		}
	}
	return nil
}

// finalizeRefundFailed 将 REFUND_PENDING 收敛为 REFUND_FAILED，并归还进入 pending 时保留的扣减。
// 认领状态、归还扣减与审计在同一事务内完成：两者分开提交时，进程在中间退出会留下
// 已失败但扣减未归还、且没有 REFUND_ROLLBACK_FAILED 标记的订单，重试退款会再扣一次。
func (s *PaymentService) finalizeRefundFailed(ctx context.Context, p *RefundPlan, gErr error) (*RefundResult, error) {
	return s.finalizeRefundFailure(ctx, p, gErr, false)
}

func (s *PaymentService) finalizeRefundFailure(ctx context.Context, p *RefundPlan, gErr error, localRecoveryOnly bool) (*RefundResult, error) {
	warning := "gateway refund failed: " + psErrMsg(gErr)
	returnErr, err := s.finalizeRefundFailedTx(ctx, p, gErr, nil, OrderStatusRefundPending)
	if err != nil {
		return nil, err
	}
	if returnErr == nil {
		if p.deductionHeld && p.DeductionType == payment.DeductionTypeBalance && p.BalanceToDeduct > 0 {
			s.invalidateRefundBalanceCaches(ctx, p.Order.UserID)
		}
		if p.deductionHeld && p.DeductionType == payment.DeductionTypeSubscription && p.SubscriptionID > 0 && p.Order.SubscriptionGroupID != nil {
			if cacheErr := s.subscriptionSvc.invalidateSubscriptionCaches(p.Order.UserID, *p.Order.SubscriptionGroupID); cacheErr != nil {
				slog.Warn("invalidate returned pending refund subscription cache", "subID", p.SubscriptionID, "error", cacheErr)
			}
		}
		return &RefundResult{Success: false, Warning: warning}, nil
	}
	if localRecoveryOnly {
		// The failed transaction left the complete recovery record pending.
		// Keep it independent of provider availability until compensation works.
		return &RefundResult{Success: false, Warning: warning + "; local deduction recovery is pending: " + psErrMsg(returnErr)}, nil
	}

	// 归还失败（整个事务已回滚，订单仍为 REFUND_PENDING）：仍收敛为 REFUND_FAILED，
	// 并同事务写入 REFUND_ROLLBACK_FAILED，使重试退款跳过扣减，避免二次扣减。
	slog.Error("[CRITICAL] refund failed but held deduction could not be returned", "orderID", p.OrderID, "error", returnErr)
	if _, err := s.finalizeRefundFailedTx(ctx, p, gErr, returnErr, OrderStatusRefundPending); err != nil {
		return nil, err
	}
	return &RefundResult{Success: false, Warning: warning + "; refund deduction rollback failed"}, nil
}

// finalizeRefundFailedTx 在单个事务内认领 pending 或同步网关失败。rollbackErr 为 nil 时同事务
// 归还保留的扣减，归还失败则回滚整个事务并通过 returnErr 返回；rollbackErr 非 nil 时不再归还，
// 改为同事务记录 REFUND_ROLLBACK_FAILED。
func (s *PaymentService) finalizeRefundFailedTx(ctx context.Context, p *RefundPlan, gErr, rollbackErr error, sourceStatus string) (returnErr error, err error) {
	tx, err := s.entClient.Tx(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin refund failure finalization: %w", err)
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()
	txCtx := dbent.NewTxContext(ctx, tx)

	update := tx.PaymentOrder.Update().Where(paymentorder.IDEQ(p.OrderID), paymentorder.StatusEQ(sourceStatus))
	if sourceStatus == OrderStatusRefundPending {
		update.Where(paymentorder.UpdatedAtEQ(p.Order.UpdatedAt))
	}
	if sourceStatus == OrderStatusRefunding && rollbackErr == nil {
		status := OrderStatusCompleted
		if p.Order.Status == OrderStatusRefundRequested {
			status = OrderStatusRefundRequested
		}
		update.SetStatus(status).ClearFailedAt().ClearFailedReason()
	} else {
		update.SetStatus(OrderStatusRefundFailed).SetFailedAt(time.Now()).SetFailedReason(psErrMsg(gErr))
	}
	claimed, err := update.Save(txCtx)
	if err != nil {
		return nil, fmt.Errorf("mark refund failed: %w", err)
	}
	if claimed == 0 {
		return nil, infraerrors.Conflict("CONFLICT", "order status changed")
	}

	if p.deductionHeld && rollbackErr == nil {
		if err := s.returnHeldRefundDeduction(txCtx, p); err != nil {
			return err, nil
		}
		if err := createPaymentAuditLogTx(txCtx, tx.Client(), p.OrderID, "REFUND_ROLLBACK_SUCCEEDED", refundHeldAuditDetail(p)); err != nil {
			return nil, err
		}
	}
	if rollbackErr != nil {
		if err := createPaymentAuditLogTx(txCtx, tx.Client(), p.OrderID, "REFUND_ROLLBACK_FAILED", refundRollbackAuditDetail{
			refundPendingAuditDetail: refundHeldAuditDetail(p),
			GatewayError:             psErrMsg(gErr), RollbackError: psErrMsg(rollbackErr),
		}); err != nil {
			return nil, err
		}
	}
	action := "REFUND_FAILED"
	if sourceStatus == OrderStatusRefunding && rollbackErr == nil {
		action = "REFUND_GATEWAY_FAILED"
	}
	if err := createPaymentAuditLogTx(txCtx, tx.Client(), p.OrderID, action, map[string]any{"detail": psErrMsg(gErr)}); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit refund failure finalization: %w", err)
	}
	committed = true
	return nil, nil
}

// returnHeldRefundDeduction 归还进入 REFUND_PENDING 时保留的扣减；ctx 携带事务时随事务提交或回滚。
func (s *PaymentService) returnHeldRefundDeduction(ctx context.Context, p *RefundPlan) error {
	if p.DeductionType == payment.DeductionTypeBalance && p.BalanceToDeduct > 0 {
		if err := s.userRepo.UpdateBalance(ctx, p.Order.UserID, p.BalanceToDeduct); err != nil {
			return fmt.Errorf("return held balance deduction: %w", err)
		}
	}
	if p.DeductionType == payment.DeductionTypeSubscription && p.SubDaysToDeduct > 0 && p.SubscriptionID > 0 {
		if err := s.restoreRefundSubscription(ctx, p); err != nil {
			return fmt.Errorf("return held subscription deduction: %w", err)
		}
	}
	return nil
}

func createPaymentAuditLogTx(ctx context.Context, client *dbent.Client, oid int64, action string, detail any) error {
	raw, err := json.Marshal(detail)
	if err != nil {
		return fmt.Errorf("marshal %s audit: %w", action, err)
	}
	if _, err := client.PaymentAuditLog.Create().
		SetOrderID(strconv.FormatInt(oid, 10)).
		SetAction(action).
		SetDetail(string(raw)).
		SetOperator("admin").
		Save(ctx); err != nil {
		return fmt.Errorf("write %s audit: %w", action, err)
	}
	return nil
}

// The version identifies only the revocation performed by this refund. It
// prevents a later failure from undoing an administrator's subsequent action.
type refundSubscriptionRevocation struct {
	ID                int64      `json:"id"`
	UserID            int64      `json:"userID"`
	GroupID           int64      `json:"groupID"`
	DeletedAt         *time.Time `json:"deletedAt"`
	UpdatedAt         time.Time  `json:"updatedAt"`
	DailyResetVersion int64      `json:"dailyResetVersion"`
}

func refundRevocationSnapshot(sub *dbent.UserSubscription) *refundSubscriptionRevocation {
	if sub == nil {
		return nil
	}
	return &refundSubscriptionRevocation{ID: sub.ID, UserID: sub.UserID, GroupID: sub.GroupID,
		DeletedAt: sub.DeletedAt, UpdatedAt: sub.UpdatedAt, DailyResetVersion: sub.DailyResetVersion}
}

// refundPendingAuditDetail 记录在 REFUND_PENDING 审计日志中。DeductionHeld 为 true 表示
// 进入 pending 时扣减被保留；旧记录没有该字段，扣减已在 pending 时归还（DeductionRollbackOK）。
type refundPendingAuditDetail struct {
	RevokedSubscription *refundSubscriptionRevocation `json:"revokedSubscription,omitempty"`
	GatewayConfirmed    bool                          `json:"gatewayConfirmed"`
	GatewayFailure      string                        `json:"gatewayFailure,omitempty"`
	DeductBalance       *bool                         `json:"deductBalance,omitempty"`
	RefundID            string                        `json:"refundID"`
	RefundAmount        float64                       `json:"refundAmount"`
	Reason              string                        `json:"reason"`
	Force               bool                          `json:"force"`
	DeductionRollbackOK bool                          `json:"deductionRollbackOK,omitempty"`
	DeductionHeld       bool                          `json:"deductionHeld,omitempty"`
	DeductionType       string                        `json:"deductionType,omitempty"`
	BalanceDeducted     float64                       `json:"balanceDeducted"`
	SubDaysDeducted     int                           `json:"subDaysDeducted"`
	SubscriptionID      int64                         `json:"subscriptionID,omitempty"`
	// requestedAt 取自审计记录的创建时间，不参与序列化。
	requestedAt time.Time
}

func (d refundPendingAuditDetail) applyTo(p *RefundPlan) {
	p.deductionHeld = true
	p.DeductionType = d.DeductionType
	if p.DeductionType == "" {
		p.DeductionType = payment.DeductionTypeNone
	}
	p.BalanceToDeduct = d.BalanceDeducted
	p.SubDaysToDeduct = d.SubDaysDeducted
	p.SubscriptionID = d.SubscriptionID
	if sub := d.RevokedSubscription; sub != nil {
		p.revokedSubscription = &dbent.UserSubscription{ID: sub.ID, UserID: sub.UserID, GroupID: sub.GroupID,
			DeletedAt: sub.DeletedAt, UpdatedAt: sub.UpdatedAt, DailyResetVersion: sub.DailyResetVersion}
	}
}

func (d refundPendingAuditDetail) shouldDeduct() bool {
	if d.DeductBalance != nil {
		return *d.DeductBalance
	}
	// Older confirmed recovery records can explicitly identify no deduction.
	// Older ordinary pending records have no intent field at all; their zero
	// amounts also occur after forced refunds with no available balance, so
	// retain the existing default instead of guessing or rewriting history.
	return d.DeductionType != payment.DeductionTypeNone
}

func refundPlanShouldDeduct(p *RefundPlan) bool {
	return p.DeductBalance || p.DeductionType == payment.DeductionTypeBalance || p.DeductionType == payment.DeductionTypeSubscription
}

// A rollback failure is outstanding only until a later committed compensation
// or refund success. Never use the existence of an old failure as a permanent
// exemption from deductions.
// Order transitions serialize these audits, so IDs establish operation order.
// Application timestamps can go backwards across instances or clock corrections.
func (s *PaymentService) heldRefundRetryPlan(ctx context.Context, o *dbent.PaymentOrder) (*RefundPlan, error) {
	entry, err := s.entClient.PaymentAuditLog.Query().Where(
		paymentauditlog.OrderIDEQ(strconv.FormatInt(o.ID, 10)),
		paymentauditlog.ActionIn("REFUND_ROLLBACK_FAILED", "REFUND_ROLLBACK_SUCCEEDED", "REFUND_SUCCESS"),
	).Order(paymentauditlog.ByID(sql.OrderDesc())).First(ctx)
	if dbent.IsNotFound(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("load held refund deduction: %w", err)
	}
	if entry.Action != "REFUND_ROLLBACK_FAILED" {
		return nil, nil
	}
	invalid := func() (*RefundPlan, error) {
		return nil, infraerrors.Conflict("REFUND_HELD_METADATA_INVALID", "held refund deduction metadata is incomplete; reconcile the previous refund before retrying")
	}
	var detail refundPendingAuditDetail
	if err := json.Unmarshal([]byte(entry.Detail), &detail); err != nil {
		return nil, fmt.Errorf("decode held refund deduction: %w", err)
	}
	if !detail.DeductionHeld {
		// Old rollback audits recorded amounts only. Borrow metadata solely
		// from the same unfinished pending operation. A completed older attempt
		// can leave refund fields on the order that do not describe this hold.
		pending, err := s.entClient.PaymentAuditLog.Query().Where(
			paymentauditlog.OrderIDEQ(strconv.FormatInt(o.ID, 10)),
			paymentauditlog.ActionIn("REFUND_PENDING", "REFUND_FAILED", "REFUND_GATEWAY_FAILED", "REFUND_SUCCESS", "REFUND_ROLLBACK_SUCCEEDED"),
			paymentauditlog.IDLT(entry.ID),
		).Order(paymentauditlog.ByID(sql.OrderDesc())).First(ctx)
		if err != nil && !dbent.IsNotFound(err) {
			return nil, fmt.Errorf("load legacy held refund deduction: %w", err)
		}
		if pending == nil || pending.Action != "REFUND_PENDING" {
			return invalid()
		}
		var original refundPendingAuditDetail
		if err := json.Unmarshal([]byte(pending.Detail), &original); err != nil {
			return nil, fmt.Errorf("decode legacy held refund deduction: %w", err)
		}
		if !original.DeductionHeld {
			return invalid()
		}
		// The failure's recorded amounts remain authoritative; the pending
		// snapshot supplies only metadata absent in older audits.
		original.BalanceDeducted = detail.BalanceDeducted
		original.SubDaysDeducted = detail.SubDaysDeducted
		detail = original
		if detail.RefundAmount == 0 {
			detail.RefundAmount = o.RefundAmount
		}
		if detail.Reason == "" {
			detail.Reason = psStringValue(o.RefundReason)
		}
		if detail.DeductionType == "" && detail.BalanceDeducted > 0 && detail.SubDaysDeducted == 0 {
			detail.DeductionType = payment.DeductionTypeBalance
		}
	}
	if math.IsNaN(detail.RefundAmount) || math.IsInf(detail.RefundAmount, 0) || detail.RefundAmount <= 0 || detail.RefundAmount > o.Amount || !detail.shouldDeduct() {
		return invalid()
	}
	switch detail.DeductionType {
	case payment.DeductionTypeBalance:
		if o.OrderType != payment.OrderTypeBalance || math.IsNaN(detail.BalanceDeducted) || math.IsInf(detail.BalanceDeducted, 0) || detail.BalanceDeducted <= 0 || detail.BalanceDeducted > detail.RefundAmount {
			return invalid()
		}
	case payment.DeductionTypeSubscription:
		if o.OrderType != payment.OrderTypeSubscription || detail.SubDaysDeducted <= 0 || detail.SubscriptionID <= 0 || o.SubscriptionGroupID == nil {
			return invalid()
		}
		sub, err := s.entClient.UserSubscription.Get(mixins.SkipSoftDelete(ctx), detail.SubscriptionID)
		if err != nil {
			return nil, fmt.Errorf("load held subscription: %w", err)
		}
		if sub.UserID != o.UserID || sub.GroupID != *o.SubscriptionGroupID || (sub.DeletedAt != nil && detail.RevokedSubscription == nil) {
			return invalid()
		}
		if revoked := detail.RevokedSubscription; revoked != nil && (revoked.ID != sub.ID || revoked.UserID != sub.UserID || revoked.GroupID != sub.GroupID || revoked.DeletedAt == nil) {
			return invalid()
		}
	default:
		return invalid()
	}
	plan := s.refundFinalizePlan(o)
	plan.RefundAmount, plan.Reason, plan.Force = detail.RefundAmount, detail.Reason, detail.Force
	plan.GatewayAmount = calculateGatewayRefundAmount(o.Amount, o.PayAmount, plan.RefundAmount, PaymentOrderCurrency(o))
	plan.DeductBalance = detail.shouldDeduct()
	detail.applyTo(plan)
	return plan, nil
}

func validateHeldRefundRetry(request, held *RefundPlan) error {
	if request.RefundAmount != held.RefundAmount || refundPlanShouldDeduct(request) != held.DeductBalance {
		return infraerrors.Conflict("REFUND_HELD_CONFLICT", "a previous refund still holds a deduction; retry its original amount and deduction choice or reconcile it first")
	}
	return nil
}

func refundHeldAuditDetail(p *RefundPlan) refundPendingAuditDetail {
	deduct := refundPlanShouldDeduct(p)
	return refundPendingAuditDetail{
		RefundAmount: p.RefundAmount, Reason: p.Reason, Force: p.Force,
		DeductBalance: &deduct, DeductionHeld: true, DeductionType: p.DeductionType,
		BalanceDeducted: p.BalanceToDeduct, SubDaysDeducted: p.SubDaysToDeduct,
		SubscriptionID: p.SubscriptionID, RevokedSubscription: refundRevocationSnapshot(p.revokedSubscription),
	}
}

type refundRollbackAuditDetail struct {
	refundPendingAuditDetail
	GatewayError  string `json:"gatewayError"`
	RollbackError string `json:"rollbackError"`
}

func (s *PaymentService) latestRefundPendingDetail(ctx context.Context, oid int64) (refundPendingAuditDetail, error) {
	logEntry, err := s.entClient.PaymentAuditLog.Query().
		Where(paymentauditlog.OrderIDEQ(strconv.FormatInt(oid, 10)), paymentauditlog.ActionEQ("REFUND_PENDING")).
		Order(paymentauditlog.ByID(sql.OrderDesc())).
		First(ctx)
	if dbent.IsNotFound(err) {
		return refundPendingAuditDetail{DeductionRollbackOK: true}, nil
	}
	if err != nil {
		return refundPendingAuditDetail{}, fmt.Errorf("load refund recovery metadata: %w", err)
	}
	detail := refundPendingAuditDetail{DeductionRollbackOK: true}
	if err := json.Unmarshal([]byte(logEntry.Detail), &detail); err != nil {
		return refundPendingAuditDetail{}, fmt.Errorf("decode refund recovery metadata: %w", err)
	}
	detail.RefundID = strings.TrimSpace(detail.RefundID)
	detail.requestedAt = logEntry.CreatedAt
	return detail, nil
}

// getRefundProvider creates a provider using the order's original instance config.
// Delegates to getOrderProvider which handles instance lookup and fallback.
func (s *PaymentService) getRefundProvider(ctx context.Context, o *dbent.PaymentOrder) (payment.Provider, error) {
	inst, err := s.getRefundOrderProviderInstance(ctx, o)
	if err != nil {
		return nil, err
	}
	if inst == nil {
		return nil, fmt.Errorf("refund provider instance is unavailable for order %d", o.ID)
	}
	return s.createProviderFromInstance(ctx, inst)
}

func (s *PaymentService) handleGwFail(ctx context.Context, p *RefundPlan, gErr error) (*RefundResult, error) {
	// A disconnected admin request can cancel the gateway call after the
	// entitlement deduction. Keep rollback, order recovery, and audit writable.
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	p.deductionHeld = true
	returnErr, err := s.finalizeRefundFailedTx(ctx, p, gErr, nil, OrderStatusRefunding)
	if err != nil {
		return s.preserveFailedRefund(ctx, p, gErr, err)
	}
	if returnErr == nil {
		if p.DeductionType == payment.DeductionTypeBalance && p.BalanceToDeduct > 0 {
			s.invalidateRefundBalanceCaches(ctx, p.Order.UserID)
		}
		if p.DeductionType == payment.DeductionTypeSubscription && p.SubscriptionID > 0 && p.Order.SubscriptionGroupID != nil {
			if err := s.subscriptionSvc.invalidateSubscriptionCaches(p.Order.UserID, *p.Order.SubscriptionGroupID); err != nil {
				slog.Warn("invalidate returned refund subscription cache", "subID", p.SubscriptionID, "error", err)
			}
		}
		p.BalanceToDeduct, p.SubDaysToDeduct = 0, 0
		p.revokedSubscription, p.deductionHeld = nil, false
		return &RefundResult{Success: false, Warning: "gateway failed: " + psErrMsg(gErr) + ", rolled back"}, nil
	}
	if _, err := s.finalizeRefundFailedTx(ctx, p, gErr, returnErr, OrderStatusRefunding); err != nil {
		return s.preserveFailedRefund(ctx, p, gErr, err)
	}
	return nil, infraerrors.InternalServer("REFUND_FAILED", psErrMsg(gErr))
}

func (s *PaymentService) preserveFailedRefund(ctx context.Context, p *RefundPlan, gatewayErr, recoveryErr error) (*RefundResult, error) {
	detail := refundHeldAuditDetail(p)
	detail.GatewayFailure = psErrMsg(gatewayErr)
	if _, err := s.saveRefundPending(ctx, p, detail); err != nil {
		return nil, errors.Join(recoveryErr, fmt.Errorf("preserve failed refund for local recovery: %w", err))
	}
	return &RefundResult{Success: false, Warning: "gateway refund failed; local deduction recovery is pending: " + psErrMsg(recoveryErr)}, nil
}

func (s *PaymentService) markRefundOk(ctx context.Context, p *RefundPlan) (*RefundResult, error) {
	tx, err := s.entClient.Tx(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin refund finalization: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	txCtx := dbent.NewTxContext(ctx, tx)
	result, err := s.markRefundOkTx(txCtx, tx.Client(), p)
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit refund finalization: %w", err)
	}
	return result, nil
}

func (s *PaymentService) markRefundOkTx(ctx context.Context, client *dbent.Client, p *RefundPlan) (*RefundResult, error) {
	fs := OrderStatusRefunded
	if p.RefundAmount < p.Order.Amount {
		fs = OrderStatusPartiallyRefunded
	}
	now := time.Now()
	updated, err := client.PaymentOrder.Update().
		Where(paymentorder.IDEQ(p.OrderID), paymentorder.StatusEQ(OrderStatusRefunding)).
		SetStatus(fs).SetRefundAmount(p.RefundAmount).SetRefundReason(p.Reason).SetRefundAt(now).SetForceRefund(p.Force).Save(ctx)
	if err != nil {
		return nil, fmt.Errorf("mark refund: %w", err)
	}
	if updated == 0 {
		current, err := client.PaymentOrder.Get(ctx, p.OrderID)
		if err != nil {
			return nil, fmt.Errorf("reload refund order: %w", err)
		}
		if current.Status == OrderStatusRefunded || current.Status == OrderStatusPartiallyRefunded {
			return &RefundResult{Success: true}, nil
		}
		return nil, infraerrors.Conflict("CONFLICT", "order status changed before refund completion")
	}
	if p.Order.OrderType == payment.OrderTypeSubscription && s.welfarePaymentRepo != nil {
		if err := s.welfarePaymentRepo.ReverseSubscriptionPurchase(ctx, p.OrderID); err != nil {
			return nil, fmt.Errorf("reverse subscription welfare: %w", err)
		}
	}
	detail, err := json.Marshal(map[string]any{"refundAmount": p.RefundAmount, "reason": p.Reason, "balanceDeducted": p.BalanceToDeduct, "force": p.Force})
	if err != nil {
		return nil, fmt.Errorf("marshal refund audit: %w", err)
	}
	if _, err := client.PaymentAuditLog.Create().
		SetOrderID(strconv.FormatInt(p.OrderID, 10)).
		SetAction("REFUND_SUCCESS").
		SetDetail(string(detail)).
		SetOperator("admin").
		Save(ctx); err != nil {
		return nil, fmt.Errorf("write refund audit: %w", err)
	}
	return &RefundResult{Success: true, BalanceDeducted: p.BalanceToDeduct, SubDaysDeducted: p.SubDaysToDeduct}, nil
}

func (s *PaymentService) recoverConfirmedRefund(ctx context.Context, p *RefundPlan, resp *payment.RefundResponse, finalizeErr error, pendingDetail *refundPendingAuditDetail) (*RefundResult, error) {
	recovered, recoveryErr := s.preserveConfirmedRefund(ctx, p, resp, finalizeErr, pendingDetail)
	if recoveryErr != nil {
		return nil, errors.Join(fmt.Errorf("finalize confirmed refund: %w", finalizeErr), fmt.Errorf("preserve confirmed refund for retry: %w", recoveryErr))
	}
	if recovered != nil {
		return recovered, nil
	}
	return nil, fmt.Errorf("finalize confirmed refund (saved for retry): %w", finalizeErr)
}

// preserveConfirmedRefund is deliberately separate from markRefundPending:
// gateway success must never roll back an already-applied entitlement deduction.
// The order and recovery metadata are committed together to prevent a retry
// from seeing REFUND_PENDING without knowing that its deduction already ran.
func (s *PaymentService) preserveConfirmedRefund(ctx context.Context, p *RefundPlan, resp *payment.RefundResponse, finalizeErr error, pendingDetail *refundPendingAuditDetail) (*RefundResult, error) {
	// A disconnected admin request cannot cancel persistence of an irreversible
	// gateway result. Keep this cleanup bounded if the database is unavailable.
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	tx, err := s.entClient.Tx(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()
	txCtx := dbent.NewTxContext(ctx, tx)
	update := tx.PaymentOrder.Update().Where(paymentorder.IDEQ(p.OrderID))
	deductionRollbackOK := false
	deductionHeld := p.deductionHeld
	deductBalance := refundPlanShouldDeduct(p)
	if pendingDetail != nil {
		update.Where(paymentorder.StatusEQ(OrderStatusRefundPending), paymentorder.UpdatedAtEQ(p.Order.UpdatedAt))
		deductionRollbackOK = pendingDetail.DeductionRollbackOK
		deductionHeld = pendingDetail.DeductionHeld
		deductBalance = pendingDetail.shouldDeduct()
	} else {
		update.Where(paymentorder.StatusEQ(OrderStatusRefunding))
	}
	updated, err := update.
		SetStatus(OrderStatusRefundPending).
		SetRefundAmount(p.RefundAmount).
		SetRefundReason(p.Reason).
		SetForceRefund(p.Force).
		ClearRefundAt().ClearFailedAt().ClearFailedReason().Save(txCtx)
	if err != nil {
		return nil, err
	}
	if updated == 0 {
		current, err := tx.PaymentOrder.Get(txCtx, p.OrderID)
		if err != nil {
			return nil, err
		}
		if current.Status == OrderStatusRefunded || current.Status == OrderStatusPartiallyRefunded {
			return &RefundResult{Success: true}, nil
		}
		return nil, infraerrors.Conflict("CONFLICT", "order status changed while preserving confirmed refund")
	}
	balanceDeducted, subDaysDeducted := p.BalanceToDeduct, p.SubDaysToDeduct
	if deductionRollbackOK && !deductionHeld {
		// The failed finalization rolled these writes back; they are still due.
		balanceDeducted, subDaysDeducted = 0, 0
	}
	detail, err := json.Marshal(map[string]any{
		"refundID": refundResponseID(resp), "refundAmount": p.RefundAmount,
		"reason": p.Reason, "force": p.Force,
		"gatewayConfirmed": true, "deductionRollbackOK": deductionRollbackOK,
		"deductionHeld": deductionHeld, "subscriptionID": p.SubscriptionID,
		"revokedSubscription": refundRevocationSnapshot(p.revokedSubscription),
		"deductBalance":       deductBalance,
		"deductionType":       p.DeductionType, "balanceDeducted": balanceDeducted,
		"subDaysDeducted": subDaysDeducted, "finalizationError": psErrMsg(finalizeErr),
	})
	if err != nil {
		return nil, err
	}
	if _, err := tx.PaymentAuditLog.Create().
		SetOrderID(strconv.FormatInt(p.OrderID, 10)).SetAction("REFUND_PENDING").
		SetDetail(string(detail)).SetOperator("system").Save(txCtx); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return nil, nil
}

// markRefundPending 在网关未给出终态时保留已执行的扣减：若此时归还，用户可在 pending 期间
// 消费这部分余额，终态成功时只能扣到剩余余额。网关确认失败后由 finalizeRefundFailed 归还。
// 订单状态与审计记录同事务写入，确保终态收敛时能读到保留的扣减。
func (s *PaymentService) markRefundPending(ctx context.Context, p *RefundPlan, resp *payment.RefundResponse) (_ *RefundResult, err error) {
	detail := refundHeldAuditDetail(p)
	detail.RefundID = refundResponseID(resp)
	result, err := s.saveRefundPending(ctx, p, detail)
	if err == nil {
		return result, nil
	}
	// The gateway may already have accepted this refund. Retry only the local
	// snapshot in a fresh bounded transaction after a transient write failure;
	// never release its deduction or submit another gateway refund.
	recovered, recoveryErr := s.saveRefundPending(ctx, p, detail)
	if recoveryErr != nil {
		return nil, errors.Join(err, fmt.Errorf("retry refund pending persistence: %w", recoveryErr))
	}
	return recovered, nil
}

func (s *PaymentService) saveRefundPending(ctx context.Context, p *RefundPlan, pendingDetail refundPendingAuditDetail) (_ *RefundResult, err error) {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	detail, err := json.Marshal(pendingDetail)
	if err != nil {
		return nil, fmt.Errorf("marshal refund pending audit: %w", err)
	}

	tx, err := s.entClient.Tx(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin mark refund pending: %w", err)
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
			slog.Error("[CRITICAL] gateway refund pending but order not marked", "orderID", p.OrderID, "error", err)
		}
	}()
	txCtx := dbent.NewTxContext(ctx, tx)
	updated, err := tx.PaymentOrder.Update().
		Where(paymentorder.IDEQ(p.OrderID), paymentorder.StatusEQ(OrderStatusRefunding)).
		SetStatus(OrderStatusRefundPending).
		SetRefundAmount(p.RefundAmount).
		SetRefundReason(p.Reason).
		ClearRefundAt().
		SetForceRefund(p.Force).
		ClearFailedAt().
		ClearFailedReason().
		Save(txCtx)
	if err != nil {
		return nil, fmt.Errorf("mark refund pending: %w", err)
	}
	if updated == 0 {
		return nil, infraerrors.Conflict("CONFLICT", "order status changed while saving pending refund")
	}
	if _, err = tx.PaymentAuditLog.Create().
		SetOrderID(strconv.FormatInt(p.OrderID, 10)).
		SetAction("REFUND_PENDING").
		SetDetail(string(detail)).
		SetOperator("admin").
		Save(txCtx); err != nil {
		return nil, fmt.Errorf("write refund pending audit: %w", err)
	}
	if err = tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit mark refund pending: %w", err)
	}
	return &RefundResult{Success: false, Warning: "gateway refund is pending confirmation"}, nil
}

func refundResponseID(resp *payment.RefundResponse) string {
	if resp == nil {
		return ""
	}
	return strings.TrimSpace(resp.RefundID)
}

// Keep the revocation and its persisted version in one transaction. A later
// rollback may restore this exact revocation, never a concurrent admin action.
func (s *PaymentService) revokeRefundSubscription(ctx context.Context, p *RefundPlan) error {
	tx, err := s.entClient.Tx(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	txCtx := dbent.NewTxContext(ctx, tx)
	sub, err := tx.UserSubscription.Get(txCtx, p.SubscriptionID)
	if err != nil {
		return err
	}
	now := time.Now()
	// A renewal may have committed after ExtendSubscription rejected the
	// shortening. Do not revoke that newer entitlement; let the admin retry.
	if sub.ExpiresAt.AddDate(0, 0, -p.SubDaysToDeduct).After(now) {
		return infraerrors.Conflict("CONFLICT", "subscription changed before refund revocation")
	}
	claimed, err := tx.UserSubscription.Update().Where(
		usersubscription.IDEQ(sub.ID), usersubscription.DeletedAtIsNil(),
		usersubscription.UpdatedAtEQ(sub.UpdatedAt),
		usersubscription.DailyResetVersionEQ(sub.DailyResetVersion),
	).SetUpdatedAt(now).Save(txCtx)
	if err != nil {
		return err
	}
	if claimed == 0 {
		return infraerrors.Conflict("CONFLICT", "subscription changed before refund revocation")
	}
	if err := s.subscriptionSvc.RevokeSubscription(txCtx, sub.ID); err != nil {
		return err
	}
	revoked, err := tx.UserSubscription.Get(mixins.SkipSoftDelete(txCtx), sub.ID)
	if err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	p.revokedSubscription = revoked
	s.invalidateRefundSubscriptionCaches(revoked)
	return nil
}

func (s *PaymentService) restoreRefundSubscription(ctx context.Context, p *RefundPlan) error {
	revoked := p.revokedSubscription
	if revoked == nil {
		_, err := s.subscriptionSvc.restoreSubscriptionDaysAfterRefund(ctx, p.SubscriptionID, p.SubDaysToDeduct)
		return err
	}
	if revoked.DeletedAt == nil {
		return errors.New("refund revocation snapshot has no deletion timestamp")
	}
	tx := dbent.TxFromContext(ctx)
	ownsTx := tx == nil
	if ownsTx {
		var err error
		tx, err = s.entClient.Tx(ctx)
		if err != nil {
			return err
		}
		defer func() { _ = tx.Rollback() }()
	}
	txCtx := dbent.NewTxContext(ctx, tx)
	claimed, err := tx.UserSubscription.Update().Where(
		usersubscription.IDEQ(revoked.ID), usersubscription.DeletedAtEQ(*revoked.DeletedAt),
		usersubscription.UpdatedAtEQ(revoked.UpdatedAt),
		usersubscription.DailyResetVersionEQ(revoked.DailyResetVersion),
	).SetUpdatedAt(time.Now()).Save(mixins.SkipSoftDelete(txCtx))
	if err != nil {
		return err
	}
	if claimed == 0 {
		return infraerrors.Conflict("CONFLICT", "subscription changed after refund revocation")
	}
	// Restore only the deletion flag/status. The revocation did not change the
	// expiry, usage windows, or paid daily-reset preferences, so adding days
	// here would grant extra entitlement and could reset those preferences.
	if _, err := s.subscriptionSvc.RestoreSubscription(txCtx, revoked.ID); err != nil {
		return err
	}
	if ownsTx {
		if err := tx.Commit(); err != nil {
			return err
		}
		p.revokedSubscription = nil
		s.invalidateRefundSubscriptionCaches(revoked)
	}
	return nil
}

func (s *PaymentService) invalidateRefundSubscriptionCaches(sub *dbent.UserSubscription) {
	// SubscriptionService also invalidates inside our transaction. Invalidate
	// again after commit so a concurrent reader cannot leave its old snapshot.
	if err := s.subscriptionSvc.invalidateSubscriptionCaches(sub.UserID, sub.GroupID); err != nil {
		slog.Warn("invalidate committed refund subscription cache", "subID", sub.ID, "error", err)
	}
}

// invalidateRefundBalanceCaches is only called after a balance mutation has
// committed. Cache failures must not make an irreversible refund retryable.
// These best-effort invalidations share the existing balance-update contract;
// Redis outages remain bounded by cache TTL, rather than a durable outbox.
func (s *PaymentService) invalidateRefundBalanceCaches(ctx context.Context, userID int64) {
	if dbent.TxFromContext(ctx) != nil {
		return
	}
	if s.refundBalanceInvalidator != nil {
		cacheCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Second)
		err := s.refundBalanceInvalidator.InvalidateUserBalance(cacheCtx, userID)
		cancel()
		if err != nil {
			slog.Warn("invalidate committed refund balance cache", "userID", userID, "error", err)
		}
	}
	if s.refundAuthInvalidator != nil {
		cacheCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Second)
		err := s.refundAuthInvalidator.InvalidateAuthCacheByUserIDReliable(cacheCtx, userID)
		cancel()
		if err != nil {
			slog.Warn("invalidate committed refund auth cache", "userID", userID, "error", err)
		}
	}
}

func (s *PaymentService) RollbackRefund(ctx context.Context, p *RefundPlan, gErr error) bool {
	if p.DeductionType == payment.DeductionTypeBalance && p.BalanceToDeduct > 0 {
		if err := s.userRepo.UpdateBalance(ctx, p.Order.UserID, p.BalanceToDeduct); err != nil {
			slog.Error("[CRITICAL] rollback failed", "orderID", p.OrderID, "amount", p.BalanceToDeduct, "error", err)
			s.writeAuditLog(ctx, p.OrderID, "REFUND_ROLLBACK_FAILED", "admin", map[string]any{"gatewayError": psErrMsg(gErr), "rollbackError": psErrMsg(err), "balanceDeducted": p.BalanceToDeduct})
			return false
		}
		s.invalidateRefundBalanceCaches(ctx, p.Order.UserID)
	}
	if p.DeductionType == payment.DeductionTypeSubscription && p.SubDaysToDeduct > 0 && p.SubscriptionID > 0 {
		if err := s.restoreRefundSubscription(ctx, p); err != nil {
			slog.Error("[CRITICAL] subscription rollback failed", "orderID", p.OrderID, "subID", p.SubscriptionID, "days", p.SubDaysToDeduct, "error", err)
			s.writeAuditLog(ctx, p.OrderID, "REFUND_ROLLBACK_FAILED", "admin", map[string]any{"gatewayError": psErrMsg(gErr), "rollbackError": psErrMsg(err), "subDaysDeducted": p.SubDaysToDeduct})
			return false
		}
		p.SubDaysToDeduct = 0
	}
	return true
}

func (s *PaymentService) restoreStatus(ctx context.Context, p *RefundPlan) {
	// A request can be canceled after the refund claim but before any gateway
	// call. Releasing that claim must survive cancellation and stay bounded.
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	rs := OrderStatusCompleted
	if p.Order.Status == OrderStatusRefundRequested {
		rs = OrderStatusRefundRequested
	}
	updated, err := s.entClient.PaymentOrder.Update().Where(
		paymentorder.IDEQ(p.OrderID), paymentorder.StatusEQ(OrderStatusRefunding),
	).SetStatus(rs).Save(ctx)
	if err != nil {
		slog.Error("restore refund order status failed", "orderID", p.OrderID, "status", rs, "error", err)
		return
	}
	if updated == 0 {
		slog.Warn("refund order status restore skipped after state changed", "orderID", p.OrderID, "status", rs)
	}
}
