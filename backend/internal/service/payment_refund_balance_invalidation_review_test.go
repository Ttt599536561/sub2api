//go:build unit

package service

import (
	"context"
	"errors"
	"testing"
	"time"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/payment"
	"github.com/stretchr/testify/require"
)

// This cache stores actual values and models an ordinary miss/refill. The test
// runs the production payment state machine and BillingCacheService admission,
// rather than asserting that a particular invalidation callback was called.
type reviewRefundBalanceCache struct {
	*memInflightCache
	present bool
}

func (c *reviewRefundBalanceCache) GetUserBalance(context.Context, int64) (float64, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.present {
		return 0, errors.New("cache miss")
	}
	return c.balance, nil
}
func (c *reviewRefundBalanceCache) SetUserBalance(_ context.Context, _ int64, balance float64) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.balance, c.present = balance, true
	return nil
}
func (c *reviewRefundBalanceCache) InvalidateUserBalance(context.Context, int64) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.present = false
	return nil
}

func TestReviewRefundBalanceInvalidationAdmission(t *testing.T) {
	for _, pending := range []bool{false, true} {
		name := "synchronous_success"
		if pending {
			name = "pending_finalization_success"
		}
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			client, order := refundWelfareFixture(t, OrderStatusCompleted, payment.OrderTypeBalance)
			_, err := client.User.UpdateOneID(order.UserID).SetBalance(100).Save(ctx)
			require.NoError(t, err)
			repo := &postMergeRefundUserRepository{client: client}
			cache := &reviewRefundBalanceCache{memInflightCache: newMemInflightCache(0)}
			cfg := &config.Config{}
			cfg.Billing.InflightReservation.Enabled = true
			billing := NewBillingCacheService(cache, repo, nil, nil, nil, nil, cfg, nil)
			t.Cleanup(billing.Stop)
			user := &User{ID: order.UserID, Balance: 100}
			require.NoError(t, billing.CheckBillingEligibility(ctx, user, nil, nil, nil, ""))
			require.Eventually(t, func() bool {
				balance, err := cache.GetUserBalance(ctx, user.ID)
				return err == nil && balance == 100
			}, time.Second, time.Millisecond)

			provider := &refundRevocationProvider{}
			provider.refundStatus = payment.ProviderStatusSuccess
			if pending {
				provider.refundStatus = payment.ProviderStatusPending
			}
			restore := replacePaymentProviderFactoryForTest(t, provider)
			defer restore()
			payments := NewPaymentService(client, payment.NewRegistry(), &captureLoadBalancer{}, nil, nil, nil, repo, nil, nil)
			payments.SetRefundCacheInvalidators(billing, nil)
			plan, early, err := payments.PrepareRefund(ctx, order.ID, 100, "return entire balance", false, true)
			require.NoError(t, err)
			require.Nil(t, early)
			result, err := payments.ExecuteRefund(ctx, plan)
			require.NoError(t, err)
			if pending {
				require.False(t, result.Success)
				// The pending gateway outcome restores the pre-deduction.
				pendingUser, err := client.User.Get(ctx, user.ID)
				require.NoError(t, err)
				require.Equal(t, 100.0, pendingUser.Balance)
				result, err = payments.QueryAndFinalizeRefund(ctx, order.ID)
				require.NoError(t, err)
			}
			require.True(t, result.Success)
			persisted, err := client.User.Get(ctx, user.ID)
			require.NoError(t, err)
			require.Zero(t, persisted.Balance, "the refund really deducted the stored user balance")
			admissionErr := billing.CheckBillingEligibility(ctx, user, nil, nil, nil, "")
			if admissionErr == nil {
				reservation, reserveErr := billing.ReserveInflight(ctx, user, nil, nil, 1)
				require.NoError(t, reserveErr)
				require.NotNil(t, reservation, "inflight admission does not recheck an allowed stale balance")
				reservation.HandlerDone()
			}
			require.ErrorIs(t, admissionErr, ErrInsufficientBalance,
				"a completed refund must prevent the next request from spending the removed balance")
		})
	}
}

func TestReviewRefundBalanceInvalidationAfterGatewayRollback(t *testing.T) {
	ctx := context.Background()
	client, order := refundWelfareFixture(t, OrderStatusCompleted, payment.OrderTypeBalance)
	_, err := client.User.UpdateOneID(order.UserID).SetBalance(100).Save(ctx)
	require.NoError(t, err)
	repo := &postMergeRefundUserRepository{client: client}
	cache := &reviewRefundBalanceCache{memInflightCache: newMemInflightCache(0)}
	billing := NewBillingCacheService(cache, repo, nil, nil, nil, nil, &config.Config{}, nil)
	t.Cleanup(billing.Stop)
	user := &User{ID: order.UserID, Balance: 100}
	provider := &refundRevocationProvider{}
	provider.refundStatus = payment.ProviderStatusFailed
	provider.beforeReturn = func() {
		// A request whose Redis entry expired while the gateway call is in
		// flight reads the committed provisional deduction and caches zero.
		current, err := client.User.Get(ctx, user.ID)
		require.NoError(t, err)
		require.Zero(t, current.Balance)
		balance, err := billing.GetUserBalance(ctx, user.ID)
		require.NoError(t, err)
		require.Zero(t, balance)
		require.Eventually(t, func() bool {
			balance, err := cache.GetUserBalance(ctx, user.ID)
			return err == nil && balance == 0
		}, time.Second, time.Millisecond)
	}
	restore := replacePaymentProviderFactoryForTest(t, provider)
	defer restore()
	payments := NewPaymentService(client, payment.NewRegistry(), &captureLoadBalancer{}, nil, nil, nil, repo, nil, nil)
	payments.SetRefundCacheInvalidators(billing, nil)
	plan, early, err := payments.PrepareRefund(ctx, order.ID, 100, "gateway will fail", false, true)
	require.NoError(t, err)
	require.Nil(t, early)
	result, err := payments.ExecuteRefund(ctx, plan)
	require.NoError(t, err)
	require.False(t, result.Success)
	persisted, err := client.User.Get(ctx, user.ID)
	require.NoError(t, err)
	require.Equal(t, 100.0, persisted.Balance)
	require.NoError(t, billing.CheckBillingEligibility(ctx, user, nil, nil, nil, ""),
		"a successful rollback must make the restored balance spendable")
}

type reviewRefundInvalidators struct {
	balance func(context.Context, int64) error
	auth    func(context.Context, int64) error
}

func (i *reviewRefundInvalidators) InvalidateUserBalance(ctx context.Context, id int64) error {
	return i.balance(ctx, id)
}
func (i *reviewRefundInvalidators) InvalidateAuthCacheByUserIDReliable(ctx context.Context, id int64) error {
	return i.auth(ctx, id)
}

func TestReviewRefundBalanceInvalidationFailuresNeverRepeatRefund(t *testing.T) {
	for _, pending := range []bool{false, true} {
		name := "synchronous"
		if pending {
			name = "pending"
		}
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			client, order := refundWelfareFixture(t, OrderStatusCompleted, payment.OrderTypeBalance)
			_, err := client.User.UpdateOneID(order.UserID).SetBalance(100).Save(ctx)
			require.NoError(t, err)
			repo := &postMergeRefundUserRepository{client: client}
			provider := &refundRevocationProvider{}
			provider.refundStatus = payment.ProviderStatusSuccess
			if pending {
				provider.refundStatus = payment.ProviderStatusPending
			}
			restore := replacePaymentProviderFactoryForTest(t, provider)
			defer restore()
			payments := NewPaymentService(client, payment.NewRegistry(), &captureLoadBalancer{}, nil, nil, nil, repo, nil, nil)
			var balanceSnapshots, authSnapshots []float64
			observe := func(callCtx context.Context, userID int64, snapshots *[]float64) error {
				require.NoError(t, callCtx.Err())
				require.Nil(t, dbent.TxFromContext(callCtx), "invalidation must occur after commit")
				_, bounded := callCtx.Deadline()
				require.True(t, bounded)
				persisted, err := client.User.Get(callCtx, userID)
				require.NoError(t, err, "ordinary readers must see the committed balance")
				*snapshots = append(*snapshots, persisted.Balance)
				return errors.New("cache unavailable")
			}
			invalidators := &reviewRefundInvalidators{
				balance: func(ctx context.Context, id int64) error { return observe(ctx, id, &balanceSnapshots) },
				auth:    func(ctx context.Context, id int64) error { return observe(ctx, id, &authSnapshots) },
			}
			payments.SetRefundCacheInvalidators(invalidators, invalidators)
			plan, early, err := payments.PrepareRefund(ctx, order.ID, 100, "cache failure", false, true)
			require.NoError(t, err)
			require.Nil(t, early)
			result, err := payments.ExecuteRefund(ctx, plan)
			require.NoError(t, err)
			if pending {
				require.False(t, result.Success)
				result, err = payments.QueryAndFinalizeRefund(ctx, order.ID)
				require.NoError(t, err)
			}
			require.True(t, result.Success)
			wantSnapshots := []float64{0}
			if pending {
				wantSnapshots = []float64{0, 100, 0}
			}
			require.Equal(t, wantSnapshots, balanceSnapshots)
			require.Equal(t, wantSnapshots, authSnapshots, "one failed cache must not skip the other cache")
			require.Len(t, provider.refundAmounts, 1)
			result, err = payments.QueryAndFinalizeRefund(ctx, order.ID)
			require.NoError(t, err)
			require.True(t, result.Success)
			persisted, err := client.User.Get(ctx, order.UserID)
			require.NoError(t, err)
			require.Zero(t, persisted.Balance, "retrying completion must not repeat the financial deduction")
			require.Equal(t, wantSnapshots, balanceSnapshots)
			require.Len(t, provider.refundAmounts, 1)
		})
	}
}

type reviewCancelRefundDeductionRepository struct {
	postMergeRefundUserRepository
	cancel context.CancelFunc
}

func (r *reviewCancelRefundDeductionRepository) DeductAvailableBalance(ctx context.Context, id int64, amount float64) (float64, error) {
	deducted, err := r.postMergeRefundUserRepository.DeductAvailableBalance(ctx, id, amount)
	if err == nil {
		r.cancel()
	}
	return deducted, err
}

func TestReviewRefundBalanceInvalidationDetachedFromCanceledRequest(t *testing.T) {
	for _, pending := range []bool{false, true} {
		name := "canceled_after_prededuction"
		if pending {
			name = "canceled_after_pending_commit"
		}
		t.Run(name, func(t *testing.T) {
			base := context.Background()
			ctx, cancel := context.WithCancel(base)
			defer cancel()
			status := OrderStatusCompleted
			if pending {
				status = OrderStatusRefundPending
			}
			client, order := refundWelfareFixture(t, status, payment.OrderTypeBalance)
			order, err := client.PaymentOrder.UpdateOneID(order.ID).SetRefundAmount(100).Save(base)
			require.NoError(t, err)
			_, err = client.User.UpdateOneID(order.UserID).SetBalance(100).Save(base)
			require.NoError(t, err)
			var repo UserRepository = &reviewCancelRefundDeductionRepository{
				postMergeRefundUserRepository: postMergeRefundUserRepository{client: client}, cancel: cancel,
			}
			if pending {
				repo = &postMergeRefundUserRepository{client: client}
				client.PaymentOrder.Use(func(next dbent.Mutator) dbent.Mutator {
					return dbent.MutateFunc(func(ctx context.Context, mutation dbent.Mutation) (dbent.Value, error) {
						change, ok := mutation.(*dbent.PaymentOrderMutation)
						if ok {
							if status, _ := change.Status(); status == OrderStatusRefunded {
								tx := dbent.TxFromContext(ctx)
								require.NotNil(t, tx)
								tx.OnCommit(func(next dbent.Committer) dbent.Committer {
									return dbent.CommitFunc(func(ctx context.Context, tx *dbent.Tx) error {
										err := next.Commit(ctx, tx)
										if err == nil {
											cancel()
										}
										return err
									})
								})
							}
						}
						return next.Mutate(ctx, mutation)
					})
				})
			}
			provider := &refundRevocationProvider{}
			provider.refundStatus = payment.ProviderStatusFailed
			restore := replacePaymentProviderFactoryForTest(t, provider)
			defer restore()
			payments := NewPaymentService(client, payment.NewRegistry(), &captureLoadBalancer{}, nil, nil, nil, repo, nil, nil)
			balanceCalls, authCalls := 0, 0
			inspect := func(callCtx context.Context, id int64) {
				require.ErrorIs(t, ctx.Err(), context.Canceled, "request was canceled at the commit boundary")
				require.NoError(t, callCtx.Err(), "committed cleanup must survive caller cancellation")
				require.Nil(t, dbent.TxFromContext(callCtx))
				_, bounded := callCtx.Deadline()
				require.True(t, bounded)
				require.Equal(t, order.UserID, id)
			}
			invalidators := &reviewRefundInvalidators{
				balance: func(ctx context.Context, id int64) error { inspect(ctx, id); balanceCalls++; return nil },
				auth:    func(ctx context.Context, id int64) error { inspect(ctx, id); authCalls++; return nil },
			}
			payments.SetRefundCacheInvalidators(invalidators, invalidators)
			var result *RefundResult
			if pending {
				result, err = payments.QueryAndFinalizeRefund(ctx, order.ID)
			} else {
				var plan *RefundPlan
				var early *RefundResult
				plan, early, err = payments.PrepareRefund(ctx, order.ID, 100, "canceled request", false, true)
				require.NoError(t, err)
				require.Nil(t, early)
				result, err = payments.ExecuteRefund(ctx, plan)
			}
			require.NoError(t, err)
			require.Equal(t, pending, result.Success)
			wantCalls, wantBalance := 2, 100.0
			if pending {
				wantCalls, wantBalance = 1, 0
			}
			require.Equal(t, wantCalls, balanceCalls)
			require.Equal(t, wantCalls, authCalls)
			user, err := client.User.Get(base, order.UserID)
			require.NoError(t, err)
			require.Equal(t, wantBalance, user.Balance)
		})
	}
}

func TestReviewRefundBalanceInvalidationTimeoutStillAttemptsAuth(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	balanceCalled, authCalled := false, false
	invalidators := &reviewRefundInvalidators{
		balance: func(callCtx context.Context, id int64) error {
			balanceCalled = true
			require.NoError(t, callCtx.Err())
			<-callCtx.Done()
			return callCtx.Err()
		},
		auth: func(callCtx context.Context, id int64) error {
			authCalled = true
			require.NoError(t, callCtx.Err(), "auth cleanup needs its own timeout after balance cleanup expires")
			deadline, ok := callCtx.Deadline()
			require.True(t, ok)
			require.Greater(t, time.Until(deadline), time.Second)
			return nil
		},
	}
	payments := &PaymentService{}
	payments.SetRefundCacheInvalidators(invalidators, invalidators)
	payments.invalidateRefundBalanceCaches(ctx, 7)
	require.True(t, balanceCalled)
	require.True(t, authCalled)
}

func TestReviewRefundBalanceInvalidationSkipsUncommittedTransaction(t *testing.T) {
	client := newPaymentConfigServiceTestClient(t)
	tx, err := client.Tx(context.Background())
	require.NoError(t, err)
	defer tx.Rollback()
	invalidators := &reviewRefundInvalidators{
		balance: func(context.Context, int64) error { t.Fatal("balance cache invalidation preceded commit"); return nil },
		auth:    func(context.Context, int64) error { t.Fatal("auth cache invalidation preceded commit"); return nil },
	}
	payments := &PaymentService{}
	payments.SetRefundCacheInvalidators(invalidators, invalidators)
	payments.invalidateRefundBalanceCaches(dbent.NewTxContext(context.Background(), tx), 7)
}
