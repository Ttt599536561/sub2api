package service

import (
	"context"
	"testing"
	"testing/synctest"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

func TestProvideSubscriptionServiceDailyResetScannerRunMode(t *testing.T) {
	for _, tt := range []struct {
		name    string
		cfg     *config.Config
		enabled bool
	}{
		{name: "simple", cfg: &config.Config{RunMode: config.RunModeSimple}},
		{name: "standard", cfg: &config.Config{RunMode: config.RunModeStandard}, enabled: true},
		{name: "nil config", enabled: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				state := dailyResetScannerState(77)
				before := state.Subscription.ExpiresAt
				stateRepo := &dailyResetServiceRepo{state: state}
				var scans, applies int
				repo := &dailyResetScannerRepo{
					list: func(context.Context, int64, int) ([]SubscriptionDailyResetCandidate, error) {
						scans++
						return []SubscriptionDailyResetCandidate{{ID: state.Subscription.ID}}, nil
					},
					state: stateRepo.GetState,
					apply: func(ctx context.Context, command *SubscriptionDailyResetCommand) (*SubscriptionDailyResetResult, error) {
						applies++
						return stateRepo.Apply(ctx, command)
					},
				}
				subRepo := &dailyResetScannerSubscriptionRepo{get: func(context.Context, int64) (*UserSubscription, error) {
					return state.Subscription, nil
				}}
				svc := ProvideSubscriptionService(nil, subRepo, nil, nil, tt.cfg, repo)
				defer svc.Stop()
				synctest.Wait()

				if tt.enabled {
					require.Equal(t, 1, applies, "standard mode must still reset exhausted subscriptions at startup")
					require.Equal(t, before.Add(-24*time.Hour), state.Subscription.ExpiresAt)
					require.Equal(t, 1, scans)
				} else {
					require.Zero(t, applies, "simple mode must not spend subscription validity")
					require.Equal(t, before, state.Subscription.ExpiresAt)
					require.Zero(t, scans, "simple mode must not start the scanner")
				}

				time.Sleep(30 * time.Second)
				synctest.Wait()
				if tt.enabled {
					require.Equal(t, 2, scans, "standard mode must retain periodic recovery scans")
					require.Equal(t, 1, applies, "the refreshed daily quota must not be reset again")
				} else {
					require.Zero(t, scans)
					require.Zero(t, applies)
					require.Equal(t, before, state.Subscription.ExpiresAt)
				}
			})
		})
	}
}

func TestProvideSubscriptionServiceSimpleModeDailyResetEntryPoints(t *testing.T) {
	for _, action := range []string{"automatic trigger", "manual reset", "enable automatic reset", "disable automatic reset"} {
		t.Run(action, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				state := dailyResetScannerState(77)
				before := *state.Subscription
				stateRepo := &dailyResetServiceRepo{state: state}
				repo := &dailyResetScannerRepo{
					SubscriptionDailyResetRepository: stateRepo,
					list: func(context.Context, int64, int) ([]SubscriptionDailyResetCandidate, error) {
						return nil, nil
					},
					state: stateRepo.GetState,
					apply: stateRepo.Apply,
				}
				subRepo := &dailyResetScannerSubscriptionRepo{get: func(context.Context, int64) (*UserSubscription, error) {
					return state.Subscription, nil
				}}
				svc := ProvideSubscriptionService(nil, subRepo, nil, nil, &config.Config{RunMode: config.RunModeSimple}, repo)
				defer svc.Stop()
				synctest.Wait()

				ctx := context.Background()
				switch action {
				case "automatic trigger":
					require.NoError(t, svc.TriggerAutoDailyReset(ctx, before.ID))
				case "manual reset":
					out, err := svc.ResetSubscriptionDaily(ctx, before.UserID, before.ID, before.DailyResetVersion, state.View().CountDate, "simple-mode-reset")
					require.ErrorIs(t, err, ErrResetNotAllowed)
					require.Nil(t, out)
				case "enable automatic reset", "disable automatic reset":
					out, err := svc.SetAutoDailyReset(ctx, before.UserID, before.ID, before.DailyResetVersion, action == "enable automatic reset")
					require.ErrorIs(t, err, ErrResetNotAllowed)
					require.Nil(t, out)
				}
				require.Nil(t, stateRepo.command, "simple mode must not attempt a paid reset")
				require.Equal(t, before, *state.Subscription, "simple mode must preserve validity, quota, and reset preferences")
			})
		})
	}
}
