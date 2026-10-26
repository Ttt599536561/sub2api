package service

import (
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/shopspring/decimal"
)

// Fixed Shanghai civil-time rules do not depend on the server's local timezone.
var welfareShanghai = time.FixedZone("Asia/Shanghai", 8*60*60)
var welfareAmountPattern = regexp.MustCompile(`^(0|[1-9][0-9]*)(\.[0-9]{1,2})?$`)

func welfareBusinessDate(now time.Time) string { return now.In(welfareShanghai).Format("2006-01-02") }
func welfareMoney(cents int64) string          { return decimal.NewFromInt(cents).Shift(-2).StringFixed(2) }
func parseWelfareAmount(s string) (int64, error) {
	if !welfareAmountPattern.MatchString(s) {
		return 0, ErrWelfareInvalidAmount
	}
	d, err := decimal.NewFromString(s)
	if err != nil || !d.IsPositive() {
		return 0, ErrWelfareInvalidAmount
	}
	c := d.Shift(2)
	if c.GreaterThan(decimal.NewFromInt(math.MaxInt64)) {
		return 0, ErrWelfareInvalidAmount
	}
	return c.IntPart(), nil
}

// A spent ticket is never undone by refunds. Remaining spend pays down ticket
// debt before it can produce a new available ticket.
func welfareTickets(spend string, used, subscriptionDraws int64) (available, debt int64, remaining string, err error) {
	d, err := decimal.NewFromString(spend)
	if err != nil || d.IsNegative() || used < 0 || subscriptionDraws < 0 {
		return 0, 0, "", fmt.Errorf("invalid welfare ticket state")
	}
	threshold := decimal.NewFromInt(50)
	apiEarned := d.Div(threshold).Floor()
	if apiEarned.GreaterThan(decimal.NewFromInt(math.MaxInt64 - subscriptionDraws)) {
		return 0, 0, "", fmt.Errorf("welfare draw entitlement overflow")
	}
	earned := apiEarned.IntPart() + subscriptionDraws
	if earned >= used {
		available = earned - used
	} else {
		debt = used - earned
	}
	// Only API spend contributes a fractional progress bar. Subscription order
	// remainders never enter this dollar amount; their integer draws pay debt.
	target := apiEarned.Add(decimal.NewFromInt(1))
	if used > earned {
		target = decimal.NewFromInt(used - subscriptionDraws).Add(decimal.NewFromInt(1))
	}
	remaining = threshold.Mul(target).Sub(d).StringFixed(8)
	return available, debt, remaining, nil
}

type welfareDailyAudit struct {
	RulesVersion string              `json:"rules_version"`
	Surprise     bool                `json:"surprise"`
	LowBefore    int                 `json:"low_before"`
	LowAfter     int                 `json:"low_after"`
	BranchRoll   int64               `json:"branch_roll"`
	AmountRoll   int64               `json:"amount_roll"`
	BandRoll     int64               `json:"band_roll,omitempty"`
	AmountCents  int64               `json:"amount_cents"`
	Streak       *welfareStreakAudit `json:"streak,omitempty"`
}

type welfareStreakAudit struct {
	Day          int64  `json:"day"`
	BranchRoll   *int64 `json:"branch_roll,omitempty"`
	BandMinCents int64  `json:"band_min_cents"`
	BandMaxCents int64  `json:"band_max_cents"`
	AmountRoll   int64  `json:"amount_roll"`
	AmountCents  int64  `json:"amount_cents"`
}

func sampleWelfareDaily(low int, random WelfareRandom) (int64, int, welfareDailyAudit, error) {
	audit := welfareDailyAudit{RulesVersion: "v3", LowBefore: low}
	if low < 0 || low > 3 {
		return 0, low, audit, fmt.Errorf("invalid welfare daily counter")
	}
	roll, err := random(100)
	if err != nil {
		return 0, low, audit, err
	}
	audit.BranchRoll = roll
	chance := []int64{20, 30, 50, 100}[low]
	audit.Surprise = roll < chance
	roll, err = random(100)
	if err != nil {
		return 0, low, audit, err
	}
	audit.AmountRoll = roll
	amount, next := int64(1), low+1
	if audit.Surprise {
		amount, next = 3, 0
		if roll >= 95 {
			amount = 5
		} else if roll >= 70 {
			amount = 4
		}
	} else if roll >= 70 {
		amount = 2
	}
	audit.LowAfter, audit.AmountCents = next, amount
	return amount, next, audit, nil
}

func sampleWelfareStreak(day int64, random WelfareRandom) (int64, *welfareStreakAudit, error) {
	audit := &welfareStreakAudit{Day: day}
	min, max := int64(0), int64(0)
	switch day {
	case 7:
		min, max = 3, 8
	case 15:
		min, max = 8, 20
	case 30:
		roll, err := random(100)
		if err != nil {
			return 0, audit, err
		}
		audit.BranchRoll = &roll
		switch {
		case roll < 80:
			min, max = 20, 50
		case roll < 98:
			min, max = 51, 100
		default:
			min, max = 101, 195
		}
	default:
		return 0, nil, nil
	}
	audit.BandMinCents, audit.BandMaxCents = min, max
	within, err := random(max - min + 1)
	if err != nil {
		return 0, audit, err
	}
	audit.AmountRoll, audit.AmountCents = within, min+within
	return min + within, audit, nil
}

var welfareLotteryAmounts = []int64{1, 2, 5, 10, 20, 50, 100}
var welfareLotteryWeights = []int64{450, 300, 180, 50, 15, 4, 1}

func sampleWelfareLottery(random WelfareRandom) (int64, error) {
	roll, err := random(1000)
	if err != nil {
		return 0, err
	}
	for i, weight := range welfareLotteryWeights {
		if roll < weight {
			return welfareLotteryAmounts[i], nil
		}
		roll -= weight
	}
	return 0, fmt.Errorf("invalid welfare random sample")
}

func advanceWelfareCheckin(w *WelfareWallet, date string) error {
	day, err := time.Parse("2006-01-02", date)
	if err != nil {
		return ErrWelfareInvalidRequest
	}
	if w.LastCheckinDate >= date {
		return ErrWelfareAlreadyCheckedIn
	}
	consecutive := w.LastCheckinDate == day.AddDate(0, 0, -1).Format("2006-01-02")
	if !consecutive || w.CycleDay >= 30 {
		w.CycleID++
		w.CycleDay = 1
	} else {
		w.CycleDay++
	}
	w.LastCheckinDate = date
	w.TotalCheckinDays++
	return nil
}

func welfarePrizeFor(cents int64) WelfarePrize {
	for i, amount := range welfareLotteryAmounts {
		if amount == cents {
			return WelfarePrize{ID: strconv.FormatInt(cents, 10), Name: "$" + welfareMoney(cents), Amount: welfareMoney(cents), Probability: decimal.NewFromInt(welfareLotteryWeights[i]).Div(decimal.NewFromInt(10)).String() + "%"}
		}
	}
	return WelfarePrize{}
}

func validWelfareKey(key string) bool {
	return len(key) > 0 && len(key) <= 128 && strings.TrimSpace(key) == key
}
