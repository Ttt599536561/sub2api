package service

import (
	"context"
	"errors"
	"fmt"
	"math/big"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func welfareV3CheckinRepo(day int64) (*welfarePublicContractRepo, time.Time) {
	now := time.Date(2026, 10, 26, 12, 0, 0, 0, welfareShanghai)
	launch := now.AddDate(0, -2, 0)
	return &welfarePublicContractRepo{state: WelfareState{
		Program: WelfareSettings{Enabled: true, LaunchAt: &launch}, AccountBalance: "12.34000000",
		Wallet: WelfareWallet{UserID: 42, CycleID: 1, CycleDay: day - 1, TotalCheckinDays: day - 1, LastCheckinDate: "2026-10-25", EligibleSpend: "0.00000000"},
	}}, now
}

// Enumerate every milestone draw through the complete service mutation. Pin the
// daily reward to an ordinary outcome, then discover each milestone's own random
// ranges; count leaves with their exact probability rather than equally.
func enumerateWelfareV3Streak(t *testing.T, day int64) map[int64]*big.Rat {
	t.Helper()
	law := map[int64]*big.Rat{}
	needChoice := errors.New("enumeration requires next choice")
	var visit func([]int64, int64)
	visit = func(prefix []int64, denominator int64) {
		repo, now := welfareV3CheckinRepo(day)
		index, nextRange := 0, int64(0)
		svc := NewWelfareService(repo, WithWelfareClock(func() time.Time { return now }), WithWelfareRandom(func(n int64) (int64, error) {
			index++
			if index <= 2 {
				return 99, nil
			}
			if index-3 == len(prefix) {
				nextRange = n
				return 0, needChoice
			}
			require.Greater(t, n, prefix[index-3])
			return prefix[index-3], nil
		}))
		result, err := svc.CheckIn(context.Background(), 42)
		if errors.Is(err, needChoice) {
			require.Less(t, len(prefix), 2)
			require.Positive(t, nextRange)
			require.LessOrEqual(t, nextRange, int64(100))
			for roll := int64(0); roll < nextRange; roll++ {
				visit(append(prefix, roll), denominator*nextRange)
			}
			return
		}
		require.NoError(t, err)
		cents := new(big.Rat)
		_, ok := cents.SetString(result.StreakRewardAmount)
		require.True(t, ok)
		cents.Mul(cents, big.NewRat(100, 1))
		require.True(t, cents.IsInt())
		amount := cents.Num().Int64()
		if law[amount] == nil {
			law[amount] = new(big.Rat)
		}
		law[amount].Add(law[amount], big.NewRat(1, denominator))
	}
	visit(nil, 1)
	return law
}

func TestWelfareV3MilestoneDistributionsAndFirstCycleCost(t *testing.T) {
	totalExpected := new(big.Rat)
	for _, tc := range []struct {
		day   int64
		bands []struct{ min, max, percent int64 }
	}{
		{7, []struct{ min, max, percent int64 }{{3, 8, 100}}},
		{15, []struct{ min, max, percent int64 }{{8, 20, 100}}},
		{30, []struct{ min, max, percent int64 }{{20, 50, 80}, {51, 100, 18}, {101, 195, 2}}},
	} {
		t.Run(fmt.Sprintf("day_%d", tc.day), func(t *testing.T) {
			law := enumerateWelfareV3Streak(t, tc.day)
			want := map[int64]*big.Rat{}
			for _, band := range tc.bands {
				for amount := band.min; amount <= band.max; amount++ {
					want[amount] = big.NewRat(band.percent, 100*(band.max-band.min+1))
				}
			}
			require.Len(t, law, len(want))
			probability := new(big.Rat)
			for amount, mass := range law {
				require.Contains(t, want, amount)
				require.Equal(t, want[amount].RatString(), mass.RatString(), "amount %d", amount)
				probability.Add(probability, mass)
				totalExpected.Add(totalExpected, new(big.Rat).Mul(mass, big.NewRat(amount, 100)))
				if tc.day == 30 {
					require.LessOrEqual(t, amount+5, int64(200), "even the maximum daily reward respects the $2 cap")
				}
			}
			require.Equal(t, "1", probability.RatString())
		})
	}
	require.Equal(t, "1281/2000", totalExpected.RatString()) // $0.6405
	// Independently compute daily cost from the exact sampler law and combine it
	// with the milestone expectation for a new wallet's first complete cycle.
	state := welfareDailyZeroState()
	state[0].SetInt64(1)
	var laws [4]welfareDailyDistribution
	for low := range laws {
		laws[low] = enumerateWelfareDailyDistribution(t, low)
	}
	for day := 0; day < 30; day++ {
		next := welfareDailyZeroState()
		for low, stateMass := range state {
			for outcome, probability := range laws[low] {
				mass := new(big.Rat).Mul(stateMass, probability)
				next[outcome.nextLow].Add(next[outcome.nextLow], mass)
				totalExpected.Add(totalExpected, new(big.Rat).Mul(mass, big.NewRat(outcome.cents, 100)))
			}
		}
		state = next
	}
	mean, _ := totalExpected.Float64()
	require.InDelta(t, 1.258837838889, mean, 5e-13)
}

func TestWelfareV3NonMilestoneNeverSamplesStreak(t *testing.T) {
	for _, day := range []int64{1, 6, 8, 14, 16, 29} {
		t.Run(fmt.Sprintf("day_%d", day), func(t *testing.T) {
			repo, now := welfareV3CheckinRepo(day)
			calls := 0
			svc := NewWelfareService(repo, WithWelfareClock(func() time.Time { return now }), WithWelfareRandom(func(n int64) (int64, error) {
				calls++
				require.EqualValues(t, 100, n)
				require.LessOrEqual(t, calls, 2)
				return 99, nil
			}))
			result, err := svc.CheckIn(context.Background(), 42)
			require.NoError(t, err)
			require.Equal(t, "0.00", result.StreakRewardAmount)
			require.Equal(t, 2, calls)
		})
	}
}

func TestWelfareV3MilestoneBoundariesAndPrivateAudit(t *testing.T) {
	for _, tc := range []struct {
		day, amount int64
		rolls       []int64
		ranges      []int64
	}{
		{7, 3, []int64{0}, []int64{6}}, {7, 8, []int64{5}, []int64{6}},
		{15, 8, []int64{0}, []int64{13}}, {15, 20, []int64{12}, []int64{13}},
		{30, 20, []int64{0, 0}, []int64{100, 31}},
		{30, 50, []int64{79, 30}, []int64{100, 31}},
		{30, 51, []int64{80, 0}, []int64{100, 50}},
		{30, 100, []int64{97, 49}, []int64{100, 50}},
		{30, 101, []int64{98, 0}, []int64{100, 95}},
		{30, 195, []int64{99, 94}, []int64{100, 95}},
	} {
		t.Run(fmt.Sprintf("day_%d_amount_%d", tc.day, tc.amount), func(t *testing.T) {
			index := 0
			amount, audit, err := sampleWelfareStreak(tc.day, func(n int64) (int64, error) {
				require.Less(t, index, len(tc.rolls))
				require.Equal(t, tc.ranges[index], n)
				roll := tc.rolls[index]
				index++
				return roll, nil
			})
			require.NoError(t, err)
			require.Equal(t, len(tc.rolls), index)
			require.Equal(t, tc.amount, amount)
			require.Equal(t, amount, audit.AmountCents)
			require.Equal(t, tc.day, audit.Day)
			require.Equal(t, tc.rolls[len(tc.rolls)-1], audit.AmountRoll)
			require.Equal(t, audit.BandMinCents+audit.AmountRoll, amount)
			require.Equal(t, tc.ranges[len(tc.ranges)-1], audit.BandMaxCents-audit.BandMinCents+1)
			if tc.day == 30 {
				require.NotNil(t, audit.BranchRoll)
				require.Equal(t, tc.rolls[0], *audit.BranchRoll)
				object := welfarePublicContractObject(t, audit)
				require.Equal(t, float64(tc.rolls[0]), object["branch_roll"], "zero is a real band sample, not an omitted input")
			} else {
				require.Nil(t, audit.BranchRoll)
			}
		})
	}
}

func TestWelfareV3EveryRandomFailureAbortsMutationAndCanRetry(t *testing.T) {
	for _, day := range []int64{1, 7, 15, 30} {
		stages := 2
		if day == 7 || day == 15 {
			stages = 3
		} else if day == 30 {
			stages = 4
		}
		for stage := 1; stage <= stages; stage++ {
			t.Run(fmt.Sprintf("day_%d_stage_%d", day, stage), func(t *testing.T) {
				repo, now := welfareV3CheckinRepo(day)
				repo.state.Wallet.DailyLowCount = 2
				before := repo.state
				failure := errors.New("entropy unavailable")
				calls := 0
				svc := NewWelfareService(repo, WithWelfareClock(func() time.Time { return now }), WithWelfareRandom(func(int64) (int64, error) {
					calls++
					if calls == stage {
						return 0, failure
					}
					return 0, nil
				}))
				_, err := svc.CheckIn(context.Background(), 42)
				require.ErrorIs(t, err, failure)
				require.Equal(t, before, repo.state)
				require.Nil(t, repo.mutation)
				require.Empty(t, repo.snapshot)
				svc.random = func(int64) (int64, error) { return 0, nil }
				_, err = svc.CheckIn(context.Background(), 42)
				require.NoError(t, err)
				require.Equal(t, day, repo.state.Wallet.CycleDay)
				require.Zero(t, repo.state.Wallet.DailyLowCount)
			})
		}
	}
}
