package service

import (
	"context"
	"errors"
	"time"
)

// activeSubscriptionForGroupAccess authorizes binding a key to a group when
// either entitlement is valid. It does not select a billing source or consult
// quota: the gateway checks those independently for each request.
func activeSubscriptionForGroupAccess(ctx context.Context, repo UserSubscriptionRepository, userID, groupID int64) (*UserSubscription, error) {
	if repo == nil {
		return nil, ErrBillingServiceUnavailable
	}
	standard, err := repo.GetActiveByUserIDAndGroupID(ctx, userID, groupID)
	if err == nil {
		return standard, nil
	}
	if !errors.Is(err, ErrSubscriptionNotFound) {
		return nil, err
	}
	welfareRepo, ok := repo.(WelfareSubscriptionRepository)
	if !ok {
		return nil, ErrSubscriptionNotFound
	}
	candidates, err := welfareRepo.ListWelfareByUserAndGroup(ctx, userID, groupID)
	if err != nil {
		return nil, err
	}
	now := time.Now()
	for i := range candidates {
		sub := &candidates[i]
		if sub.IsWelfare() && sub.UserID == userID && sub.GroupID == groupID &&
			sub.DeletedAt == nil && !sub.WelfareCycleClosed && sub.Status == SubscriptionStatusActive &&
			!now.Before(sub.StartsAt) && sub.ExpiresAt.After(now) {
			return sub, nil
		}
	}
	return nil, ErrSubscriptionNotFound
}
