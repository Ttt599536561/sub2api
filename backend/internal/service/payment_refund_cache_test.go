//go:build unit

package service

import (
	"context"
	"testing"
	"time"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/dgraph-io/ristretto"
	"github.com/stretchr/testify/require"
)

func TestPendingRefundCommitInvalidatesSubscriptionRefilledBeforeCommit(t *testing.T) {
	for _, revoke := range []bool{false, true} {
		name := "shorten"
		if revoke {
			name = "revoke"
		}
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			svc, order, original, _ := refundRevocationFixture(t)
			var err error
			if !revoke {
				original, err = svc.entClient.UserSubscription.UpdateOneID(original.ID).
					SetExpiresAt(time.Now().UTC().AddDate(0, 0, 30)).Save(ctx)
				require.NoError(t, err)
			}
			order, err = svc.entClient.PaymentOrder.UpdateOneID(order.ID).
				SetStatus(OrderStatusRefundPending).SetRefundAmount(25).Save(ctx)
			require.NoError(t, err)
			cache, err := ristretto.NewCache(&ristretto.Config{NumCounters: 1000, MaxCost: 100, BufferItems: 64})
			require.NoError(t, err)
			t.Cleanup(cache.Close)
			svc.subscriptionSvc.subCacheL1 = cache
			svc.subscriptionSvc.subCacheTTL = time.Minute
			key := subCacheKey(original.UserID, original.GroupID)
			svc.welfarePaymentRepo = &refundWelfareRepositoryStub{reverse: func(txCtx context.Context, id int64) error {
				require.NotNil(t, dbent.TxFromContext(txCtx))
				// A concurrent database reader still sees the original committed
				// entitlement after deduction invalidates caches but before commit.
				// Refill that snapshot at this deterministic transaction boundary.
				cache.Wait()
				require.True(t, cache.SetWithTTL(key, &UserSubscription{ID: original.ID,
					UserID: original.UserID, GroupID: original.GroupID, Status: original.Status,
					ExpiresAt: original.ExpiresAt}, 1, time.Minute))
				cache.Wait()
				recordRefundWelfareEvent(t, txCtx, id)
				return nil
			}}
			result, err := svc.QueryAndFinalizeRefund(ctx, order.ID)
			require.NoError(t, err)
			require.True(t, result.Success)
			active, err := svc.subscriptionSvc.GetActiveSubscription(ctx, original.UserID, original.GroupID)
			if revoke {
				require.Error(t, err, "a committed refund must not return the revoked entitlement from L1")
				require.Nil(t, active)
			} else {
				require.NoError(t, err)
				require.True(t, original.ExpiresAt.AddDate(0, 0, -7).Equal(active.ExpiresAt),
					"a committed refund must not return the original expiry from L1")
			}
		})
	}
}
