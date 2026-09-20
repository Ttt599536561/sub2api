package service

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type welfareDomainRepo struct {
	WelfareRepository
	state    WelfareState
	mutation *WelfareMutation
	filter   WelfareRecordFilter
}

func (r *welfareDomainRepo) GetSettings(context.Context) (*WelfareSettings, error) {
	return &r.state.Program, nil
}
func (r *welfareDomainRepo) Records(_ context.Context, _ int64, filter WelfareRecordFilter) (*WelfareRecords, error) {
	r.filter = filter
	return &WelfareRecords{Items: []WelfareRecord{}}, nil
}
func (r *welfareDomainRepo) Mutate(_ context.Context, _ int64, _, _, _ string, apply func(*WelfareState) (*WelfareMutation, error)) (*WelfareOperation, error) {
	mutation, err := apply(&r.state)
	if err != nil {
		return nil, err
	}
	r.mutation = mutation
	return mutation.Result, nil
}

func TestWelfareRecordsAllFilterAndLotteryPrivateAudit(t *testing.T) {
	launch := time.Now().Add(-time.Hour)
	repo := &welfareDomainRepo{state: WelfareState{Program: WelfareSettings{Enabled: true, LaunchAt: &launch}, AccountBalance: "0.00000000", Wallet: WelfareWallet{EligibleSpend: "50.00000000"}}}
	s := NewWelfareService(repo, WithWelfareRandom(welfareSequence(777)))
	_, err := s.Records(context.Background(), 1, WelfareRecordFilter{Type: "all"})
	require.NoError(t, err)
	require.Equal(t, "", repo.filter.Type)
	result, err := s.Draw(context.Background(), 1, "draw-audit")
	require.NoError(t, err)
	audit, err := json.Marshal(repo.mutation.PrivateAudit)
	require.NoError(t, err)
	require.Contains(t, string(audit), `"uniform_roll":777`)
	public, err := json.Marshal(result)
	require.NoError(t, err)
	require.NotContains(t, string(public), "uniform_roll")
	require.NotContains(t, string(public), "daily_low_count")
}

func welfareSequence(values ...int64) WelfareRandom {
	i := 0
	return func(n int64) (int64, error) {
		if i >= len(values) {
			panic("unexpected random sample")
		}
		v := values[i]
		i++
		if v < 0 || v >= n {
			panic("random sample out of range")
		}
		return v, nil
	}
}

func TestWelfareDailyStatefulDistributionBoundaries(t *testing.T) {
	for _, tc := range []struct {
		low      int
		roll     int64
		surprise bool
		next     int
	}{
		{0, 19, true, 0}, {0, 20, false, 1}, {1, 29, true, 0}, {1, 30, false, 2},
		{2, 49, true, 0}, {2, 50, false, 3}, {3, 99, true, 0},
	} {
		amount, next, audit, err := sampleWelfareDaily(tc.low, welfareSequence(tc.roll, 0, 0))
		require.NoError(t, err)
		require.Equal(t, tc.next, next)
		require.Equal(t, tc.surprise, audit.Surprise)
		if tc.surprise {
			require.EqualValues(t, 10, amount)
		} else {
			require.EqualValues(t, 1, amount)
		}
	}
	for _, tc := range []struct{ roll, want int64 }{{0, 1}, {7, 1}, {8, 2}, {56, 2}, {57, 3}, {399, 9}} {
		amount, _, _, err := sampleWelfareDaily(0, welfareSequence(99, tc.roll))
		require.NoError(t, err)
		require.Equal(t, tc.want, amount)
	}
	for _, tc := range []struct{ tier, within, want int64 }{{64, 10, 20}, {65, 0, 21}, {89, 14, 35}, {90, 0, 36}, {98, 14, 50}, {99, 49, 100}} {
		amount, _, _, err := sampleWelfareDaily(0, welfareSequence(0, tc.tier, tc.within))
		require.NoError(t, err)
		require.Equal(t, tc.want, amount)
	}
}

func TestWelfareCheckinCalendarGapCycleAndMilestones(t *testing.T) {
	date := time.Date(2026, 9, 19, 0, 0, 0, 0, time.UTC)
	for _, tc := range []struct {
		last                      string
		day, total, cycle         int64
		wantDay, wantCycle, bonus int64
	}{
		{"", 0, 0, 0, 1, 1, 0}, {"2026-09-18", 6, 6, 1, 7, 1, 60},
		{"2026-09-18", 14, 100, 5, 15, 5, 200}, {"2026-09-18", 29, 29, 1, 30, 1, 500},
		{"2026-09-18", 30, 30, 1, 1, 2, 0}, {"2026-09-17", 6, 6, 1, 1, 2, 0},
	} {
		w := WelfareWallet{LastCheckinDate: tc.last, CycleDay: tc.day, TotalCheckinDays: tc.total, CycleID: tc.cycle, DailyLowCount: 3}
		bonus, err := advanceWelfareCheckin(&w, date.Format("2006-01-02"))
		require.NoError(t, err)
		require.Equal(t, tc.wantDay, w.CycleDay)
		require.Equal(t, tc.wantCycle, w.CycleID)
		require.Equal(t, tc.total+1, w.TotalCheckinDays)
		require.Equal(t, tc.bonus, bonus)
		require.Equal(t, 3, w.DailyLowCount, "gap and rollover preserve hidden counter")
	}
	w := WelfareWallet{LastCheckinDate: "2026-09-19"}
	_, err := advanceWelfareCheckin(&w, "2026-09-19")
	require.ErrorIs(t, err, ErrWelfareAlreadyCheckedIn)
	_, err = advanceWelfareCheckin(&WelfareWallet{LastCheckinDate: "2026-09-20"}, "2026-09-19")
	require.Error(t, err, "clock rollback cannot award old days")
}

func TestWelfareLotteryHasExactThousandWeightPartition(t *testing.T) {
	counts := map[int64]int{}
	for i := int64(0); i < 1000; i++ {
		cents, err := sampleWelfareLottery(welfareSequence(i))
		require.NoError(t, err)
		counts[cents]++
	}
	require.Equal(t, map[int64]int{50: 450, 100: 300, 200: 180, 500: 50, 1000: 15, 2000: 4, 5000: 1}, counts)
	errRandom := errors.New("entropy unavailable")
	_, err := sampleWelfareLottery(func(int64) (int64, error) { return 0, errRandom })
	require.ErrorIs(t, err, errRandom)
}

func TestWelfareExactAmountsAndTicketDebt(t *testing.T) {
	for _, tc := range []struct {
		amount string
		want   int64
	}{{"0.01", 1}, {"5", 500}, {"5.00", 500}, {"92233720368547758.07", 9223372036854775807}} {
		got, err := parseWelfareAmount(tc.amount)
		require.NoError(t, err)
		require.Equal(t, tc.want, got)
	}
	for _, s := range []string{"0", "-1", "+1", "1e2", "1.001", "NaN", " 1.00", "92233720368547758.08", ".5", "01"} {
		_, err := parseWelfareAmount(s)
		require.Error(t, err, s)
	}
	for _, tc := range []struct {
		spend                 string
		used, available, debt int64
		remaining             string
	}{
		{"49.99999999", 0, 0, 0, "0.00000001"}, {"50.00000000", 0, 1, 0, "50.00000000"},
		{"149.99999999", 3, 0, 1, "50.00000001"}, {"150.00000000", 3, 0, 0, "50.00000000"},
	} {
		available, debt, remaining, err := welfareTickets(tc.spend, tc.used, 0)
		require.NoError(t, err)
		require.Equal(t, tc.available, available)
		require.Equal(t, tc.debt, debt)
		require.Equal(t, tc.remaining, remaining)
	}
}

func TestWelfareBusinessDateUsesShanghaiBoundary(t *testing.T) {
	before := time.Date(2026, 9, 18, 15, 59, 59, 0, time.UTC)
	require.Equal(t, "2026-09-18", welfareBusinessDate(before))
	require.Equal(t, "2026-09-19", welfareBusinessDate(before.Add(time.Second)))
}

func TestWelfareRecordedLaunchIsNotScheduledAgainstAnotherClock(t *testing.T) {
	// launch_at is an audit timestamp from PostgreSQL. It is not a scheduled
	// launch: small database/application clock offsets must not reject rewards.
	now := time.Now()
	databaseLaunch := now.Add(time.Second)
	require.NoError(t, welfareLaunched(&WelfareSettings{Enabled: true, LaunchAt: &databaseLaunch}, now))
	require.ErrorIs(t, welfareLaunched(&WelfareSettings{Enabled: true}, now), ErrWelfareNotLaunched)
}
