//go:build integration

package service_test

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/payment"
	"github.com/Wei-Shaw/sub2api/internal/repository"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func installPaidRenewalReceipts(t *testing.T, f *welfareRedemptionFixture) {
	t.Helper()
	var exists bool
	require.NoError(t, f.db.QueryRowContext(f.ctx,
		"SELECT to_regclass('paid_subscription_monthly_renewals') IS NOT NULL").Scan(&exists))
	if exists {
		return
	}
	migration, err := os.ReadFile("../../migrations/264_paid_subscription_monthly_renewals.sql")
	require.NoError(t, err)
	// Ent already created the paid-cap columns. Apply the receipt and trigger DDL.
	_, receiptDDL, found := strings.Cut(string(migration), "CREATE TABLE paid_subscription_monthly_renewals")
	require.True(t, found)
	_, err = f.db.ExecContext(f.ctx, "CREATE TABLE paid_subscription_monthly_renewals"+receiptDDL)
	require.NoError(t, err)
}

func TestPaidMonthlyRenewalGrantsQuotaWithoutResettingUsage(t *testing.T) {
	const monthlyLimit = 1120.0
	for _, used := range []float64{monthlyLimit, 201.5} {
		t.Run(fmt.Sprintf("used_%.2f", used), func(t *testing.T) {
			f := newWelfareRedemptionFixture(t)
			installPaidRenewalReceipts(t, f)
			_, err := f.client.Group.UpdateOneID(f.group.ID).SetMonthlyLimitUsd(monthlyLimit).Save(f.ctx)
			require.NoError(t, err)
			now := time.Now().UTC().Truncate(time.Microsecond)
			start := now.AddDate(0, 0, -15)
			expiry := start.AddDate(0, 0, 30)
			original, err := f.client.UserSubscription.Create().SetUserID(f.userID).SetGroupID(f.group.ID).
				SetStartsAt(start).SetExpiresAt(expiry).SetMonthlyWindowStart(start).
				SetDailyUsageUsd(3).SetWeeklyUsageUsd(8).SetMonthlyUsageUsd(used).Save(f.ctx)
			require.NoError(t, err)
			order, err := f.client.PaymentOrder.Create().SetUserID(f.userID).SetUserEmail("welfare@example.test").
				SetUserName("monthly-renewal-test").SetAmount(100).SetPayAmount(100).SetFeeRate(0).
				SetOrderType(payment.OrderTypeSubscription).SetStatus(service.OrderStatusPending).
				SetSubscriptionGroupID(f.group.ID).SetSubscriptionDays(30).SetPaymentType(payment.TypeAlipay).
				SetRechargeCode("paid-renewal-test").SetOutTradeNo("paid-renewal-test").
				SetPaymentTradeNo("").SetExpiresAt(now.Add(time.Hour)).SetClientIP("127.0.0.1").SetSrcHost("example.test").Save(f.ctx)
			require.NoError(t, err)
			groupRepo := repository.NewGroupRepository(f.client, f.db)
			svc := service.NewPaymentService(f.client, nil, nil, nil, f.subs, nil, nil, groupRepo, nil)
			notification := &payment.PaymentNotification{
				TradeNo: "paid-renewal-trade", OrderID: order.OutTradeNo,
				Amount: order.PayAmount, Status: payment.NotificationStatusSuccess,
			}

			require.NoError(t, svc.HandlePaymentNotification(f.ctx, notification, payment.TypeAlipay))
			require.NoError(t, svc.HandlePaymentNotification(f.ctx, notification, payment.TypeAlipay))
			var receiptCount int
			require.NoError(t, f.db.QueryRowContext(f.ctx, "SELECT COUNT(*) FROM paid_subscription_monthly_renewals WHERE payment_order_id=$1", order.ID).Scan(&receiptCount))
			require.Equal(t, 1, receiptCount)

			completed, err := f.client.PaymentOrder.Get(f.ctx, order.ID)
			require.NoError(t, err)
			require.Equal(t, service.OrderStatusCompleted, completed.Status)
			persisted, err := f.client.UserSubscription.Get(f.ctx, original.ID)
			require.NoError(t, err)
			require.Equal(t, original.StartsAt, persisted.StartsAt)
			require.Equal(t, original.MonthlyWindowStart, persisted.MonthlyWindowStart)
			require.Equal(t, original.ExpiresAt.AddDate(0, 0, 30), persisted.ExpiresAt)
			require.Equal(t, original.DailyUsageUsd, persisted.DailyUsageUsd)
			require.Equal(t, original.WeeklyUsageUsd, persisted.WeeklyUsageUsd)
			require.Equal(t, used, persisted.MonthlyUsageUsd)

			sub, group, err := f.subs.GetSubscriptionForAdmission(f.ctx, f.userID, f.group.ID)
			require.NoError(t, err)
			want := 2*monthlyLimit - used
			require.NoError(t, f.subs.CheckUsageLimits(f.ctx, sub, group, want))
			require.ErrorIs(t, f.subs.CheckUsageLimits(f.ctx, sub, group, want+0.01), service.ErrMonthlyLimitExceeded)

			// Simulate a confirmed provider refund being recovered after a
			// local finalization error. Recovery must never call the provider.
			_, err = f.client.PaymentOrder.UpdateOneID(order.ID).SetStatus(service.OrderStatusRefundPending).
				SetRefundAmount(order.Amount).Save(f.ctx)
			require.NoError(t, err)
			_, err = f.client.PaymentAuditLog.Create().SetOrderID(strconv.FormatInt(order.ID, 10)).
				SetAction("REFUND_PENDING").
				SetDetail(`{"gatewayConfirmed":true,"deductionHeld":true,"deductBalance":true,"deductionType":"subscription"}`).
				Save(f.ctx)
			require.NoError(t, err)
			refunded, err := svc.QueryAndFinalizeRefund(f.ctx, order.ID)
			require.NoError(t, err)
			require.True(t, refunded.Success)
			require.Equal(t, 30, refunded.SubDaysDeducted)
			_, err = svc.QueryAndFinalizeRefund(f.ctx, order.ID)
			require.NoError(t, err)
			persisted, err = f.client.UserSubscription.Get(f.ctx, original.ID)
			require.NoError(t, err)
			require.Equal(t, original.ExpiresAt, persisted.ExpiresAt)
			require.Equal(t, used, persisted.MonthlyUsageUsd)
			require.NotNil(t, persisted.PaidMonthlyLimitUsd)
			require.Equal(t, monthlyLimit, *persisted.PaidMonthlyLimitUsd)
		})
	}
}
