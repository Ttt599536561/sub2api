package provider

import (
	"context"
	"fmt"
	"math"
	"net/http"
	"testing"
)

func TestEasyPayConfirmNotificationRejectsNonFiniteAmounts(t *testing.T) {
	for _, tc := range []struct {
		name, queried string
		notified      float64
	}{
		{"queried_nan", "NaN", 10},
		{"notified_nan", "10.00", math.NaN()},
		{"both_nan", "NaN", math.NaN()},
		{"both_positive_infinity", "+Inf", math.Inf(1)},
		{"both_negative_infinity", "-Inf", math.Inf(-1)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			body := fmt.Sprintf(`{"code":1,"trade_no":"2026101600001","money":%q,"status":1}`, tc.queried)
			provider, _ := newConfirmTestEasyPay(t, body, http.StatusOK)
			notification := confirmTestNotification()
			notification.Amount = tc.notified
			if err := provider.ConfirmNotification(context.Background(), notification); err == nil {
				t.Fatalf("non-finite amount confirmed: queried=%q notified=%v", tc.queried, tc.notified)
			}
		})
	}
}
