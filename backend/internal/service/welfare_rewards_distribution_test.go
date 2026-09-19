package service

import (
	"errors"
	"fmt"
	"math/big"
	"testing"

	"github.com/stretchr/testify/require"
)

type welfareDailyOutcome struct {
	cents    int64
	nextLow  int
	surprise bool
}

type welfareDailyDistribution map[welfareDailyOutcome]*big.Rat

// Discover every random choice from the sampler itself, including the changing
// range of its final draw. A leaf has probability 1 / product(requested ranges),
// so equally counting leaves would incorrectly overweight the wider bands.
func enumerateWelfareDailyDistribution(t *testing.T, low int) welfareDailyDistribution {
	t.Helper()
	needChoice := errors.New("enumeration needs another random choice")
	counts := make(map[welfareDailyOutcome]map[int64]int64)
	var visit func([]int64, int64)
	visit = func(prefix []int64, denominator int64) {
		index, nextRange := 0, int64(0)
		cents, next, audit, err := sampleWelfareDaily(low, func(n int64) (int64, error) {
			// Keep a broken sampler from making exhaustive enumeration unbounded.
			if n <= 0 || n > 1000 {
				t.Fatalf("low=%d prefix=%v: invalid enumeration range %d", low, prefix, n)
			}
			if index == len(prefix) {
				nextRange = n
				return 0, needChoice
			}
			value := prefix[index]
			index++
			if value < 0 || value >= n {
				t.Fatalf("low=%d prefix=%v: choice %d outside [0,%d)", low, prefix, value, n)
			}
			return value, nil
		})
		if errors.Is(err, needChoice) {
			if len(prefix) >= 3 {
				t.Fatalf("low=%d prefix=%v: sampler requested more than three choices", low, prefix)
			}
			for choice := int64(0); choice < nextRange; choice++ {
				visit(append(prefix, choice), denominator*nextRange)
			}
			return
		}
		if err != nil || index != len(prefix) {
			t.Fatalf("low=%d prefix=%v: consumed=%d error=%v", low, prefix, index, err)
		}
		if next < 0 || next > 3 || audit.LowBefore != low || audit.LowAfter != next {
			t.Fatalf("low=%d prefix=%v: invalid next state %d or audit %+v", low, prefix, next, audit)
		}
		outcome := welfareDailyOutcome{cents: cents, nextLow: next, surprise: audit.Surprise}
		if counts[outcome] == nil {
			counts[outcome] = make(map[int64]int64)
		}
		counts[outcome][denominator]++
	}
	visit(nil, 1)

	// Aggregate equal denominators before constructing fractions: this preserves
	// exact probabilities without allocating a big.Rat for every enumerated leaf.
	law := make(welfareDailyDistribution, len(counts))
	for outcome, denominators := range counts {
		probability := new(big.Rat)
		for denominator, count := range denominators {
			probability.Add(probability, big.NewRat(count, denominator))
		}
		law[outcome] = probability
	}
	return law
}

func welfareDailyZeroState() [4]*big.Rat {
	return [4]*big.Rat{new(big.Rat), new(big.Rat), new(big.Rat), new(big.Rat)}
}

func TestWelfareDailyApprovedV2DistributionAndCost(t *testing.T) {
	var laws [4]welfareDailyDistribution
	for low := range laws {
		laws[low] = enumerateWelfareDailyDistribution(t, low)
	}

	t.Run("complete_joint_distribution", func(t *testing.T) {
		for low, surprisePercent := range []int64{20, 30, 50, 100} {
			t.Run(fmt.Sprintf("low_%d", low), func(t *testing.T) {
				// These are the approved probabilities, independent of the sampler's
				// roll thresholds. Check every amount, branch and resulting state.
				want := make(welfareDailyDistribution)
				if surprisePercent < 100 {
					for cents := int64(1); cents <= 9; cents++ {
						weight := int64(49)
						if cents == 1 {
							weight = 8
						}
						want[welfareDailyOutcome{cents: cents, nextLow: low + 1}] = big.NewRat((100-surprisePercent)*weight, 100*400)
					}
				}
				for _, band := range []struct{ min, max, weight int64 }{
					{10, 20, 65}, {21, 35, 25}, {36, 50, 9}, {51, 100, 1},
				} {
					for cents := band.min; cents <= band.max; cents++ {
						want[welfareDailyOutcome{cents: cents, nextLow: 0, surprise: true}] = big.NewRat(surprisePercent*band.weight, 100*100*(band.max-band.min+1))
					}
				}
				require.Len(t, laws[low], len(want), "unexpected amount, branch or state")
				total, surprise := new(big.Rat), new(big.Rat)
				for outcome, probability := range laws[low] {
					expected, ok := want[outcome]
					require.True(t, ok, "unexpected outcome %+v", outcome)
					require.Equal(t, expected.RatString(), probability.RatString(), "outcome %+v", outcome)
					total.Add(total, probability)
					if outcome.surprise {
						surprise.Add(surprise, probability)
					}
				}
				require.Equal(t, "1", total.RatString())
				require.Equal(t, big.NewRat(surprisePercent, 100).RatString(), surprise.RatString())
			})
		}
	})

	t.Run("conditional_amount_expectations", func(t *testing.T) {
		for _, branch := range []struct {
			name     string
			surprise bool
			wantUSD  *big.Rat
		}{
			{"ordinary", false, big.NewRat(541, 10000)},   // $0.0541
			{"surprise", true, big.NewRat(21375, 100000)}, // $0.21375
		} {
			t.Run(branch.name, func(t *testing.T) {
				for low, law := range laws {
					probability, weightedUSD := new(big.Rat), new(big.Rat)
					for outcome, mass := range law {
						if outcome.surprise == branch.surprise {
							probability.Add(probability, mass)
							weightedUSD.Add(weightedUSD, new(big.Rat).Mul(mass, big.NewRat(outcome.cents, 100)))
						}
					}
					if probability.Sign() == 0 {
						continue // The guaranteed-surprise state has no ordinary branch.
					}
					mean := new(big.Rat).Quo(weightedUSD, probability)
					require.Equal(t, branch.wantUSD.RatString(), mean.RatString(), "low=%d", low)
				}
			})
		}
	})

	t.Run("first_30_draws_from_low_zero", func(t *testing.T) {
		state := welfareDailyZeroState()
		state[0].SetInt64(1)
		surprises, rewardUSD := new(big.Rat), new(big.Rat)
		for day := 1; day <= 30; day++ {
			next := welfareDailyZeroState()
			for low, stateMass := range state {
				for outcome, probability := range laws[low] {
					mass := new(big.Rat).Mul(stateMass, probability)
					next[outcome.nextLow].Add(next[outcome.nextLow], mass)
					if outcome.surprise {
						surprises.Add(surprises, mass)
					}
					rewardUSD.Add(rewardUSD, new(big.Rat).Mul(mass, big.NewRat(outcome.cents, 100)))
				}
			}
			state = next
			total := new(big.Rat)
			for _, mass := range state {
				total.Add(total, mass)
			}
			require.Equal(t, "1", total.RatString(), "day %d must conserve probability", day)
		}
		surpriseMean, _ := surprises.Float64()
		rewardMean, _ := rewardUSD.Float64()
		require.InDelta(t, 11.1384311653, surpriseMean, 5e-11)
		require.InDelta(t, 3.4012505355, rewardMean, 5e-11)
	})

	t.Run("at_most_three_consecutive_ordinary_rewards", func(t *testing.T) {
		state := welfareDailyZeroState()
		state[0].SetInt64(1)
		for length := 1; length <= 4; length++ {
			next := welfareDailyZeroState()
			for low, stateMass := range state {
				for outcome, probability := range laws[low] {
					if !outcome.surprise {
						mass := new(big.Rat).Mul(stateMass, probability)
						next[outcome.nextLow].Add(next[outcome.nextLow], mass)
					}
				}
			}
			state = next
			total := new(big.Rat)
			for _, mass := range state {
				total.Add(total, mass)
			}
			if length <= 3 {
				require.Positive(t, total.Sign(), "an ordinary streak of length %d must be possible", length)
			} else {
				require.Zero(t, total.Sign(), "four consecutive ordinary rewards must be impossible")
			}
		}
	})

	t.Run("long_run_daily_cost", func(t *testing.T) {
		// A surprise returns to state zero and ends a renewal cycle. Compute the
		// expected cycle duration and payout backwards from the guaranteed draw,
		// using only transitions and amounts discovered from sampleWelfareDaily.
		duration, rewardUSD := welfareDailyZeroState(), welfareDailyZeroState()
		for low := len(laws) - 1; low >= 0; low-- {
			duration[low].SetInt64(1)
			for outcome, probability := range laws[low] {
				rewardUSD[low].Add(rewardUSD[low], new(big.Rat).Mul(probability, big.NewRat(outcome.cents, 100)))
				if outcome.surprise {
					require.Zero(t, outcome.nextLow, "a surprise must restart the renewal cycle")
					continue
				}
				require.Greater(t, outcome.nextLow, low, "ordinary draws must advance toward the guarantee")
				duration[low].Add(duration[low], new(big.Rat).Mul(probability, duration[outcome.nextLow]))
				rewardUSD[low].Add(rewardUSD[low], new(big.Rat).Mul(probability, rewardUSD[outcome.nextLow]))
			}
		}
		mean := new(big.Rat).Quo(rewardUSD[0], duration[0])
		meanUSD, _ := mean.Float64()
		require.InDelta(t, 0.11457348485, meanUSD, 5e-12)
	})
}
