//go:build integration

package repository

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/ent/paymentauditlog"
	"github.com/Wei-Shaw/sub2api/ent/schema/mixins"
	"github.com/Wei-Shaw/sub2api/internal/payment"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

type v217RefundRoundTripper func(*http.Request) (*http.Response, error)

func (f v217RefundRoundTripper) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

type v217RefundLoadBalancer struct {
	payment.LoadBalancer
	config map[string]string
}

func (l *v217RefundLoadBalancer) GetInstanceConfig(context.Context, int64) (map[string]string, error) {
	cfg := make(map[string]string, len(l.config))
	for k, v := range l.config {
		cfg[k] = v
	}
	return cfg, nil
}

func TestV217PendingRefundFailure_PostgresRestoresRevocationAtomically(t *testing.T) {
	_, fixture, now := newDailyResetFixture(t)
	ctx := context.Background()
	client := testEntClient(t)
	original, err := client.UserSubscription.UpdateOneID(fixture.ID).
		SetExpiresAt(now.Add(3 * 24 * time.Hour)).SetAutoDailyResetEnabled(true).
		SetPreserveCalendarDailyReset(true).SetDailyResetVersion(12).Save(ctx)
	require.NoError(t, err)
	instance, err := client.PaymentProviderInstance.Create().SetProviderKey(payment.TypeAirwallex).
		SetName("pending-refund-" + uuid.NewString()).SetConfig("{}").SetSupportedTypes(payment.TypeAirwallex).
		SetEnabled(true).SetRefundEnabled(true).Save(ctx)
	require.NoError(t, err)
	order, err := client.PaymentOrder.Create().SetUserID(fixture.UserID).
		SetUserEmail("v217-refund-postgres@example.com").SetUserName("v217-refund-postgres").
		SetAmount(25).SetPayAmount(25).SetRechargeCode(uuid.NewString()).
		SetOutTradeNo(uuid.NewString()).SetPaymentType(payment.TypeAirwallex).SetPaymentTradeNo("v217-test-intent").
		SetProviderInstanceID(strconv.FormatInt(instance.ID, 10)).
		SetClientIP("127.0.0.1").SetSrcHost("localhost").
		SetOrderType(payment.OrderTypeSubscription).SetSubscriptionGroupID(fixture.GroupID).
		SetSubscriptionDays(7).SetStatus(service.OrderStatusCompleted).
		SetExpiresAt(now.Add(time.Hour)).Save(ctx)
	require.NoError(t, err)
	t.Cleanup(func() {
		_, _ = client.PaymentAuditLog.Delete().Where(paymentauditlog.OrderIDEQ(strconv.FormatInt(order.ID, 10))).Exec(ctx)
		_ = client.PaymentOrder.DeleteOneID(order.ID).Exec(ctx)
		_ = client.PaymentProviderInstance.DeleteOneID(instance.ID).Exec(ctx)
	})
	originalTransport := http.DefaultTransport
	refundCalls, queryCalls := 0, 0
	http.DefaultTransport = v217RefundRoundTripper(func(req *http.Request) (*http.Response, error) {
		if req.URL.Host != "api-demo.airwallex.com" {
			return originalTransport.RoundTrip(req)
		}
		var body string
		switch req.URL.Path {
		case "/api/v1/authentication/login":
			body = fmt.Sprintf(`{"token":"v217-only-test-token","expires_at":%q}`, time.Now().Add(time.Hour).UTC().Format(time.RFC3339))
		case "/api/v1/pa/refunds/create":
			refundCalls++
			body = `{"id":"v217-test-refund","status":"ACCEPTED"}`
		case "/api/v1/pa/refunds/v217-test-refund":
			queryCalls++
			body = `{"id":"v217-test-refund","status":"FAILED"}`
		default:
			return nil, fmt.Errorf("unexpected refund test URL: %s", req.URL)
		}
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body)), Request: req}, nil
	})
	t.Cleanup(func() { http.DefaultTransport = originalTransport })
	subscriptions := service.NewSubscriptionService(NewGroupRepository(client, integrationDB), NewUserSubscriptionRepository(client), nil, client, nil)
	t.Cleanup(subscriptions.Stop)
	lb := &v217RefundLoadBalancer{config: map[string]string{"clientId": uuid.NewString(), "apiKey": "test", "webhookSecret": "test", "apiBase": "https://api-demo.airwallex.com/api/v1"}}
	payments := service.NewPaymentService(client, payment.NewRegistry(), lb, nil, subscriptions, nil, nil, nil, nil)
	plan, early, err := payments.PrepareRefund(ctx, order.ID, 25, "v217 pending", false, true)
	require.NoError(t, err)
	require.Nil(t, early)
	result, err := payments.ExecuteRefund(ctx, plan)
	require.NoError(t, err)
	require.False(t, result.Success)
	held, err := client.UserSubscription.Get(mixins.SkipSoftDelete(ctx), fixture.ID)
	require.NoError(t, err)
	require.NotNil(t, held.DeletedAt)
	pending, err := client.PaymentOrder.Get(ctx, order.ID)
	require.NoError(t, err)
	require.Equal(t, service.OrderStatusRefundPending, pending.Status)
	_, err = integrationDB.ExecContext(ctx, fmt.Sprintf(`CREATE FUNCTION reject_v217_refund_failure() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.action='REFUND_FAILED' AND NEW.order_id='%d' THEN RAISE EXCEPTION 'v217 failure audit unavailable'; END IF; RETURN NEW; END $$`, order.ID))
	require.NoError(t, err)
	t.Cleanup(func() {
		_, _ = integrationDB.Exec(`DROP TRIGGER IF EXISTS reject_v217_refund_failure ON payment_audit_logs; DROP FUNCTION IF EXISTS reject_v217_refund_failure()`)
	})
	_, err = integrationDB.ExecContext(ctx, `CREATE TRIGGER reject_v217_refund_failure BEFORE INSERT ON payment_audit_logs FOR EACH ROW EXECUTE FUNCTION reject_v217_refund_failure()`)
	require.NoError(t, err)
	_, err = payments.QueryAndFinalizeRefund(ctx, order.ID)
	require.ErrorContains(t, err, "v217 failure audit unavailable")
	afterFailure, err := client.UserSubscription.Get(mixins.SkipSoftDelete(ctx), fixture.ID)
	require.NoError(t, err)
	require.NotNil(t, afterFailure.DeletedAt)
	require.Equal(t, held.DailyResetVersion, afterFailure.DailyResetVersion)
	pending, err = client.PaymentOrder.Get(ctx, order.ID)
	require.NoError(t, err)
	require.Equal(t, service.OrderStatusRefundPending, pending.Status)
	_, err = integrationDB.ExecContext(ctx, `DROP TRIGGER reject_v217_refund_failure ON payment_audit_logs`)
	require.NoError(t, err)
	result, err = payments.QueryAndFinalizeRefund(ctx, order.ID)
	require.NoError(t, err)
	require.NotContains(t, result.Warning, "rollback failed")
	restored, err := client.UserSubscription.Get(ctx, fixture.ID)
	require.NoError(t, err)
	require.Nil(t, restored.DeletedAt)
	require.True(t, original.ExpiresAt.Equal(restored.ExpiresAt))
	require.True(t, original.StartsAt.Equal(restored.StartsAt))
	require.True(t, original.DailyWindowStart.Equal(*restored.DailyWindowStart))
	require.Equal(t, original.DailyUsageUsd, restored.DailyUsageUsd)
	require.Equal(t, original.WeeklyUsageUsd, restored.WeeklyUsageUsd)
	require.Equal(t, original.MonthlyUsageUsd, restored.MonthlyUsageUsd)
	require.True(t, restored.AutoDailyResetEnabled)
	require.True(t, restored.PreserveCalendarDailyReset)
	require.Equal(t, original.DailyResetVersion+1, restored.DailyResetVersion)
	failed, err := client.PaymentOrder.Get(ctx, order.ID)
	require.NoError(t, err)
	require.Equal(t, service.OrderStatusRefundFailed, failed.Status)
	_, err = payments.QueryAndFinalizeRefund(ctx, order.ID)
	require.Error(t, err)
	require.Equal(t, 1, refundCalls)
	require.Equal(t, 2, queryCalls)
}
