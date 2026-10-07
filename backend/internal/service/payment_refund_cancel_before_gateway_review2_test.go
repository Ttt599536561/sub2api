//go:build unit

package service

import (
	"context"
	"errors"
	"testing"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/internal/payment"
	"github.com/stretchr/testify/require"
)

type review2CanceledRefundDeductor struct {
	postMergeRefundUserRepository
	deduct func(context.Context, int64, float64) (float64, error)
}

func (r *review2CanceledRefundDeductor) DeductAvailableBalance(ctx context.Context, id int64, amount float64) (float64, error) {
	return r.deduct(ctx, id, amount)
}

func TestReview2RefundCancellationBeforeGatewayRestoresOrder(t *testing.T) {
	for _, status := range []string{OrderStatusCompleted, OrderStatusRefundRequested} {
		for _, stage := range []string{"before_deduction", "deduction_error"} {
			t.Run(status+"/"+stage, func(t *testing.T) {
				base := context.Background()
				client, order := refundWelfareFixture(t, status, payment.OrderTypeBalance)
				_, err := client.User.UpdateOneID(order.UserID).SetBalance(100).Save(base)
				require.NoError(t, err)
				ctx, cancel := context.WithCancel(base)
				defer cancel()
				provider := &postMergePendingRefundProvider{}
				restore := replacePaymentProviderFactoryForTest(t, provider)
				defer restore()
				calls := 0
				repo := &review2CanceledRefundDeductor{postMergeRefundUserRepository: postMergeRefundUserRepository{client: client}}
				repo.deduct = func(callCtx context.Context, id int64, amount float64) (float64, error) {
					calls++
					require.Equal(t, order.UserID, id)
					require.Equal(t, 25.0, amount)
					if stage == "before_deduction" {
						require.ErrorIs(t, callCtx.Err(), context.Canceled)
						return 0, callCtx.Err()
					}
					cancel()
					return 0, errors.New("deduction failed while request was canceled")
				}
				svc := &PaymentService{entClient: client, loadBalancer: &captureLoadBalancer{}, userRepo: repo}
				plan, early, err := svc.PrepareRefund(ctx, order.ID, 25, "canceled before gateway", false, true)
				require.NoError(t, err)
				require.Nil(t, early)
				if stage == "before_deduction" {
					client.PaymentOrder.Use(func(next dbent.Mutator) dbent.Mutator {
						return dbent.MutateFunc(func(callCtx context.Context, mutation dbent.Mutation) (dbent.Value, error) {
							value, err := next.Mutate(callCtx, mutation)
							if change, ok := mutation.(*dbent.PaymentOrderMutation); ok && err == nil {
								if nextStatus, _ := change.Status(); nextStatus == OrderStatusRefunding {
									cancel()
								}
							}
							return value, err
						})
					})
				}
				result, err := svc.ExecuteRefund(ctx, plan)
				require.Error(t, err)
				require.Nil(t, result)
				require.Equal(t, 1, calls)
				require.Empty(t, provider.refundAmounts, "no irreversible gateway action took place")
				user, err := client.User.Get(base, order.UserID)
				require.NoError(t, err)
				require.Equal(t, 100.0, user.Balance)
				persisted, err := client.PaymentOrder.Get(base, order.ID)
				require.NoError(t, err)
				require.Equal(t, status, persisted.Status, "pre-gateway cancellation must release the refund claim")
			})
		}
	}
}

func TestReview2RefundStatusRestorePreservesLaterState(t *testing.T) {
	for _, status := range []string{OrderStatusRefundPending, OrderStatusRefunded, OrderStatusPartiallyRefunded} {
		t.Run(status, func(t *testing.T) {
			client, order := refundWelfareFixture(t, OrderStatusCompleted, payment.OrderTypeBalance)
			_, err := client.PaymentOrder.UpdateOneID(order.ID).SetStatus(status).Save(context.Background())
			require.NoError(t, err)
			svc := &PaymentService{entClient: client}
			svc.restoreStatus(context.Background(), &RefundPlan{OrderID: order.ID, Order: order})
			persisted, err := client.PaymentOrder.Get(context.Background(), order.ID)
			require.NoError(t, err)
			require.Equal(t, status, persisted.Status)
		})
	}
}
