//go:build unit

package service

import (
	"context"
	"encoding/json"
	"strconv"
	"testing"
	"time"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/internal/payment"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/stretchr/testify/require"
)

func createRefundAuditAtForReview(t *testing.T, client *dbent.Client, orderID int64, action string, createdAt time.Time, detail any) {
	t.Helper()
	raw, err := json.Marshal(detail)
	require.NoError(t, err)
	_, err = client.PaymentAuditLog.Create().SetOrderID(strconv.FormatInt(orderID, 10)).
		SetAction(action).SetCreatedAt(createdAt).SetDetail(string(raw)).SetOperator("admin").Save(context.Background())
	require.NoError(t, err)
}

func TestRefundAuditOrderResolvedHoldSurvivesClockRollback(t *testing.T) {
	for _, clockSkew := range []time.Duration{-time.Minute, time.Minute} {
		t.Run(clockSkew.String(), func(t *testing.T) {
			ctx := context.Background()
			client := newPaymentConfigServiceTestClient(t)
			order := createCompletedRefundableOrderForTest(t, ctx, client, "resolved-clock")
			_, err := client.User.UpdateOneID(order.UserID).SetBalance(100).Save(ctx)
			require.NoError(t, err)
			order, err = client.PaymentOrder.UpdateOneID(order.ID).SetStatus(OrderStatusRefundFailed).Save(ctx)
			require.NoError(t, err)
			now := time.Now().UTC()
			held := refundPendingAuditDetail{RefundAmount: 100, Reason: "old attempt", DeductionHeld: true,
				DeductionType: payment.DeductionTypeBalance, BalanceDeducted: 100}
			// The older failure was committed by another application instance.
			// Its wall clock may be ahead of the instance returning the balance.
			createRefundAuditAtForReview(t, client, order.ID, "REFUND_ROLLBACK_FAILED", now.Add(clockSkew), held)
			createRefundAuditAtForReview(t, client, order.ID, "REFUND_ROLLBACK_SUCCEEDED", now, held)
			provider := &postMergePendingRefundProvider{refundStatus: payment.ProviderStatusSuccess}
			t.Cleanup(replacePaymentProviderFactoryForTest(t, provider))
			repo := &heldRetryUserRepository{postMergeRefundUserRepository: postMergeRefundUserRepository{client: client}}
			svc := &PaymentService{entClient: client, userRepo: repo, loadBalancer: &captureLoadBalancer{}}
			plan, early, err := svc.PrepareRefund(ctx, order.ID, 100, "new attempt", true, true)
			require.NoError(t, err)
			require.Nil(t, early)
			result, err := svc.ExecuteRefund(ctx, plan)
			require.NoError(t, err)
			require.True(t, result.Success)
			user, err := client.User.Get(ctx, order.UserID)
			require.NoError(t, err)
			require.Zero(t, user.Balance, "a resolved hold must not exempt a later refund from deduction")
			require.Equal(t, 1, repo.deductions)
		})
	}
}

func TestRefundAuditOrderPendingFailureUsesLatestAttemptDespiteClockRollback(t *testing.T) {
	ctx := context.Background()
	client := newPaymentConfigServiceTestClient(t)
	order := createCompletedRefundableOrderForTest(t, ctx, client, "pending-clock")
	order, err := client.PaymentOrder.UpdateOneID(order.ID).SetStatus(OrderStatusRefundPending).SetRefundAmount(100).Save(ctx)
	require.NoError(t, err)
	_, err = client.User.UpdateOneID(order.UserID).SetBalance(40).Save(ctx)
	require.NoError(t, err)
	now := time.Now().UTC()
	old := refundPendingAuditDetail{RefundID: "previous_refund", RefundAmount: 100,
		DeductionHeld: true, DeductionType: payment.DeductionTypeBalance, BalanceDeducted: 100}
	createRefundAuditAtForReview(t, client, order.ID, "REFUND_PENDING", now.Add(time.Minute), old)
	createRefundAuditAtForReview(t, client, order.ID, "REFUND_ROLLBACK_SUCCEEDED", now, old)
	createRefundAuditAtForReview(t, client, order.ID, "REFUND_FAILED", now, map[string]any{})
	current := refundPendingAuditDetail{RefundID: "current_refund", RefundAmount: 100,
		DeductionHeld: true, DeductionType: payment.DeductionTypeBalance, BalanceDeducted: 60}
	createRefundAuditAtForReview(t, client, order.ID, "REFUND_PENDING", now, current)
	provider := &postMergePendingRefundProvider{queryStatus: payment.ProviderStatusFailed}
	t.Cleanup(replacePaymentProviderFactoryForTest(t, provider))
	svc := &PaymentService{entClient: client, userRepo: &postMergeRefundUserRepository{client: client}, loadBalancer: &captureLoadBalancer{}}
	result, err := svc.QueryAndFinalizeRefund(ctx, order.ID)
	require.NoError(t, err)
	require.False(t, result.Success)
	user, err := client.User.Get(ctx, order.UserID)
	require.NoError(t, err)
	require.Equal(t, 100.0, user.Balance, "failure must return only the current attempt's 60, never the old attempt's 100")
}

func TestRefundAuditOrderLegacyMetadataDoesNotCrossResolvedAttemptOnClockRollback(t *testing.T) {
	ctx := context.Background()
	client := newPaymentConfigServiceTestClient(t)
	order := createCompletedRefundableOrderForTest(t, ctx, client, "legacy-clock")
	order, err := client.PaymentOrder.UpdateOneID(order.ID).SetStatus(OrderStatusRefundFailed).SetRefundAmount(100).Save(ctx)
	require.NoError(t, err)
	now := time.Now().UTC()
	old := refundPendingAuditDetail{RefundID: "old_refund", RefundAmount: 100,
		DeductionHeld: true, DeductionType: payment.DeductionTypeBalance, BalanceDeducted: 100}
	createRefundAuditAtForReview(t, client, order.ID, "REFUND_PENDING", now.Add(time.Minute), old)
	createRefundAuditAtForReview(t, client, order.ID, "REFUND_FAILED", now, map[string]any{})
	createRefundAuditAtForReview(t, client, order.ID, "REFUND_ROLLBACK_FAILED", now, map[string]any{"balanceDeducted": 50})
	svc := &PaymentService{entClient: client}
	_, _, err = svc.PrepareRefund(ctx, order.ID, 100, "legacy retry", true, true)
	require.Equal(t, "REFUND_HELD_METADATA_INVALID", infraerrors.Reason(err), "old pending metadata cannot prove a later synchronous refund's amount")
}
