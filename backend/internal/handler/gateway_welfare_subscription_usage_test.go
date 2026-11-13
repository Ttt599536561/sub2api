//go:build unit

package handler

import (
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestSubscriptionRemainingTreatsWelfareZeroAsExhausted(t *testing.T) {
	zero, paidLimit := 0.0, 100.0
	h := &GatewayHandler{}
	welfare := &service.UserSubscription{EntitlementKind: service.SubscriptionEntitlementWelfare,
		WelfareMonthlyLimitUSD: &zero}
	require.Zero(t, h.calculateSubscriptionRemaining(&service.Group{MonthlyLimitUSD: &paidLimit}, welfare))
	welfare.MonthlyUsageUSD = 7
	require.Zero(t, h.calculateSubscriptionRemaining(&service.Group{MonthlyLimitUSD: &paidLimit}, welfare))
	require.Equal(t, -1.0, h.calculateSubscriptionRemaining(&service.Group{MonthlyLimitUSD: &zero}, &service.UserSubscription{}))
}
