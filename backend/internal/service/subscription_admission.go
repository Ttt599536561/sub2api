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
	return s.getSubscriptionForAdmission(ctx, userID, groupID, 0)
}

// GetSubscriptionByIDForAdmission preserves the entitlement selected for this
// request. A newly granted welfare card must not redirect an in-flight paid
// request, and an exhausted welfare card must not charge a paid subscription.
func (s *SubscriptionService) GetSubscriptionByIDForAdmission(ctx context.Context, userID, groupID, subscriptionID int64) (*UserSubscription, *Group, error) {
	if subscriptionID <= 0 {
		return nil, nil, ErrSubscriptionInvalid
	}
	return s.getSubscriptionForAdmission(ctx, userID, groupID, subscriptionID)
}

func (s *SubscriptionService) getSubscriptionForAdmission(ctx context.Context, userID, groupID, subscriptionID int64) (*UserSubscription, *Group, error) {
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
		var sub *UserSubscription
		var welfareErr error
		if subscriptionID > 0 {
			sub, err = s.userSubRepo.GetByID(ctx, subscriptionID)
		} else {
			// The optional repository keeps standard-only integrations compatible.
			// Welfare candidates always come from the database, never the standard
			// user/group cache. Every candidate retains its own expiry and usage.
			if repo, ok := s.userSubRepo.(interface {
				ListWelfareByUserAndGroup(context.Context, int64, int64) ([]UserSubscription, error)
			}); ok && attempt == 0 {
				candidates, listErr := repo.ListWelfareByUserAndGroup(ctx, userID, groupID)
				if listErr != nil {
					return nil, group, ErrBillingServiceUnavailable.WithCause(listErr)
				}
				for i := range candidates {
					candidate := &candidates[i]
					if !candidate.IsWelfare() || candidate.UserID != userID || candidate.GroupID != groupID {
						return nil, group, ErrSubscriptionInvalid
					}
					if candidate.DeletedAt != nil || candidate.WelfareCycleClosed || candidate.Status != SubscriptionStatusActive || !candidate.ExpiresAt.After(s.now()) || s.now().Before(candidate.StartsAt) {
						continue
					}
					candidate, candidateErr := s.prepareSubscriptionForAdmission(ctx, candidate, group)
					if candidateErr == nil {
						return candidate, candidate.Group, nil
					}
					if !isSubscriptionQuotaExceeded(candidateErr) && !errors.Is(candidateErr, ErrSubscriptionExpired) && !errors.Is(candidateErr, ErrSubscriptionNotFound) {
						return nil, group, candidateErr
					}
					welfareErr = candidateErr
				}
			}
			sub, err = s.userSubRepo.GetByUserIDAndGroupID(ctx, userID, groupID)
		}
		if err != nil {
			if errors.Is(err, ErrSubscriptionNotFound) {
				if welfareErr != nil {
					return nil, group, welfareErr
				}
				return nil, group, err
			}
			return nil, group, ErrBillingServiceUnavailable.WithCause(err)
		}
		if sub == nil || sub.UserID != userID || sub.GroupID != groupID || sub.DeletedAt != nil || (subscriptionID > 0 && sub.ID != subscriptionID) {
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
		sub, err = s.prepareSubscriptionForAdmission(ctx, sub, group)
		if sub == nil {
			return nil, group, err
		}
		group = sub.Group
		if attempt == 0 && !sub.IsWelfare() && errors.Is(err, ErrDailyLimitExceeded) && sub.AutoDailyResetEnabled && group.AllowSubscriptionDayReset {
			if resetErr := s.TriggerAutoDailyReset(ctx, sub.ID); resetErr != nil {
				return nil, group, ErrBillingServiceUnavailable.WithCause(resetErr)
			}
			continue
		}
		return sub, group, err
	}
	return nil, nil, ErrSubscriptionInvalid
}

func isSubscriptionQuotaExceeded(err error) bool {
	return errors.Is(err, ErrDailyLimitExceeded) || errors.Is(err, ErrWeeklyLimitExceeded) || errors.Is(err, ErrMonthlyLimitExceeded)
}

func (s *SubscriptionService) prepareSubscriptionForAdmission(ctx context.Context, sub *UserSubscription, group *Group) (*UserSubscription, error) {
	group = sub.EffectiveGroup(group)
	sub.Group = group
	needsMaintenance, _ := s.ValidateAndCheckLimits(sub, group)
	if needsMaintenance {
		var err error
		sub, err = s.EnsureWindowMaintenance(ctx, sub)
		if err != nil {
			return nil, ErrBillingServiceUnavailable.WithCause(err)
		}
		// A concurrent grant can change the welfare caps while a window is being
		// maintained; use the refreshed subscription rather than its old caps.
		sub.Group = sub.EffectiveGroup(group)
	}
	return sub, checkAuthoritativeSubscriptionLimits(sub, sub.Group, s.now())
}

func checkAuthoritativeSubscriptionLimits(sub *UserSubscription, group *Group, now time.Time) error {
	if sub == nil || sub.Status != SubscriptionStatusActive || sub.DeletedAt != nil || sub.WelfareCycleClosed || now.Before(sub.StartsAt) {
		return ErrSubscriptionInvalid
	}
	if !sub.ExpiresAt.After(now) {
		return ErrSubscriptionExpired
	}
	group = sub.EffectiveGroup(group)
	if group == nil {
		return ErrSubscriptionInvalid
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
	if sub.IsWelfare() && (group.MonthlyLimitUSD == nil || *group.MonthlyLimitUSD < 0) {
		return ErrSubscriptionInvalid
	}
	if group.HasWeeklyLimit() && sub.WeeklyUsageUSD >= *group.WeeklyLimitUSD {
		return ErrWeeklyLimitExceeded
	}
	if (group.HasMonthlyLimit() || sub.IsWelfare()) && sub.MonthlyUsageUSD >= *group.MonthlyLimitUSD {
		return ErrMonthlyLimitExceeded
	}
	if group.HasDailyLimit() && sub.DailyUsageUSD >= *group.DailyLimitUSD {
		return ErrDailyLimitExceeded
	}
	return nil
}
