//go:build unit

package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/ent/paymentauditlog"
	"github.com/Wei-Shaw/sub2api/internal/payment"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/stretchr/testify/require"
)

type refundWelfareRepositoryStub struct {
	reverse func(context.Context, int64) error
}

func (*refundWelfareRepositoryStub) AccrueSubscriptionPurchase(context.Context, int64) error {
	return errors.New("unexpected purchase accrual during refund")
}

func (r *refundWelfareRepositoryStub) ReverseSubscriptionPurchase(ctx context.Context, orderID int64) error {
	return r.reverse(ctx, orderID)
}

func refundWelfareFixture(t *testing.T, status, orderType string) (*dbent.Client, *dbent.PaymentOrder) {
	t.Helper()
	ctx := context.Background()
	client := newPaymentConfigServiceTestClient(t)
	order := createPendingRefundOrderForTest(t, ctx, client, "welfare-refund")
	order, err := client.PaymentOrder.UpdateOneID(order.ID).
		SetStatus(status).SetOrderType(orderType).Save(ctx)
	require.NoError(t, err)
	_, err = client.ExecContext(ctx, "CREATE TABLE refund_welfare_events (order_id INTEGER PRIMARY KEY)")
	require.NoError(t, err)
	return client, order
}

func recordRefundWelfareEvent(t *testing.T, ctx context.Context, orderID int64) {
	t.Helper()
	tx := dbent.TxFromContext(ctx)
	require.NotNil(t, tx, "welfare reversal must use the payment transaction")
	order, err := tx.PaymentOrder.Get(ctx, orderID)
	require.NoError(t, err)
	require.Contains(t, []string{OrderStatusRefunded, OrderStatusPartiallyRefunded}, order.Status)
	_, err = tx.Client().ExecContext(ctx, "INSERT INTO refund_welfare_events(order_id) VALUES (?)", orderID)
	require.NoError(t, err)
}

func assertRefundWelfareEventCount(t *testing.T, client *dbent.Client, count int) {
	t.Helper()
	rows, err := client.QueryContext(context.Background(), "SELECT COUNT(*) FROM refund_welfare_events")
	require.NoError(t, err)
	defer rows.Close()
	require.True(t, rows.Next())
	var actual int
	require.NoError(t, rows.Scan(&actual))
	require.Equal(t, count, actual)
}

func TestWelfareRefundFinalizationIsAtomicAndIdempotent(t *testing.T) {
	for _, pending := range []bool{false, true} {
		for _, amount := range []float64{25, 100} {
			t.Run(fmt.Sprintf("pending=%t/amount=%.0f", pending, amount), func(t *testing.T) {
				ctx := context.Background()
				status := OrderStatusRefunding
				if pending {
					status = OrderStatusRefundPending
				}
				client, order := refundWelfareFixture(t, status, payment.OrderTypeSubscription)
				calls := 0
				svc := &PaymentService{entClient: client, welfarePaymentRepo: &refundWelfareRepositoryStub{
					reverse: func(ctx context.Context, id int64) error {
						calls++
						recordRefundWelfareEvent(t, ctx, id)
						persisted, err := dbent.TxFromContext(ctx).PaymentOrder.Get(ctx, id)
						require.NoError(t, err)
						require.Equal(t, amount, persisted.RefundAmount)
						return nil
					},
				}}
				plan := &RefundPlan{OrderID: order.ID, Order: order, RefundAmount: amount, Reason: "subscription refund"}
				var result *RefundResult
				var err error
				if pending {
					result, err = svc.finalizePendingRefundSuccess(ctx, plan)
				} else {
					result, err = svc.markRefundOk(ctx, plan)
				}
				require.NoError(t, err)
				require.True(t, result.Success)
				require.Equal(t, 1, calls)
				assertRefundWelfareEventCount(t, client, 1)

				// A stale worker cannot apply welfare or rewrite a terminal refund.
				replay := *plan
				replay.RefundAmount = 99
				result, err = svc.markRefundOk(ctx, &replay)
				require.NoError(t, err)
				require.True(t, result.Success)
				require.Equal(t, 1, calls)
				persisted, err := client.PaymentOrder.Get(ctx, order.ID)
				require.NoError(t, err)
				require.Equal(t, amount, persisted.RefundAmount)
				wantStatus := OrderStatusRefunded
				if amount < order.Amount {
					wantStatus = OrderStatusPartiallyRefunded
				}
				require.Equal(t, wantStatus, persisted.Status)
				count, err := client.PaymentAuditLog.Query().Where(
					paymentauditlog.OrderIDEQ(strconv.FormatInt(order.ID, 10)),
					paymentauditlog.ActionEQ("REFUND_SUCCESS"),
				).Count(ctx)
				require.NoError(t, err)
				require.Equal(t, 1, count)
			})
		}
	}
}

func TestWelfareRefundSkipsBalanceOrders(t *testing.T) {
	for _, pending := range []bool{false, true} {
		t.Run(strconv.FormatBool(pending), func(t *testing.T) {
			ctx := context.Background()
			status := OrderStatusRefunding
			if pending {
				status = OrderStatusRefundPending
			}
			client, order := refundWelfareFixture(t, status, payment.OrderTypeBalance)
			svc := &PaymentService{entClient: client, welfarePaymentRepo: &refundWelfareRepositoryStub{
				reverse: func(context.Context, int64) error {
					return errors.New("balance orders must not reverse subscription rewards")
				},
			}}
			plan := &RefundPlan{OrderID: order.ID, Order: order, RefundAmount: 100, Reason: "balance refund"}
			var result *RefundResult
			var err error
			if pending {
				result, err = svc.finalizePendingRefundSuccess(ctx, plan)
			} else {
				result, err = svc.markRefundOk(ctx, plan)
			}
			require.NoError(t, err)
			require.True(t, result.Success)
			assertRefundWelfareEventCount(t, client, 0)
		})
	}
}

func TestWelfareRefundFailureRollsBackAndRecoversWithoutProviderOrDeduction(t *testing.T) {
	ctx := context.Background()
	client, order := refundWelfareFixture(t, OrderStatusRefunding, payment.OrderTypeSubscription)
	// Recovery must not require the provider to exist or support refund queries.
	order, err := client.PaymentOrder.UpdateOneID(order.ID).ClearProviderInstanceID().Save(ctx)
	require.NoError(t, err)
	injected := errors.New("injected welfare reversal failure")
	fail := true
	calls := 0
	svc := &PaymentService{entClient: client, welfarePaymentRepo: &refundWelfareRepositoryStub{
		reverse: func(ctx context.Context, id int64) error {
			calls++
			recordRefundWelfareEvent(t, ctx, id)
			if fail {
				return injected
			}
			return nil
		},
	}}
	plan := &RefundPlan{OrderID: order.ID, Order: order, RefundAmount: 25, Reason: "confirmed subscription refund",
		Force: true, DeductionType: payment.DeductionTypeSubscription, SubDaysToDeduct: 7, SubscriptionID: 42}
	result, err := svc.markRefundOk(ctx, plan)
	require.ErrorIs(t, err, injected)
	require.Nil(t, result)
	assertRefundWelfareEventCount(t, client, 0)
	persisted, err := client.PaymentOrder.Get(ctx, order.ID)
	require.NoError(t, err)
	require.Equal(t, OrderStatusRefunding, persisted.Status)
	require.Nil(t, persisted.RefundAt)

	result, err = svc.finishRefund(ctx, plan, &payment.RefundResponse{Status: payment.ProviderStatusSuccess, RefundID: "confirmed-1"})
	require.ErrorIs(t, err, injected)
	require.Nil(t, result)
	persisted, err = client.PaymentOrder.Get(ctx, order.ID)
	require.NoError(t, err)
	require.Equal(t, OrderStatusRefundPending, persisted.Status)
	require.Equal(t, 25.0, persisted.RefundAmount)
	require.Equal(t, plan.Reason, *persisted.RefundReason)
	require.True(t, persisted.ForceRefund)
	require.Nil(t, persisted.FailedAt)
	assertRefundWelfareEventCount(t, client, 0)
	audit, err := client.PaymentAuditLog.Query().Where(paymentauditlog.ActionEQ("REFUND_PENDING")).
		Order(dbent.Desc(paymentauditlog.FieldID)).First(ctx)
	require.NoError(t, err)
	var detail map[string]any
	require.NoError(t, json.Unmarshal([]byte(audit.Detail), &detail))
	require.Equal(t, true, detail["gatewayConfirmed"])
	require.Equal(t, false, detail["deductionRollbackOK"])
	require.Equal(t, payment.DeductionTypeSubscription, detail["deductionType"])
	require.Equal(t, 7.0, detail["subDaysDeducted"])

	// Both subscriptionSvc and userRepo are nil: neither entitlement deduction
	// nor rollback is permitted after the provider has already confirmed success.
	fail = false
	result, err = svc.QueryAndFinalizeRefund(ctx, order.ID)
	require.NoError(t, err)
	require.True(t, result.Success)
	assertRefundWelfareEventCount(t, client, 1)
	require.Equal(t, 3, calls)
	result, err = svc.QueryAndFinalizeRefund(ctx, order.ID)
	require.NoError(t, err)
	require.True(t, result.Success)
	require.Equal(t, 3, calls)
	count, err := client.PaymentAuditLog.Query().Where(paymentauditlog.ActionEQ("REFUND_GATEWAY_FAILED")).Count(ctx)
	require.NoError(t, err)
	require.Zero(t, count)
}

func TestWelfarePendingRefundFailureRollsBackFinalization(t *testing.T) {
	ctx := context.Background()
	client, order := refundWelfareFixture(t, OrderStatusRefundPending, payment.OrderTypeSubscription)
	injected := errors.New("pending welfare reversal failure")
	svc := &PaymentService{entClient: client, welfarePaymentRepo: &refundWelfareRepositoryStub{
		reverse: func(ctx context.Context, id int64) error {
			recordRefundWelfareEvent(t, ctx, id)
			return injected
		},
	}}
	result, err := svc.finalizePendingRefundSuccess(ctx, &RefundPlan{OrderID: order.ID, Order: order, RefundAmount: 100, Reason: "pending refund"})
	require.ErrorIs(t, err, injected)
	require.Nil(t, result)
	persisted, err := client.PaymentOrder.Get(ctx, order.ID)
	require.NoError(t, err)
	require.Equal(t, OrderStatusRefundPending, persisted.Status)
	require.Nil(t, persisted.RefundAt)
	assertRefundWelfareEventCount(t, client, 0)
	count, err := client.PaymentAuditLog.Query().Where(paymentauditlog.ActionEQ("REFUND_SUCCESS")).Count(ctx)
	require.NoError(t, err)
	require.Zero(t, count)
}

func TestWelfareRefundRecoveryMetadataIsAtomic(t *testing.T) {
	ctx := context.Background()
	client, order := refundWelfareFixture(t, OrderStatusRefunding, payment.OrderTypeSubscription)
	_, err := client.ExecContext(ctx, `CREATE TRIGGER reject_refund_recovery BEFORE INSERT ON payment_audit_logs
 WHEN NEW.action='REFUND_PENDING' BEGIN SELECT RAISE(ABORT, 'recovery audit unavailable'); END`)
	require.NoError(t, err)
	injected := errors.New("welfare unavailable")
	svc := &PaymentService{entClient: client, welfarePaymentRepo: &refundWelfareRepositoryStub{
		reverse: func(context.Context, int64) error { return injected },
	}}
	plan := &RefundPlan{OrderID: order.ID, Order: order, RefundAmount: 25, Reason: "confirmed refund"}
	result, err := svc.finishRefund(ctx, plan, &payment.RefundResponse{Status: payment.ProviderStatusSuccess})
	require.ErrorIs(t, err, injected)
	require.ErrorContains(t, err, "recovery audit unavailable")
	require.Nil(t, result)
	persisted, err := client.PaymentOrder.Get(ctx, order.ID)
	require.NoError(t, err)
	require.Equal(t, OrderStatusRefunding, persisted.Status, "must not expose pending recovery without its deduction metadata")
	require.Equal(t, order.RefundAmount, persisted.RefundAmount)
	assertRefundWelfareEventCount(t, client, 0)
}

func TestWelfareRefundSuccessAuditFailureRollsBackReversal(t *testing.T) {
	ctx := context.Background()
	client, order := refundWelfareFixture(t, OrderStatusRefunding, payment.OrderTypeSubscription)
	_, err := client.ExecContext(ctx, `CREATE TRIGGER reject_refund_success BEFORE INSERT ON payment_audit_logs
 WHEN NEW.action='REFUND_SUCCESS' BEGIN SELECT RAISE(ABORT, 'success audit unavailable'); END`)
	require.NoError(t, err)
	svc := &PaymentService{entClient: client, welfarePaymentRepo: &refundWelfareRepositoryStub{
		reverse: func(ctx context.Context, id int64) error {
			recordRefundWelfareEvent(t, ctx, id)
			return nil
		},
	}}
	result, err := svc.markRefundOk(ctx, &RefundPlan{OrderID: order.ID, Order: order, RefundAmount: 100, Reason: "audit rollback"})
	require.ErrorContains(t, err, "success audit unavailable")
	require.Nil(t, result)
	persisted, err := client.PaymentOrder.Get(ctx, order.ID)
	require.NoError(t, err)
	require.Equal(t, OrderStatusRefunding, persisted.Status)
	assertRefundWelfareEventCount(t, client, 0)
}

func TestWelfareRefundOrdinaryRetryUsesConfirmedRecovery(t *testing.T) {
	ctx := context.Background()
	client, order := refundWelfareFixture(t, OrderStatusRefunding, payment.OrderTypeSubscription)
	fail := true
	svc := &PaymentService{entClient: client, welfarePaymentRepo: &refundWelfareRepositoryStub{
		reverse: func(ctx context.Context, id int64) error {
			if fail {
				return errors.New("temporary welfare failure")
			}
			recordRefundWelfareEvent(t, ctx, id)
			return nil
		},
	}}
	plan := &RefundPlan{OrderID: order.ID, Order: order, RefundAmount: 25, Reason: "retry local writes",
		DeductionType: payment.DeductionTypeSubscription, SubDaysToDeduct: 7, SubscriptionID: 42}
	_, err := svc.finishRefund(ctx, plan, &payment.RefundResponse{Status: payment.ProviderStatusSuccess})
	require.Error(t, err)
	fail = false
	// A stale retry plan cannot request a second gateway refund or deduction.
	plan.RefundAmount = 100
	result, err := svc.ExecuteRefund(ctx, plan)
	require.NoError(t, err)
	require.True(t, result.Success)
	persisted, err := client.PaymentOrder.Get(ctx, order.ID)
	require.NoError(t, err)
	require.Equal(t, 25.0, persisted.RefundAmount)
	require.Equal(t, OrderStatusPartiallyRefunded, persisted.Status)
	assertRefundWelfareEventCount(t, client, 1)
}

func TestWelfareRefundConcurrentFinalizersApplyOnce(t *testing.T) {
	ctx := context.Background()
	client, order := refundWelfareFixture(t, OrderStatusRefunding, payment.OrderTypeSubscription)
	var calls atomic.Int32
	svc := &PaymentService{entClient: client, welfarePaymentRepo: &refundWelfareRepositoryStub{
		reverse: func(ctx context.Context, id int64) error {
			calls.Add(1)
			recordRefundWelfareEvent(t, ctx, id)
			return nil
		},
	}}
	plan := &RefundPlan{OrderID: order.ID, Order: order, RefundAmount: 100, Reason: "concurrent completion"}
	const workers = 6
	start := make(chan struct{})
	results := make(chan error, workers)
	var done sync.WaitGroup
	for range workers {
		done.Go(func() {
			<-start
			_, err := svc.markRefundOk(ctx, plan)
			results <- err
		})
	}
	close(start)
	done.Wait()
	close(results)
	for err := range results {
		if err != nil {
			// SQLite uses database-wide locks; a caller rejected by that lock
			// can retry, while PostgreSQL waits on the order's row-level CAS.
			require.True(t, strings.Contains(err.Error(), "locked") || strings.Contains(err.Error(), "SQLITE_BUSY"), err.Error())
		}
	}
	result, err := svc.markRefundOk(ctx, plan)
	require.NoError(t, err)
	require.True(t, result.Success)
	require.EqualValues(t, 1, calls.Load())
	assertRefundWelfareEventCount(t, client, 1)
	count, err := client.PaymentAuditLog.Query().Where(paymentauditlog.ActionEQ("REFUND_SUCCESS")).Count(ctx)
	require.NoError(t, err)
	require.Equal(t, 1, count)
}

func TestWelfareRefundPreservesConfirmedOutcomeAfterRequestCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	client, order := refundWelfareFixture(t, OrderStatusRefunding, payment.OrderTypeSubscription)
	svc := &PaymentService{entClient: client, welfarePaymentRepo: &refundWelfareRepositoryStub{
		reverse: func(context.Context, int64) error {
			cancel()
			return context.Canceled
		},
	}}
	_, err := svc.finishRefund(ctx, &RefundPlan{OrderID: order.ID, Order: order, RefundAmount: 100, Reason: "request cancelled"},
		&payment.RefundResponse{Status: payment.ProviderStatusSuccess})
	require.ErrorIs(t, err, context.Canceled)
	persisted, err := client.PaymentOrder.Get(context.Background(), order.ID)
	require.NoError(t, err)
	require.Equal(t, OrderStatusRefundPending, persisted.Status)
	detail, err := svc.latestRefundPendingDetail(context.Background(), order.ID)
	require.NoError(t, err)
	require.True(t, detail.GatewayConfirmed)
	require.False(t, detail.DeductionRollbackOK)
}

type refundWelfareCountingProvider struct {
	refundProviderTestDouble
	refundCalls int
	queryCalls  int
}

func (p *refundWelfareCountingProvider) Refund(context.Context, payment.RefundRequest) (*payment.RefundResponse, error) {
	p.refundCalls++
	return &payment.RefundResponse{Status: payment.ProviderStatusSuccess}, nil
}

func (p *refundWelfareCountingProvider) QueryRefund(context.Context, payment.RefundQueryRequest) (*payment.RefundResponse, error) {
	p.queryCalls++
	return &payment.RefundResponse{Status: payment.ProviderStatusSuccess}, nil
}

func TestWelfareRefundClaimRejectsNewConfirmedRecoveryAfterPrecheck(t *testing.T) {
	ctx := context.Background()
	client, order := refundWelfareFixture(t, OrderStatusCompleted, payment.OrderTypeSubscription)
	prov := &refundWelfareCountingProvider{}
	restore := replacePaymentProviderFactoryForTest(t, prov)
	defer restore()
	var raced bool
	client.PaymentOrder.Use(func(next dbent.Mutator) dbent.Mutator {
		return dbent.MutateFunc(func(ctx context.Context, mutation dbent.Mutation) (dbent.Value, error) {
			if mutation.Op().Is(dbent.OpUpdate) && !raced {
				raced = true
				_, err := client.PaymentOrder.UpdateOneID(order.ID).
					SetStatus(OrderStatusRefundPending).SetRefundAmount(25).
					SetRefundReason("already confirmed").SetUpdatedAt(order.UpdatedAt.Add(time.Second)).Save(ctx)
				require.NoError(t, err)
				_, err = client.PaymentAuditLog.Create().
					SetOrderID(strconv.FormatInt(order.ID, 10)).SetAction("REFUND_PENDING").
					SetOperator("system").SetDetail(`{"gatewayConfirmed":true,"deductionRollbackOK":false,"deductionType":"subscription"}`).Save(ctx)
				require.NoError(t, err)
			}
			return next.Mutate(ctx, mutation)
		})
	})
	svc := &PaymentService{entClient: client, loadBalancer: &captureLoadBalancer{}, welfarePaymentRepo: &refundWelfareRepositoryStub{
		reverse: func(ctx context.Context, id int64) error {
			recordRefundWelfareEvent(t, ctx, id)
			return nil
		},
	}}
	plan := &RefundPlan{OrderID: order.ID, Order: order, RefundAmount: 100, Reason: "stale refund plan"}
	result, err := svc.ExecuteRefund(ctx, plan)
	require.Error(t, err)
	require.Nil(t, result)
	require.Equal(t, "CONFLICT", infraerrors.Reason(err))
	require.Zero(t, prov.refundCalls)
	assertRefundWelfareEventCount(t, client, 0)
	result, err = svc.ExecuteRefund(ctx, plan)
	require.NoError(t, err)
	require.True(t, result.Success)
	require.Zero(t, prov.refundCalls)
	persisted, err := client.PaymentOrder.Get(ctx, order.ID)
	require.NoError(t, err)
	require.Equal(t, 25.0, persisted.RefundAmount)
	assertRefundWelfareEventCount(t, client, 1)
}

func TestWelfarePendingRefundRejectsStaleDeductionPlan(t *testing.T) {
	ctx := context.Background()
	client, order := refundWelfareFixture(t, OrderStatusRefundPending, payment.OrderTypeBalance)
	// Another worker completes the entitlement deduction, then persists a new
	// confirmed recovery while this worker still has the earlier pending plan.
	_, err := client.PaymentOrder.UpdateOneID(order.ID).
		SetUpdatedAt(order.UpdatedAt.Add(time.Second)).Save(ctx)
	require.NoError(t, err)
	_, err = client.PaymentAuditLog.Create().
		SetOrderID(strconv.FormatInt(order.ID, 10)).SetAction("REFUND_PENDING").
		SetOperator("system").SetDetail(`{"gatewayConfirmed":true,"deductionRollbackOK":false}`).Save(ctx)
	require.NoError(t, err)
	deductions := 0
	svc := &PaymentService{entClient: client, userRepo: &mockUserRepo{
		deductAvailableBalanceFn: func(context.Context, int64, float64) (float64, error) {
			deductions++
			return 100, nil
		},
	}}
	result, err := svc.finalizePendingRefundSuccess(ctx, svc.refundFinalizePlan(order))
	require.Error(t, err)
	require.Nil(t, result)
	require.Equal(t, "CONFLICT", infraerrors.Reason(err))
	require.Zero(t, deductions)
	result, err = svc.QueryAndFinalizeRefund(ctx, order.ID)
	require.NoError(t, err)
	require.True(t, result.Success)
	require.Zero(t, deductions)
}

func TestWelfareRefundStaleProviderFailureCannotOverwriteRecoveryOrCompletion(t *testing.T) {
	for _, status := range []string{OrderStatusRefundPending, OrderStatusRefunded, OrderStatusPartiallyRefunded} {
		t.Run(status, func(t *testing.T) {
			ctx := context.Background()
			client, oldOrder := refundWelfareFixture(t, OrderStatusRefundPending, payment.OrderTypeSubscription)
			_, err := client.PaymentOrder.UpdateOneID(oldOrder.ID).
				SetStatus(status).SetUpdatedAt(oldOrder.UpdatedAt.Add(time.Second)).Save(ctx)
			require.NoError(t, err)
			if status == OrderStatusRefundPending {
				_, err := client.PaymentAuditLog.Create().
					SetOrderID(strconv.FormatInt(oldOrder.ID, 10)).SetAction("REFUND_PENDING").
					SetOperator("system").SetDetail(`{"gatewayConfirmed":true,"deductionRollbackOK":false}`).Save(ctx)
				require.NoError(t, err)
			}
			svc := &PaymentService{entClient: client}
			result, err := svc.finalizeRefundFailed(ctx, oldOrder, errors.New("stale provider failure"))
			require.Error(t, err)
			require.Nil(t, result)
			require.Equal(t, "CONFLICT", infraerrors.Reason(err))
			current, err := client.PaymentOrder.Get(ctx, oldOrder.ID)
			require.NoError(t, err)
			require.Equal(t, status, current.Status)
			require.Nil(t, current.FailedAt)
			count, err := client.PaymentAuditLog.Query().Where(paymentauditlog.ActionEQ("REFUND_FAILED")).Count(ctx)
			require.NoError(t, err)
			require.Zero(t, count)
		})
	}
}

func TestWelfareRefundPrepareConfirmedRecoverySkipsProviderAndDeductionChecks(t *testing.T) {
	for _, unavailableProvider := range []string{"disabled", "missing"} {
		t.Run(unavailableProvider, func(t *testing.T) {
			ctx := context.Background()
			client, order := refundWelfareFixture(t, OrderStatusRefundPending, payment.OrderTypeSubscription)
			order, err := client.PaymentOrder.UpdateOneID(order.ID).
				SetRefundAmount(25).SetRefundReason("confirmed amount").SetSubscriptionGroupID(7).SetSubscriptionDays(7).Save(ctx)
			require.NoError(t, err)
			if unavailableProvider == "missing" {
				_, err = client.PaymentOrder.UpdateOneID(order.ID).ClearProviderInstanceID().Save(ctx)
			} else {
				providerID, parseErr := strconv.ParseInt(*order.ProviderInstanceID, 10, 64)
				require.NoError(t, parseErr)
				_, err = client.PaymentProviderInstance.UpdateOneID(providerID).SetRefundEnabled(false).Save(ctx)
			}
			require.NoError(t, err)
			_, err = client.PaymentAuditLog.Create().
				SetOrderID(strconv.FormatInt(order.ID, 10)).SetAction("REFUND_PENDING").
				SetOperator("system").SetDetail(`{"gatewayConfirmed":true,"deductionRollbackOK":false,"deductionType":"subscription"}`).Save(ctx)
			require.NoError(t, err)
			svc := &PaymentService{entClient: client, welfarePaymentRepo: &refundWelfareRepositoryStub{
				reverse: func(ctx context.Context, id int64) error {
					recordRefundWelfareEvent(t, ctx, id)
					return nil
				},
			}}
			plan, earlyResult, err := svc.PrepareRefund(ctx, order.ID, 100, "new retry amount", false, true)
			require.NoError(t, err)
			require.Nil(t, earlyResult)
			require.NotNil(t, plan)
			require.Equal(t, 25.0, plan.RefundAmount)
			require.Zero(t, plan.BalanceToDeduct)
			require.Zero(t, plan.SubDaysToDeduct)
			result, err := svc.ExecuteRefund(ctx, plan)
			require.NoError(t, err)
			require.True(t, result.Success)
			assertRefundWelfareEventCount(t, client, 1)
		})
	}
}

type refundWelfareSubscriptionRepository struct {
	userSubRepoNoop
	client *dbent.Client
	subID  int64
}

func (r *refundWelfareSubscriptionRepository) txClient(ctx context.Context) *dbent.Client {
	if tx := dbent.TxFromContext(ctx); tx != nil {
		return tx.Client()
	}
	return r.client
}

func (r *refundWelfareSubscriptionRepository) GetByID(ctx context.Context, id int64) (*UserSubscription, error) {
	sub, err := r.txClient(ctx).UserSubscription.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	return &UserSubscription{ID: sub.ID, UserID: sub.UserID, GroupID: sub.GroupID,
		StartsAt: sub.StartsAt, ExpiresAt: sub.ExpiresAt, Status: sub.Status}, nil
}

func (r *refundWelfareSubscriptionRepository) GetByIDForUpdate(ctx context.Context, id int64) (*UserSubscription, error) {
	return r.GetByID(ctx, id)
}

func (r *refundWelfareSubscriptionRepository) GetActiveByUserIDAndGroupID(ctx context.Context, _, _ int64) (*UserSubscription, error) {
	return r.GetByID(ctx, r.subID)
}

func (r *refundWelfareSubscriptionRepository) ExtendExpiry(ctx context.Context, id int64, expiry time.Time) error {
	_, err := r.txClient(ctx).UserSubscription.UpdateOneID(id).SetExpiresAt(expiry).Save(ctx)
	return err
}

func TestWelfareRefundQuerySuccessFailureRecoversWithExactlyOneEntitlementDeduction(t *testing.T) {
	ctx := context.Background()
	client, order := refundWelfareFixture(t, OrderStatusRefundPending, payment.OrderTypeSubscription)
	group, err := client.Group.Create().SetName("refund-subscription").Save(ctx)
	require.NoError(t, err)
	expiry := time.Now().UTC().AddDate(0, 0, 30)
	sub, err := client.UserSubscription.Create().SetUserID(order.UserID).SetGroupID(group.ID).
		SetStartsAt(time.Now().UTC()).SetExpiresAt(expiry).Save(ctx)
	require.NoError(t, err)
	_, err = client.PaymentOrder.UpdateOneID(order.ID).SetSubscriptionGroupID(group.ID).SetSubscriptionDays(7).Save(ctx)
	require.NoError(t, err)
	prov := &refundWelfareCountingProvider{}
	restore := replacePaymentProviderFactoryForTest(t, prov)
	defer restore()
	subRepo := &refundWelfareSubscriptionRepository{client: client, subID: sub.ID}
	fail := true
	injected := errors.New("query success welfare failure")
	svc := &PaymentService{entClient: client, loadBalancer: &captureLoadBalancer{},
		subscriptionSvc: &SubscriptionService{userSubRepo: subRepo},
		welfarePaymentRepo: &refundWelfareRepositoryStub{reverse: func(ctx context.Context, id int64) error {
			recordRefundWelfareEvent(t, ctx, id)
			if fail {
				return injected
			}
			return nil
		}},
	}
	_, err = svc.QueryAndFinalizeRefund(ctx, order.ID)
	require.ErrorIs(t, err, injected)
	currentSub, err := client.UserSubscription.Get(ctx, sub.ID)
	require.NoError(t, err)
	require.True(t, expiry.Equal(currentSub.ExpiresAt), "failed finalization must roll back its entitlement deduction")
	detail, err := svc.latestRefundPendingDetail(ctx, order.ID)
	require.NoError(t, err)
	require.True(t, detail.GatewayConfirmed)
	require.True(t, detail.DeductionRollbackOK, "a rolled-back deduction must still be applied during recovery")
	assertRefundWelfareEventCount(t, client, 0)
	fail = false
	plan, early, err := svc.PrepareRefund(ctx, order.ID, 100, "ordinary retry", false, true)
	require.NoError(t, err)
	require.Nil(t, early)
	result, err := svc.ExecuteRefund(ctx, plan)
	require.NoError(t, err)
	require.True(t, result.Success)
	currentSub, err = client.UserSubscription.Get(ctx, sub.ID)
	require.NoError(t, err)
	require.True(t, expiry.AddDate(0, 0, -7).Equal(currentSub.ExpiresAt))
	require.Zero(t, prov.refundCalls)
	require.Equal(t, 1, prov.queryCalls)
	assertRefundWelfareEventCount(t, client, 1)
	_, err = svc.QueryAndFinalizeRefund(ctx, order.ID)
	require.NoError(t, err)
	currentSub, err = client.UserSubscription.Get(ctx, sub.ID)
	require.NoError(t, err)
	require.True(t, expiry.AddDate(0, 0, -7).Equal(currentSub.ExpiresAt))
}
