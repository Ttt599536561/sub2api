package service

import (
	"context"
	"fmt"
	"math"
	"strconv"
	"time"

	"github.com/shopspring/decimal"
)

func (s *WelfareService) GetSettings(ctx context.Context) (*WelfareSettings, error) {
	return s.repo.GetSettings(ctx)
}
func (s *WelfareService) UpdateSettings(ctx context.Context, enabled bool) (*WelfareSettings, error) {
	return s.repo.UpdateSettings(ctx, enabled)
}

func (s *WelfareService) requireLaunched(ctx context.Context) error {
	p, err := s.repo.GetSettings(ctx)
	if err != nil {
		return err
	}
	return welfareLaunched(p, s.now())
}

func welfareLaunched(p *WelfareSettings, _ time.Time) error {
	// launch_at records the first enable in the database; it is not a schedule.
	// Comparing it to another machine's clock can reject just-enabled programs.
	if p.LaunchAt == nil {
		return ErrWelfareNotLaunched
	}
	return nil
}

func welfareRewardsAllowed(p *WelfareSettings, now time.Time) error {
	if err := welfareLaunched(p, now); err != nil {
		return err
	}
	if !p.Enabled {
		return ErrWelfarePaused
	}
	return nil
}

func (s *WelfareService) Overview(ctx context.Context, userID int64) (*WelfareOverview, error) {
	state, err := s.repo.State(ctx, userID)
	if err != nil {
		return nil, err
	}
	if err = welfareLaunched(&state.Program, s.now()); err != nil {
		return nil, err
	}
	return welfareOverview(state, s.now())
}

func welfareOverview(state *WelfareState, now time.Time) (*WelfareOverview, error) {
	w := &state.Wallet
	available, debt, remaining, err := welfareTickets(w.EligibleSpend, w.DrawsUsed, w.SubscriptionDraws)
	if err != nil {
		return nil, err
	}
	date := welfareBusinessDate(now)
	today, _ := time.ParseInLocation("2006-01-02", date, welfareShanghai)
	cycle := w.CycleDay
	checked := w.LastCheckinDate == date
	if !checked && (w.LastCheckinDate != today.AddDate(0, 0, -1).Format("2006-01-02") || cycle == 30) {
		cycle = 0
	}
	milestones := make([]WelfareMilestone, 0, 3)
	for _, day := range []int64{7, 15, 30} {
		status := "locked"
		if cycle >= day {
			status = "claimed"
		} else if cycle+1 == day && !checked {
			status = "available"
		}
		milestones = append(milestones, WelfareMilestone{Day: day, Status: status})
	}
	return &WelfareOverview{WelfareBalance: welfareMoney(w.BalanceCents), AccountBalance: state.AccountBalance,
		AvailableDraws: available, DrawsUsed: w.DrawsUsed, TotalCheckinDays: w.TotalCheckinDays, CycleDay: cycle, TodayCheckedIn: checked,
		BusinessDate: date, NextResetAt: today.AddDate(0, 0, 1), EligibleSpend: w.EligibleSpend, NextDrawRemaining: remaining, TicketDebt: debt,
		SubscriptionDraws: w.SubscriptionDraws,
		WalletVersion:     w.WalletVersion, WelfareBalanceVersion: w.WelfareBalanceVersion,
		RewardsEnabled: state.Program.Enabled, RulesVersion: 2, Milestones: milestones}, nil
}

func (s *WelfareService) Rules(ctx context.Context) (*WelfareRules, error) {
	if err := s.requireLaunched(ctx); err != nil {
		return nil, err
	}
	prizes := make([]WelfarePrize, 0, len(welfareLotteryAmounts))
	for _, amount := range welfareLotteryAmounts {
		prizes = append(prizes, welfarePrizeFor(amount))
	}
	return &WelfareRules{RulesVersion: 2, Timezone: "Asia/Shanghai", DrawThreshold: "50.00", SubscriptionDrawThreshold: "50.00", SubscriptionDrawCurrency: "CNY", RedemptionRate: "1:1", Prizes: prizes}, nil
}

func (s *WelfareService) Calendar(ctx context.Context, userID int64, month string) (*WelfareCalendar, error) {
	if month == "" {
		month = s.now().In(welfareShanghai).Format("2006-01")
	}
	start, err := time.Parse("2006-01", month)
	if err != nil || start.Year() < 2000 || start.Year() > 9999 {
		return nil, ErrWelfareInvalidRequest
	}
	if err = s.requireLaunched(ctx); err != nil {
		return nil, err
	}
	checked, err := s.repo.Calendar(ctx, userID, month)
	if err != nil {
		return nil, err
	}
	byDate := make(map[string]WelfareCalendarDay, len(checked))
	for _, day := range checked {
		byDate[day.Date] = day
	}
	days := make([]WelfareCalendarDay, 0, 31)
	for day, end := start, start.AddDate(0, 1, 0); day.Before(end); day = day.AddDate(0, 0, 1) {
		date := day.Format("2006-01-02")
		entry := WelfareCalendarDay{Date: date}
		if found, ok := byDate[date]; ok {
			entry = found
		}
		days = append(days, entry)
	}
	return &WelfareCalendar{Month: month, Days: days}, nil
}

func (s *WelfareService) CheckIn(ctx context.Context, userID int64) (*WelfareOperation, error) {
	now := s.now()
	date := welfareBusinessDate(now)
	return s.repo.Mutate(ctx, userID, "checkin", date, "checkin:"+date, func(state *WelfareState) (*WelfareMutation, error) {
		if err := welfareRewardsAllowed(&state.Program, now); err != nil {
			return nil, err
		}
		w := &state.Wallet
		bonus, err := advanceWelfareCheckin(w, date)
		if err != nil {
			return nil, err
		}
		base, low, audit, err := sampleWelfareDaily(w.DailyLowCount, s.random)
		if err != nil {
			return nil, err
		}
		w.DailyLowCount = low
		if w.BalanceCents > math.MaxInt64-base-bonus || w.TotalEarnedCents > math.MaxInt64-base-bonus {
			return nil, fmt.Errorf("welfare reward overflow")
		}
		w.BalanceCents += base
		entries := []WelfareLedgerEntry{{Type: "daily", AmountCents: base, BalanceAfterCents: w.BalanceCents}}
		if bonus > 0 {
			w.BalanceCents += bonus
			entries = append(entries, WelfareLedgerEntry{Type: "streak", AmountCents: bonus, BalanceAfterCents: w.BalanceCents})
		}
		w.TotalEarnedCents += base + bonus
		w.WalletVersion++
		w.WelfareBalanceVersion++
		overview, err := welfareOverview(state, now)
		if err != nil {
			return nil, err
		}
		return &WelfareMutation{BusinessDate: date, Checkin: true, Entries: entries, PrivateAudit: audit, Result: &WelfareOperation{Status: "completed", Overview: overview,
			RewardAmount: welfareMoney(base + bonus), BaseRewardAmount: welfareMoney(base), StreakRewardAmount: welfareMoney(bonus)}}, nil
	})
}

func (s *WelfareService) Draw(ctx context.Context, userID int64, key string) (*WelfareOperation, error) {
	if !validWelfareKey(key) {
		return nil, ErrWelfareInvalidRequest
	}
	now := s.now()
	return s.repo.Mutate(ctx, userID, "draw", key, "draw:v2", func(state *WelfareState) (*WelfareMutation, error) {
		if err := welfareRewardsAllowed(&state.Program, now); err != nil {
			return nil, err
		}
		w := &state.Wallet
		available, _, _, err := welfareTickets(w.EligibleSpend, w.DrawsUsed, w.SubscriptionDraws)
		if err != nil {
			return nil, err
		}
		if available < 1 {
			return nil, ErrWelfareNoTickets
		}
		var uniformRoll int64
		amount, err := sampleWelfareLottery(func(n int64) (int64, error) {
			roll, err := s.random(n)
			uniformRoll = roll
			return roll, err
		})
		if err != nil {
			return nil, err
		}
		if w.BalanceCents > math.MaxInt64-amount || w.TotalEarnedCents > math.MaxInt64-amount {
			return nil, fmt.Errorf("welfare reward overflow")
		}
		w.DrawsUsed++
		w.BalanceCents += amount
		w.TotalEarnedCents += amount
		w.WalletVersion++
		w.WelfareBalanceVersion++
		overview, err := welfareOverview(state, now)
		if err != nil {
			return nil, err
		}
		prize := welfarePrizeFor(amount)
		return &WelfareMutation{BusinessDate: welfareBusinessDate(now), Entries: []WelfareLedgerEntry{{Type: "draw", AmountCents: amount, BalanceAfterCents: w.BalanceCents}},
			PrivateAudit: map[string]any{"rules_version": "v2", "amount_cents": amount, "uniform_roll": uniformRoll}, Result: &WelfareOperation{Status: "completed", Overview: overview, RewardAmount: welfareMoney(amount), Prize: &prize}}, nil
	})
}

func (s *WelfareService) Quote(ctx context.Context, userID int64, mode, amount string) (*WelfareQuote, error) {
	if mode != "all" && mode != "partial" && mode != "custom" && mode != "amount" {
		return nil, ErrWelfareInvalidRequest
	}
	state, err := s.repo.State(ctx, userID)
	if err != nil {
		return nil, err
	}
	if err = welfareLaunched(&state.Program, s.now()); err != nil {
		return nil, err
	}
	cents := state.Wallet.BalanceCents
	if mode != "all" {
		cents, err = parseWelfareAmount(amount)
		if err != nil {
			return nil, err
		}
	}
	if cents <= 0 || cents > state.Wallet.BalanceCents {
		return nil, ErrWelfareInsufficientBalance
	}
	balance, err := decimal.NewFromString(state.AccountBalance)
	if err != nil {
		return nil, err
	}
	return &WelfareQuote{Amount: welfareMoney(cents), WelfareBalance: welfareMoney(state.Wallet.BalanceCents), AccountBalance: state.AccountBalance,
		AccountBalanceAfter: balance.Add(decimal.NewFromInt(cents).Shift(-2)).StringFixed(8), WelfareBalanceAfter: welfareMoney(state.Wallet.BalanceCents - cents),
		WelfareBalanceVersion: state.Wallet.WelfareBalanceVersion}, nil
}

func (s *WelfareService) Redeem(ctx context.Context, userID int64, key, amount string, balanceVersion int64) (*WelfareOperation, error) {
	if !validWelfareKey(key) || balanceVersion < 0 {
		return nil, ErrWelfareInvalidRequest
	}
	cents, err := parseWelfareAmount(amount)
	if err != nil {
		return nil, err
	}
	fingerprint := welfareMoney(cents) + ":" + strconv.FormatInt(balanceVersion, 10)
	now := s.now()
	return s.repo.Mutate(ctx, userID, "redeem", key, fingerprint, func(state *WelfareState) (*WelfareMutation, error) {
		if err := welfareLaunched(&state.Program, now); err != nil {
			return nil, err
		}
		w := &state.Wallet
		if balanceVersion != w.WelfareBalanceVersion {
			return nil, ErrWelfareQuoteStale
		}
		if cents > w.BalanceCents {
			return nil, ErrWelfareInsufficientBalance
		}
		balance, err := decimal.NewFromString(state.AccountBalance)
		if err != nil {
			return nil, err
		}
		w.BalanceCents -= cents
		w.TotalRedeemedCents += cents
		w.WalletVersion++
		w.WelfareBalanceVersion++
		state.AccountBalance = balance.Add(decimal.NewFromInt(cents).Shift(-2)).StringFixed(8)
		overview, err := welfareOverview(state, now)
		if err != nil {
			return nil, err
		}
		return &WelfareMutation{BusinessDate: welfareBusinessDate(now), TransferCents: cents, Entries: []WelfareLedgerEntry{{Type: "redeem", AmountCents: -cents, BalanceAfterCents: w.BalanceCents}},
			Result: &WelfareOperation{Status: "completed", Overview: overview, Amount: welfareMoney(cents)}}, nil
	})
}

func (s *WelfareService) Records(ctx context.Context, userID int64, filter WelfareRecordFilter) (*WelfareRecords, error) {
	if filter.Type == "all" {
		filter.Type = ""
	}
	if filter.Type != "" && filter.Type != "daily" && filter.Type != "streak" && filter.Type != "draw" && filter.Type != "redeem" {
		return nil, ErrWelfareInvalidRequest
	}
	if filter.Page <= 0 {
		filter.Page = 1
	}
	if filter.PageSize <= 0 {
		filter.PageSize = 20
	}
	if filter.PageSize > 100 {
		filter.PageSize = 100
	}
	if filter.Page > 1000000 {
		return nil, ErrWelfareInvalidRequest
	}
	for _, date := range []string{filter.DateFrom, filter.DateTo} {
		if date != "" {
			if parsed, err := time.Parse("2006-01-02", date); err != nil || parsed.Year() < 2000 || parsed.Year() > 9999 {
				return nil, ErrWelfareInvalidRequest
			}
		}
	}
	if filter.DateFrom != "" && filter.DateTo != "" && filter.DateFrom > filter.DateTo {
		return nil, ErrWelfareInvalidRequest
	}
	if err := s.requireLaunched(ctx); err != nil {
		return nil, err
	}
	return s.repo.Records(ctx, userID, filter)
}

func (s *WelfareService) Operation(ctx context.Context, userID int64, id string) (*WelfareOperation, error) {
	if !validWelfareKey(id) {
		return nil, ErrWelfareInvalidRequest
	}
	return s.repo.Operation(ctx, userID, id)
}
