//go:build unit

package service

import (
	"context"
	"errors"
	"strconv"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/payment"
	"github.com/stretchr/testify/require"
)

func TestRefundRestoresStatusClaimedAfterPreparation(t *testing.T) {
	for _, stage := range []string{"deduction_failed", "gateway_failed"} {
		t.Run(stage, func(t *testing.T) {
			ctx := context.Background()
			client := newPaymentConfigServiceTestClient(t)
			order := createCompletedRefundableOrderForTest(t, ctx, client, "request-after-prepare")
			_, err := client.User.UpdateOneID(order.UserID).SetBalance(100).Save(ctx)
			require.NoError(t, err)
			instanceID, err := strconv.ParseInt(psStringValue(order.ProviderInstanceID), 10, 64)
			require.NoError(t, err)
			_, err = client.PaymentProviderInstance.UpdateOneID(instanceID).SetAllowUserRefund(true).Save(ctx)
			require.NoError(t, err)
			repo := &review2CanceledRefundDeductor{postMergeRefundUserRepository: postMergeRefundUserRepository{client: client}}
			repo.deduct = func(callCtx context.Context, userID int64, amount float64) (float64, error) {
				if stage == "deduction_failed" {
					return 0, errors.New("temporary deduction failure")
				}
				return repo.postMergeRefundUserRepository.DeductAvailableBalance(callCtx, userID, amount)
			}
			provider := &postMergePendingRefundProvider{refundStatus: payment.ProviderStatusFailed}
			t.Cleanup(replacePaymentProviderFactoryForTest(t, provider))
			svc := &PaymentService{entClient: client, userRepo: repo, loadBalancer: &captureLoadBalancer{}}
			plan, early, err := svc.PrepareRefund(ctx, order.ID, 100, "administrator refund", false, true)
			require.NoError(t, err)
			require.Nil(t, early)
			require.NoError(t, svc.RequestRefund(ctx, order.ID, order.UserID, "new user request"))
			_, err = svc.ExecuteRefund(ctx, plan)
			if stage == "deduction_failed" {
				require.ErrorContains(t, err, "temporary deduction failure")
			} else {
				require.NoError(t, err)
			}
			current, err := client.PaymentOrder.Get(ctx, order.ID)
			require.NoError(t, err)
			require.Equal(t, OrderStatusRefundRequested, current.Status, "failure must restore the state actually claimed, preserving a request accepted after preparation")
			require.Equal(t, "new user request", psStringValue(current.RefundRequestReason))
			user, err := client.User.Get(ctx, order.UserID)
			require.NoError(t, err)
			require.Equal(t, 100.0, user.Balance)
		})
	}
}
