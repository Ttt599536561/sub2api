//go:build unit

package service

import (
	"context"
	"errors"
	"testing"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/ent/schema/mixins"
	"github.com/Wei-Shaw/sub2api/internal/payment"
	"github.com/stretchr/testify/require"
)

func TestRefundPendingRecoversTransientAuditFailureWithoutRepeatingGateway(t *testing.T) {
	for _, kind := range []string{"balance", "revoked_subscription"} {
		for _, terminal := range []string{payment.ProviderStatusSuccess, payment.ProviderStatusFailed} {
			t.Run(kind+"/"+terminal, func(t *testing.T) {
				ctx := context.Background()
				svc, order, original, provider := refundRevocationFixture(t)
				provider.refundStatus = payment.ProviderStatusPending
				provider.queryStatus = terminal
				if kind == "balance" {
					var err error
					order, err = svc.entClient.PaymentOrder.UpdateOneID(order.ID).SetOrderType(payment.OrderTypeBalance).Save(ctx)
					require.NoError(t, err)
					_, err = svc.entClient.User.UpdateOneID(order.UserID).SetBalance(100).Save(ctx)
					require.NoError(t, err)
					svc.userRepo = &postMergeRefundUserRepository{client: svc.entClient}
				}
				priorPendingAudits := countRefundAuditsForTest(t, ctx, svc.entClient, order.ID, "REFUND_PENDING")
				pendingWrites := 0
				svc.entClient.PaymentAuditLog.Use(func(next dbent.Mutator) dbent.Mutator {
					return dbent.MutateFunc(func(callCtx context.Context, mutation dbent.Mutation) (dbent.Value, error) {
						if audit, ok := mutation.(*dbent.PaymentAuditLogMutation); ok {
							if action, _ := audit.Action(); action == "REFUND_PENDING" {
								pendingWrites++
								if pendingWrites == 1 {
									return nil, errors.New("transient pending audit write failure")
								}
							}
						}
						return next.Mutate(callCtx, mutation)
					})
				})
				plan, early, err := svc.PrepareRefund(ctx, order.ID, 25, "pending persistence recovery", false, true)
				require.NoError(t, err)
				require.Nil(t, early)
				result, err := svc.ExecuteRefund(ctx, plan)
				require.NoError(t, err, "retry only the pending snapshot after the first transaction rolls back")
				require.False(t, result.Success)
				current, err := svc.entClient.PaymentOrder.Get(ctx, order.ID)
				require.NoError(t, err)
				require.Equal(t, OrderStatusRefundPending, current.Status)
				require.Equal(t, priorPendingAudits+1, countRefundAuditsForTest(t, ctx, svc.entClient, order.ID, "REFUND_PENDING"))
				require.Equal(t, 2, pendingWrites)
				if kind == "balance" {
					user, err := svc.entClient.User.Get(ctx, order.UserID)
					require.NoError(t, err)
					require.Equal(t, 75.0, user.Balance, "pending persistence retry must retain the original deduction")
				} else {
					sub, err := svc.entClient.UserSubscription.Get(mixins.SkipSoftDelete(ctx), original.ID)
					require.NoError(t, err)
					require.NotNil(t, sub.DeletedAt)
				}
				result, err = svc.QueryAndFinalizeRefund(ctx, order.ID)
				require.NoError(t, err)
				require.Equal(t, terminal == payment.ProviderStatusSuccess, result.Success)
				require.Len(t, provider.refundAmounts, 1, "persistence recovery cannot send a second gateway refund")
				if kind == "balance" {
					user, err := svc.entClient.User.Get(ctx, order.UserID)
					require.NoError(t, err)
					expected := 75.0
					if terminal == payment.ProviderStatusFailed {
						expected = 100
					}
					require.Equal(t, expected, user.Balance)
				} else {
					sub, err := svc.entClient.UserSubscription.Get(mixins.SkipSoftDelete(ctx), original.ID)
					require.NoError(t, err)
					require.Equal(t, terminal == payment.ProviderStatusSuccess, sub.DeletedAt != nil)
					require.True(t, original.ExpiresAt.Equal(sub.ExpiresAt))
				}
			})
		}
	}
}

type refundUnknownOutcomeReviewProvider struct {
	postMergePendingRefundProvider
	queryIDs []string
}

func (p *refundUnknownOutcomeReviewProvider) Refund(ctx context.Context, req payment.RefundRequest) (*payment.RefundResponse, error) {
	response, _ := p.postMergePendingRefundProvider.Refund(ctx, req)
	return response, errors.New("gateway result cannot yet be confirmed")
}

func (p *refundUnknownOutcomeReviewProvider) QueryRefund(ctx context.Context, req payment.RefundQueryRequest) (*payment.RefundResponse, error) {
	p.queryIDs = append(p.queryIDs, req.RefundID)
	return p.postMergePendingRefundProvider.QueryRefund(ctx, req)
}

func TestRefundUnknownOutcomeKeepsDeductionAndOnlyQueriesPersistedIdentity(t *testing.T) {
	for _, terminal := range []string{payment.ProviderStatusSuccess, payment.ProviderStatusFailed} {
		t.Run(terminal, func(t *testing.T) {
			ctx := context.Background()
			client := newPaymentConfigServiceTestClient(t)
			order := createCompletedRefundableOrderForTest(t, ctx, client, "unknown-refund-outcome")
			_, err := client.User.UpdateOneID(order.UserID).SetBalance(100).Save(ctx)
			require.NoError(t, err)
			provider := &refundUnknownOutcomeReviewProvider{postMergePendingRefundProvider: postMergePendingRefundProvider{
				refundStatus: payment.ProviderStatusPending, queryStatus: terminal}}
			t.Cleanup(replacePaymentProviderFactoryForTest(t, provider))
			svc := &PaymentService{entClient: client, userRepo: &postMergeRefundUserRepository{client: client}, loadBalancer: &captureLoadBalancer{}}
			plan, early, err := svc.PrepareRefund(ctx, order.ID, 100, "unknown outcome", false, true)
			require.NoError(t, err)
			require.Nil(t, early)
			result, err := svc.ExecuteRefund(ctx, plan)
			require.NoError(t, err)
			require.False(t, result.Success)
			user, err := client.User.Get(ctx, order.UserID)
			require.NoError(t, err)
			require.Zero(t, user.Balance, "an unknown gateway outcome must retain its deduction")
			retry, early, err := svc.PrepareRefund(ctx, order.ID, 1, "changed retry", true, false)
			require.NoError(t, err)
			require.Nil(t, early)
			result, err = svc.ExecuteRefund(ctx, retry)
			require.NoError(t, err)
			require.Equal(t, terminal == payment.ProviderStatusSuccess, result.Success)
			require.Equal(t, []string{"100.00"}, provider.refundAmounts)
			require.Equal(t, []string{"rf_postmerge"}, provider.queryIDs)
			user, err = client.User.Get(ctx, order.UserID)
			require.NoError(t, err)
			expected := 0.0
			if terminal == payment.ProviderStatusFailed {
				expected = 100
			}
			require.Equal(t, expected, user.Balance)
		})
	}
}
