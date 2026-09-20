//go:build unit

package service

import (
	"context"
	"errors"
	"testing"
	"time"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/ent/paymentauditlog"
	"github.com/Wei-Shaw/sub2api/internal/payment"
	"github.com/stretchr/testify/require"
)

type paymentWelfareCompletionRepo struct {
	t     *testing.T
	calls int
	fail  bool
}

func (r *paymentWelfareCompletionRepo) AccrueSubscriptionPurchase(ctx context.Context, id int64) error {
	r.calls++
	tx := dbent.TxFromContext(ctx)
	require.NotNil(r.t, tx, "reward and order completion must share a transaction")
	order, err := tx.PaymentOrder.Get(ctx, id)
	require.NoError(r.t, err)
	require.Equal(r.t, OrderStatusCompleted, order.Status)
	_, err = tx.PaymentAuditLog.Create().SetOrderID("welfare-test").SetAction("WELFARE_TEST_GRANT").SetDetail("{}").SetOperator("system").Save(ctx)
	require.NoError(r.t, err)
	if r.fail {
		return errors.New("welfare write failed")
	}
	return nil
}

func (*paymentWelfareCompletionRepo) ReverseSubscriptionPurchase(context.Context, int64) error {
	return nil
}

func TestPaymentWelfareCompletionIsAtomicAndReplaySafe(t *testing.T) {
	for _, fail := range []bool{false, true} {
		t.Run(map[bool]string{false: "success", true: "rollback"}[fail], func(t *testing.T) {
			ctx := context.Background()
			client := newPaymentConfigServiceTestClient(t)
			order := createPaymentFulfillmentSubscriptionOrder(t, ctx, client, OrderStatusRecharging, time.Now())
			repo := &paymentWelfareCompletionRepo{t: t, fail: fail}
			svc := &PaymentService{entClient: client, welfarePaymentRepo: repo}
			lease := &paymentFulfillmentLease{version: order.UpdatedAt}
			err := svc.markCompleted(ctx, order, lease, "SUBSCRIPTION_SUCCESS")
			require.Equal(t, 1, repo.calls)
			reloaded, readErr := client.PaymentOrder.Get(ctx, order.ID)
			require.NoError(t, readErr)
			count, readErr := client.PaymentAuditLog.Query().Where(paymentauditlog.ActionEQ("WELFARE_TEST_GRANT")).Count(ctx)
			require.NoError(t, readErr)
			if fail {
				require.ErrorContains(t, err, "welfare write failed")
				require.Equal(t, OrderStatusRecharging, reloaded.Status)
				require.Nil(t, reloaded.CompletedAt)
				require.Zero(t, count)
				repo.fail = false
				require.NoError(t, svc.markCompleted(ctx, order, lease, "SUBSCRIPTION_SUCCESS"))
				require.Equal(t, 2, repo.calls)
			} else {
				require.NoError(t, err)
				require.Equal(t, OrderStatusCompleted, reloaded.Status)
				require.Equal(t, 1, count)
			}
			calls := repo.calls
			require.NoError(t, svc.markCompleted(ctx, order, lease, "SUBSCRIPTION_SUCCESS"))
			require.Equal(t, calls, repo.calls, "completed orders must never be backfilled")
		})
	}
}

func TestPaymentWelfareCompletionSkipsBalanceAndLostLease(t *testing.T) {
	ctx := context.Background()
	client := newPaymentConfigServiceTestClient(t)
	repo := &paymentWelfareCompletionRepo{t: t}
	svc := &PaymentService{entClient: client, welfarePaymentRepo: repo}
	order := createPaymentFulfillmentSubscriptionOrder(t, ctx, client, OrderStatusRecharging, time.Now())
	err := svc.markCompleted(ctx, order, &paymentFulfillmentLease{version: order.UpdatedAt.Add(-time.Hour)}, "SUBSCRIPTION_SUCCESS")
	require.Error(t, err)
	require.Zero(t, repo.calls)
	order, err = client.PaymentOrder.UpdateOneID(order.ID).SetOrderType(payment.OrderTypeBalance).SetStatus(OrderStatusPaid).Save(ctx)
	require.NoError(t, err)
	lease, err := svc.acquirePaymentFulfillmentLease(ctx, order)
	require.NoError(t, err)
	require.NotNil(t, lease)
	require.NoError(t, svc.markCompleted(ctx, order, lease, "RECHARGE_SUCCESS"))
	require.Zero(t, repo.calls)
}
