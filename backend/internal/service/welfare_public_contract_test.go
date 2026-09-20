package service

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// This repository only supplies state and a serialized committed response for
// public-contract tests. Database integration tests own financial atomicity and
// concurrent idempotency; no persistence guarantees are inferred from this fake.
type welfarePublicContractRepo struct {
	WelfareRepository
	state       WelfareState
	days        []WelfareCalendarDay
	mutation    *WelfareMutation
	applyCalls  int
	snapshot    []byte
	kind        string
	key         string
	fingerprint string
	operationID string
}

func (r *welfarePublicContractRepo) State(context.Context, int64) (*WelfareState, error) {
	state := r.state
	return &state, nil
}

func (r *welfarePublicContractRepo) GetSettings(context.Context) (*WelfareSettings, error) {
	settings := r.state.Program
	return &settings, nil
}

func (r *welfarePublicContractRepo) Calendar(_ context.Context, _ int64, month string) ([]WelfareCalendarDay, error) {
	var days []WelfareCalendarDay
	for _, day := range r.days {
		if strings.HasPrefix(day.Date, month+"-") {
			days = append(days, day)
		}
	}
	return days, nil
}

func (r *welfarePublicContractRepo) savedOperation() (*WelfareOperation, error) {
	var operation WelfareOperation
	if err := json.Unmarshal(r.snapshot, &operation); err != nil {
		return nil, err
	}
	return &operation, nil
}

func (r *welfarePublicContractRepo) Operation(_ context.Context, _ int64, id string) (*WelfareOperation, error) {
	if id != r.operationID || r.snapshot == nil {
		return nil, ErrWelfareOperationNotFound
	}
	return r.savedOperation()
}

func (r *welfarePublicContractRepo) Mutate(_ context.Context, _ int64, kind, key, fingerprint string, apply func(*WelfareState) (*WelfareMutation, error)) (*WelfareOperation, error) {
	if r.snapshot != nil && kind == r.kind && key == r.key {
		if fingerprint != r.fingerprint {
			return nil, ErrWelfareIdempotencyConflict
		}
		return r.savedOperation()
	}
	r.applyCalls++
	state := r.state
	mutation, err := apply(&state)
	if err != nil {
		return nil, err
	}
	mutation.Result.OperationID = "eb6c9526-d3c5-4a39-928f-4b48a8f3d6e7"
	snapshot, err := json.Marshal(mutation.Result)
	if err != nil {
		return nil, err
	}
	r.state, r.mutation, r.snapshot = state, mutation, snapshot
	r.kind, r.key, r.fingerprint, r.operationID = kind, key, fingerprint, mutation.Result.OperationID
	if mutation.Checkin {
		for _, entry := range mutation.Entries {
			if entry.Type == "daily" {
				r.days = append(r.days, WelfareCalendarDay{Date: mutation.BusinessDate, CheckedIn: true, RewardAmount: welfareMoney(entry.AmountCents)})
			}
		}
	}
	return mutation.Result, nil
}

func welfarePublicContractObject(t *testing.T, value any) map[string]any {
	t.Helper()
	raw, err := json.Marshal(value)
	require.NoError(t, err)
	var object map[string]any
	require.NoError(t, json.Unmarshal(raw, &object))
	require.NotNil(t, object)
	return object
}

func requireWelfarePublicKeys(t *testing.T, object map[string]any, allowed ...string) {
	t.Helper()
	keys := make([]string, 0, len(object))
	for key := range object {
		keys = append(keys, key)
	}
	require.ElementsMatch(t, allowed, keys, "public JSON must contain exactly its approved fields")
}

func requireWelfarePublicOverview(t *testing.T, object map[string]any, statuses []string) {
	t.Helper()
	requireWelfarePublicKeys(t, object,
		"welfare_balance", "account_balance", "available_draws", "draws_used", "total_checkin_days", "cycle_day",
		"today_checked_in", "business_date", "next_reset_at", "eligible_spend", "subscription_draws", "next_draw_remaining", "ticket_debt",
		"wallet_version", "welfare_balance_version", "rewards_enabled", "rules_version", "milestones")
	milestones, ok := object["milestones"].([]any)
	require.True(t, ok)
	require.Len(t, milestones, 3)
	for i, day := range []float64{7, 15, 30} {
		milestone, ok := milestones[i].(map[string]any)
		require.True(t, ok)
		requireWelfarePublicKeys(t, milestone, "day", "status")
		require.Equal(t, day, milestone["day"])
		require.Equal(t, statuses[i], milestone["status"])
	}
}

func requireWelfarePublicRules(t *testing.T, rules *WelfareRules) {
	t.Helper()
	object := welfarePublicContractObject(t, rules)
	requireWelfarePublicKeys(t, object, "rules_version", "timezone", "draw_threshold", "subscription_draw_threshold", "subscription_draw_currency", "redemption_rate", "prizes")
	require.Equal(t, float64(2), object["rules_version"])
	require.Equal(t, "Asia/Shanghai", object["timezone"])
	require.Equal(t, "50.00", object["draw_threshold"])
	require.Equal(t, "50.00", object["subscription_draw_threshold"])
	require.Equal(t, "CNY", object["subscription_draw_currency"])
	require.Equal(t, "1:1", object["redemption_rate"])
	prizes, ok := object["prizes"].([]any)
	require.True(t, ok)
	// Lottery amounts and probabilities are intentionally public. In particular,
	// $2.00 and $5.00 here must not be mistaken for undisclosed streak bonuses.
	want := []struct{ id, amount, probability string }{
		{"50", "0.50", "45%"}, {"100", "1.00", "30%"}, {"200", "2.00", "18%"},
		{"500", "5.00", "5%"}, {"1000", "10.00", "1.5%"}, {"2000", "20.00", "0.4%"}, {"5000", "50.00", "0.1%"},
	}
	require.Len(t, prizes, len(want))
	for i, expected := range want {
		prize, ok := prizes[i].(map[string]any)
		require.True(t, ok)
		requireWelfarePublicKeys(t, prize, "id", "name", "amount", "probability")
		require.Equal(t, map[string]any{"id": expected.id, "name": "$" + expected.amount, "amount": expected.amount, "probability": expected.probability}, prize)
	}
}

func requireWelfarePublicCalendar(t *testing.T, calendar *WelfareCalendar, earned map[string]string) {
	t.Helper()
	object := welfarePublicContractObject(t, calendar)
	requireWelfarePublicKeys(t, object, "month", "days")
	require.Equal(t, "2026-09", object["month"])
	days, ok := object["days"].([]any)
	require.True(t, ok)
	require.Len(t, days, 30)
	for i, value := range days {
		day, ok := value.(map[string]any)
		require.True(t, ok)
		date := fmt.Sprintf("2026-09-%02d", i+1)
		require.Equal(t, date, day["date"])
		if amount, claimed := earned[date]; claimed {
			requireWelfarePublicKeys(t, day, "date", "checked_in", "reward_amount")
			require.Equal(t, true, day["checked_in"])
			require.Equal(t, amount, day["reward_amount"])
		} else {
			requireWelfarePublicKeys(t, day, "date", "checked_in")
			require.Equal(t, false, day["checked_in"])
		}
	}
}

func TestWelfarePublicPreviewsNeverRevealUnclaimedRewards(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 9, 19, 12, 0, 0, 0, welfareShanghai)
	launch := now.AddDate(0, -2, 0)
	for _, tc := range []struct {
		name     string
		cycleDay int64
		statuses []string
	}{
		{"nothing_claimed", 0, []string{"locked", "locked", "locked"}},
		{"day_7_available", 6, []string{"available", "locked", "locked"}},
		{"day_7_claimed", 7, []string{"claimed", "locked", "locked"}},
		{"day_15_available", 14, []string{"claimed", "available", "locked"}},
		{"day_15_claimed", 15, []string{"claimed", "claimed", "locked"}},
		{"day_30_available", 29, []string{"claimed", "claimed", "available"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			repo := &welfarePublicContractRepo{state: WelfareState{
				Program: WelfareSettings{Enabled: true, LaunchAt: &launch, RulesVersion: 2}, AccountBalance: "12.34000000",
				Wallet: WelfareWallet{UserID: 42, CycleDay: tc.cycleDay, TotalCheckinDays: tc.cycleDay, DailyLowCount: 3, EligibleSpend: "0.00000000"},
			}}
			earned := make(map[string]string)
			if tc.cycleDay > 0 {
				repo.state.Wallet.LastCheckinDate = "2026-09-18"
				for offset := int64(1); offset <= tc.cycleDay; offset++ {
					date := now.AddDate(0, 0, -int(offset)).Format("2006-01-02")
					repo.days = append(repo.days, WelfareCalendarDay{Date: date, CheckedIn: true, RewardAmount: "0.09"})
					if strings.HasPrefix(date, "2026-09-") {
						earned[date] = "0.09"
					}
				}
			}
			svc := NewWelfareService(repo, WithWelfareClock(func() time.Time { return now }), WithWelfareRandom(func(int64) (int64, error) {
				t.Fatal("preview endpoints must not sample an unclaimed reward")
				return 0, nil
			}))
			rules, err := svc.Rules(ctx)
			require.NoError(t, err)
			requireWelfarePublicRules(t, rules)
			overview, err := svc.Overview(ctx, 42)
			require.NoError(t, err)
			requireWelfarePublicOverview(t, welfarePublicContractObject(t, overview), tc.statuses)
			calendar, err := svc.Calendar(ctx, 42, "2026-09")
			require.NoError(t, err)
			requireWelfarePublicCalendar(t, calendar, earned)
			require.Zero(t, repo.applyCalls)
		})
	}
}

func TestWelfarePublicCheckinAndSavedReplayExposeOnlyEarnedAmounts(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 9, 19, 12, 0, 0, 0, welfareShanghai)
	launch := now.AddDate(0, -2, 0)
	for _, tc := range []struct {
		day      int64
		base     string
		bonus    string
		total    string
		rolls    []int64
		statuses []string
	}{
		{1, "0.09", "0.00", "0.09", []int64{99, 399}, []string{"locked", "locked", "locked"}},
		{7, "0.28", "0.60", "0.88", []int64{0, 65, 7}, []string{"claimed", "locked", "locked"}},
		{15, "0.28", "2.00", "2.28", []int64{0, 65, 7}, []string{"claimed", "claimed", "locked"}},
		{30, "0.28", "5.00", "5.28", []int64{0, 65, 7}, []string{"claimed", "claimed", "claimed"}},
	} {
		t.Run(fmt.Sprintf("day_%d", tc.day), func(t *testing.T) {
			repo := &welfarePublicContractRepo{state: WelfareState{
				Program: WelfareSettings{Enabled: true, LaunchAt: &launch, RulesVersion: 2}, AccountBalance: "12.34000000",
				Wallet: WelfareWallet{UserID: 42, CycleDay: tc.day - 1, TotalCheckinDays: tc.day - 1, LastCheckinDate: "2026-09-18", DailyLowCount: 2, EligibleSpend: "0.00000000"},
			}}
			rollIndex := 0
			svc := NewWelfareService(repo, WithWelfareClock(func() time.Time { return now }), WithWelfareRandom(func(n int64) (int64, error) {
				require.Less(t, rollIndex, len(tc.rolls), "a committed replay must never sample again")
				roll := tc.rolls[rollIndex]
				rollIndex++
				require.Less(t, roll, n)
				return roll, nil
			}))
			assertOperation := func(operation *WelfareOperation) map[string]any {
				object := welfarePublicContractObject(t, operation)
				requireWelfarePublicKeys(t, object, "operation_id", "status", "overview", "reward_amount", "base_reward_amount", "streak_reward_amount")
				require.Equal(t, repo.operationID, object["operation_id"])
				require.Equal(t, "completed", object["status"])
				require.Equal(t, tc.total, object["reward_amount"])
				require.Equal(t, tc.base, object["base_reward_amount"])
				require.Equal(t, tc.bonus, object["streak_reward_amount"])
				overview, ok := object["overview"].(map[string]any)
				require.True(t, ok)
				requireWelfarePublicOverview(t, overview, tc.statuses)
				require.Equal(t, tc.total, overview["welfare_balance"])
				require.Equal(t, true, overview["today_checked_in"])
				return object
			}
			operation, err := svc.CheckIn(ctx, 42)
			require.NoError(t, err)
			original := assertOperation(operation)
			require.NotNil(t, repo.mutation.PrivateAudit, "the test must contain real private audit data to protect")
			audit := welfarePublicContractObject(t, repo.mutation.PrivateAudit)
			require.Contains(t, audit, "branch_roll")
			require.Contains(t, audit, "amount_roll")
			require.Contains(t, audit, "low_before")
			require.Equal(t, len(tc.rolls), rollIndex)

			overview, err := svc.Overview(ctx, 42)
			require.NoError(t, err)
			requireWelfarePublicOverview(t, welfarePublicContractObject(t, overview), tc.statuses)
			calendar, err := svc.Calendar(ctx, 42, "2026-09")
			require.NoError(t, err)
			requireWelfarePublicCalendar(t, calendar, map[string]string{"2026-09-19": tc.base})
			rules, err := svc.Rules(ctx)
			require.NoError(t, err)
			requireWelfarePublicRules(t, rules)

			// A replay must come from the saved public JSON, not from a mutable
			// caller object or today's wallet/private state.
			operation.RewardAmount = "mutated caller result"
			operation.Overview.Milestones[0].Status = "mutated caller result"
			repo.state.Program.Enabled = false
			repo.state.Wallet.BalanceCents = 99999
			repo.state.Wallet.DailyLowCount = 3
			replay, err := svc.CheckIn(ctx, 42)
			require.NoError(t, err)
			require.Equal(t, original, assertOperation(replay))
			stored, err := svc.Operation(ctx, 42, repo.operationID)
			require.NoError(t, err)
			require.Equal(t, original, assertOperation(stored))
			require.Equal(t, 1, repo.applyCalls)
			require.Equal(t, len(tc.rolls), rollIndex)
		})
	}
}
