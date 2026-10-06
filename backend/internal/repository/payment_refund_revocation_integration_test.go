//go:build integration

package repository

import (
	"context"
	"strconv"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/ent/paymentauditlog"
	"github.com/Wei-Shaw/sub2api/ent/schema/mixins"
	"github.com/Wei-Shaw/sub2api/internal/payment"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestPaymentRefundRevocationRollback_PostgresPreservesDailyResetEntitlement(t *testing.T) {
	_, fixture, now := newDailyResetFixture(t)
	ctx := context.Background()
	client := testEntClient(t)
	original, err := client.UserSubscription.UpdateOneID(fixture.ID).
		SetExpiresAt(now.Add(3 * 24 * time.Hour)).SetAutoDailyResetEnabled(true).
		SetPreserveCalendarDailyReset(true).SetDailyResetVersion(12).Save(ctx)
	require.NoError(t, err)
	order, err := client.PaymentOrder.Create().SetUserID(fixture.UserID).
		SetUserEmail("refund-postgres@example.com").SetUserName("refund-postgres").
		SetAmount(25).SetPayAmount(25).SetRechargeCode(uuid.NewString()).
		SetOutTradeNo(uuid.NewString()).SetPaymentType("alipay").SetPaymentTradeNo("test-only-no-provider").
		SetClientIP("127.0.0.1").SetSrcHost("localhost").
		SetOrderType(payment.OrderTypeSubscription).SetSubscriptionGroupID(fixture.GroupID).
		SetSubscriptionDays(7).SetStatus(service.OrderStatusCompleted).
		SetExpiresAt(now.Add(time.Hour)).Save(ctx)
	require.NoError(t, err)
	t.Cleanup(func() {
		_, _ = client.PaymentAuditLog.Delete().Where(paymentauditlog.OrderIDEQ(strconv.FormatInt(order.ID, 10))).Exec(ctx)
		_ = client.PaymentOrder.DeleteOneID(order.ID).Exec(ctx)
	})
	subscriptions := service.NewSubscriptionService(NewGroupRepository(client, integrationDB),
		NewUserSubscriptionRepository(client), nil, client, nil)
	t.Cleanup(subscriptions.Stop)
	payments := service.NewPaymentService(client, payment.NewRegistry(), nil, nil, subscriptions, nil, nil, nil, nil)
	plan := &service.RefundPlan{OrderID: order.ID, Order: order, RefundAmount: 25,
		GatewayAmount: 25, Reason: "test rollback", DeductBalance: true,
		DeductionType: payment.DeductionTypeSubscription, SubscriptionID: fixture.ID, SubDaysToDeduct: 7}
	// The explicit trade number without a provider binding fails before any
	// network call, after the same entitlement deduction used by gateway errors.
	result, err := payments.ExecuteRefund(ctx, plan)
	require.NoError(t, err)
	require.False(t, result.Success)
	require.Contains(t, result.Warning, "refund provider instance is unavailable")
	require.Contains(t, result.Warning, "rolled back")
	persisted, err := client.UserSubscription.Get(mixins.SkipSoftDelete(ctx), fixture.ID)
	require.NoError(t, err)
	require.Nil(t, persisted.DeletedAt)
	require.True(t, original.ExpiresAt.Equal(persisted.ExpiresAt))
	require.True(t, original.DailyWindowStart.Equal(*persisted.DailyWindowStart))
	require.Equal(t, original.DailyUsageUsd, persisted.DailyUsageUsd)
	require.Equal(t, original.WeeklyUsageUsd, persisted.WeeklyUsageUsd)
	require.Equal(t, original.MonthlyUsageUsd, persisted.MonthlyUsageUsd)
	require.True(t, persisted.AutoDailyResetEnabled)
	require.True(t, persisted.PreserveCalendarDailyReset)
	require.Equal(t, original.DailyResetVersion+1, persisted.DailyResetVersion)
	// Reusing the in-memory rollback plan cannot add the purchased days again.
	require.True(t, payments.RollbackRefund(ctx, plan, nil))
	afterRepeat, err := client.UserSubscription.Get(ctx, fixture.ID)
	require.NoError(t, err)
	require.True(t, persisted.ExpiresAt.Equal(afterRepeat.ExpiresAt))
	require.Equal(t, persisted.DailyResetVersion, afterRepeat.DailyResetVersion)
	persistedOrder, err := client.PaymentOrder.Get(ctx, order.ID)
	require.NoError(t, err)
	require.Equal(t, service.OrderStatusCompleted, persistedOrder.Status)
}
