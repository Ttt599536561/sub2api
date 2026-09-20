//go:build integration

package repository

import (
	"context"
	"database/sql"
	"fmt"
	"sync"
	"testing"
	"time"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/google/uuid"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/require"
)

func welfarePaymentFixture(t *testing.T) (service.WelfarePaymentRepository, int64) {
	t.Helper()
	_, userID, _ := welfareSpendFixture(t)
	return NewWelfarePaymentRepository(), userID
}

func welfarePaymentOrder(t *testing.T, userID int64, amount, paid string) int64 {
	t.Helper()
	var orderID int64
	require.NoError(t, integrationDB.QueryRow(`INSERT INTO payment_orders
		(user_id,amount,pay_amount,order_type,status,paid_at,completed_at,expires_at,out_trade_no,provider_snapshot)
		VALUES($1,$2,$3,'subscription','COMPLETED',NOW(),NOW(),NOW()+interval '1 hour',$4,'{"currency":"CNY"}') RETURNING id`,
		userID, amount, paid, uuid.NewString()).Scan(&orderID))
	return orderID
}

func welfarePaymentApply(repo service.WelfarePaymentRepository, orderID int64, refund bool) error {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	tx, err := integrationEntClient.Tx(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	ctx = dbent.NewTxContext(ctx, tx)
	if refund {
		err = repo.ReverseSubscriptionPurchase(ctx, orderID)
	} else {
		err = repo.AccrueSubscriptionPurchase(ctx, orderID)
	}
	if err != nil {
		return err
	}
	return tx.Commit()
}

type welfarePaymentState struct {
	Draws, Used, Version, BalanceVersion, Rewards, Outbox, Wallets int64
	Spend, Balance                                                 string
}

func welfarePaymentStateFor(t *testing.T, userID int64) welfarePaymentState {
	t.Helper()
	var state welfarePaymentState
	require.NoError(t, integrationDB.QueryRow(`SELECT COALESCE(w.subscription_draws,0),COALESCE(w.draws_used,0),
		COALESCE(w.wallet_version,0),COALESCE(w.welfare_balance_version,0),COALESCE(w.eligible_spend,0)::numeric(20,8)::text,u.balance::text,
		(SELECT count(*) FROM welfare_subscription_rewards WHERE user_id=u.id),
		(SELECT count(*) FROM welfare_balance_outbox WHERE user_id=u.id),
		(SELECT count(*) FROM welfare_wallets WHERE user_id=u.id)
		FROM users u LEFT JOIN welfare_wallets w ON w.user_id=u.id WHERE u.id=$1`, userID).
		Scan(&state.Draws, &state.Used, &state.Version, &state.BalanceVersion, &state.Spend, &state.Balance, &state.Rewards, &state.Outbox, &state.Wallets))
	return state
}

func TestWelfarePaymentAccrualPerOrder(t *testing.T) {
	for _, tc := range []struct {
		paid  string
		draws int64
	}{{"0", 0}, {"25", 0}, {"49.99", 0}, {"50", 1}, {"75", 1}, {"99", 1}, {"99.99", 1}, {"100", 2}, {"200", 4}} {
		t.Run(tc.paid, func(t *testing.T) {
			repo, userID := welfarePaymentFixture(t)
			orderID := welfarePaymentOrder(t, userID, "7", tc.paid)
			require.NoError(t, welfarePaymentApply(repo, orderID, false))
			state := welfarePaymentStateFor(t, userID)
			require.Equal(t, tc.draws, state.Draws)
			require.Equal(t, "0.00000000", state.Spend)
			require.Equal(t, "200.00000000", state.Balance)
			require.Zero(t, state.BalanceVersion)
			require.Zero(t, state.Outbox)
			if tc.draws == 0 {
				require.Zero(t, state.Wallets)
				require.Zero(t, state.Rewards)
			} else {
				require.EqualValues(t, 1, state.Version)
				require.EqualValues(t, 1, state.Rewards)
				var paid string
				var granted int64
				require.NoError(t, integrationDB.QueryRow(`SELECT paid_amount_cny::text,granted_draws FROM welfare_subscription_rewards WHERE order_id=$1`, orderID).Scan(&paid, &granted))
				require.Equal(t, decimal.RequireFromString(tc.paid).StringFixed(2), paid)
				require.Equal(t, tc.draws, granted)
			}
		})
	}
}

func TestWelfarePaymentDoesNotCombineOrdersOrAPIConsumption(t *testing.T) {
	usage, userID, keyID := welfareSpendFixture(t)
	repo := NewWelfarePaymentRepository()
	_, err := usage.Apply(context.Background(), &service.UsageBillingCommand{RequestID: uuid.NewString(), UserID: userID, APIKeyID: keyID, BalanceCost: 49})
	require.NoError(t, err)
	for _, paid := range []string{"25", "25"} {
		require.NoError(t, welfarePaymentApply(repo, welfarePaymentOrder(t, userID, "50", paid), false))
	}
	state := welfarePaymentStateFor(t, userID)
	require.Zero(t, state.Draws)
	require.Zero(t, state.Rewards)
	require.EqualValues(t, 1, state.Version)
	for _, paid := range []string{"75", "75"} {
		require.NoError(t, welfarePaymentApply(repo, welfarePaymentOrder(t, userID, "50", paid), false))
	}
	state = welfarePaymentStateFor(t, userID)
	require.EqualValues(t, 2, state.Draws)
	require.EqualValues(t, 2, state.Rewards)
	require.EqualValues(t, 3, state.Version)
	require.Equal(t, "49.00000000", state.Spend)
	require.Equal(t, "151.00000000", state.Balance)
	_, err = usage.Apply(context.Background(), &service.UsageBillingCommand{RequestID: uuid.NewString(), UserID: userID, APIKeyID: keyID, BalanceCost: 1})
	require.NoError(t, err)
	state = welfarePaymentStateFor(t, userID)
	require.EqualValues(t, 2, state.Draws)
	require.Equal(t, "50.00000000", state.Spend)
	var earned int64
	require.NoError(t, integrationDB.QueryRow(`SELECT floor(eligible_spend/50)::bigint+subscription_draws FROM welfare_wallets WHERE user_id=$1`, userID).Scan(&earned))
	require.EqualValues(t, 3, earned)
}

func TestWelfarePaymentAccrualEligibility(t *testing.T) {
	for _, tc := range []struct{ name, update string }{
		{"never_launched", `UPDATE welfare_programs SET enabled=false,launch_at=NULL WHERE id=1`},
		{"enabled_without_launch", `UPDATE welfare_programs SET enabled=true,launch_at=NULL WHERE id=1`},
		{"paused", `UPDATE welfare_programs SET enabled=false WHERE id=1`},
		{"future_launch", `UPDATE welfare_programs SET launch_at=NOW()+interval '1 hour' WHERE id=1`},
		{"paid_before_launch", `UPDATE payment_orders SET paid_at=NOW()-interval '2 hours' WHERE id=$1`},
		{"unpaid", `UPDATE payment_orders SET paid_at=NULL WHERE id=$1`},
		{"processing", `UPDATE payment_orders SET status='PROCESSING' WHERE id=$1`},
		{"paid_only", `UPDATE payment_orders SET status='PAID' WHERE id=$1`},
		{"balance_recharge", `UPDATE payment_orders SET order_type='balance' WHERE id=$1`},
		{"foreign_currency", `UPDATE payment_orders SET provider_snapshot='{"currency":"USD"}' WHERE id=$1`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			repo, userID := welfarePaymentFixture(t)
			orderID := welfarePaymentOrder(t, userID, "200", "200")
			var err error
			if tc.name == "never_launched" || tc.name == "enabled_without_launch" || tc.name == "paused" || tc.name == "future_launch" {
				_, err = integrationDB.Exec(tc.update)
			} else {
				_, err = integrationDB.Exec(tc.update, orderID)
			}
			require.NoError(t, err)
			require.NoError(t, welfarePaymentApply(repo, orderID, false))
			state := welfarePaymentStateFor(t, userID)
			require.Zero(t, state.Wallets)
			require.Zero(t, state.Rewards)
			require.Equal(t, "200.00000000", state.Balance)
		})
	}
	t.Run("legacy_currency_is_cny", func(t *testing.T) {
		repo, userID := welfarePaymentFixture(t)
		orderID := welfarePaymentOrder(t, userID, "7", "200")
		_, err := integrationDB.Exec(`UPDATE payment_orders SET provider_snapshot=NULL WHERE id=$1`, orderID)
		require.NoError(t, err)
		require.NoError(t, welfarePaymentApply(repo, orderID, false))
		require.EqualValues(t, 4, welfarePaymentStateFor(t, userID).Draws)
	})
	t.Run("paid_exactly_at_launch", func(t *testing.T) {
		repo, userID := welfarePaymentFixture(t)
		orderID := welfarePaymentOrder(t, userID, "7", "50")
		_, err := integrationDB.Exec(`UPDATE payment_orders SET paid_at=(SELECT launch_at FROM welfare_programs WHERE id=1) WHERE id=$1`, orderID)
		require.NoError(t, err)
		require.NoError(t, welfarePaymentApply(repo, orderID, false))
		require.EqualValues(t, 1, welfarePaymentStateFor(t, userID).Draws)
	})
}

func TestWelfarePaymentConcurrentAccrual(t *testing.T) {
	for _, sameOrder := range []bool{true, false} {
		t.Run(fmt.Sprintf("same_order_%v", sameOrder), func(t *testing.T) {
			repo, userID := welfarePaymentFixture(t)
			const calls = 12
			orders := make([]int64, calls)
			orders[0] = welfarePaymentOrder(t, userID, "8", "75")
			for i := 1; i < calls; i++ {
				orders[i] = orders[0]
				if !sameOrder {
					orders[i] = welfarePaymentOrder(t, userID, "8", "75")
				}
			}
			var wg sync.WaitGroup
			errs := make(chan error, calls)
			start := make(chan struct{})
			for _, orderID := range orders {
				wg.Add(1)
				go func(orderID int64) {
					defer wg.Done()
					<-start
					errs <- welfarePaymentApply(repo, orderID, false)
				}(orderID)
			}
			close(start)
			wg.Wait()
			close(errs)
			for err := range errs {
				require.NoError(t, err)
			}
			want := int64(calls)
			if sameOrder {
				want = 1
			}
			state := welfarePaymentStateFor(t, userID)
			require.Equal(t, want, state.Draws)
			require.Equal(t, want, state.Rewards)
			require.Equal(t, want, state.Version)
		})
	}
}

func TestWelfarePaymentAccrualSnapshotConflict(t *testing.T) {
	for _, mutation := range []string{"pay_amount=pay_amount+1", "amount=amount+1", "provider_snapshot='{\"currency\":\"USD\"}'", "paid_at=paid_at-interval '1 second'"} {
		t.Run(mutation, func(t *testing.T) {
			repo, userID := welfarePaymentFixture(t)
			orderID := welfarePaymentOrder(t, userID, "20", "200")
			require.NoError(t, welfarePaymentApply(repo, orderID, false))
			before := welfarePaymentStateFor(t, userID)
			_, err := integrationDB.Exec(`UPDATE payment_orders SET `+mutation+` WHERE id=$1`, orderID)
			require.NoError(t, err)
			require.ErrorContains(t, welfarePaymentApply(repo, orderID, false), "conflict")
			require.Equal(t, before, welfarePaymentStateFor(t, userID))
		})
	}
}

func TestWelfarePaymentRequiresTransactionAndExistingOrder(t *testing.T) {
	repo, _ := welfarePaymentFixture(t)
	require.ErrorContains(t, repo.AccrueSubscriptionPurchase(context.Background(), 1), "transaction")
	require.ErrorContains(t, repo.ReverseSubscriptionPurchase(context.Background(), 1), "transaction")
	require.Error(t, welfarePaymentApply(repo, -1, false))
	require.Error(t, welfarePaymentApply(repo, -1, true))
}

func TestWelfarePaymentRollbackIncludesOrderGrantAndWallet(t *testing.T) {
	repo, userID := welfarePaymentFixture(t)
	orderID := welfarePaymentOrder(t, userID, "20", "200")
	_, err := integrationDB.Exec(`UPDATE payment_orders SET status='PAID',completed_at=NULL WHERE id=$1`, orderID)
	require.NoError(t, err)
	tx := testEntTx(t)
	ctx := dbent.NewTxContext(context.Background(), tx)
	_, err = tx.Client().ExecContext(ctx, `UPDATE payment_orders SET status='COMPLETED',completed_at=NOW() WHERE id=$1`, orderID)
	require.NoError(t, err)
	require.NoError(t, repo.AccrueSubscriptionPurchase(ctx, orderID))
	require.NoError(t, tx.Rollback())
	state := welfarePaymentStateFor(t, userID)
	require.Zero(t, state.Wallets)
	require.Zero(t, state.Rewards)
	var status string
	require.NoError(t, integrationDB.QueryRow(`SELECT status FROM payment_orders WHERE id=$1`, orderID).Scan(&status))
	require.Equal(t, "PAID", status)
}

func TestWelfarePaymentRefundRetainsNetPaidDrawsAndDebt(t *testing.T) {
	for _, tc := range []struct {
		name, amount, paid, refund, status, refunded string
		retained                                     int64
	}{
		{"partial", "200", "200", "60", "PARTIALLY_REFUNDED", "60.00", 2},
		{"proportional", "20", "200", "6", "PARTIALLY_REFUNDED", "60.00", 2},
		{"full", "20", "200", "20", "REFUNDED", "200.00", 0},
		{"round_half_cent", "30", "200", "7.5", "PARTIALLY_REFUNDED", "50.00", 3},
		{"round_to_cent", "6", "200", "1", "PARTIALLY_REFUNDED", "33.33", 3},
		{"within_full_tolerance", "40", "200", "39.99", "PARTIALLY_REFUNDED", "200.00", 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			repo, userID := welfarePaymentFixture(t)
			orderID := welfarePaymentOrder(t, userID, tc.amount, tc.paid)
			require.NoError(t, welfarePaymentApply(repo, orderID, false))
			_, err := integrationDB.Exec(`UPDATE welfare_wallets SET draws_used=4,balance_cents=200,total_earned_cents=200 WHERE user_id=$1`, userID)
			require.NoError(t, err)
			_, err = integrationDB.Exec(`UPDATE welfare_programs SET enabled=false WHERE id=1`)
			require.NoError(t, err)
			_, err = integrationDB.Exec(`UPDATE payment_orders SET status=$2,refund_amount=$3,refund_reason='provider confirmed refund',refund_at=NOW() WHERE id=$1`, orderID, tc.status, tc.refund)
			require.NoError(t, err)
			require.NoError(t, welfarePaymentApply(repo, orderID, true))
			state := welfarePaymentStateFor(t, userID)
			require.Equal(t, tc.retained, state.Draws)
			require.EqualValues(t, 4, state.Used)
			require.EqualValues(t, 2, state.Version)
			require.Zero(t, state.BalanceVersion)
			require.Zero(t, state.Outbox)
			require.Equal(t, "0.00000000", state.Spend)
			require.Equal(t, "200.00000000", state.Balance)
			var refunded string
			var reversed, rewardBalance, debt int64
			require.NoError(t, integrationDB.QueryRow(`SELECT refunded_amount_cny::text,reversed_draws FROM welfare_subscription_rewards WHERE order_id=$1`, orderID).Scan(&refunded, &reversed))
			require.Equal(t, tc.refunded, refunded)
			require.Equal(t, 4-tc.retained, reversed)
			require.NoError(t, integrationDB.QueryRow(`SELECT balance_cents,draws_used-subscription_draws-floor(eligible_spend/50)::bigint FROM welfare_wallets WHERE user_id=$1`, userID).Scan(&rewardBalance, &debt))
			require.EqualValues(t, 200, rewardBalance)
			require.Equal(t, 4-tc.retained, debt)
			require.NoError(t, welfarePaymentApply(repo, orderID, true))
			require.Equal(t, state, welfarePaymentStateFor(t, userID))
		})
	}
}

func TestWelfarePaymentRefundNoGrantOrUnconfirmedIsNoOp(t *testing.T) {
	for _, tc := range []struct {
		status string
		grant  bool
	}{{"REFUNDED", false}, {"PARTIALLY_REFUNDED", false}, {"COMPLETED", true}, {"REFUND_PENDING", true}, {"REFUND_FAILED", true}, {"REFUND_REQUESTED", true}} {
		t.Run(fmt.Sprintf("%s_grant_%v", tc.status, tc.grant), func(t *testing.T) {
			repo, userID := welfarePaymentFixture(t)
			orderID := welfarePaymentOrder(t, userID, "20", "200")
			if tc.grant {
				require.NoError(t, welfarePaymentApply(repo, orderID, false))
			}
			before := welfarePaymentStateFor(t, userID)
			_, err := integrationDB.Exec(`UPDATE payment_orders SET status=$2,refund_amount=20 WHERE id=$1`, orderID, tc.status)
			require.NoError(t, err)
			require.NoError(t, welfarePaymentApply(repo, orderID, true))
			require.Equal(t, before, welfarePaymentStateFor(t, userID))
		})
	}
}

func TestWelfarePaymentRefundConcurrentIdempotenceAndConflict(t *testing.T) {
	repo, userID := welfarePaymentFixture(t)
	orderID := welfarePaymentOrder(t, userID, "20", "200")
	require.NoError(t, welfarePaymentApply(repo, orderID, false))
	_, err := integrationDB.Exec(`UPDATE payment_orders SET status='PARTIALLY_REFUNDED',refund_amount=6 WHERE id=$1`, orderID)
	require.NoError(t, err)
	var wg sync.WaitGroup
	errs := make(chan error, 12)
	for i := 0; i < 12; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			errs <- welfarePaymentApply(repo, orderID, true)
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		require.NoError(t, err)
	}
	state := welfarePaymentStateFor(t, userID)
	require.EqualValues(t, 2, state.Draws)
	require.EqualValues(t, 2, state.Version)
	_, err = integrationDB.Exec(`UPDATE payment_orders SET refund_amount=7 WHERE id=$1`, orderID)
	require.NoError(t, err)
	require.ErrorContains(t, welfarePaymentApply(repo, orderID, true), "conflict")
	require.Equal(t, state, welfarePaymentStateFor(t, userID))
}

func TestWelfarePaymentRefundAfterUserSoftDeletion(t *testing.T) {
	for _, tc := range []struct {
		status, amount string
		retained       int64
	}{{"REFUNDED", "20", 0}, {"PARTIALLY_REFUNDED", "6", 2}} {
		t.Run(tc.status, func(t *testing.T) {
			repo, userID := welfarePaymentFixture(t)
			orderID := welfarePaymentOrder(t, userID, "20", "200")
			require.NoError(t, welfarePaymentApply(repo, orderID, false))
			before := welfarePaymentStateFor(t, userID)
			_, err := integrationDB.Exec(`UPDATE users SET deleted_at=NOW() WHERE id=$1`, userID)
			require.NoError(t, err)
			_, err = integrationDB.Exec(`UPDATE payment_orders SET status=$2,refund_amount=$3,refund_at=NOW() WHERE id=$1`, orderID, tc.status, tc.amount)
			require.NoError(t, err)
			require.NoError(t, welfarePaymentApply(repo, orderID, true))
			after := welfarePaymentStateFor(t, userID)
			require.Equal(t, tc.retained, after.Draws)
			require.Equal(t, before.Version+1, after.Version)
			require.Equal(t, before.Balance, after.Balance)
			require.Equal(t, before.Spend, after.Spend)
			require.Equal(t, before.BalanceVersion, after.BalanceVersion)
			require.Equal(t, before.Outbox, after.Outbox)
			require.Equal(t, before.Used, after.Used)
			require.NoError(t, welfarePaymentApply(repo, orderID, true))
			require.Equal(t, after, welfarePaymentStateFor(t, userID))
		})
	}
}

func TestWelfarePaymentAccrualRejectsSoftDeletedUser(t *testing.T) {
	repo, userID := welfarePaymentFixture(t)
	orderID := welfarePaymentOrder(t, userID, "20", "200")
	_, err := integrationDB.Exec(`UPDATE users SET deleted_at=NOW() WHERE id=$1`, userID)
	require.NoError(t, err)
	require.Error(t, welfarePaymentApply(repo, orderID, false))
	state := welfarePaymentStateFor(t, userID)
	require.Zero(t, state.Rewards)
	require.Zero(t, state.Wallets)
	require.Equal(t, "200.00000000", state.Balance)
}

func TestWelfarePaymentRefundRejectsInvalidAmountAndRollsBack(t *testing.T) {
	for _, amount := range []string{"0", "-1", "20.02"} {
		t.Run(amount, func(t *testing.T) {
			repo, userID := welfarePaymentFixture(t)
			orderID := welfarePaymentOrder(t, userID, "20", "200")
			require.NoError(t, welfarePaymentApply(repo, orderID, false))
			before := welfarePaymentStateFor(t, userID)
			tx := testEntTx(t)
			ctx := dbent.NewTxContext(context.Background(), tx)
			_, err := tx.Client().ExecContext(ctx, `UPDATE payment_orders SET status='REFUNDED',refund_amount=$2 WHERE id=$1`, orderID, amount)
			require.NoError(t, err)
			require.Error(t, repo.ReverseSubscriptionPurchase(ctx, orderID))
			require.NoError(t, tx.Rollback())
			require.Equal(t, before, welfarePaymentStateFor(t, userID))
		})
	}
	t.Run("caller_rollback", func(t *testing.T) {
		repo, userID := welfarePaymentFixture(t)
		orderID := welfarePaymentOrder(t, userID, "20", "200")
		require.NoError(t, welfarePaymentApply(repo, orderID, false))
		before := welfarePaymentStateFor(t, userID)
		tx := testEntTx(t)
		ctx := dbent.NewTxContext(context.Background(), tx)
		_, err := tx.Client().ExecContext(ctx, `UPDATE payment_orders SET status='REFUNDED',refund_amount=20 WHERE id=$1`, orderID)
		require.NoError(t, err)
		require.NoError(t, repo.ReverseSubscriptionPurchase(ctx, orderID))
		require.NoError(t, tx.Rollback())
		require.Equal(t, before, welfarePaymentStateFor(t, userID))
		var refundAt sql.NullTime
		require.NoError(t, integrationDB.QueryRow(`SELECT refund_processed_at FROM welfare_subscription_rewards WHERE order_id=$1`, orderID).Scan(&refundAt))
		require.False(t, refundAt.Valid)
	})
}

func TestWelfarePaymentMigrationPreservesUsageRefundAudit(t *testing.T) {
	_, userID := welfarePaymentFixture(t)
	_, err := integrationDB.Exec(`INSERT INTO welfare_spend_events(user_id,source_type,source_id,action,amount,refund_source_id,reason)
		VALUES($1,'usage',$2,'refund',-1,'original-charge','manual correction')`, userID, uuid.NewString())
	require.Error(t, err, "usage refund must still require an actor")
}
