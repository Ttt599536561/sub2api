package service

import (
	"context"
	"math"
)

type SubscriptionSupplementReader interface {
	LoadSubscriptionSupplement(context.Context, *UserSubscription) error
}

func loadSubscriptionSupplement(ctx context.Context, repo UserSubscriptionRepository, sub *UserSubscription) error {
	if reader, ok := repo.(SubscriptionSupplementReader); ok {
		return reader.LoadSubscriptionSupplement(ctx, sub)
	}
	return nil
}

func (s *SubscriptionService) loadSubscriptionSupplements(ctx context.Context, subs []UserSubscription) error {
	for i := range subs {
		if err := loadSubscriptionSupplement(ctx, s.userSubRepo, &subs[i]); err != nil {
			return err
		}
	}
	return nil
}

func (s *UserSubscription) HasSubscriptionSupplement() bool {
	return s != nil && (s.SupplementGrantedUSD > 0 || s.SupplementUsedUSD > 0)
}

// SupplementRemainingUSD excludes already-settled overuse without erasing it.
func (s *UserSubscription) SupplementRemainingUSD() float64 {
	if s == nil {
		return 0
	}
	return math.Max(0, s.SupplementGrantedUSD-s.SupplementUsedUSD)
}

// BaseQuotaRemaining is the capacity shared by all finite quota windows.
func (s *UserSubscription) BaseQuotaRemaining(group *Group) float64 {
	group = s.EffectiveGroup(group)
	if s == nil || group == nil {
		return 0
	}
	remaining := math.Inf(1)
	if group.HasDailyLimit() {
		remaining = math.Min(remaining, math.Max(0, *group.DailyLimitUSD-s.DailyUsageUSD))
	}
	if group.HasWeeklyLimit() {
		remaining = math.Min(remaining, math.Max(0, *group.WeeklyLimitUSD-s.WeeklyUsageUSD))
	}
	if group.HasMonthlyLimit() || s.IsWelfare() {
		if group.MonthlyLimitUSD == nil {
			return 0
		}
		remaining = math.Min(remaining, math.Max(0, *group.MonthlyLimitUSD-s.MonthlyUsageUSD))
	}
	return remaining
}

func (s *UserSubscription) AvailableQuota(group *Group) float64 {
	return s.BaseQuotaRemaining(group) + s.SupplementRemainingUSD()
}
