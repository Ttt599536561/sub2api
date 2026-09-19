//go:build integration

package repository

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func welfareFixture(t *testing.T) (*service.WelfareService, int64) {
	t.Helper()
	u := mustCreateUser(t, testEntClient(t), &service.User{Email: fmt.Sprintf("welfare-%d@example.com", time.Now().UnixNano()), Balance: 5.12345678})
	repo := NewWelfareRepository(integrationDB)
	_, err := repo.UpdateSettings(context.Background(), true)
	require.NoError(t, err)
	s := service.NewWelfareService(repo, service.WithWelfareClock(func() time.Time { return time.Now() }), service.WithWelfareRandom(func(int64) (int64, error) { return 0, nil }))
	return s, u.ID
}

func TestWelfareRepositoryConcurrentCheckinReplaysExactly(t *testing.T) {
	s, id := welfareFixture(t)
	ctx := context.Background()
	const concurrency = 20
	results := make(chan *service.WelfareOperation, concurrency)
	errs := make(chan error, concurrency)
	var wg sync.WaitGroup
	for i := 0; i < concurrency; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); result, err := s.CheckIn(ctx, id); results <- result; errs <- err }()
	}
	wg.Wait()
	close(results)
	close(errs)
	for err := range errs {
		require.NoError(t, err)
	}
	first := ""
	for result := range results {
		if first == "" {
			first = result.OperationID
		}
		require.Equal(t, first, result.OperationID)
		require.Equal(t, "0.10", result.RewardAmount)
	}
	overview, err := s.Overview(ctx, id)
	require.NoError(t, err)
	require.EqualValues(t, 1, overview.TotalCheckinDays)
	require.Equal(t, "0.10", overview.WelfareBalance)
	var ledger, checkins int
	require.NoError(t, integrationDB.QueryRow(`SELECT count(*) FROM welfare_ledger WHERE user_id=$1`, id).Scan(&ledger))
	require.Equal(t, 1, ledger)
	require.NoError(t, integrationDB.QueryRow(`SELECT count(*) FROM welfare_checkins WHERE user_id=$1`, id).Scan(&checkins))
	require.Equal(t, 1, checkins)
}

func TestWelfareRepositoryRedeemAtomicReplayVersionAndPausedProgram(t *testing.T) {
	s, id := welfareFixture(t)
	ctx := context.Background()
	_, err := s.CheckIn(ctx, id)
	require.NoError(t, err)
	q, err := s.Quote(ctx, id, "all", "")
	require.NoError(t, err)
	require.Equal(t, "5.22345678", q.AccountBalanceAfter)
	// Usage changes the general wallet version, not the quoted welfare balance version.
	_, err = integrationDB.Exec(`UPDATE welfare_wallets SET eligible_spend=100,wallet_version=wallet_version+1 WHERE user_id=$1`, id)
	require.NoError(t, err)
	_, err = s.UpdateSettings(ctx, false)
	require.NoError(t, err)
	result, err := s.Redeem(ctx, id, "redeem-1", q.Amount, q.WelfareBalanceVersion)
	require.NoError(t, err)
	replay, err := s.Redeem(ctx, id, "redeem-1", q.Amount, q.WelfareBalanceVersion)
	require.NoError(t, err)
	resultJSON, err := json.Marshal(result)
	require.NoError(t, err)
	replayJSON, err := json.Marshal(replay)
	require.NoError(t, err)
	require.JSONEq(t, string(resultJSON), string(replayJSON))
	_, err = s.Redeem(ctx, id, "redeem-1", "0.01", q.WelfareBalanceVersion)
	require.ErrorIs(t, err, service.ErrWelfareIdempotencyConflict)
	_, err = s.Redeem(ctx, id, "redeem-2", q.Amount, q.WelfareBalanceVersion)
	require.ErrorIs(t, err, service.ErrWelfareQuoteStale)
	var balance string
	var outbox, ledger int
	require.NoError(t, integrationDB.QueryRow(`SELECT balance::text FROM users WHERE id=$1`, id).Scan(&balance))
	require.Equal(t, "5.22345678", balance)
	require.NoError(t, integrationDB.QueryRow(`SELECT count(*) FROM welfare_balance_outbox WHERE user_id=$1`, id).Scan(&outbox))
	require.Equal(t, 1, outbox)
	require.NoError(t, integrationDB.QueryRow(`SELECT count(*) FROM welfare_ledger WHERE user_id=$1 AND type='redeem'`, id).Scan(&ledger))
	require.Equal(t, 1, ledger)
	_, err = s.Draw(ctx, id, "paused-draw")
	require.ErrorIs(t, err, service.ErrWelfarePaused)
}

func TestWelfareRepositoryDrawSerializesLastTicketAndImmutableLedger(t *testing.T) {
	s, id := welfareFixture(t)
	ctx := context.Background()
	_, err := s.Overview(ctx, id)
	require.NoError(t, err)
	_, err = integrationDB.Exec(`INSERT INTO welfare_wallets(user_id,eligible_spend) VALUES($1,50)`, id)
	require.NoError(t, err)
	var wg sync.WaitGroup
	errs := make(chan error, 2)
	for _, key := range []string{"draw-a", "draw-b"} {
		wg.Add(1)
		go func(key string) { defer wg.Done(); _, err := s.Draw(ctx, id, key); errs <- err }(key)
	}
	wg.Wait()
	close(errs)
	ok, failed := 0, 0
	for err := range errs {
		if err == nil {
			ok++
		} else {
			require.ErrorIs(t, err, service.ErrWelfareNoTickets)
			failed++
		}
	}
	require.Equal(t, 1, ok)
	require.Equal(t, 1, failed)
	overview, err := s.Overview(ctx, id)
	require.NoError(t, err)
	require.Equal(t, "0.50", overview.WelfareBalance)
	require.EqualValues(t, 1, overview.DrawsUsed)
	_, err = integrationDB.Exec(`UPDATE welfare_ledger SET amount_cents=999 WHERE user_id=$1`, id)
	require.Error(t, err, "financial ledger must be immutable")
	_, err = integrationDB.Exec(`UPDATE welfare_wallets SET eligible_spend=0 WHERE user_id=$1`, id)
	require.NoError(t, err)
	overview, err = s.Overview(ctx, id)
	require.NoError(t, err)
	require.EqualValues(t, 1, overview.TicketDebt)
	_, err = s.Draw(ctx, id, "draw-c")
	require.ErrorIs(t, err, service.ErrWelfareNoTickets)
}

func TestWelfareRepositoryOverviewDoesNotCreateWalletOrWaitForBillingLock(t *testing.T) {
	s, id := welfareFixture(t)
	tx, err := integrationDB.Begin()
	require.NoError(t, err)
	defer func() { _ = tx.Rollback() }()
	_, err = tx.Exec(`SELECT id FROM users WHERE id=$1 FOR UPDATE`, id)
	require.NoError(t, err)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	overview, err := s.Overview(ctx, id)
	require.NoError(t, err)
	require.Equal(t, "0.00", overview.WelfareBalance)
	var wallets int
	require.NoError(t, integrationDB.QueryRow(`SELECT count(*) FROM welfare_wallets WHERE user_id=$1`, id).Scan(&wallets))
	require.Zero(t, wallets)
}

func TestWelfareRepositoryMilestoneCalendarLedgerAndOperationOwnership(t *testing.T) {
	s, id := welfareFixture(t)
	ctx := context.Background()
	_, err := integrationDB.Exec(`INSERT INTO welfare_wallets(user_id,total_checkin_days,cycle_id,cycle_day,last_checkin_date,daily_low_count) VALUES($1,6,1,6,(NOW() AT TIME ZONE 'Asia/Shanghai')::date-1,3)`, id)
	require.NoError(t, err)
	result, err := s.CheckIn(ctx, id)
	require.NoError(t, err)
	require.Equal(t, "0.10", result.BaseRewardAmount)
	require.Equal(t, "0.60", result.StreakRewardAmount)
	require.Equal(t, "0.70", result.RewardAmount)
	calendar, err := s.Calendar(ctx, id, result.Overview.BusinessDate[:7])
	require.NoError(t, err)
	var checked int
	for _, day := range calendar.Days {
		if day.CheckedIn {
			checked++
			require.Equal(t, "0.10", day.RewardAmount)
		}
	}
	require.Equal(t, 1, checked)
	records, err := s.Records(ctx, id, service.WelfareRecordFilter{Type: "all", DateFrom: result.Overview.BusinessDate, DateTo: result.Overview.BusinessDate, PageSize: 1})
	require.NoError(t, err)
	require.EqualValues(t, 2, records.Total)
	require.Len(t, records.Items, 1)
	require.Equal(t, "streak", records.Items[0].Type)
	require.Equal(t, "0.60", records.Items[0].Amount)
	require.Equal(t, "0.70", records.Items[0].BalanceAfter)
	op, err := s.Operation(ctx, id, result.OperationID)
	require.NoError(t, err)
	require.Equal(t, result.OperationID, op.OperationID)
	_, err = s.Operation(ctx, id+1, result.OperationID)
	require.ErrorIs(t, err, service.ErrWelfareOperationNotFound)
	var low int
	require.NoError(t, integrationDB.QueryRow(`SELECT daily_low_count FROM welfare_wallets WHERE user_id=$1`, id).Scan(&low))
	require.Zero(t, low)
}

func TestWelfareRepositoryEntropyFailureRollsBackAndRewardInvalidatesQuote(t *testing.T) {
	s, id := welfareFixture(t)
	ctx := context.Background()
	errEntropy := errors.New("entropy unavailable")
	broken := service.NewWelfareService(NewWelfareRepository(integrationDB), service.WithWelfareRandom(func(int64) (int64, error) { return 0, errEntropy }))
	_, err := broken.CheckIn(ctx, id)
	require.ErrorIs(t, err, errEntropy)
	var wallets int
	require.NoError(t, integrationDB.QueryRow(`SELECT count(*) FROM welfare_wallets WHERE user_id=$1`, id).Scan(&wallets))
	require.Zero(t, wallets)
	_, err = s.CheckIn(ctx, id)
	require.NoError(t, err)
	q, err := s.Quote(ctx, id, "partial", "0.01")
	require.NoError(t, err)
	_, err = integrationDB.Exec(`UPDATE welfare_wallets SET eligible_spend=50 WHERE user_id=$1`, id)
	require.NoError(t, err)
	_, err = s.Draw(ctx, id, "new-prize")
	require.NoError(t, err)
	_, err = s.Redeem(ctx, id, "stale-quote", q.Amount, q.WelfareBalanceVersion)
	require.ErrorIs(t, err, service.ErrWelfareQuoteStale)
	var count int
	require.NoError(t, integrationDB.QueryRow(`SELECT count(*) FROM welfare_operations WHERE user_id=$1 AND type='redeem'`, id).Scan(&count))
	require.Zero(t, count)
}

func TestWelfareRepositoryConcurrentRedemptionsCannotDoubleCredit(t *testing.T) {
	s, id := welfareFixture(t)
	ctx := context.Background()
	_, err := s.CheckIn(ctx, id)
	require.NoError(t, err)
	q, err := s.Quote(ctx, id, "all", "")
	require.NoError(t, err)
	var wg sync.WaitGroup
	errs := make(chan error, 12)
	for i := 0; i < 12; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, err := s.Redeem(ctx, id, fmt.Sprintf("redeem-%d", i), q.Amount, q.WelfareBalanceVersion)
			errs <- err
		}(i)
	}
	wg.Wait()
	close(errs)
	var won int
	for err := range errs {
		if err == nil {
			won++
		} else {
			require.ErrorIs(t, err, service.ErrWelfareQuoteStale)
		}
	}
	require.Equal(t, 1, won)
	overview, err := s.Overview(ctx, id)
	require.NoError(t, err)
	require.Equal(t, "0.00", overview.WelfareBalance)
	require.Equal(t, q.AccountBalanceAfter, overview.AccountBalance)
}

func TestWelfareRepositoryProgramLaunchTimestampPersistsAcrossPause(t *testing.T) {
	repo := NewWelfareRepository(integrationDB)
	ctx := context.Background()
	first, err := repo.UpdateSettings(ctx, true)
	require.NoError(t, err)
	require.NotNil(t, first.LaunchAt)
	paused, err := repo.UpdateSettings(ctx, false)
	require.NoError(t, err)
	require.False(t, paused.Enabled)
	require.True(t, first.LaunchAt.Equal(*paused.LaunchAt))
	resumed, err := repo.UpdateSettings(ctx, true)
	require.NoError(t, err)
	require.True(t, resumed.Enabled)
	require.True(t, first.LaunchAt.Equal(*resumed.LaunchAt))
}
