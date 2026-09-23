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

func TestWelfareAtomicGuardPreservesSimpleModeErrors(t *testing.T) {
	for _, missing := range []string{"repository", "request_id", "api_key"} {
		t.Run(missing, func(t *testing.T) {
			p := &postUsageBillingParams{
				Cost: &CostBreakdown{ActualCost: 1}, User: &User{ID: 7},
				APIKey: &APIKey{ID: 13, RateLimit5h: 10}, Account: &Account{ID: 9},
				SimpleModeKeyRateLimitOnly: true,
			}
			var repo UsageBillingRepository = &simpleModeUsageBillingRepoStub{}
			requestID := "simple-welfare-guard"
			switch missing {
			case "repository":
				repo = nil
			case "request_id":
				requestID = ""
			case "api_key":
				p.APIKey = nil
			}
			applied, err := applyUsageBilling(context.Background(), requestID, nil, p,
				&billingDeps{userRepo: &welfareAtomicUserRepo{}, deferredService: &DeferredService{}}, repo)
			require.False(t, applied)
			require.ErrorIs(t, err, ErrSimpleModeKeyRateLimitBillingUnavailable)
		})
	}
}
