//go:build unit

package service

import (
	"context"
	"fmt"
	"strconv"
	"testing"

	"github.com/Wei-Shaw/sub2api/ent/paymentauditlog"
	"github.com/Wei-Shaw/sub2api/internal/payment"
	"github.com/stretchr/testify/require"
)

type refundAmountQueryProvider struct {
	refundProviderTestDouble
	expectedAmount string
}

func (p *refundAmountQueryProvider) QueryRefund(_ context.Context, req payment.RefundQueryRequest) (*payment.RefundResponse, error) {
	// WeChat reconstructs its amount-specific out_refund_no when legacy
	// pending metadata does not contain a refund ID.
	if req.RefundID != "" || req.Amount != p.expectedAmount {
		return nil, fmt.Errorf("refund identity mismatch: id=%q amount=%q, want %q", req.RefundID, req.Amount, p.expectedAmount)
	}
	return &payment.RefundResponse{Status: payment.ProviderStatusSuccess}, nil
}

func TestPendingRefundQueryUsesOriginalGatewayAmount(t *testing.T) {
	for _, tc := range []struct {
		name     string
		paid     float64
		refund   float64
		expected string
	}{
		{name: "discounted full refund", paid: 80, refund: 100, expected: "80.00"},
		{name: "converted partial refund", paid: 730, refund: 25, expected: "182.50"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			client := newPaymentConfigServiceTestClient(t)
			order := createPendingRefundOrderForTest(t, ctx, client, "gateway-query-amount")
			order, err := client.PaymentOrder.UpdateOneID(order.ID).SetPayAmount(tc.paid).SetRefundAmount(tc.refund).Save(ctx)
			require.NoError(t, err)
			_, err = client.PaymentAuditLog.Update().Where(paymentauditlog.OrderIDEQ(strconv.FormatInt(order.ID, 10))).
				SetDetail(`{"deductionRollbackOK":true,"deductBalance":false}`).Save(ctx)
			require.NoError(t, err)
			restore := replacePaymentProviderFactoryForTest(t, &refundAmountQueryProvider{expectedAmount: tc.expected})
			defer restore()
			svc := &PaymentService{entClient: client, loadBalancer: &captureLoadBalancer{}}
			result, err := svc.QueryAndFinalizeRefund(ctx, order.ID)
			require.NoError(t, err)
			require.True(t, result.Success)
			persisted, err := client.PaymentOrder.Get(ctx, order.ID)
			require.NoError(t, err)
			require.Equal(t, tc.refund, persisted.RefundAmount, "order refund accounting stays in credit units")
		})
	}
}
