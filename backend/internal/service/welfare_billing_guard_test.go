package service

import (
	"context"
	"github.com/stretchr/testify/require"
	"testing"
)

type welfareAtomicUserRepo struct{ UserRepository }

func (*welfareAtomicUserRepo) RequiresAtomicUsageBilling() bool { return true }
func TestWelfareLegacyCreateCannotBypassAtomicBilling(t *testing.T) {
	svc := &UsageService{userRepo: &welfareAtomicUserRepo{}}
	_, err := svc.Create(context.Background(), CreateUsageLogRequest{UserID: 7, ActualCost: 1})
	require.ErrorIs(t, err, ErrAtomicUsageBillingRequired)
}
func TestWelfareGatewayCannotFallbackWithoutAtomicRepository(t *testing.T) {
	_, err := applyUsageBilling(context.Background(), "request", nil, &postUsageBillingParams{}, &billingDeps{userRepo: &welfareAtomicUserRepo{}}, nil)
	require.ErrorIs(t, err, ErrAtomicUsageBillingRequired)
}
