package service

import (
	"math"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/timezone"
)

const (
	subscriptionDayDuration   = 24 * time.Hour
	subscriptionMonthDuration = 30 * subscriptionDayDuration
)

const (
	SubscriptionEntitlementStandard = "standard"
	SubscriptionEntitlementWelfare  = "welfare"
)

type UserSubscription struct {
	ID      int64
	UserID  int64
	GroupID int64

	EntitlementKind        string
	WelfareDailyLimitUSD   *float64
	WelfareWeeklyLimitUSD  *float64
	WelfareMonthlyLimitUSD *float64
	WelfareGrantCount      int64
	WelfareCycleClosed     bool

	// Paid quota belongs to this term. The legacy horizon records the time
	// already covered before the first upfront monthly renewal.
	PaidMonthlyLimitUSD        *float64
	PaidMonthlyLegacyExpiresAt *time.Time
	PaidMonthlyLegacyLimitUSD  *float64

	// One-time credit belongs to this subscription's StartsAt term. It never
	// resets with daily/weekly/monthly usage and may retain settled overuse.
	SupplementGrantedUSD float64
	SupplementUsedUSD    float64

	StartsAt  time.Time
	ExpiresAt time.Time
	Status    string

	DailyWindowStart   *time.Time
	WeeklyWindowStart  *time.Time
	MonthlyWindowStart *time.Time

	DailyUsageUSD   float64
	WeeklyUsageUSD  float64
	MonthlyUsageUSD float64

	AutoDailyResetEnabled      bool
	DailyResetVersion          int64
	PreserveCalendarDailyReset bool
	DailyResetState            *DailyResetState

	AssignedBy *int64
	AssignedAt time.Time
	Notes      string

	CreatedAt time.Time
	UpdatedAt time.Time
	DeletedAt *time.Time

	User           *User
	Group          *Group
	AssignedByUser *User
}

func (s *UserSubscription) IsWelfare() bool {
	return s != nil && s.EntitlementKind == SubscriptionEntitlementWelfare
}

// EffectiveGroup keeps entitlement-specific limits local to this request.
func (s *UserSubscription) EffectiveGroup(group *Group) *Group {
	if group == nil || s == nil || (!s.IsWelfare() && s.PaidMonthlyLimitUSD == nil) {
		return group
	}
	effective := *group
	if s.IsWelfare() {
		effective.DailyLimitUSD = s.WelfareDailyLimitUSD
		effective.WeeklyLimitUSD = s.WelfareWeeklyLimitUSD
	}
	effective.MonthlyLimitUSD = s.EffectiveMonthlyLimitUSD(group)
	return &effective
}

func (s *UserSubscription) EffectiveMonthlyLimitUSD(group *Group) *float64 {
	if s.IsWelfare() {
		return s.WelfareMonthlyLimitUSD
	}
	if s != nil && s.PaidMonthlyLimitUSD != nil {
		return s.PaidMonthlyLimitUSD
	}
	if group == nil {
		return nil
	}
	return group.MonthlyLimitUSD
}

// HasFiniteMonthlyLimit distinguishes a zero paid or welfare cap from an
// ordinary group whose zero configuration means unrestricted monthly usage.
func (s *UserSubscription) HasFiniteMonthlyLimit(group *Group) bool {
	return s.IsWelfare() || (s != nil && s.PaidMonthlyLimitUSD != nil) ||
		(group != nil && group.HasMonthlyLimit())
}

func (s *UserSubscription) IsActive() bool {
	return s.Status == SubscriptionStatusActive && time.Now().Before(s.ExpiresAt)
}

func (s *UserSubscription) IsExpired() bool {
	return time.Now().After(s.ExpiresAt)
}

func (s *UserSubscription) DaysRemaining() int {
	return s.daysRemainingAt(time.Now())
}

func (s *UserSubscription) daysRemainingAt(now time.Time) int {
	remaining := s.ExpiresAt.Sub(now)
	if remaining <= 0 {
		return 0
	}

	days := int(remaining / subscriptionDayDuration)
	if remaining%subscriptionDayDuration != 0 {
		days++
	}
	return days
}

func (s *UserSubscription) IsWindowActivated() bool {
	return s.DailyWindowStart != nil || s.WeeklyWindowStart != nil || s.MonthlyWindowStart != nil
}

func (s *UserSubscription) needsWindowActivation(group *Group) bool {
	if !s.IsWindowActivated() {
		return true
	}
	group = s.EffectiveGroup(group)
	return (group != nil && (group.HasDailyLimit() && s.DailyWindowStart == nil || group.HasWeeklyLimit() && s.WeeklyWindowStart == nil)) ||
		(s.HasFiniteMonthlyLimit(group) && s.MonthlyWindowStart == nil)
}

func (s *UserSubscription) HasOneTimeDailyQuota() bool {
	if s == nil || s.PreserveCalendarDailyReset || s.StartsAt.IsZero() || s.ExpiresAt.IsZero() {
		return false
	}
	return !s.ExpiresAt.After(s.StartsAt.AddDate(0, 0, 1))
}

func (s *UserSubscription) NeedsDailyReset() bool {
	return s.NeedsDailyResetAt(time.Now())
}

func (s *UserSubscription) NeedsDailyResetAt(now time.Time) bool {
	_, ok := s.automaticDailyWindowStartAt(now)
	return ok
}

func (s *UserSubscription) NeedsWeeklyReset() bool {
	return s.NeedsWeeklyResetAt(time.Now())
}

func (s *UserSubscription) NeedsWeeklyResetAt(now time.Time) bool {
	if s.WeeklyWindowStart == nil {
		return false
	}
	return !now.Before(s.WeeklyWindowStart.Add(7 * 24 * time.Hour))
}

func (s *UserSubscription) NeedsMonthlyReset() bool {
	return s.NeedsMonthlyResetAt(time.Now())
}

func (s *UserSubscription) NeedsMonthlyResetAt(now time.Time) bool {
	if s.IsWelfare() || s.MonthlyWindowStart == nil {
		return false
	}
	if s.PaidMonthlyLimitUSD != nil {
		_, _, ok := s.MonthlyWindowAdvanceAt(now)
		return ok
	}
	return !now.Before(s.MonthlyWindowStart.Add(subscriptionMonthDuration))
}

func (s *UserSubscription) canAutomaticallyResetDailyAt(now time.Time) bool {
	_, ok := s.automaticDailyWindowStartAt(now)
	return ok
}

// automaticDailyWindowStartAt 计算日窗口按“配置时区日历日”对齐后的当前窗口起点。
// 日额度固定在每天 0 点刷新（与周/月的期限对齐滚动窗口语义不同），因此只要持久化
// 的窗口起点落在更早的日历日，就允许推进到今天 0 点。手动重置、激活等写入的任何
// 非 0 点锚点都会在下一个 0 点被拉回日历日边界，不会永久漂移刷新时刻。
func (s *UserSubscription) automaticDailyWindowStartAt(now time.Time) (time.Time, bool) {
	if s.DailyWindowStart == nil {
		return time.Time{}, false
	}
	if s.HasOneTimeDailyQuota() {
		return time.Time{}, false
	}
	today := timezone.StartOfDay(now)
	if !today.After(timezone.StartOfDay(*s.DailyWindowStart)) {
		return time.Time{}, false
	}
	return today, true
}

func (s *UserSubscription) canAutomaticallyResetWeeklyAt(now time.Time) bool {
	_, ok := s.automaticWindowStartAt(s.WeeklyWindowStart, 7*24*time.Hour, now)
	return ok
}

func (s *UserSubscription) canAutomaticallyResetMonthlyAt(now time.Time) bool {
	_, _, ok := s.MonthlyWindowAdvanceAt(now)
	return ok
}

// MonthlyWindowAdvanceAt returns the new anchor and any allowance still due
// for legacy months. An ordinary window reset has zero allowance.
func (s *UserSubscription) MonthlyWindowAdvanceAt(now time.Time) (time.Time, float64, bool) {
	start, ok := s.automaticWindowStartAt(s.MonthlyWindowStart, subscriptionMonthDuration, now)
	if !ok || s.PaidMonthlyLimitUSD == nil {
		return start, 0, ok
	}
	periods := start.Sub(s.windowResetAnchor(*s.MonthlyWindowStart)) / subscriptionMonthDuration
	return start, QuantizeUsageBillingAmount(float64(periods) * *s.PaidMonthlyLegacyLimitUSD), true
}

func (s *UserSubscription) ApplyMonthlyWindowAdvance(start time.Time, paidAllowanceUSD float64) {
	s.MonthlyWindowStart = &start
	if s.PaidMonthlyLimitUSD == nil {
		s.MonthlyUsageUSD = 0
		return
	}
	limit := QuantizeUsageBillingAmount(*s.PaidMonthlyLimitUSD + paidAllowanceUSD)
	s.PaidMonthlyLimitUSD = &limit
}

func (s *UserSubscription) monthlyRolloverExpiry() time.Time {
	if s.PaidMonthlyLimitUSD == nil {
		return s.ExpiresAt
	}
	if s.PaidMonthlyLegacyExpiresAt == nil || s.PaidMonthlyLegacyLimitUSD == nil {
		return time.Time{}
	}
	if s.PaidMonthlyLegacyExpiresAt.Before(s.ExpiresAt) {
		return *s.PaidMonthlyLegacyExpiresAt
	}
	return s.ExpiresAt
}

// windowResetAnchor 返回周/月窗口实际推进所依据的锚点。
// 早期订阅把首个窗口初始化在开通日零点；只有这个初始值是无歧义的，之后出现的
// 零点锚点可能来自手动重置，必须保持权威。
// 自动推进（automaticWindowStartAt）与对外展示的重置时间（WeeklyResetTime/
// MonthlyResetTime）必须共用这一修正，否则仪表盘显示的重置时间会早于窗口实际
// 滚动的时间。
// 日窗口按日历日对齐（automaticDailyWindowStartAt），不走这里。
func (s *UserSubscription) windowResetAnchor(previous time.Time) time.Time {
	legacyAnchor := startOfDay(s.StartsAt)
	if legacyAnchor.Before(s.StartsAt) && previous.Equal(legacyAnchor) {
		return s.StartsAt
	}
	return previous
}

// automaticWindowStartAt 计算周/月窗口（期限对齐滚动窗口）的当前窗口起点。
// 窗口从锚点按整数个 period 步进，且不越过订阅到期时间，避免最后一个不完整
// 周期重复发放额度（issue #5051）。日窗口不走此函数，见 automaticDailyWindowStartAt。
func (s *UserSubscription) automaticWindowStartAt(previous *time.Time, period time.Duration, now time.Time) (time.Time, bool) {
	// Welfare monthly quota is the total granted for this entitlement's entire
	// term. An administrator may extend that term without issuing another grant.
	// Keep this guard at the shared primitive so every maintenance path preserves
	// consumed quota, including locked daily-reset maintenance and list views.
	if s.IsWelfare() && period == subscriptionMonthDuration {
		return time.Time{}, false
	}
	if previous == nil {
		return time.Time{}, false
	}

	expiresAt := s.ExpiresAt
	if period == subscriptionMonthDuration {
		expiresAt = s.monthlyRolloverExpiry()
	}
	anchor := s.windowResetAnchor(*previous)
	next := anchor.Add(period)
	if now.Before(next) || !next.Before(expiresAt) {
		return time.Time{}, false
	}

	periods := now.Sub(anchor) / period
	lastPeriodBeforeExpiry := (expiresAt.Sub(anchor) - 1) / period
	if periods > lastPeriodBeforeExpiry {
		periods = lastPeriodBeforeExpiry
	}
	return anchor.Add(periods * period), true
}

func (s *UserSubscription) DailyResetTime() *time.Time {
	if s.DailyWindowStart == nil {
		return nil
	}
	if s.HasOneTimeDailyQuota() {
		t := s.ExpiresAt
		return &t
	}
	// 日窗口按日历日对齐：下次刷新固定在窗口起点所在日的次日 0 点。
	t := timezone.StartOfDay(*s.DailyWindowStart).AddDate(0, 0, 1)
	return &t
}

func (s *UserSubscription) WeeklyResetTime() *time.Time {
	if s.WeeklyWindowStart == nil {
		return nil
	}
	t := s.windowResetAnchor(*s.WeeklyWindowStart).Add(7 * 24 * time.Hour)
	return &t
}

func (s *UserSubscription) MonthlyResetTime() *time.Time {
	if s.PaidMonthlyLimitUSD != nil {
		if s.MonthlyWindowStart != nil {
			next := s.windowResetAnchor(*s.MonthlyWindowStart).Add(subscriptionMonthDuration)
			if next.Before(s.monthlyRolloverExpiry()) {
				return &next
			}
		}
		expiresAt := s.ExpiresAt
		return &expiresAt
	}
	if s.MonthlyWindowStart == nil {
		return nil
	}
	if s.IsWelfare() {
		// The quota ends with this welfare cycle; it does not refresh at day 30.
		t := s.ExpiresAt
		return &t
	}
	t := s.windowResetAnchor(*s.MonthlyWindowStart).Add(subscriptionMonthDuration)
	return &t
}

func (s *UserSubscription) CheckDailyLimit(group *Group, additionalCost float64) bool {
	group = s.EffectiveGroup(group)
	if !group.HasDailyLimit() {
		return true
	}
	if s.SupplementRemainingUSD() > 0 {
		return additionalCost <= math.Max(0, *group.DailyLimitUSD-s.DailyUsageUSD)+s.SupplementRemainingUSD()
	}
	return s.DailyUsageUSD+additionalCost <= *group.DailyLimitUSD
}

func (s *UserSubscription) CheckWeeklyLimit(group *Group, additionalCost float64) bool {
	group = s.EffectiveGroup(group)
	if !group.HasWeeklyLimit() {
		return true
	}
	if s.SupplementRemainingUSD() > 0 {
		return additionalCost <= math.Max(0, *group.WeeklyLimitUSD-s.WeeklyUsageUSD)+s.SupplementRemainingUSD()
	}
	return s.WeeklyUsageUSD+additionalCost <= *group.WeeklyLimitUSD
}

func (s *UserSubscription) CheckMonthlyLimit(group *Group, additionalCost float64) bool {
	if !s.HasFiniteMonthlyLimit(group) {
		return true
	}
	limit := s.EffectiveMonthlyLimitUSD(group)
	if limit == nil {
		return false
	}
	if supplement := s.SupplementRemainingUSD(); supplement > 0 {
		return additionalCost <= math.Max(0, *limit-s.MonthlyUsageUSD)+supplement
	}
	return *limit > 0 && s.MonthlyUsageUSD+additionalCost <= *limit
}

func (s *UserSubscription) CheckAllLimits(group *Group, additionalCost float64) (daily, weekly, monthly bool) {
	daily = s.CheckDailyLimit(group, additionalCost)
	weekly = s.CheckWeeklyLimit(group, additionalCost)
	monthly = s.CheckMonthlyLimit(group, additionalCost)
	return
}
