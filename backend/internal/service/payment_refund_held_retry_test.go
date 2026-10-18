//go:build unit

package service

import (
	"context"
	"errors"
	"strconv"
	"testing"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/ent/paymentauditlog"
	"github.com/Wei-Shaw/sub2api/ent/schema/mixins"
	"github.com/Wei-Shaw/sub2api/internal/payment"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/stretchr/testify/require"
)

type heldRetryUserRepository struct {
	postMergeRefundUserRepository
	failReturn bool
	deductions int
	returns    int
}

func (r *heldRetryUserRepository) DeductAvailableBalance(ctx context.Context, id int64, amount float64) (float64, error) {
	r.deductions++
	return r.postMergeRefundUserRepository.DeductAvailableBalance(ctx, id, amount)
}

func (r *heldRetryUserRepository) UpdateBalance(ctx context.Context, id int64, amount float64) error {
	r.returns++
	if r.failReturn {
		return errors.New("temporary balance restore failure")
	}
	return r.postMergeRefundUserRepository.UpdateBalance(ctx, id, amount)
}

func heldRetryBalanceFixture(t *testing.T) (*PaymentService, *dbent.PaymentOrder, *heldRetryUserRepository, *postMergePendingRefundProvider) {
	t.Helper()
	ctx := context.Background()
	client := newPaymentConfigServiceTestClient(t)
	order := createCompletedRefundableOrderForTest(t, ctx, client, "held-retry")
	_, err := client.User.UpdateOneID(order.UserID).SetBalance(100).Save(ctx)
	require.NoError(t, err)
	repo := &heldRetryUserRepository{postMergeRefundUserRepository: postMergeRefundUserRepository{client: client}}
	provider := &postMergePendingRefundProvider{queryStatus: payment.ProviderStatusFailed}
	t.Cleanup(replacePaymentProviderFactoryForTest(t, provider))
	svc := &PaymentService{entClient: client, loadBalancer: &captureLoadBalancer{}, userRepo: repo}
	plan, early, err := svc.PrepareRefund(ctx, order.ID, 100, "original intent", false, true)
	require.NoError(t, err)
	require.Nil(t, early)
	_, err = svc.ExecuteRefund(ctx, plan)
	require.NoError(t, err)
	repo.failReturn = true
	result, err := svc.QueryAndFinalizeRefund(ctx, order.ID)
	require.NoError(t, err)
	require.Contains(t, result.Warning, "rollback failed")
	repo.failReturn = false
	return svc, order, repo, provider
}

func TestRefundHeldRetryPreservesBalanceUntilSettled(t *testing.T) {
	for _, outcome := range []string{"pending_failure", "pending_success", "immediate_failure", "immediate_success"} {
		t.Run(outcome, func(t *testing.T) {
			ctx := context.Background()
			svc, order, repo, provider := heldRetryBalanceFixture(t)
			plan, early, err := svc.PrepareRefund(ctx, order.ID, 0, "retry", true, true)
			require.NoError(t, err)
			require.Nil(t, early)
			switch outcome {
			case "immediate_failure":
				provider.refundStatus = payment.ProviderStatusFailed
			case "immediate_success":
				provider.refundStatus = payment.ProviderStatusSuccess
			case "pending_success":
				provider.queryStatus = payment.ProviderStatusSuccess
			}
			result, err := svc.ExecuteRefund(ctx, plan)
			require.NoError(t, err)
			if outcome == "pending_failure" || outcome == "pending_success" {
				result, err = svc.QueryAndFinalizeRefund(ctx, order.ID)
				require.NoError(t, err)
			}
			user, err := svc.entClient.User.Get(ctx, order.UserID)
			require.NoError(t, err)
			if outcome == "pending_failure" || outcome == "immediate_failure" {
				require.Equal(t, 100.0, user.Balance, "failed retry must return the original held deduction")
				require.False(t, result.Success)
			} else {
				require.Zero(t, user.Balance)
				require.True(t, result.Success)
				require.Equal(t, 100.0, result.BalanceDeducted, "success must report the original held deduction")
			}
			require.Equal(t, 1, repo.deductions, "retry must not deduct again")
		})
	}
}

func TestRefundHeldRetryPreservesOriginalAmountAndIntent(t *testing.T) {
	ctx := context.Background()
	svc, order, _, provider := heldRetryBalanceFixture(t)
	for _, request := range []struct {
		amount float64
		deduct bool
	}{{1, true}, {100, false}} {
		_, _, err := svc.PrepareRefund(ctx, order.ID, request.amount, "changed retry", false, request.deduct)
		require.Equal(t, "REFUND_HELD_CONFLICT", infraerrors.Reason(err), "changed amount or intent must be rejected while a deduction is held")
	}
	plan, early, err := svc.PrepareRefund(ctx, order.ID, 100, "retry", false, true)
	require.NoError(t, err)
	require.Nil(t, early, "held deduction requires no new available balance")
	_, err = svc.ExecuteRefund(ctx, plan)
	require.NoError(t, err)
	require.Equal(t, []string{"100.00", "100.00"}, provider.refundAmounts)
	detail, err := svc.latestRefundPendingDetail(ctx, order.ID)
	require.NoError(t, err)
	require.True(t, detail.shouldDeduct())
	require.Equal(t, 100.0, detail.RefundAmount)
	require.Equal(t, 100.0, detail.BalanceDeducted)
	require.Equal(t, "original intent", detail.Reason)
}

func TestRefundHeldRetryResolvedFailureDoesNotSkipLaterDeduction(t *testing.T) {
	ctx := context.Background()
	svc, order, repo, provider := heldRetryBalanceFixture(t)
	plan, early, err := svc.PrepareRefund(ctx, order.ID, 0, "retry", true, true)
	require.NoError(t, err)
	require.Nil(t, early)
	_, err = svc.ExecuteRefund(ctx, plan)
	require.NoError(t, err)
	_, err = svc.QueryAndFinalizeRefund(ctx, order.ID)
	require.NoError(t, err)
	provider.queryStatus = payment.ProviderStatusSuccess
	plan, early, err = svc.PrepareRefund(ctx, order.ID, 100, "new attempt", true, true)
	require.NoError(t, err)
	require.Nil(t, early)
	_, err = svc.ExecuteRefund(ctx, plan)
	require.NoError(t, err)
	result, err := svc.QueryAndFinalizeRefund(ctx, order.ID)
	require.NoError(t, err)
	require.True(t, result.Success)
	require.Equal(t, 2, repo.deductions, "resolved historical rollback failure must not suppress a new deduction")
	user, err := svc.entClient.User.Get(ctx, order.UserID)
	require.NoError(t, err)
	require.Zero(t, user.Balance)
}

func TestRefundHeldRetryRestoresSubscriptionSnapshot(t *testing.T) {
	for _, kind := range []string{"shortened", "revoked"} {
		t.Run(kind, func(t *testing.T) {
			ctx := context.Background()
			svc, order, original, provider := refundRevocationFixture(t)
			if kind == "shortened" {
				var err error
				original, err = svc.entClient.UserSubscription.UpdateOneID(original.ID).SetExpiresAt(original.ExpiresAt.AddDate(0, 0, 20)).Save(ctx)
				require.NoError(t, err)
			}
			provider.queryStatus = payment.ProviderStatusFailed
			plan, early, err := svc.PrepareRefund(ctx, order.ID, 25, "subscription retry", false, true)
			require.NoError(t, err)
			require.Nil(t, early)
			_, err = svc.ExecuteRefund(ctx, plan)
			require.NoError(t, err)
			_, err = svc.entClient.ExecContext(ctx, `CREATE TRIGGER fail_held_restore BEFORE UPDATE ON user_subscriptions BEGIN SELECT RAISE(ABORT, 'temporary subscription restore failure'); END`)
			require.NoError(t, err)
			result, err := svc.QueryAndFinalizeRefund(ctx, order.ID)
			require.NoError(t, err)
			require.Contains(t, result.Warning, "rollback failed")
			_, err = svc.entClient.ExecContext(ctx, `DROP TRIGGER fail_held_restore`)
			require.NoError(t, err)
			plan, early, err = svc.PrepareRefund(ctx, order.ID, 25, "retry", true, true)
			require.NoError(t, err)
			require.Nil(t, early)
			_, err = svc.ExecuteRefund(ctx, plan)
			require.NoError(t, err)
			result, err = svc.QueryAndFinalizeRefund(ctx, order.ID)
			require.NoError(t, err)
			require.NotContains(t, result.Warning, "rollback failed")
			restored, err := svc.entClient.UserSubscription.Get(mixins.SkipSoftDelete(ctx), original.ID)
			require.NoError(t, err)
			require.Nil(t, restored.DeletedAt)
			assertRefundCompensationTerm(t, original, restored, original.ExpiresAt)
		})
	}
}

func TestRefundHeldRetryAcceptsLegacyBalanceAudit(t *testing.T) {
	ctx := context.Background()
	client := newPaymentConfigServiceTestClient(t)
	order := createPendingRefundOrderWithDetailForTest(t, ctx, client, "legacy-held-retry", `{"refundID":"old","deductionHeld":true,"deductionType":"balance","balanceDeducted":60}`)
	_, err := client.PaymentOrder.UpdateOneID(order.ID).SetStatus(OrderStatusRefundFailed).Save(ctx)
	require.NoError(t, err)
	_, err = client.PaymentAuditLog.Create().SetOrderID(strconv.FormatInt(order.ID, 10)).SetAction("REFUND_ROLLBACK_FAILED").SetOperator("admin").SetDetail(`{"balanceDeducted":60,"rollbackError":"offline"}`).Save(ctx)
	require.NoError(t, err)
	provider := &postMergePendingRefundProvider{queryStatus: payment.ProviderStatusFailed}
	t.Cleanup(replacePaymentProviderFactoryForTest(t, provider))
	svc := &PaymentService{entClient: client, userRepo: &postMergeRefundUserRepository{client: client}, loadBalancer: &captureLoadBalancer{}}
	plan, early, err := svc.PrepareRefund(ctx, order.ID, 100, "legacy retry", true, true)
	require.NoError(t, err)
	require.Nil(t, early)
	_, err = svc.ExecuteRefund(ctx, plan)
	require.NoError(t, err)
	_, err = svc.QueryAndFinalizeRefund(ctx, order.ID)
	require.NoError(t, err)
	user, err := client.User.Get(ctx, order.UserID)
	require.NoError(t, err)
	require.Equal(t, 60.0, user.Balance)
}

func TestRefundHeldRetryDoesNotBorrowLegacyMetadataFromCompletedAttempt(t *testing.T) {
	ctx := context.Background()
	client := newPaymentConfigServiceTestClient(t)
	order := createPendingRefundOrderWithDetailForTest(t, ctx, client, "legacy-distinct-attempts",
		`{"refundID":"attempt_a","deductionHeld":true,"deductionType":"balance","balanceDeducted":100,"refundAmount":100,"reason":"attempt A"}`)
	// Legacy attempt A failed and returned its 100. Its order-level refund
	// fields remained set when a later synchronous attempt B for 50 failed.
	_, err := client.PaymentOrder.UpdateOneID(order.ID).SetStatus(OrderStatusRefundFailed).Save(ctx)
	require.NoError(t, err)
	require.NoError(t, createPaymentAuditLogTx(ctx, client, order.ID, "REFUND_FAILED", map[string]any{"detail": "attempt A failed and returned"}))
	_, err = client.User.UpdateOneID(order.UserID).SetBalance(50).Save(ctx)
	require.NoError(t, err)
	require.NoError(t, createPaymentAuditLogTx(ctx, client, order.ID, "REFUND_ROLLBACK_FAILED", map[string]any{"balanceDeducted": 50, "rollbackError": "attempt B return failed"}))
	require.NoError(t, createPaymentAuditLogTx(ctx, client, order.ID, "REFUND_FAILED", map[string]any{"detail": "attempt B failed"}))
	provider := &postMergePendingRefundProvider{}
	t.Cleanup(replacePaymentProviderFactoryForTest(t, provider))
	svc := &PaymentService{entClient: client, userRepo: &postMergeRefundUserRepository{client: client}, loadBalancer: &captureLoadBalancer{}}
	for _, amount := range []float64{100, 50} {
		_, _, err := svc.PrepareRefund(ctx, order.ID, amount, "retry B", true, true)
		require.Equal(t, "REFUND_HELD_METADATA_INVALID", infraerrors.Reason(err), "completed attempt A cannot establish the amount or intent of attempt B")
	}
	require.Empty(t, provider.refundAmounts)
	user, err := client.User.Get(ctx, order.UserID)
	require.NoError(t, err)
	require.Equal(t, 50.0, user.Balance)
}

func TestRefundHeldRetryResolutionAuditIsAtomic(t *testing.T) {
	ctx := context.Background()
	svc, order, repo, _ := heldRetryBalanceFixture(t)
	plan, early, err := svc.PrepareRefund(ctx, order.ID, 100, "retry", true, true)
	require.NoError(t, err)
	require.Nil(t, early)
	_, err = svc.ExecuteRefund(ctx, plan)
	require.NoError(t, err)
	_, err = svc.entClient.ExecContext(ctx, `CREATE TRIGGER fail_held_resolution BEFORE INSERT ON payment_audit_logs WHEN NEW.action='REFUND_ROLLBACK_SUCCEEDED' BEGIN SELECT RAISE(ABORT, 'resolution audit unavailable'); END`)
	require.NoError(t, err)
	_, err = svc.QueryAndFinalizeRefund(ctx, order.ID)
	require.ErrorContains(t, err, "resolution audit unavailable")
	user, err := svc.entClient.User.Get(ctx, order.UserID)
	require.NoError(t, err)
	require.Zero(t, user.Balance, "balance return must roll back when the resolution audit fails")
	current, err := svc.entClient.PaymentOrder.Get(ctx, order.ID)
	require.NoError(t, err)
	require.Equal(t, OrderStatusRefundPending, current.Status)
	_, err = svc.entClient.ExecContext(ctx, `DROP TRIGGER fail_held_resolution`)
	require.NoError(t, err)
	_, err = svc.QueryAndFinalizeRefund(ctx, order.ID)
	require.NoError(t, err)
	user, err = svc.entClient.User.Get(ctx, order.UserID)
	require.NoError(t, err)
	require.Equal(t, 100.0, user.Balance)
	require.Equal(t, 1, repo.deductions)
	require.Equal(t, 1, countRefundAuditsForTest(t, ctx, svc.entClient, order.ID, "REFUND_ROLLBACK_SUCCEEDED"))
	_, err = svc.QueryAndFinalizeRefund(ctx, order.ID)
	require.Equal(t, "INVALID_STATUS", infraerrors.Reason(err))
	user, err = svc.entClient.User.Get(ctx, order.UserID)
	require.NoError(t, err)
	require.Equal(t, 100.0, user.Balance, "repeated finalization must not return twice")
}

func TestRefundHeldRetryRepeatedReturnFailureKeepsOriginalDeduction(t *testing.T) {
	ctx := context.Background()
	svc, order, repo, _ := heldRetryBalanceFixture(t)
	for _, failReturn := range []bool{true, false} {
		repo.failReturn = failReturn
		plan, early, err := svc.PrepareRefund(ctx, order.ID, 100, "retry", true, true)
		require.NoError(t, err)
		require.Nil(t, early)
		_, err = svc.ExecuteRefund(ctx, plan)
		require.NoError(t, err)
		result, err := svc.QueryAndFinalizeRefund(ctx, order.ID)
		require.NoError(t, err)
		if failReturn {
			require.Contains(t, result.Warning, "rollback failed")
		}
	}
	user, err := svc.entClient.User.Get(ctx, order.UserID)
	require.NoError(t, err)
	require.Equal(t, 100.0, user.Balance)
	require.Equal(t, 1, repo.deductions)
	require.Equal(t, 2, countRefundAuditsForTest(t, ctx, svc.entClient, order.ID, "REFUND_ROLLBACK_FAILED"))
}

func TestRefundHeldRetryRejectsUnreadableRollbackAudit(t *testing.T) {
	ctx := context.Background()
	svc, order, repo, provider := heldRetryBalanceFixture(t)
	_, err := svc.entClient.PaymentAuditLog.Update().Where(paymentauditlog.OrderIDEQ(strconv.FormatInt(order.ID, 10)), paymentauditlog.ActionEQ("REFUND_ROLLBACK_FAILED")).SetDetail(`broken`).Save(ctx)
	require.NoError(t, err)
	_, _, err = svc.PrepareRefund(ctx, order.ID, 100, "retry", true, true)
	require.ErrorContains(t, err, "decode held refund deduction")
	require.Equal(t, []string{"100.00"}, provider.refundAmounts)
	require.Equal(t, 1, repo.deductions)
}

func TestRefundHeldRetryRejectsLegacyRevocationWithoutSnapshot(t *testing.T) {
	ctx := context.Background()
	svc, order, original, provider := refundRevocationFixture(t)
	plan, early, err := svc.PrepareRefund(ctx, order.ID, 25, "old revocation", false, true)
	require.NoError(t, err)
	require.Nil(t, early)
	_, err = svc.ExecuteRefund(ctx, plan)
	require.NoError(t, err)
	_, err = svc.entClient.PaymentOrder.UpdateOneID(order.ID).SetStatus(OrderStatusRefundFailed).Save(ctx)
	require.NoError(t, err)
	_, err = svc.entClient.PaymentAuditLog.Update().Where(paymentauditlog.OrderIDEQ(strconv.FormatInt(order.ID, 10)), paymentauditlog.ActionEQ("REFUND_PENDING")).SetDetail(`{"deductionHeld":true,"deductionType":"subscription","refundAmount":25,"subDaysDeducted":7,"subscriptionID":` + strconv.FormatInt(original.ID, 10) + `}`).Save(ctx)
	require.NoError(t, err)
	_, err = svc.entClient.PaymentAuditLog.Create().SetOrderID(strconv.FormatInt(order.ID, 10)).SetAction("REFUND_ROLLBACK_FAILED").SetOperator("admin").SetDetail(`{"subDaysDeducted":7}`).Save(ctx)
	require.NoError(t, err)
	_, _, err = svc.PrepareRefund(ctx, order.ID, 25, "retry", true, true)
	require.Equal(t, "REFUND_HELD_METADATA_INVALID", infraerrors.Reason(err))
	require.Equal(t, []string{"25.00"}, provider.refundAmounts, "do not guess which revocation belongs to the refund")
}

func TestRefundHeldRetryImmediateFailureAuditRecoveryNeverRepeatsGateway(t *testing.T) {
	for _, action := range []string{"REFUND_ROLLBACK_SUCCEEDED", "REFUND_GATEWAY_FAILED", "REFUND_ROLLBACK_FAILED", "REFUND_FAILED"} {
		t.Run(action, func(t *testing.T) {
			ctx := context.Background()
			client := newPaymentConfigServiceTestClient(t)
			order := createCompletedRefundableOrderForTest(t, ctx, client, "immediate-audit-recovery")
			_, err := client.User.UpdateOneID(order.UserID).SetBalance(100).Save(ctx)
			require.NoError(t, err)
			repo := &heldRetryUserRepository{postMergeRefundUserRepository: postMergeRefundUserRepository{client: client}}
			repo.failReturn = action == "REFUND_ROLLBACK_FAILED" || action == "REFUND_FAILED"
			provider := &postMergePendingRefundProvider{refundStatus: payment.ProviderStatusFailed, queryStatus: payment.ProviderStatusSuccess}
			t.Cleanup(replacePaymentProviderFactoryForTest(t, provider))
			svc := &PaymentService{entClient: client, userRepo: repo, loadBalancer: &captureLoadBalancer{}}
			_, err = client.ExecContext(ctx, `CREATE TRIGGER reject_immediate_recovery BEFORE INSERT ON payment_audit_logs WHEN NEW.action='`+action+`' BEGIN SELECT RAISE(ABORT, 'recovery audit unavailable'); END`)
			require.NoError(t, err)
			plan, early, err := svc.PrepareRefund(ctx, order.ID, 100, "sync failure", false, true)
			require.NoError(t, err)
			require.Nil(t, early)
			result, err := svc.ExecuteRefund(ctx, plan)
			require.NoError(t, err)
			require.False(t, result.Success)
			current, err := client.PaymentOrder.Get(ctx, order.ID)
			require.NoError(t, err)
			require.Equal(t, OrderStatusRefundPending, current.Status, "audit failure must leave a resumable local compensation")
			user, err := client.User.Get(ctx, order.UserID)
			require.NoError(t, err)
			require.Zero(t, user.Balance, "uncommitted compensation must not return money")
			_, err = client.ExecContext(ctx, `DROP TRIGGER reject_immediate_recovery`)
			require.NoError(t, err)
			repo.failReturn = false
			providerID, err := strconv.ParseInt(psStringValue(order.ProviderInstanceID), 10, 64)
			require.NoError(t, err)
			require.NoError(t, client.PaymentProviderInstance.DeleteOneID(providerID).Exec(ctx), "local compensation must work after the provider is removed")
			// A fresh service must recover from persistence, without the old plan.
			svc = &PaymentService{entClient: client, userRepo: repo, loadBalancer: &captureLoadBalancer{}}
			plan, early, err = svc.PrepareRefund(ctx, order.ID, 100, "retry recovery", true, true)
			require.NoError(t, err)
			require.Nil(t, early)
			repo.failReturn = true
			result, err = svc.ExecuteRefund(ctx, plan)
			require.NoError(t, err)
			require.False(t, result.Success)
			current, err = client.PaymentOrder.Get(ctx, order.ID)
			require.NoError(t, err)
			require.Equal(t, OrderStatusRefundPending, current.Status, "failed local recovery must stay retryable without a provider")
			repo.failReturn = false
			result, err = svc.ExecuteRefund(ctx, plan)
			require.NoError(t, err)
			require.False(t, result.Success)
			user, err = client.User.Get(ctx, order.UserID)
			require.NoError(t, err)
			require.Equal(t, 100.0, user.Balance)
			require.Equal(t, []string{"100.00"}, provider.refundAmounts)
			require.Zero(t, provider.queryCalls, "known gateway failure must only retry local compensation")
		})
	}
}
