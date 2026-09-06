//go:build unit

package service

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
)

type billingAutoResetAdmissionStub struct {
	resets   int
	resetErr error
	onReset  func(context.Context, int64)
}

func (s *billingAutoResetAdmissionStub) GetSubscriptionForAdmission(context.Context, int64, int64) (*UserSubscription, *Group, error) {
	return nil, nil, errors.New("unexpected admission")
}

func (s *billingAutoResetAdmissionStub) TriggerAutoDailyReset(ctx context.Context, id int64) error {
	s.resets++
	if s.onReset != nil {
		s.onReset(ctx, id)
	}
	return s.resetErr
}

type autoResetUsageBillingRepo struct {
	UsageBillingRepository
	apply func(context.Context, *UsageBillingCommand) (*UsageBillingApplyResult, error)
}

func (r *autoResetUsageBillingRepo) Apply(ctx context.Context, cmd *UsageBillingCommand) (*UsageBillingApplyResult, error) {
	return r.apply(ctx, cmd)
}

func TestUsageBillingAutoResetRunsOnlyAfterCommittedConsumption(t *testing.T) {
	for _, test := range []struct {
		name         string
		applied      bool
		billingError bool
		resetError   bool
		wantResets   int
	}{
		{name: "committed consumption", applied: true, wantResets: 1},
		{name: "duplicate settlement", wantResets: 0},
		{name: "failed settlement", billingError: true, wantResets: 0},
		{name: "reset failure preserves committed settlement", applied: true, resetError: true, wantResets: 1},
	} {
		t.Run(test.name, func(t *testing.T) {
			committed := false
			reset := &billingAutoResetAdmissionStub{onReset: func(ctx context.Context, id int64) {
				require.True(t, committed)
				require.NoError(t, ctx.Err())
				require.Equal(t, int64(4), id)
			}}
			if test.resetError {
				reset.resetErr = errors.New("temporary reset failure")
			}
			billing := &BillingCacheService{subscriptionService: reset}
			groupID := int64(2)
			params := &postUsageBillingParams{Cost: &CostBreakdown{TotalCost: 2, ActualCost: 2}, User: &User{ID: 1}, APIKey: &APIKey{ID: 3, GroupID: &groupID}, Account: &Account{ID: 5}, Subscription: &UserSubscription{ID: 4, UserID: 1, GroupID: 2}, IsSubscriptionBill: true}
			repo := &autoResetUsageBillingRepo{apply: func(context.Context, *UsageBillingCommand) (*UsageBillingApplyResult, error) {
				if test.billingError {
					return nil, errors.New("transaction failed")
				}
				committed = test.applied
				return &UsageBillingApplyResult{Applied: test.applied}, nil
			}}
			applied, err := applyUsageBilling(context.Background(), "settlement-1", nil, params, &billingDeps{billingCacheService: billing, deferredService: &DeferredService{}}, repo)
			if test.billingError {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
			require.Equal(t, test.applied, applied)
			require.Equal(t, test.wantResets, reset.resets)
		})
	}
}
