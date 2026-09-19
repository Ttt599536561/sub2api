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
func welfareTickets(spend string, used int64) (available, debt int64, remaining string, err error) {
	d, err := decimal.NewFromString(spend)
	if err != nil || d.IsNegative() || used < 0 {
		return 0, 0, "", fmt.Errorf("invalid welfare ticket state")
	}
	threshold := decimal.NewFromInt(50)
	earned := d.Div(threshold).Floor().IntPart()
	if earned >= used {
		available = earned - used
	} else {
		debt = used - earned
	}
	target := earned + 1
	if used > earned {
		target = used + 1
	}
	remaining = threshold.Mul(decimal.NewFromInt(target)).Sub(d).StringFixed(8)
	return available, debt, remaining, nil
}

type welfareDailyAudit struct {
	Surprise   bool  `json:"surprise"`
	LowBefore  int   `json:"low_before"`
	LowAfter   int   `json:"low_after"`
	BranchRoll int64 `json:"branch_roll"`
	AmountRoll int64 `json:"amount_roll"`
	BandRoll   int64 `json:"band_roll,omitempty"`
}

func sampleWelfareDaily(low int, random WelfareRandom) (int64, int, welfareDailyAudit, error) {
	audit := welfareDailyAudit{LowBefore: low}
	if low < 0 || low > 3 {
		return 0, low, audit, fmt.Errorf("invalid welfare daily counter")
	}
	roll, err := random(100)
	if err != nil {
		return 0, low, audit, err
	}
	audit.BranchRoll = roll
	chance := []int64{20, 30, 50, 100}[low]
	if roll >= chance {
		roll, err = random(400)
		if err != nil {
			return 0, low, audit, err
		}
		audit.AmountRoll = roll
		amount := int64(1)
		if roll >= 8 {
			amount = 2 + (roll-8)/49
		}
		audit.LowAfter = low + 1
		return amount, low + 1, audit, nil
	}
	audit.Surprise = true
	roll, err = random(100)
	if err != nil {
		return 0, low, audit, err
	}
	audit.AmountRoll = roll
	min, max := int64(10), int64(20)
	switch {
	case roll >= 99:
		min, max = 51, 100
	case roll >= 90:
		min, max = 36, 50
	case roll >= 65:
		min, max = 21, 35
	}
	within, err := random(max - min + 1)
	if err != nil {
		return 0, low, audit, err
	}
	audit.BandRoll = within
	audit.LowAfter = 0
	return min + within, 0, audit, nil
}

var welfareLotteryAmounts = []int64{50, 100, 200, 500, 1000, 2000, 5000}
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

func advanceWelfareCheckin(w *WelfareWallet, date string) (int64, error) {
	day, err := time.Parse("2006-01-02", date)
	if err != nil {
		return 0, ErrWelfareInvalidRequest
	}
	if w.LastCheckinDate >= date {
		return 0, ErrWelfareAlreadyCheckedIn
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
	switch w.CycleDay {
	case 7:
		return 60, nil
	case 15:
		return 200, nil
	case 30:
		return 500, nil
	}
	return 0, nil
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
