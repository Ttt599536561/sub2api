//go:build unit

package service

import (
	"context"
	"errors"
	"testing"
	"time"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/ent/schema/mixins"
	"github.com/Wei-Shaw/sub2api/ent/usersubscription"
	"github.com/Wei-Shaw/sub2api/internal/payment"
	"github.com/stretchr/testify/require"
)

type refundRevocationSubscriptionRepository struct {
	refundWelfareSubscriptionRepository
}

func (r *refundRevocationSubscriptionRepository) Delete(ctx context.Context, id int64) error {
	return r.txClient(ctx).UserSubscription.DeleteOneID(id).Exec(ctx)
}

func (r *refundRevocationSubscriptionRepository) GetByIDIncludeDeleted(ctx context.Context, id int64) (*UserSubscription, error) {
	sub, err := r.txClient(ctx).UserSubscription.Get(mixins.SkipSoftDelete(ctx), id)
	if err != nil {
		return nil, err
	}
	return &UserSubscription{ID: sub.ID, UserID: sub.UserID, GroupID: sub.GroupID,
		ExpiresAt: sub.ExpiresAt, Status: sub.Status, DeletedAt: sub.DeletedAt}, nil
}

func (r *refundRevocationSubscriptionRepository) ExistsActiveByUserIDAndGroupID(ctx context.Context, userID, groupID int64) (bool, error) {
	return r.txClient(ctx).UserSubscription.Query().Where(usersubscription.UserIDEQ(userID), usersubscription.GroupIDEQ(groupID)).Exist(ctx)
}

func (r *refundRevocationSubscriptionRepository) Restore(ctx context.Context, id int64, status string) (*UserSubscription, error) {
	_, err := r.txClient(ctx).UserSubscription.UpdateOneID(id).ClearDeletedAt().SetStatus(status).
		AddDailyResetVersion(1).Save(mixins.SkipSoftDelete(ctx))
	if err != nil {
		return nil, err
	}
	return r.GetByID(ctx, id)
}

type refundRevocationProvider struct {
	postMergePendingRefundProvider
	beforeReturn func()
}

func (p *refundRevocationProvider) Refund(ctx context.Context, req payment.RefundRequest) (*payment.RefundResponse, error) {
	if p.beforeReturn != nil {
		p.beforeReturn()
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if p.refundStatus == payment.ProviderStatusFailed {
		return nil, errors.New("gateway rejected refund")
	}
	return p.postMergePendingRefundProvider.Refund(ctx, req)
}

func refundRevocationFixture(t *testing.T) (*PaymentService, *dbent.PaymentOrder, *dbent.UserSubscription, *refundRevocationProvider) {
	t.Helper()
	ctx := context.Background()
	client, order := refundWelfareFixture(t, OrderStatusCompleted, payment.OrderTypeSubscription)
	// Like PostgreSQL, keep only wall-clock timestamps so SQLite TEXT columns
	// can participate in the production compare-and-swap predicates.
	client.UserSubscription.Use(func(next dbent.Mutator) dbent.Mutator {
		return dbent.MutateFunc(func(ctx context.Context, mutation dbent.Mutation) (dbent.Value, error) {
			if sub, ok := mutation.(*dbent.UserSubscriptionMutation); ok {
				if updated, exists := sub.UpdatedAt(); exists {
					sub.SetUpdatedAt(updated.UTC())
				}
				if deleted, exists := sub.DeletedAt(); exists {
					sub.SetDeletedAt(deleted.UTC())
				}
			}
			return next.Mutate(ctx, mutation)
		})
	})
	group, err := client.Group.Create().SetName("refund-revocation").Save(ctx)
	require.NoError(t, err)
	window := time.Now().UTC().Add(-time.Hour)
	sub, err := client.UserSubscription.Create().SetUserID(order.UserID).SetGroupID(group.ID).
		SetStartsAt(window).SetExpiresAt(time.Now().UTC().AddDate(0, 0, 3)).
		SetDailyWindowStart(window).SetDailyUsageUsd(1.5).SetWeeklyUsageUsd(8).
		SetAutoDailyResetEnabled(true).SetPreserveCalendarDailyReset(true).Save(ctx)
	require.NoError(t, err)
	order, err = client.PaymentOrder.UpdateOneID(order.ID).SetSubscriptionGroupID(group.ID).SetSubscriptionDays(7).Save(ctx)
	require.NoError(t, err)
	provider := &refundRevocationProvider{}
	restore := replacePaymentProviderFactoryForTest(t, provider)
	t.Cleanup(restore)
	svc := &PaymentService{entClient: client, loadBalancer: &captureLoadBalancer{},
		subscriptionSvc: &SubscriptionService{entClient: client,
			userSubRepo: &refundRevocationSubscriptionRepository{refundWelfareSubscriptionRepository{client: client, subID: sub.ID}}},
	}
	return svc, order, sub, provider
}

func TestRefundRevocationRollbackPreservesSubscription(t *testing.T) {
	for _, gatewayStatus := range []string{payment.ProviderStatusFailed, payment.ProviderStatusPending} {
		t.Run(gatewayStatus, func(t *testing.T) {
			ctx := context.Background()
			svc, order, original, provider := refundRevocationFixture(t)
			provider.refundStatus = gatewayStatus
			plan, early, err := svc.PrepareRefund(ctx, order.ID, 25, "refund a used subscription", false, true)
			require.NoError(t, err)
			require.Nil(t, early)
			_, refundErr := svc.ExecuteRefund(ctx, plan)
			persisted, err := svc.entClient.UserSubscription.Get(mixins.SkipSoftDelete(ctx), original.ID)
			require.NoError(t, err)
			require.Nil(t, persisted.DeletedAt, "failed or pending gateway refund must restore the subscription revoked by its own pre-deduction")
			require.NoError(t, refundErr)
			require.True(t, original.ExpiresAt.Equal(persisted.ExpiresAt), "rollback must not add the purchased days to the unchanged expiry")
			require.Equal(t, original.AutoDailyResetEnabled, persisted.AutoDailyResetEnabled)
			require.Equal(t, original.PreserveCalendarDailyReset, persisted.PreserveCalendarDailyReset)
			require.Equal(t, original.DailyUsageUsd, persisted.DailyUsageUsd)
			require.Equal(t, original.WeeklyUsageUsd, persisted.WeeklyUsageUsd)
			require.True(t, original.DailyWindowStart.Equal(*persisted.DailyWindowStart))
			require.True(t, svc.RollbackRefund(ctx, plan, nil), "replaying the same rollback must be a no-op")
			persisted, err = svc.entClient.UserSubscription.Get(ctx, original.ID)
			require.NoError(t, err)
			require.True(t, original.ExpiresAt.Equal(persisted.ExpiresAt))
			if gatewayStatus == payment.ProviderStatusPending {
				svc.welfarePaymentRepo = &refundWelfareRepositoryStub{reverse: func(ctx context.Context, id int64) error {
					recordRefundWelfareEvent(t, ctx, id)
					return nil
				}}
				result, err := svc.QueryAndFinalizeRefund(ctx, order.ID)
				require.NoError(t, err)
				require.True(t, result.Success)
				persisted, err = svc.entClient.UserSubscription.Get(mixins.SkipSoftDelete(ctx), original.ID)
				require.NoError(t, err)
				require.NotNil(t, persisted.DeletedAt, "confirmed refund must reclaim the restored subscription")
				_, err = svc.QueryAndFinalizeRefund(ctx, order.ID)
				require.NoError(t, err)
				assertRefundWelfareEventCount(t, svc.entClient, 1)
			}
		})
	}
}

func TestRefundRevocationRollbackDoesNotRestoreAdminRevocation(t *testing.T) {
	ctx := context.Background()
	svc, order, original, provider := refundRevocationFixture(t)
	provider.refundStatus = payment.ProviderStatusFailed
	var adminRevocation time.Time
	provider.beforeReturn = func() {
		refundRevoked, err := svc.entClient.UserSubscription.Get(mixins.SkipSoftDelete(ctx), original.ID)
		require.NoError(t, err)
		// The refund has revoked the record. An administrator restores it and
		// then revokes it again while the provider call is in flight.
		_, err = svc.subscriptionSvc.RestoreSubscription(ctx, original.ID)
		require.NoError(t, err)
		require.NoError(t, svc.subscriptionSvc.RevokeSubscription(ctx, original.ID))
		// A timestamp is not a unique version on every platform. Restore's
		// monotonic daily-reset version must also protect same-tick changes.
		current, err := svc.entClient.UserSubscription.UpdateOneID(original.ID).
			SetDeletedAt(*refundRevoked.DeletedAt).SetUpdatedAt(refundRevoked.UpdatedAt).
			Save(mixins.SkipSoftDelete(ctx))
		require.NoError(t, err)
		require.Greater(t, current.DailyResetVersion, refundRevoked.DailyResetVersion)
		require.NotNil(t, current.DeletedAt)
		adminRevocation = *current.DeletedAt
	}
	plan, early, err := svc.PrepareRefund(ctx, order.ID, 25, "refund", false, true)
	require.NoError(t, err)
	require.Nil(t, early)
	_, err = svc.ExecuteRefund(ctx, plan)
	require.Error(t, err, "a concurrent admin revocation requires manual reconciliation")
	current, err := svc.entClient.UserSubscription.Get(mixins.SkipSoftDelete(ctx), original.ID)
	require.NoError(t, err)
	require.NotNil(t, current.DeletedAt)
	require.True(t, adminRevocation.Equal(*current.DeletedAt), "refund rollback must not undo the independent admin decision")
	require.True(t, original.ExpiresAt.Equal(current.ExpiresAt))
}

func TestRefundRevocationRollbackDoesNotOverwriteAdminEdit(t *testing.T) {
	ctx := context.Background()
	svc, order, original, provider := refundRevocationFixture(t)
	provider.refundStatus = payment.ProviderStatusFailed
	provider.beforeReturn = func() {
		current, err := svc.entClient.UserSubscription.Get(mixins.SkipSoftDelete(ctx), original.ID)
		require.NoError(t, err)
		// UserSubscriptionRepository.Update increments this version on admin
		// edits. Keep the same timestamp to prove the version check is sufficient.
		updated, err := svc.entClient.UserSubscription.UpdateOneID(original.ID).
			SetNotes("admin hold").SetAutoDailyResetEnabled(false).
			AddDailyResetVersion(1).SetUpdatedAt(current.UpdatedAt).Save(mixins.SkipSoftDelete(ctx))
		require.NoError(t, err)
		require.Greater(t, updated.DailyResetVersion, current.DailyResetVersion)
	}
	plan, early, err := svc.PrepareRefund(ctx, order.ID, 25, "refund", false, true)
	require.NoError(t, err)
	require.Nil(t, early)
	_, err = svc.ExecuteRefund(ctx, plan)
	require.Error(t, err)
	current, err := svc.entClient.UserSubscription.Get(mixins.SkipSoftDelete(ctx), original.ID)
	require.NoError(t, err)
	require.NotNil(t, current.DeletedAt)
	require.Equal(t, "admin hold", *current.Notes)
	require.False(t, current.AutoDailyResetEnabled)
}

func TestRefundGatewayCancellationRestoresEntitlementStatusAndAudit(t *testing.T) {
	for _, tc := range []struct {
		name, orderType, status string
	}{
		{"balance", payment.OrderTypeBalance, OrderStatusCompleted},
		{"balance_requested", payment.OrderTypeBalance, OrderStatusRefundRequested},
		{"subscription", payment.OrderTypeSubscription, OrderStatusCompleted},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			svc, order, originalSub, provider := refundRevocationFixture(t)
			var err error
			order, err = svc.entClient.PaymentOrder.UpdateOneID(order.ID).
				SetOrderType(tc.orderType).SetStatus(tc.status).Save(ctx)
			require.NoError(t, err)
			_, err = svc.entClient.User.UpdateOneID(order.UserID).SetBalance(100).Save(ctx)
			require.NoError(t, err)
			svc.userRepo = &postMergeRefundUserRepository{client: svc.entClient}
			requestCtx, cancel := context.WithCancel(ctx)
			defer cancel()
			provider.beforeReturn = func() {
				if tc.orderType == payment.OrderTypeSubscription {
					sub, err := svc.entClient.UserSubscription.Get(mixins.SkipSoftDelete(ctx), originalSub.ID)
					require.NoError(t, err)
					require.NotNil(t, sub.DeletedAt, "subscription must already be revoked before provider cancellation")
				} else {
					user, err := svc.entClient.User.Get(ctx, order.UserID)
					require.NoError(t, err)
					require.Equal(t, 75.0, user.Balance, "balance must already be deducted before provider cancellation")
				}
				cancel()
			}
			plan, early, err := svc.PrepareRefund(requestCtx, order.ID, 25, "cancelled request", false, true)
			require.NoError(t, err)
			require.Nil(t, early)
			result, refundErr := svc.ExecuteRefund(requestCtx, plan)
			require.ErrorIs(t, requestCtx.Err(), context.Canceled)
			if tc.orderType == payment.OrderTypeSubscription {
				sub, err := svc.entClient.UserSubscription.Get(mixins.SkipSoftDelete(ctx), originalSub.ID)
				require.NoError(t, err)
				require.Nil(t, sub.DeletedAt, "request cancellation must not prevent revocation rollback")
				require.True(t, originalSub.ExpiresAt.Equal(sub.ExpiresAt))
				require.Equal(t, originalSub.AutoDailyResetEnabled, sub.AutoDailyResetEnabled)
				require.Equal(t, originalSub.PreserveCalendarDailyReset, sub.PreserveCalendarDailyReset)
			} else {
				user, err := svc.entClient.User.Get(ctx, order.UserID)
				require.NoError(t, err)
				require.Equal(t, 100.0, user.Balance, "request cancellation must not prevent balance rollback")
			}
			require.NoError(t, refundErr)
			require.False(t, result.Success)
			require.Contains(t, result.Warning, "context canceled")
			require.Contains(t, result.Warning, "rolled back")
			persisted, err := svc.entClient.PaymentOrder.Get(ctx, order.ID)
			require.NoError(t, err)
			require.Equal(t, tc.status, persisted.Status, "cancelled requests must not leave an order stuck REFUNDING")
			require.True(t, svc.hasAuditLog(ctx, order.ID, "REFUND_GATEWAY_FAILED"))
			require.False(t, svc.hasAuditLog(ctx, order.ID, "REFUND_ROLLBACK_FAILED"))
		})
	}
}
