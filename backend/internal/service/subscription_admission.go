package service

import (
	"context"
	"errors"
	"math"
	"time"
)

// GetSubscriptionForAdmission reads the current group and subscription without
// using L1 or Redis. It is also called after waiting for a concurrency slot.
func (s *SubscriptionService) GetSubscriptionForAdmission(ctx context.Context, userID, groupID int64) (*UserSubscription, *Group, error) {
	for attempt := 0; attempt < 2; attempt++ {
		if s == nil || s.groupRepo == nil || s.userSubRepo == nil {
			return nil, nil, ErrBillingServiceUnavailable
		}
		group, err := s.groupRepo.GetByID(ctx, groupID)
		if err != nil {
			if errors.Is(err, ErrGroupNotFound) {
				return nil, nil, ErrSubscriptionInvalid
			}
			return nil, nil, ErrBillingServiceUnavailable.WithCause(err)
		}
		if group == nil || !group.IsActive() {
			return nil, group, ErrSubscriptionInvalid
		}
		if !group.IsSubscriptionType() {
			return nil, group, nil
		}
		sub, err := s.userSubRepo.GetByUserIDAndGroupID(ctx, userID, groupID)
		if err != nil {
			if errors.Is(err, ErrSubscriptionNotFound) {
				return nil, group, err
			}
			return nil, group, ErrBillingServiceUnavailable.WithCause(err)
		}
		if sub == nil || sub.UserID != userID || sub.GroupID != groupID || sub.DeletedAt != nil {
			return nil, group, ErrSubscriptionNotFound
		}
		if sub.Status != SubscriptionStatusActive {
			if sub.Status == SubscriptionStatusExpired {
				return nil, group, ErrSubscriptionExpired
			}
			return nil, group, ErrSubscriptionInvalid
		}
		if !sub.ExpiresAt.After(s.now()) {
			return nil, group, ErrSubscriptionExpired
		}
		sub.Group = group
		needsMaintenance, _ := s.ValidateAndCheckLimits(sub, group)
		if needsMaintenance {
			sub, err = s.EnsureWindowMaintenance(ctx, sub)
			if err != nil {
				return nil, group, ErrBillingServiceUnavailable.WithCause(err)
			}
			sub.Group = group
		}
		err = checkAuthoritativeSubscriptionLimits(sub, group, s.now())
		if attempt == 0 && errors.Is(err, ErrDailyLimitExceeded) && sub.AutoDailyResetEnabled && group.AllowSubscriptionDayReset {
			if resetErr := s.TriggerAutoDailyReset(ctx, sub.ID); resetErr != nil {
				return nil, group, ErrBillingServiceUnavailable.WithCause(resetErr)
			}
			continue
		}
		return sub, group, err
	}
	return nil, nil, ErrSubscriptionInvalid
}

func checkAuthoritativeSubscriptionLimits(sub *UserSubscription, group *Group, now time.Time) error {
	if sub == nil || sub.Status != SubscriptionStatusActive || sub.DeletedAt != nil || now.Before(sub.StartsAt) {
		return ErrSubscriptionInvalid
	}
	if !sub.ExpiresAt.After(now) {
		return ErrSubscriptionExpired
	}
	for _, value := range []float64{sub.DailyUsageUSD, sub.WeeklyUsageUSD, sub.MonthlyUsageUSD} {
		if math.IsNaN(value) || math.IsInf(value, 0) || value < 0 {
			return ErrSubscriptionInvalid
		}
	}
	for _, limit := range []*float64{group.DailyLimitUSD, group.WeeklyLimitUSD, group.MonthlyLimitUSD} {
		if limit != nil && (math.IsNaN(*limit) || math.IsInf(*limit, 0)) {
			return ErrSubscriptionInvalid
		}
	}
	if group.HasWeeklyLimit() && sub.WeeklyUsageUSD >= *group.WeeklyLimitUSD {
		return ErrWeeklyLimitExceeded
	}
	if group.HasMonthlyLimit() && sub.MonthlyUsageUSD >= *group.MonthlyLimitUSD {
		return ErrMonthlyLimitExceeded
	}
	if group.HasDailyLimit() && sub.DailyUsageUSD >= *group.DailyLimitUSD {
		return ErrDailyLimitExceeded
	}
	return nil
}
