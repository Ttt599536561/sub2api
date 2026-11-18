package service

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestPaidMonthlyRenewalRejectsRemovedMonthlyCapWithoutChangingTerm(t *testing.T) {
	now := time.Date(2026, 11, 18, 12, 0, 0, 0, time.UTC)
	term := UserSubscription{ID: 1, UserID: 2, GroupID: 3, Status: SubscriptionStatusActive,
		StartsAt: now.AddDate(0, 0, -10), ExpiresAt: now.AddDate(0, 0, 20), MonthlyUsageUSD: 2500}
	repo := &lockingRenewalRepo{current: term, stale: term}
	groupRepo := &subscriptionGroupRepoStub{group: &Group{ID: term.GroupID, SubscriptionType: SubscriptionTypeSubscription}}
	svc := NewSubscriptionService(groupRepo, repo, nil, nil, nil)
	t.Cleanup(svc.Stop)
	svc.now = func() time.Time { return now }
	_, _, err := svc.assignOrExtendPaidMonthlySubscription(context.Background(), &AssignSubscriptionInput{
		UserID: term.UserID, GroupID: term.GroupID, ValidityDays: 30,
	}, 10, 1120)
	require.ErrorContains(t, err, "monthly quota was removed")
	require.Equal(t, term, repo.current)
}
