package service

import (
	"context"
	"math"
	"time"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/pkg/timezone"
)

const DailyResetLimit = 100

var (
	ErrResetNotAllowed           = infraerrors.Forbidden("RESET_NOT_ALLOWED", "daily reset is not allowed")
	ErrResetNoUsage              = infraerrors.BadRequest("RESET_NO_USAGE", "daily quota has no usage to reset")
	ErrResetInsufficientValidity = infraerrors.BadRequest("RESET_INSUFFICIENT_VALIDITY", "more than 24 hours of validity is required")
	ErrResetDailyCountLimit      = infraerrors.BadRequest("RESET_DAILY_COUNT_LIMIT", "daily reset count limit reached")
	ErrResetWeeklyLimit          = infraerrors.BadRequest("RESET_WEEKLY_LIMIT", "weekly quota is exhausted")
	ErrResetMonthlyLimit         = infraerrors.BadRequest("RESET_MONTHLY_LIMIT", "monthly quota is exhausted")
	ErrResetStateChanged         = infraerrors.Conflict("RESET_STATE_CHANGED", "subscription state changed; refresh and try again")
	ErrResetOperationConflict    = infraerrors.Conflict("RESET_OPERATION_CONFLICT", "operation ID was used with different parameters")
	ErrResetOperationNotFound    = infraerrors.NotFound("RESET_OPERATION_NOT_FOUND", "reset operation not found")
	ErrResetInvalidData          = infraerrors.BadRequest("RESET_INVALID_DATA", "subscription quota data is invalid")
)

type SubscriptionDailyResetCommand struct {
	UserID             int64
	SubscriptionID     int64
	ExpectedVersion    int64
	OperationID        string
	ObservedDate       string
	Source             string
	RequestFingerprint string
}

type SubscriptionDailyResetEvent struct {
	ID                  int64
	SubscriptionID      int64
	UserID              int64
	GroupID             int64
	Source              string
	OperationID         string
	RequestFingerprint  string
	BeforeVersion       int64
	AfterVersion        int64
	Timezone            string
	CountDate           string
	DaySequence         int
	BeforeDailyUsageUSD float64
	DailyLimitUSD       float64
	BeforeExpiresAt     time.Time
	AfterExpiresAt      time.Time
	DecidedAt           time.Time
}

type SubscriptionDailyResetState struct {
	Subscription *UserSubscription
	ServerTime   time.Time
	TodayCount   int
}

type SubscriptionDailyResetResult struct {
	State    *SubscriptionDailyResetState
	Event    *SubscriptionDailyResetEvent
	Replayed bool
}

type SubscriptionDailyResetCandidate struct {
	ID                int64
	UserID            int64
	DailyResetVersion int64
}

type SubscriptionDailyResetRepository interface {
	GetState(context.Context, int64, int64) (*SubscriptionDailyResetState, error)
	Apply(context.Context, *SubscriptionDailyResetCommand) (*SubscriptionDailyResetResult, error)
	SetAutomatic(context.Context, int64, int64, int64, bool) (*SubscriptionDailyResetState, error)
	GetOperation(context.Context, int64, int64, string) (*SubscriptionDailyResetEvent, error)
	ListAutomaticCandidates(context.Context, int64, int) ([]SubscriptionDailyResetCandidate, error)
}

type DailyResetState struct {
	Eligible              bool      `json:"eligible"`
	CanReset              bool      `json:"can_reset"`
	AutoDailyResetEnabled bool      `json:"auto_daily_reset_enabled"`
	DailyResetCount       int       `json:"today_reset_count"`
	DailyResetLimit       int       `json:"daily_reset_limit"`
	DailyResetVersion     int64     `json:"daily_reset_version"`
	CountDate             string    `json:"server_date"`
	ServerTime            time.Time `json:"server_time"`
	Reason                string    `json:"reason,omitempty"`
}

// MaintainUsageWindowsAt applies the existing calendar/periodic rules to a locked snapshot.
// The repository persists these changes even when a paid reset is subsequently declined.
func (s *UserSubscription) MaintainUsageWindowsAt(now time.Time) bool {
	changed := false
	if start, ok := s.automaticDailyWindowStartAt(now); ok {
		s.DailyWindowStart, s.DailyUsageUSD = &start, 0
		changed = true
	}
	if start, ok := s.automaticWindowStartAt(s.WeeklyWindowStart, 7*24*time.Hour, now); ok {
		s.WeeklyWindowStart, s.WeeklyUsageUSD = &start, 0
		changed = true
	}
	if start, ok := s.automaticWindowStartAt(s.MonthlyWindowStart, 30*24*time.Hour, now); ok {
		s.MonthlyWindowStart, s.MonthlyUsageUSD = &start, 0
		changed = true
	}
	if changed {
		s.DailyResetVersion++
	}
	return changed
}

func ValidateDailyReset(s *UserSubscription, now time.Time, count int, automatic bool) error {
	if s == nil || s.Group == nil || s.User == nil || s.DeletedAt != nil || s.User.DeletedAt != nil || s.User.Status != StatusActive ||
		s.Group.Status != StatusActive || s.Status != SubscriptionStatusActive || now.Before(s.StartsAt) ||
		!s.ExpiresAt.After(now) || !s.Group.IsSubscriptionType() || !s.Group.AllowSubscriptionDayReset {
		return ErrResetNotAllowed
	}
	g := s.Group
	if g.DailyLimitUSD == nil || *g.DailyLimitUSD <= 0 {
		return ErrResetNotAllowed
	}
	if !finiteNonnegative(s.DailyUsageUSD) || !finiteNonnegative(*g.DailyLimitUSD) ||
		!finiteNonnegative(s.WeeklyUsageUSD) || !finiteNonnegative(s.MonthlyUsageUSD) ||
		(g.WeeklyLimitUSD != nil && !finiteNonnegative(*g.WeeklyLimitUSD)) ||
		(g.MonthlyLimitUSD != nil && !finiteNonnegative(*g.MonthlyLimitUSD)) {
		return ErrResetInvalidData
	}
	if automatic && !s.AutoDailyResetEnabled {
		return ErrResetNotAllowed
	}
	if !s.ExpiresAt.Add(-24 * time.Hour).After(now) {
		return ErrResetInsufficientValidity
	}
	if count >= DailyResetLimit {
		return ErrResetDailyCountLimit
	}
	if g.HasWeeklyLimit() && s.WeeklyUsageUSD >= *g.WeeklyLimitUSD {
		return ErrResetWeeklyLimit
	}
	if g.HasMonthlyLimit() && s.MonthlyUsageUSD >= *g.MonthlyLimitUSD {
		return ErrResetMonthlyLimit
	}
	if s.DailyUsageUSD <= 0 || (automatic && s.DailyUsageUSD < *g.DailyLimitUSD) {
		return ErrResetNoUsage
	}
	return nil
}

func finiteNonnegative(v float64) bool { return !math.IsNaN(v) && !math.IsInf(v, 0) && v >= 0 }

func (s *SubscriptionDailyResetState) View() *DailyResetState {
	if s == nil || s.Subscription == nil {
		return nil
	}
	sub := s.Subscription
	view := &DailyResetState{
		Eligible:              sub.Group != nil && sub.Group.IsSubscriptionType() && sub.Group.AllowSubscriptionDayReset,
		AutoDailyResetEnabled: sub.AutoDailyResetEnabled, DailyResetCount: s.TodayCount,
		DailyResetLimit: DailyResetLimit, DailyResetVersion: sub.DailyResetVersion,
		CountDate: timezone.StartOfDay(s.ServerTime).Format("2006-01-02"), ServerTime: s.ServerTime,
	}
	if err := ValidateDailyReset(sub, s.ServerTime, s.TodayCount, false); err != nil {
		view.Reason = infraerrors.Reason(err)
	} else {
		view.CanReset = true
	}
	return view
}
