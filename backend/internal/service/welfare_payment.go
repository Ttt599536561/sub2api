package service

import "context"

// WelfarePaymentRepository adjusts draw eligibility inside the caller's Ent
// transaction. Amounts and identities come from the persisted payment order.
type WelfarePaymentRepository interface {
	AccrueSubscriptionPurchase(ctx context.Context, orderID int64) error
	ReverseSubscriptionPurchase(ctx context.Context, orderID int64) error
}

// PaymentGatewayRefundAmount gives reward accounting the same currency rounding
// and full-refund tolerance used by the payment provider request.
func PaymentGatewayRefundAmount(orderAmount, payAmount, refundAmount float64, currency string) float64 {
	return calculateGatewayRefundAmount(orderAmount, payAmount, refundAmount, currency)
}
