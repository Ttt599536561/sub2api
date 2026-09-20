//go:build unit

package service

import (
	"context"
	"math"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/payment"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/stretchr/testify/require"
)

func TestPrepareRefundRejectsAmountsBeyondStoredPrecision(t *testing.T) {
	for _, orderType := range []string{payment.OrderTypeSubscription, payment.OrderTypeBalance} {
		t.Run(orderType, func(t *testing.T) {
			ctx := context.Background()
			client := newPaymentConfigServiceTestClient(t)
			order := createPendingRefundOrderForTest(t, ctx, client, "refund-precision")
			order, err := client.PaymentOrder.UpdateOneID(order.ID).
				SetStatus(OrderStatusCompleted).SetOrderType(orderType).
				SetAmount(20).SetPayAmount(200).SetRefundAmount(0).Save(ctx)
			require.NoError(t, err)
			svc := &PaymentService{entClient: client}

			for _, tc := range []struct {
				name   string
				amount float64
			}{
				{name: "draw boundary mismatch", amount: 5.001},
				{name: "would persist as zero", amount: 0.001},
				{name: "half cent", amount: 5.005},
				{name: "full refund tolerance", amount: 20.001},
				{name: "NaN", amount: math.NaN()},
				{name: "positive infinity", amount: math.Inf(1)},
				{name: "negative infinity", amount: math.Inf(-1)},
			} {
				t.Run(tc.name, func(t *testing.T) {
					plan, result, err := svc.PrepareRefund(ctx, order.ID, tc.amount, "precision regression", false, false)
					require.Error(t, err)
					require.Equal(t, "INVALID_AMOUNT", infraerrors.Reason(err))
					require.Nil(t, plan, "invalid amounts must not reach refund execution")
					require.Nil(t, result)
					persisted, err := client.PaymentOrder.Get(ctx, order.ID)
					require.NoError(t, err)
					require.Equal(t, OrderStatusCompleted, persisted.Status)
					require.Zero(t, persisted.RefundAmount)
				})
			}
		})
	}
}

func TestPrepareRefundPreservesStoredPrecisionAndGatewayCurrency(t *testing.T) {
	for _, tc := range []struct {
		name, currency                   string
		paid, requested, refund, gateway float64
	}{
		{name: "whole amount", currency: "CNY", paid: 200, requested: 5, refund: 5, gateway: 50},
		{name: "cent boundary", currency: "CNY", paid: 200, requested: 5.01, refund: 5.01, gateway: 50.10},
		{name: "minimum cent", currency: "CNY", paid: 200, requested: 0.01, refund: 0.01, gateway: 0.10},
		{name: "binary float fraction", currency: "CNY", paid: 200, requested: 0.29, refund: 0.29, gateway: 2.90},
		{name: "default full refund", currency: "CNY", paid: 200, requested: 0, refund: 20, gateway: 200},
		{name: "legacy negative defaults to full", currency: "CNY", paid: 200, requested: -1, refund: 20, gateway: 200},
		{name: "three decimal gateway", currency: "KWD", paid: 12.34, requested: 5.01, refund: 5.01, gateway: 3.091},
		{name: "whole unit gateway", currency: "JPY", paid: 200, requested: 5.01, refund: 5.01, gateway: 50},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			client := newPaymentConfigServiceTestClient(t)
			order := createPendingRefundOrderForTest(t, ctx, client, "refund-currency-precision")
			order, err := client.PaymentOrder.UpdateOneID(order.ID).
				SetStatus(OrderStatusCompleted).SetOrderType(payment.OrderTypeSubscription).
				SetAmount(20).SetPayAmount(tc.paid).SetRefundAmount(0).
				SetProviderSnapshot(map[string]any{"currency": tc.currency}).Save(ctx)
			require.NoError(t, err)
			svc := &PaymentService{entClient: client}

			plan, result, err := svc.PrepareRefund(ctx, order.ID, tc.requested, "valid precision", false, false)
			require.NoError(t, err)
			require.Nil(t, result)
			require.NotNil(t, plan)
			require.Equal(t, tc.refund, plan.RefundAmount)
			require.Equal(t, tc.gateway, plan.GatewayAmount)
		})
	}
}
