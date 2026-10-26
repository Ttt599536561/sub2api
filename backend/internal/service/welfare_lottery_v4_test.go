package service

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"testing"
	"time"

	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/require"
)

func welfareLotteryV4Repo() (*welfarePublicContractRepo, time.Time) {
	now := time.Date(2026, 10, 27, 12, 0, 0, 0, welfareShanghai)
	launch := now.AddDate(0, -2, 0)
	return &welfarePublicContractRepo{state: WelfareState{
		Program: WelfareSettings{Enabled: true, LaunchAt: &launch, RulesVersion: 4}, AccountBalance: "12.34000000",
		Wallet: WelfareWallet{UserID: 42, BalanceCents: 12345, TotalEarnedCents: 13345, TotalRedeemedCents: 1000,
			EligibleSpend: "149.87654321", SubscriptionDraws: 2, DrawsUsed: 3,
			CycleID: 4, CycleDay: 14, TotalCheckinDays: 104, LastCheckinDate: "2026-10-26", DailyLowCount: 3,
			WalletVersion: 23, WelfareBalanceVersion: 19},
	}}, now
}

func TestWelfareLotteryV4PublicProbabilitiesMatchSampler(t *testing.T) {
	repo, now := welfareLotteryV4Repo()
	svc := NewWelfareService(repo, WithWelfareClock(func() time.Time { return now }))
	rules, err := svc.Rules(context.Background())
	require.NoError(t, err)
	requireWelfarePublicRules(t, rules)
	counts := make(map[int64]int64)
	for roll := int64(0); roll < 1000; roll++ {
		cents, err := sampleWelfareLottery(welfareSequence(roll))
		require.NoError(t, err)
		counts[cents]++
	}
	require.Len(t, rules.Prizes, len(counts))
	for _, prize := range rules.Prizes {
		cents, err := parseWelfareAmount(prize.Amount)
		require.NoError(t, err)
		require.Positive(t, counts[cents], "every public prize must be attainable")
		require.Equal(t, decimal.NewFromInt(counts[cents]).Div(decimal.NewFromInt(10)).String()+"%", prize.Probability)
	}
}

func TestWelfareLotteryV4CreditsExistingWalletAndReplaysOnce(t *testing.T) {
	for _, tc := range []struct {
		roll, cents int64
		amount      string
	}{
		{0, 1, "0.01"}, {450, 2, "0.02"}, {750, 5, "0.05"}, {930, 10, "0.10"},
		{980, 20, "0.20"}, {995, 50, "0.50"}, {999, 100, "1.00"},
	} {
		t.Run(tc.amount, func(t *testing.T) {
			repo, now := welfareLotteryV4Repo()
			before := repo.state
			calls := 0
			svc := NewWelfareService(repo, WithWelfareClock(func() time.Time { return now }), WithWelfareRandom(func(n int64) (int64, error) {
				calls++
				require.EqualValues(t, 1000, n)
				return tc.roll, nil
			}))
			result, err := svc.Draw(context.Background(), 42, "v4-draw")
			require.NoError(t, err)
			require.Equal(t, tc.amount, result.RewardAmount)
			require.Equal(t, strconv.FormatInt(tc.cents, 10), result.Prize.ID)
			require.Equal(t, tc.amount, result.Prize.Amount)
			require.Equal(t, "$"+tc.amount, result.Prize.Name)
			require.Equal(t, 4, result.Overview.RulesVersion)
			require.EqualValues(t, 4, result.Overview.DrawsUsed)
			require.Zero(t, result.Overview.AvailableDraws)
			require.Equal(t, welfareMoney(before.Wallet.BalanceCents+tc.cents), result.Overview.WelfareBalance)
			wantState := before
			wantState.Wallet.BalanceCents += tc.cents
			wantState.Wallet.TotalEarnedCents += tc.cents
			wantState.Wallet.DrawsUsed++
			wantState.Wallet.WalletVersion++
			wantState.Wallet.WelfareBalanceVersion++
			require.Equal(t, wantState, repo.state, "unrelated wallet and account state must be preserved")
			require.Equal(t, []WelfareLedgerEntry{{Type: "draw", AmountCents: tc.cents, BalanceAfterCents: wantState.Wallet.BalanceCents}}, repo.mutation.Entries)
			require.Equal(t, map[string]any{"rules_version": "v4", "amount_cents": tc.cents, "uniform_roll": tc.roll}, repo.mutation.PrivateAudit)
			require.Equal(t, "draw:v2", repo.fingerprint, "the request fingerprint is independent of payout rules")
			require.False(t, repo.mutation.Checkin)
			require.Zero(t, repo.mutation.TransferCents)
			require.Equal(t, "2026-10-27", repo.mutation.BusinessDate)
			require.NotContains(t, string(repo.snapshot), "uniform_roll")
			require.NotContains(t, string(repo.snapshot), "amount_cents")

			saved, err := repo.savedOperation()
			require.NoError(t, err)
			result.RewardAmount = "caller changed response"
			repo.state.Program.Enabled = false
			replayState := repo.state
			replayed, err := svc.Draw(context.Background(), 42, "v4-draw")
			require.NoError(t, err)
			require.Equal(t, saved, replayed)
			require.Equal(t, replayState, repo.state)
			require.Equal(t, 1, repo.applyCalls)
			require.Equal(t, 1, calls)
		})
	}
}

func TestWelfareLotteryV4FailurePreservesWalletAndCanRetry(t *testing.T) {
	entropyErr := errors.New("entropy unavailable")
	for _, tc := range []struct {
		name string
		err  error
	}{
		{"no_tickets", ErrWelfareNoTickets}, {"random_failure", entropyErr},
	} {
		t.Run(tc.name, func(t *testing.T) {
			repo, now := welfareLotteryV4Repo()
			if tc.name == "no_tickets" {
				repo.state.Wallet.DrawsUsed = 4
			}
			before := repo.state
			calls, fail := 0, true
			svc := NewWelfareService(repo, WithWelfareClock(func() time.Time { return now }), WithWelfareRandom(func(int64) (int64, error) {
				calls++
				if fail {
					return 0, entropyErr
				}
				return 999, nil
			}))
			result, err := svc.Draw(context.Background(), 42, "retry-draw")
			require.ErrorIs(t, err, tc.err)
			require.Nil(t, result)
			require.Equal(t, before, repo.state)
			require.Nil(t, repo.mutation)
			require.Nil(t, repo.snapshot)
			if tc.name == "no_tickets" {
				require.Zero(t, calls, "no ticket must reject before sampling")
				repo.state.Wallet.SubscriptionDraws++
			} else {
				require.Equal(t, 1, calls)
			}
			fail = false
			result, err = svc.Draw(context.Background(), 42, "retry-draw")
			require.NoError(t, err)
			require.Equal(t, "1.00", result.RewardAmount)
			require.Equal(t, before.Wallet.BalanceCents+100, repo.state.Wallet.BalanceCents)
			require.Equal(t, before.Wallet.DrawsUsed+1, repo.state.Wallet.DrawsUsed)
		})
	}
}

func TestWelfareLotteryHistoricalV2V3DrawReplaysSavedHighPrize(t *testing.T) {
	for _, version := range []int{2, 3} {
		t.Run(fmt.Sprintf("public_v%d", version), func(t *testing.T) {
			repo, now := welfareLotteryV4Repo()
			repo.kind, repo.key, repo.fingerprint = "draw", "historical-draw", "draw:v2"
			repo.operationID = "b5ec3d11-4c19-4d9e-9a54-53aef6a1add8"
			repo.snapshot = []byte(fmt.Sprintf(`{"operation_id":"b5ec3d11-4c19-4d9e-9a54-53aef6a1add8","status":"completed","overview":{"welfare_balance":"173.45","account_balance":"12.34000000","rules_version":%d,"draws_used":4},"reward_amount":"50.00","prize":{"id":"5000","name":"$50.00","amount":"50.00","probability":"0.1%%"}}`, version))
			repo.state.Program.Enabled = false
			repo.state.Wallet.DrawsUsed = 4
			before, snapshot := repo.state, string(repo.snapshot)
			want, err := repo.savedOperation()
			require.NoError(t, err)
			svc := NewWelfareService(repo, WithWelfareClock(func() time.Time { return now }), WithWelfareRandom(func(int64) (int64, error) {
				t.Fatal("historical draws must never sample v4 rewards")
				return 0, nil
			}))
			replayed, err := svc.Draw(context.Background(), 42, "historical-draw")
			require.NoError(t, err)
			require.Equal(t, want, replayed)
			require.Equal(t, version, replayed.Overview.RulesVersion)
			require.Equal(t, "50.00", replayed.RewardAmount)
			stored, err := svc.Operation(context.Background(), 42, repo.operationID)
			require.NoError(t, err)
			require.Equal(t, want, stored)
			require.Equal(t, before, repo.state)
			require.Equal(t, snapshot, string(repo.snapshot))
			require.Zero(t, repo.applyCalls)
		})
	}
}
