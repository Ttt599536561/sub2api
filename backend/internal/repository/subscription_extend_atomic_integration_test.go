//go:build integration

package repository

import (
	"context"
	"errors"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

type adjustmentRefreshFailureRepository struct {
	service.UserSubscriptionRepository
	refreshError error
}

func (r *adjustmentRefreshFailureRepository) GetByID(ctx context.Context, id int64) (*service.UserSubscription, error) {
	if r.refreshError != nil {
		return nil, r.refreshError
	}
	return r.UserSubscriptionRepository.GetByID(ctx, id)
}

func TestSubscriptionAdjustmentReadFailure_PostgresRollsBack(t *testing.T) {
	_, original, _ := newDailyResetFixture(t)
	ctx := context.Background()
	client := testEntClient(t)
	refreshErr := errors.New("read failed after duration adjustment")
	repo := &adjustmentRefreshFailureRepository{
		UserSubscriptionRepository: NewUserSubscriptionRepository(client),
		refreshError:               refreshErr,
	}
	svc := service.NewSubscriptionService(nil, repo, nil, client, nil)
	t.Cleanup(svc.Stop)

	_, err := svc.ExtendSubscription(ctx, original.ID, -1)
	require.ErrorIs(t, err, refreshErr)
	unchanged, err := client.UserSubscription.Get(ctx, original.ID)
	require.NoError(t, err)
	require.True(t, original.ExpiresAt.Equal(unchanged.ExpiresAt))
	require.Equal(t, original.DailyResetVersion, unchanged.DailyResetVersion)
	require.Equal(t, original.DailyUsageUSD, unchanged.DailyUsageUsd)

	repo.refreshError = nil
	updated, err := svc.ExtendSubscription(ctx, original.ID, -1)
	require.NoError(t, err)
	require.True(t, original.ExpiresAt.AddDate(0, 0, -1).Equal(updated.ExpiresAt))
	require.Equal(t, original.DailyResetVersion+1, updated.DailyResetVersion)
}
