package service

import (
	"context"
	"errors"
)

// GetSubscriptionForUsage selects the entitlement shown by /v1/usage without
// rejecting an exhausted subscription or resetting its persisted windows.
// Payment fulfillment deliberately continues to use GetActiveSubscription,
// which only returns the standard entitlement.
func (s *SubscriptionService) GetSubscriptionForUsage(ctx context.Context, userID, groupID int64) (*UserSubscription, error) {
	repo, ok := s.userSubRepo.(interface {
		ListWelfareByUserAndGroup(context.Context, int64, int64) ([]UserSubscription, error)
	})
	if !ok {
		return s.GetActiveSubscription(ctx, userID, groupID)
	}
	candidates, err := repo.ListWelfareByUserAndGroup(ctx, userID, groupID)
	if err != nil {
		return nil, err
	}
	var exhausted *UserSubscription
	for i := range candidates {
		sub := &candidates[i]
		if !sub.IsWelfare() || sub.UserID != userID || sub.GroupID != groupID {
			return nil, ErrSubscriptionInvalid
		}
		if sub.WelfareCycleClosed || sub.DeletedAt != nil || sub.Status != SubscriptionStatusActive || !sub.ExpiresAt.After(s.now()) || s.now().Before(sub.StartsAt) {
			continue
		}
		group := sub.Group
		if group == nil {
			group, err = s.groupRepo.GetByID(ctx, groupID)
			if err != nil {
				return nil, err
			}
		}
		if group == nil || !group.IsActive() || !group.IsSubscriptionType() {
			return nil, ErrSubscriptionInvalid
		}
		normalizeExpiredWindowsAt(candidates[i:i+1], s.now())
		sub.Group = sub.EffectiveGroup(group)
		err = checkAuthoritativeSubscriptionLimits(sub, sub.Group, s.now())
		if err == nil {
			return sub, nil
		}
		if !isSubscriptionQuotaExceeded(err) {
			return nil, err
		}
		exhausted = sub
	}
	standard, err := s.GetActiveSubscription(ctx, userID, groupID)
	if errors.Is(err, ErrSubscriptionNotFound) && exhausted != nil {
		return exhausted, nil
	}
	return standard, err
}
