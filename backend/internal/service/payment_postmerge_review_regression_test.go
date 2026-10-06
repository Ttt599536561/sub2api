//go:build unit

package service

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"testing"
	"time"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/internal/payment"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/stretchr/testify/require"
)

// An admin can deliberately refund the payment without reclaiming the user's
// credit. A pending gateway response must preserve that choice until settlement.
func TestPostMergePendingRefundPreservesNoDeductionChoice(t *testing.T) {
	ctx := context.Background()
	client := newPaymentConfigServiceTestClient(t)
	order := createPendingRefundOrderForTest(t, ctx, client, "postmerge-no-deduction")
	order, err := client.PaymentOrder.UpdateOneID(order.ID).SetStatus(OrderStatusCompleted).Save(ctx)
	require.NoError(t, err)
	_, err = client.User.UpdateOneID(order.UserID).SetBalance(100).Save(ctx)
	require.NoError(t, err)
	provider := &postMergePendingRefundProvider{}
	restore := replacePaymentProviderFactoryForTest(t, provider)
	defer restore()
	svc := &PaymentService{entClient: client, loadBalancer: &captureLoadBalancer{},
		userRepo: &mockUserRepo{deductAvailableBalanceFn: func(ctx context.Context, id int64, amount float64) (float64, error) {
			tx := dbent.TxFromContext(ctx)
			require.NotNil(t, tx)
			_, err := tx.User.UpdateOneID(id).AddBalance(-amount).Save(ctx)
			return amount, err
		}},
	}
	plan, early, err := svc.PrepareRefund(ctx, order.ID, 25, "goodwill refund", false, false)
	require.NoError(t, err)
	require.Nil(t, early)
	require.Equal(t, payment.DeductionTypeNone, plan.DeductionType)
	result, err := svc.ExecuteRefund(ctx, plan)
	require.NoError(t, err)
	require.False(t, result.Success)
	result, err = svc.QueryAndFinalizeRefund(ctx, order.ID)
	require.NoError(t, err)
	require.True(t, result.Success)
	persisted, err := client.User.Get(ctx, order.UserID)
	require.NoError(t, err)
	require.Equal(t, 100.0, persisted.Balance, "pending refund must retain the admin's no-deduction choice")
	require.Zero(t, result.BalanceDeducted)
}

type postMergePendingRefundProvider struct {
	refundProviderTestDouble
	refundAmounts []string
	refundStatus  string
	queryStatus   string
	queryCalls    int
}

func (p *postMergePendingRefundProvider) Refund(_ context.Context, req payment.RefundRequest) (*payment.RefundResponse, error) {
	p.refundAmounts = append(p.refundAmounts, req.Amount)
	status := p.refundStatus
	if status == "" {
		status = payment.ProviderStatusPending
	}
	return &payment.RefundResponse{RefundID: "rf_postmerge", Status: status}, nil
}

func (p *postMergePendingRefundProvider) QueryRefund(context.Context, payment.RefundQueryRequest) (*payment.RefundResponse, error) {
	p.queryCalls++
	status := p.queryStatus
	if status == "" {
		status = payment.ProviderStatusSuccess
	}
	return &payment.RefundResponse{RefundID: "rf_postmerge", Status: status}, nil
}

func TestPostMergePendingRefundRetryCannotCreateAnotherAmount(t *testing.T) {
	ctx := context.Background()
	client := newPaymentConfigServiceTestClient(t)
	order := createPendingRefundOrderForTest(t, ctx, client, "postmerge-pending-retry")
	order, err := client.PaymentOrder.UpdateOneID(order.ID).SetStatus(OrderStatusCompleted).Save(ctx)
	require.NoError(t, err)
	provider := &postMergePendingRefundProvider{}
	restore := replacePaymentProviderFactoryForTest(t, provider)
	defer restore()
	svc := &PaymentService{entClient: client, loadBalancer: &captureLoadBalancer{}, userRepo: &mockUserRepo{}}
	plan, early, err := svc.PrepareRefund(ctx, order.ID, 25, "first request", false, false)
	require.NoError(t, err)
	require.Nil(t, early)
	result, err := svc.ExecuteRefund(ctx, plan)
	require.NoError(t, err)
	require.False(t, result.Success)

	// A second admin request while the first gateway operation remains pending
	// must query that operation or fail. It must not create a new refund identity.
	plan, early, err = svc.PrepareRefund(ctx, order.ID, 30, "retry with changed amount", false, false)
	if err == nil && early == nil {
		_, _ = svc.ExecuteRefund(ctx, plan)
	}
	require.Equal(t, []string{"25.00"}, provider.refundAmounts, "Stripe/WeChat use amount-specific refund IDs, so this refunds twice")
	persisted, err := client.PaymentOrder.Get(ctx, order.ID)
	require.NoError(t, err)
	require.Equal(t, 25.0, persisted.RefundAmount, "a pending refund's recorded amount must remain immutable")
}

type postMergeRefundUserRepository struct {
	mockUserRepo
	client *dbent.Client
}

func (r *postMergeRefundUserRepository) txClient(ctx context.Context) *dbent.Client {
	if tx := dbent.TxFromContext(ctx); tx != nil {
		return tx.Client()
	}
	return r.client
}

func (r *postMergeRefundUserRepository) GetByID(ctx context.Context, id int64) (*User, error) {
	u, err := r.txClient(ctx).User.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	return &User{ID: u.ID, Balance: u.Balance}, nil
}

func (r *postMergeRefundUserRepository) UpdateBalance(ctx context.Context, id int64, amount float64) error {
	_, err := r.txClient(ctx).User.UpdateOneID(id).AddBalance(amount).Save(ctx)
	return err
}

func (r *postMergeRefundUserRepository) DeductAvailableBalance(ctx context.Context, id int64, amount float64) (float64, error) {
	u, err := r.GetByID(ctx, id)
	if err != nil {
		return 0, err
	}
	amount = math.Min(amount, math.Max(0, u.Balance))
	return amount, r.UpdateBalance(ctx, id, -amount)
}

func TestPostMergeRefundIntentAndIdentitySurviveFinalizationFailure(t *testing.T) {
	for _, orderType := range []string{payment.OrderTypeBalance, payment.OrderTypeSubscription} {
		for _, deduct := range []bool{false, true} {
			for _, gatewayStatus := range []string{payment.ProviderStatusPending, payment.ProviderStatusSuccess} {
				t.Run(fmt.Sprintf("%s/deduct=%t/%s", orderType, deduct, gatewayStatus), func(t *testing.T) {
					ctx := context.Background()
					client, order := refundWelfareFixture(t, OrderStatusCompleted, orderType)
					_, err := client.User.UpdateOneID(order.UserID).SetBalance(100).Save(ctx)
					require.NoError(t, err)
					group, err := client.Group.Create().SetName("postmerge-refund").Save(ctx)
					require.NoError(t, err)
					expiry := time.Now().UTC().AddDate(0, 0, 30)
					sub, err := client.UserSubscription.Create().SetUserID(order.UserID).SetGroupID(group.ID).
						SetStartsAt(time.Now().UTC()).SetExpiresAt(expiry).Save(ctx)
					require.NoError(t, err)
					_, err = client.PaymentOrder.UpdateOneID(order.ID).SetSubscriptionGroupID(group.ID).SetSubscriptionDays(7).Save(ctx)
					require.NoError(t, err)
					provider := &postMergePendingRefundProvider{refundStatus: gatewayStatus, queryStatus: payment.ProviderStatusPending}
					restore := replacePaymentProviderFactoryForTest(t, provider)
					defer restore()
					svc := &PaymentService{entClient: client, loadBalancer: &captureLoadBalancer{},
						userRepo:        &postMergeRefundUserRepository{client: client},
						subscriptionSvc: &SubscriptionService{userSubRepo: &refundWelfareSubscriptionRepository{client: client, subID: sub.ID}},
						welfarePaymentRepo: &refundWelfareRepositoryStub{reverse: func(ctx context.Context, id int64) error {
							recordRefundWelfareEvent(t, ctx, id)
							return nil
						}},
					}
					_, err = client.ExecContext(ctx, `CREATE TRIGGER reject_postmerge_refund_success BEFORE INSERT ON payment_audit_logs
 WHEN NEW.action='REFUND_SUCCESS' BEGIN SELECT RAISE(ABORT, 'postmerge finalization failure'); END`)
					require.NoError(t, err)
					plan, early, err := svc.PrepareRefund(ctx, order.ID, 25, "original intent", false, deduct)
					require.NoError(t, err)
					require.Nil(t, early)
					result, err := svc.ExecuteRefund(ctx, plan)
					if gatewayStatus == payment.ProviderStatusPending {
						require.NoError(t, err)
						require.False(t, result.Success)
						// Same amount and changed amount both query the original
						// pending operation without repeating its gateway mutation.
						for _, retryAmount := range []float64{25, 30} {
							plan, early, err = svc.PrepareRefund(ctx, order.ID, retryAmount, "changed intent", true, !deduct)
							require.NoError(t, err)
							require.Nil(t, early)
							result, err = svc.ExecuteRefund(ctx, plan)
							require.NoError(t, err)
							require.False(t, result.Success)
						}
						provider.queryStatus = payment.ProviderStatusSuccess
						_, err = svc.QueryAndFinalizeRefund(ctx, order.ID)
					}
					require.ErrorContains(t, err, "postmerge finalization failure")
					assertRefundWelfareEventCount(t, client, 0)
					detail, err := svc.latestRefundPendingDetail(ctx, order.ID)
					require.NoError(t, err)
					require.True(t, detail.GatewayConfirmed)
					require.NotNil(t, detail.DeductBalance)
					require.Equal(t, deduct, *detail.DeductBalance)
					queryCalls := provider.queryCalls
					_, err = client.ExecContext(ctx, "DROP TRIGGER reject_postmerge_refund_success")
					require.NoError(t, err)
					plan, early, err = svc.PrepareRefund(ctx, order.ID, 100, "changed again", true, !deduct)
					require.NoError(t, err)
					require.Nil(t, early)
					result, err = svc.ExecuteRefund(ctx, plan)
					require.NoError(t, err)
					require.True(t, result.Success)
					_, err = svc.QueryAndFinalizeRefund(ctx, order.ID)
					require.NoError(t, err)
					require.Equal(t, []string{"25.00"}, provider.refundAmounts)
					require.Equal(t, queryCalls, provider.queryCalls, "confirmed recovery must not contact the gateway")
					user, err := client.User.Get(ctx, order.UserID)
					require.NoError(t, err)
					wantBalance := 100.0
					wantExpiry := expiry
					if deduct {
						if orderType == payment.OrderTypeBalance {
							wantBalance = 75
						} else {
							wantExpiry = expiry.AddDate(0, 0, -7)
						}
					}
					require.Equal(t, wantBalance, user.Balance)
					currentSub, err := client.UserSubscription.Get(ctx, sub.ID)
					require.NoError(t, err)
					require.True(t, wantExpiry.Equal(currentSub.ExpiresAt))
					wantReversals := 0
					if orderType == payment.OrderTypeSubscription {
						wantReversals = 1
					}
					assertRefundWelfareEventCount(t, client, wantReversals)
					persisted, err := client.PaymentOrder.Get(ctx, order.ID)
					require.NoError(t, err)
					require.Equal(t, 25.0, persisted.RefundAmount)
					require.Equal(t, "original intent", *persisted.RefundReason)
					require.Equal(t, OrderStatusPartiallyRefunded, persisted.Status)
				})
			}
		}
	}
}

func TestPostMergeRefundIntentLegacyCompatibility(t *testing.T) {
	for _, tc := range []struct {
		name   string
		detail string
		deduct bool
	}{
		{"legacy missing intent", `{"deductionRollbackOK":true}`, true},
		{"legacy zero deduction remains ambiguous", `{"deductionRollbackOK":true,"balanceRolledBack":0}`, true},
		{"legacy explicit none", `{"deductionType":"none"}`, false},
		{"legacy explicit subscription", `{"deductionType":"subscription"}`, true},
		{"new no deduction", `{"deductBalance":false}`, false},
		{"new forced zero balance", `{"deductBalance":true,"deductionType":"none"}`, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var detail refundPendingAuditDetail
			require.NoError(t, json.Unmarshal([]byte(tc.detail), &detail))
			require.Equal(t, tc.deduct, detail.shouldDeduct())
		})
	}
}

func TestPostMergePendingRefundAuditAndIntentAreAtomic(t *testing.T) {
	ctx := context.Background()
	client, order := refundWelfareFixture(t, OrderStatusRefunding, payment.OrderTypeBalance)
	_, err := client.ExecContext(ctx, `CREATE TRIGGER reject_postmerge_pending BEFORE INSERT ON payment_audit_logs
 WHEN NEW.action='REFUND_PENDING' BEGIN SELECT RAISE(ABORT, 'pending intent unavailable'); END`)
	require.NoError(t, err)
	svc := &PaymentService{entClient: client}
	result, err := svc.finishRefund(ctx, &RefundPlan{OrderID: order.ID, Order: order, RefundAmount: 25,
		Reason: "no deduction", DeductionType: payment.DeductionTypeNone},
		&payment.RefundResponse{RefundID: "rf_postmerge", Status: payment.ProviderStatusPending})
	require.ErrorContains(t, err, "pending intent unavailable")
	require.Nil(t, result)
	persisted, err := client.PaymentOrder.Get(ctx, order.ID)
	require.NoError(t, err)
	require.Equal(t, OrderStatusRefunding, persisted.Status, "must not expose pending without its intent")
}

func TestPostMergePendingRetryWithoutQuerySupportNeverResubmits(t *testing.T) {
	ctx := context.Background()
	client := newPaymentConfigServiceTestClient(t)
	order := createPendingRefundOrderForTest(t, ctx, client, "postmerge-unsupported-query")
	provider := &postMergeRefundWithoutQueryProvider{}
	restore := replacePaymentProviderFactoryForTest(t, provider)
	defer restore()
	svc := &PaymentService{entClient: client, loadBalancer: &captureLoadBalancer{}}
	plan, early, err := svc.PrepareRefund(ctx, order.ID, 25, "retry", false, false)
	require.NoError(t, err)
	require.Nil(t, early)
	result, err := svc.ExecuteRefund(ctx, plan)
	require.Nil(t, result)
	require.Equal(t, "REFUND_QUERY_UNSUPPORTED", infraerrors.Reason(err))
	require.Zero(t, provider.refundCalls)
}

func TestPostMergePendingRecoveryPlanCannotBecomeFreshRefundAfterConcurrentFailure(t *testing.T) {
	ctx := context.Background()
	client := newPaymentConfigServiceTestClient(t)
	order := createPendingRefundOrderForTest(t, ctx, client, "postmerge-pending-concurrent-failure")
	_, err := client.User.UpdateOneID(order.UserID).SetBalance(100).Save(ctx)
	require.NoError(t, err)
	provider := &postMergePendingRefundProvider{queryStatus: payment.ProviderStatusFailed}
	restore := replacePaymentProviderFactoryForTest(t, provider)
	defer restore()
	svc := &PaymentService{entClient: client, loadBalancer: &captureLoadBalancer{},
		userRepo: &postMergeRefundUserRepository{client: client}}
	plan, early, err := svc.PrepareRefund(ctx, order.ID, 100, "retry pending", false, true)
	require.NoError(t, err)
	require.Nil(t, early)
	require.Equal(t, OrderStatusRefundPending, plan.Order.Status)
	require.Zero(t, plan.BalanceToDeduct)

	// Another request observes that the gateway refund has failed while the
	// first request still owns the recovery-only plan returned above.
	result, err := svc.QueryAndFinalizeRefund(ctx, order.ID)
	require.NoError(t, err)
	require.False(t, result.Success)
	persisted, err := client.PaymentOrder.Get(ctx, order.ID)
	require.NoError(t, err)
	require.Equal(t, OrderStatusRefundFailed, persisted.Status)

	result, err = svc.ExecuteRefund(ctx, plan)
	require.Nil(t, result)
	require.Equal(t, "INVALID_STATUS", infraerrors.Reason(err))
	require.Empty(t, provider.refundAmounts, "a pending recovery plan cannot initiate a fresh refund after its order changes state")
	persisted, err = client.PaymentOrder.Get(ctx, order.ID)
	require.NoError(t, err)
	require.Equal(t, OrderStatusRefundFailed, persisted.Status)
	user, err := client.User.Get(ctx, order.UserID)
	require.NoError(t, err)
	require.Equal(t, 100.0, user.Balance)
}

func TestPostMergePendingRecoveryPlanIsIdempotentAfterConcurrentCompletion(t *testing.T) {
	for _, status := range []string{OrderStatusRefunded, OrderStatusPartiallyRefunded} {
		t.Run(status, func(t *testing.T) {
			ctx := context.Background()
			client := newPaymentConfigServiceTestClient(t)
			order := createPendingRefundOrderForTest(t, ctx, client, "postmerge-pending-concurrent-completion")
			provider := &postMergePendingRefundProvider{}
			restore := replacePaymentProviderFactoryForTest(t, provider)
			defer restore()
			svc := &PaymentService{entClient: client, loadBalancer: &captureLoadBalancer{}}
			plan, early, err := svc.PrepareRefund(ctx, order.ID, 100, "retry pending", false, false)
			require.NoError(t, err)
			require.Nil(t, early)
			_, err = client.PaymentOrder.UpdateOneID(order.ID).SetStatus(status).Save(ctx)
			require.NoError(t, err)
			result, err := svc.ExecuteRefund(ctx, plan)
			require.NoError(t, err)
			require.True(t, result.Success)
			require.Empty(t, provider.refundAmounts)
			require.Zero(t, provider.queryCalls)
		})
	}
}

type postMergeRefundWithoutQueryProvider struct {
	refundProviderTestDouble
	refundCalls int
}

func (p *postMergeRefundWithoutQueryProvider) Refund(context.Context, payment.RefundRequest) (*payment.RefundResponse, error) {
	p.refundCalls++
	return &payment.RefundResponse{Status: payment.ProviderStatusSuccess}, nil
}
