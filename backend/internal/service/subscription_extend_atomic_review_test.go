//go:build unit

package service_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/repository"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

type subscriptionRefreshFailureRepo struct {
	service.UserSubscriptionRepository
	refreshError error
}

func (r *subscriptionRefreshFailureRepo) GetByIDForUpdate(ctx context.Context, id int64) (*service.UserSubscription, error) {
	// SQLite has no SELECT FOR UPDATE. Keep the real transaction and persistence
	// while testing the failure after the write, independently of row locking.
	return r.UserSubscriptionRepository.GetByID(ctx, id)
}

func (r *subscriptionRefreshFailureRepo) GetByID(ctx context.Context, id int64) (*service.UserSubscription, error) {
	if r.refreshError != nil {
		return nil, r.refreshError
	}
	return r.UserSubscriptionRepository.GetByID(ctx, id)
}

func TestPostmergeExtendSubscription_ReadFailureLeavesNoMutation(t *testing.T) {
	for _, tc := range []struct {
		name   string
		status string
		expiry time.Duration
		days   int
	}{
		{"refund shortening", service.SubscriptionStatusActive, 30 * 24 * time.Hour, -7},
		{"active extension", service.SubscriptionStatusActive, 30 * 24 * time.Hour, 7},
		{"expired renewal", service.SubscriptionStatusExpired, -time.Hour, 7},
		{"status reactivation", service.SubscriptionStatusExpired, 30 * 24 * time.Hour, 7},
	} {
		t.Run(tc.name, func(t *testing.T) {
			client, _ := postmergeResetEntFixture(t)
			ctx := context.Background()
			expires := time.Now().UTC().Add(tc.expiry).Truncate(time.Microsecond)
			original := postmergeResetSubscription(t, client, "atomic-extend", tc.status, expires)
			realRepo := repository.NewUserSubscriptionRepository(client)
			refreshErr := errors.New("subscription refresh unavailable")
			repo := &subscriptionRefreshFailureRepo{UserSubscriptionRepository: realRepo, refreshError: refreshErr}
			svc := service.NewSubscriptionService(nil, repo, nil, client, nil)
			t.Cleanup(svc.Stop)

			_, err := svc.ExtendSubscription(ctx, original.ID, tc.days)
			require.ErrorIs(t, err, refreshErr)
			afterFailure, err := client.UserSubscription.Get(ctx, original.ID)
			require.NoError(t, err)
			require.True(t, original.ExpiresAt.Equal(afterFailure.ExpiresAt), "a failed adjustment must not leave a committed duration change")
			require.Equal(t, original.Status, afterFailure.Status)
			require.Equal(t, original.DailyResetVersion, afterFailure.DailyResetVersion)
			require.Equal(t, original.DailyUsageUsd, afterFailure.DailyUsageUsd)
			require.Equal(t, original.AutoDailyResetEnabled, afterFailure.AutoDailyResetEnabled)
			require.Equal(t, original.PreserveCalendarDailyReset, afterFailure.PreserveCalendarDailyReset)

			repo.refreshError = nil
			updated, err := svc.ExtendSubscription(ctx, original.ID, tc.days)
			require.NoError(t, err)
			persisted, err := client.UserSubscription.Get(ctx, original.ID)
			require.NoError(t, err)
			require.True(t, updated.ExpiresAt.Equal(persisted.ExpiresAt))
			require.Equal(t, updated.DailyResetVersion, persisted.DailyResetVersion)
			if tc.expiry > 0 {
				require.True(t, original.ExpiresAt.AddDate(0, 0, tc.days).Equal(persisted.ExpiresAt), "retry must adjust the duration exactly once")
			} else {
				require.False(t, updated.AutoDailyResetEnabled)
				require.Zero(t, updated.DailyUsageUSD)
			}
		})
	}
}
