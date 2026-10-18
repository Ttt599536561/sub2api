//go:build unit

package service

import (
	"context"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/ent/schema/mixins"
	"github.com/Wei-Shaw/sub2api/internal/payment"
	"github.com/stretchr/testify/require"
)

func TestV217PendingRefundRestoresRevokedSubscriptionFromPersistedSnapshot(t *testing.T) {
	ctx := context.Background()
	svc, order, original, provider := refundRevocationFixture(t)
	provider.refundStatus = payment.ProviderStatusPending
	provider.queryStatus = payment.ProviderStatusFailed
	plan, early, err := svc.PrepareRefund(ctx, order.ID, 25, "pending revocation", false, true)
	require.NoError(t, err)
	require.Nil(t, early)
	result, err := svc.ExecuteRefund(ctx, plan)
	require.NoError(t, err)
	require.False(t, result.Success)
	held, err := svc.entClient.UserSubscription.Get(mixins.SkipSoftDelete(ctx), original.ID)
	require.NoError(t, err)
	require.NotNil(t, held.DeletedAt, "pending must retain the revocation")
	// Finalization reconstructs its plan entirely from persisted metadata.
	result, err = svc.QueryAndFinalizeRefund(ctx, order.ID)
	require.NoError(t, err)
	require.False(t, result.Success)
	require.NotContains(t, result.Warning, "rollback failed")
	restored, err := svc.entClient.UserSubscription.Get(ctx, original.ID)
	require.NoError(t, err)
	assertRefundCompensationTerm(t, original, restored, original.ExpiresAt)
	require.Nil(t, restored.DeletedAt)
}

func TestV217PendingRefundRestoresShortenedSubscriptionWithoutResettingTerm(t *testing.T) {
	ctx := context.Background()
	svc, order, original, provider, _, now := refundCompensationFixture(t)
	provider.refundStatus = payment.ProviderStatusPending
	provider.queryStatus = payment.ProviderStatusFailed
	plan, early, err := svc.PrepareRefund(ctx, order.ID, 25, "pending shortening", false, true)
	require.NoError(t, err)
	require.Nil(t, early)
	_, err = svc.ExecuteRefund(ctx, plan)
	require.NoError(t, err)
	*now = now.Add(2 * time.Second)
	result, err := svc.QueryAndFinalizeRefund(ctx, order.ID)
	require.NoError(t, err)
	require.NotContains(t, result.Warning, "rollback failed")
	restored, err := svc.entClient.UserSubscription.Get(ctx, original.ID)
	require.NoError(t, err)
	assertRefundCompensationTerm(t, original, restored, original.ExpiresAt)
}

func TestV217PendingRefundFailureRestorationRollsBackWithAudit(t *testing.T) {
	ctx := context.Background()
	svc, order, original, provider := refundRevocationFixture(t)
	provider.refundStatus = payment.ProviderStatusPending
	provider.queryStatus = payment.ProviderStatusFailed
	plan, early, err := svc.PrepareRefund(ctx, order.ID, 25, "atomic pending recovery", false, true)
	require.NoError(t, err)
	require.Nil(t, early)
	_, err = svc.ExecuteRefund(ctx, plan)
	require.NoError(t, err)
	_, err = svc.entClient.ExecContext(ctx, `CREATE TRIGGER reject_v217_failure BEFORE INSERT ON payment_audit_logs WHEN NEW.action='REFUND_FAILED' BEGIN SELECT RAISE(ABORT, 'failure audit unavailable'); END`)
	require.NoError(t, err)
	_, err = svc.QueryAndFinalizeRefund(ctx, order.ID)
	require.ErrorContains(t, err, "failure audit unavailable")
	current, err := svc.entClient.PaymentOrder.Get(ctx, order.ID)
	require.NoError(t, err)
	require.Equal(t, OrderStatusRefundPending, current.Status)
	revoked, err := svc.entClient.UserSubscription.Get(mixins.SkipSoftDelete(ctx), original.ID)
	require.NoError(t, err)
	require.NotNil(t, revoked.DeletedAt, "subscription restoration must roll back with the failure audit")
	_, err = svc.entClient.ExecContext(ctx, `DROP TRIGGER reject_v217_failure`)
	require.NoError(t, err)
	result, err := svc.QueryAndFinalizeRefund(ctx, order.ID)
	require.NoError(t, err)
	require.NotContains(t, result.Warning, "rollback failed")
	restored, err := svc.entClient.UserSubscription.Get(ctx, original.ID)
	require.NoError(t, err)
	assertRefundCompensationTerm(t, original, restored, original.ExpiresAt)
}

func TestV217PendingRefundFailurePreservesConcurrentAdminRevocation(t *testing.T) {
	ctx := context.Background()
	svc, order, original, provider := refundRevocationFixture(t)
	provider.refundStatus = payment.ProviderStatusPending
	provider.queryStatus = payment.ProviderStatusFailed
	plan, early, err := svc.PrepareRefund(ctx, order.ID, 25, "admin changed pending", false, true)
	require.NoError(t, err)
	require.Nil(t, early)
	_, err = svc.ExecuteRefund(ctx, plan)
	require.NoError(t, err)
	held, err := svc.entClient.UserSubscription.Get(mixins.SkipSoftDelete(ctx), original.ID)
	require.NoError(t, err)
	_, err = svc.subscriptionSvc.RestoreSubscription(ctx, original.ID)
	require.NoError(t, err)
	require.NoError(t, svc.subscriptionSvc.RevokeSubscription(ctx, original.ID))
	admin, err := svc.entClient.UserSubscription.UpdateOneID(original.ID).
		SetDeletedAt(*held.DeletedAt).SetUpdatedAt(held.UpdatedAt).Save(mixins.SkipSoftDelete(ctx))
	require.NoError(t, err)
	require.Greater(t, admin.DailyResetVersion, held.DailyResetVersion)
	result, err := svc.QueryAndFinalizeRefund(ctx, order.ID)
	require.NoError(t, err)
	require.Contains(t, result.Warning, "rollback failed")
	current, err := svc.entClient.UserSubscription.Get(mixins.SkipSoftDelete(ctx), original.ID)
	require.NoError(t, err)
	require.NotNil(t, current.DeletedAt)
	require.Equal(t, admin.DailyResetVersion, current.DailyResetVersion)
	require.True(t, original.ExpiresAt.Equal(current.ExpiresAt))
}

func TestV217HeldRefundConfirmedRecoveryNeverDeductsAgain(t *testing.T) {
	for _, kind := range []string{"balance", "shortened", "revoked"} {
		t.Run(kind, func(t *testing.T) {
			ctx := context.Background()
			svc, order, original, provider := refundRevocationFixture(t)
			var err error
			if kind == "balance" {
				order, err = svc.entClient.PaymentOrder.UpdateOneID(order.ID).SetOrderType(payment.OrderTypeBalance).Save(ctx)
				require.NoError(t, err)
				_, err = svc.entClient.User.UpdateOneID(order.UserID).SetBalance(100).Save(ctx)
				require.NoError(t, err)
				svc.userRepo = &postMergeRefundUserRepository{client: svc.entClient}
			} else {
				if kind == "shortened" {
					original, err = svc.entClient.UserSubscription.UpdateOneID(original.ID).SetExpiresAt(time.Now().UTC().AddDate(0, 0, 30)).Save(ctx)
					require.NoError(t, err)
				}
				svc.welfarePaymentRepo = &refundWelfareRepositoryStub{reverse: func(ctx context.Context, id int64) error {
					recordRefundWelfareEvent(t, ctx, id)
					return nil
				}}
			}
			provider.refundStatus = payment.ProviderStatusPending
			provider.queryStatus = payment.ProviderStatusSuccess
			plan, early, err := svc.PrepareRefund(ctx, order.ID, 25, "held confirmed recovery", false, true)
			require.NoError(t, err)
			require.Nil(t, early)
			_, err = svc.ExecuteRefund(ctx, plan)
			require.NoError(t, err)
			held, err := svc.entClient.UserSubscription.Get(mixins.SkipSoftDelete(ctx), original.ID)
			require.NoError(t, err)
			_, err = svc.entClient.ExecContext(ctx, `CREATE TRIGGER reject_v217_success BEFORE INSERT ON payment_audit_logs WHEN NEW.action='REFUND_SUCCESS' BEGIN SELECT RAISE(ABORT, 'success audit unavailable'); END`)
			require.NoError(t, err)
			_, err = svc.QueryAndFinalizeRefund(ctx, order.ID)
			require.ErrorContains(t, err, "success audit unavailable")
			detail, err := svc.latestRefundPendingDetail(ctx, order.ID)
			require.NoError(t, err)
			require.True(t, detail.GatewayConfirmed)
			require.True(t, detail.DeductionHeld)
			if kind == "balance" {
				require.Equal(t, 25.0, detail.BalanceDeducted)
			} else {
				require.Equal(t, 7, detail.SubDaysDeducted)
				require.Equal(t, original.ID, detail.SubscriptionID)
				if kind == "revoked" {
					require.NotNil(t, detail.RevokedSubscription)
				}
			}
			assertRefundWelfareEventCount(t, svc.entClient, 0)
			_, err = svc.entClient.ExecContext(ctx, `DROP TRIGGER reject_v217_success`)
			require.NoError(t, err)
			retry, early, err := svc.PrepareRefund(ctx, order.ID, 100, "must not replace persisted refund", true, false)
			require.NoError(t, err)
			require.Nil(t, early)
			result, err := svc.ExecuteRefund(ctx, retry)
			require.NoError(t, err)
			require.True(t, result.Success)
			require.Equal(t, 1, provider.queryCalls, "persisted gateway success must not call the provider again")
			require.Len(t, provider.refundAmounts, 1)
			actual, err := svc.entClient.UserSubscription.Get(mixins.SkipSoftDelete(ctx), original.ID)
			require.NoError(t, err)
			require.True(t, held.ExpiresAt.Equal(actual.ExpiresAt))
			require.Equal(t, held.DeletedAt, actual.DeletedAt)
			require.Equal(t, held.DailyResetVersion, actual.DailyResetVersion)
			if kind == "balance" {
				user, err := svc.entClient.User.Get(ctx, order.UserID)
				require.NoError(t, err)
				require.Equal(t, 75.0, user.Balance)
			} else {
				assertRefundWelfareEventCount(t, svc.entClient, 1)
			}
		})
	}
}
