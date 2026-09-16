package service

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/require"
)

type dailyResetScannerRepo struct {
	SubscriptionDailyResetRepository
	list  func(context.Context, int64, int) ([]SubscriptionDailyResetCandidate, error)
	state func(context.Context, int64, int64) (*SubscriptionDailyResetState, error)
	apply func(context.Context, *SubscriptionDailyResetCommand) (*SubscriptionDailyResetResult, error)
}

func (r *dailyResetScannerRepo) ListAutomaticCandidates(ctx context.Context, afterID int64, limit int) ([]SubscriptionDailyResetCandidate, error) {
	return r.list(ctx, afterID, limit)
}

func (r *dailyResetScannerRepo) GetState(ctx context.Context, userID, id int64) (*SubscriptionDailyResetState, error) {
	return r.state(ctx, userID, id)
}

func (r *dailyResetScannerRepo) Apply(ctx context.Context, command *SubscriptionDailyResetCommand) (*SubscriptionDailyResetResult, error) {
	return r.apply(ctx, command)
}

type dailyResetScannerSubscriptionRepo struct {
	UserSubscriptionRepository
	get func(context.Context, int64) (*UserSubscription, error)
}

func (r *dailyResetScannerSubscriptionRepo) GetByID(ctx context.Context, id int64) (*UserSubscription, error) {
	return r.get(ctx, id)
}

func dailyResetScannerState(id int64) *SubscriptionDailyResetState {
	now := time.Date(2026, 9, 6, 12, 0, 0, 0, time.UTC)
	sub := resetTestSubscription(now)
	sub.ID, sub.UserID, sub.GroupID = id, id+1000, id+2000
	sub.User.ID, sub.Group.ID = sub.UserID, sub.GroupID
	sub.AutoDailyResetEnabled = true
	sub.DailyUsageUSD = *sub.Group.DailyLimitUSD
	sub.DailyResetVersion = 9
	return &SubscriptionDailyResetState{Subscription: sub, ServerTime: now}
}

func awaitDailyResetScanner(t *testing.T, done <-chan struct{}) {
	t.Helper()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("daily reset scanner did not finish")
	}
}

func TestDailyResetScannerStartsImmediatelyAndStopCancelsLookup(t *testing.T) {
	entered := make(chan struct{})
	var lookupDeadline time.Time
	var lookupErr error
	repo := &dailyResetScannerRepo{list: func(ctx context.Context, _ int64, _ int) ([]SubscriptionDailyResetCandidate, error) {
		lookupDeadline, _ = ctx.Deadline()
		close(entered)
		<-ctx.Done()
		lookupErr = ctx.Err()
		return nil, lookupErr
	}}
	svc := &SubscriptionService{dailyResetRepo: repo}
	svc.startAutoDailyResetScanner()
	t.Cleanup(svc.Stop)
	awaitDailyResetScanner(t, entered)
	require.WithinDuration(t, time.Now().Add(25*time.Second), lookupDeadline, time.Second)

	stopped := make(chan struct{})
	go func() { svc.Stop(); close(stopped) }()
	awaitDailyResetScanner(t, stopped)
	require.ErrorIs(t, lookupErr, context.Canceled)
	awaitDailyResetScanner(t, svc.autoResetDone)
	svc.Stop() // Repeated shutdown must remain safe.
}

func TestDailyResetScannerDisabledRepositoryNeedsNoShutdown(t *testing.T) {
	svc := &SubscriptionService{}
	svc.startAutoDailyResetScanner()
	require.Nil(t, svc.autoResetCancel)
	require.Nil(t, svc.autoResetDone)
	svc.Stop()
}

func TestDailyResetScannerRetriesLookupOnThirtySecondTickAndStopsTicking(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var scans int
		repo := &dailyResetScannerRepo{list: func(context.Context, int64, int) ([]SubscriptionDailyResetCandidate, error) {
			scans++
			if scans == 1 {
				return nil, errors.New("temporary lookup failure")
			}
			return nil, nil
		}}
		svc := &SubscriptionService{dailyResetRepo: repo}
		svc.startAutoDailyResetScanner()
		t.Cleanup(svc.Stop)
		synctest.Wait()
		require.Equal(t, 1, scans, "startup must scan before the first tick")
		time.Sleep(29 * time.Second)
		synctest.Wait()
		require.Equal(t, 1, scans)
		time.Sleep(time.Second)
		synctest.Wait()
		require.Equal(t, 2, scans, "a lookup failure must recover on the next scheduled tick")
		svc.Stop()
		time.Sleep(30 * time.Second)
		synctest.Wait()
		require.Equal(t, 2, scans, "Stop must prevent further scheduled scans")
	})
}

func TestDailyResetScannerStopWaitsForAllFourWorkers(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var active, peak atomic.Int32
		repo := &dailyResetScannerRepo{list: func(context.Context, int64, int) ([]SubscriptionDailyResetCandidate, error) {
			return []SubscriptionDailyResetCandidate{{ID: 1}, {ID: 2}, {ID: 3}, {ID: 4}, {ID: 5}, {ID: 6}}, nil
		}}
		svc := &SubscriptionService{
			dailyResetRepo: repo,
			userSubRepo: &dailyResetScannerSubscriptionRepo{get: func(ctx context.Context, _ int64) (*UserSubscription, error) {
				current := active.Add(1)
				defer active.Add(-1)
				for previous := peak.Load(); current > previous && !peak.CompareAndSwap(previous, current); previous = peak.Load() {
				}
				<-ctx.Done()
				return nil, ctx.Err()
			}},
		}
		svc.startAutoDailyResetScanner()
		t.Cleanup(svc.Stop)
		synctest.Wait()
		require.Equal(t, int32(4), active.Load(), "only four candidates may execute concurrently")
		svc.Stop()
		require.Equal(t, int32(4), peak.Load())
		require.Zero(t, active.Load(), "Stop must join every worker before returning")
	})
}

func TestDailyResetScannerPaginatesHundredAndWrapsAfterShortPage(t *testing.T) {
	var cursors []int64
	var mu sync.Mutex
	visited := make(map[int64]int)
	repo := &dailyResetScannerRepo{
		list: func(_ context.Context, afterID int64, limit int) ([]SubscriptionDailyResetCandidate, error) {
			cursors = append(cursors, afterID)
			if limit != 100 {
				return nil, fmt.Errorf("unexpected scan limit: %d", limit)
			}
			candidates := make([]SubscriptionDailyResetCandidate, 0, limit)
			for id := afterID + 1; id <= 105 && len(candidates) < limit; id++ {
				candidates = append(candidates, SubscriptionDailyResetCandidate{ID: id})
			}
			return candidates, nil
		},
		state: func(_ context.Context, _, id int64) (*SubscriptionDailyResetState, error) {
			state := dailyResetScannerState(id)
			state.Subscription.AutoDailyResetEnabled = false
			return state, nil
		},
	}
	svc := &SubscriptionService{dailyResetRepo: repo, userSubRepo: &dailyResetScannerSubscriptionRepo{
		get: func(_ context.Context, id int64) (*UserSubscription, error) {
			mu.Lock()
			visited[id]++
			mu.Unlock()
			return dailyResetScannerState(id).Subscription, nil
		},
	}}
	var cursor int64
	svc.scanAutoDailyResets(context.Background(), &cursor)
	require.Equal(t, int64(100), cursor)
	svc.scanAutoDailyResets(context.Background(), &cursor)
	require.Zero(t, cursor)
	svc.scanAutoDailyResets(context.Background(), &cursor)
	require.Equal(t, []int64{0, 100, 0}, cursors)
	require.Len(t, visited, 105)
	require.Equal(t, 2, visited[1])
	require.Equal(t, 1, visited[105])
}

func TestDailyResetScannerLookupFailureRetainsCursorAndEmptyPageWraps(t *testing.T) {
	for _, fail := range []bool{true, false} {
		t.Run(fmt.Sprintf("lookup_failure=%t", fail), func(t *testing.T) {
			repo := &dailyResetScannerRepo{list: func(context.Context, int64, int) ([]SubscriptionDailyResetCandidate, error) {
				if fail {
					return nil, errors.New("temporary lookup failure")
				}
				return nil, nil
			}}
			svc := &SubscriptionService{dailyResetRepo: repo}
			cursor := int64(500)
			svc.scanAutoDailyResets(context.Background(), &cursor)
			if fail {
				require.Equal(t, int64(500), cursor)
			} else {
				require.Zero(t, cursor)
			}
		})
	}
}

func TestDailyResetScannerDeadlineRetainsUnfinishedPageForNextScan(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var interrupted atomic.Bool
		interrupted.Store(true)
		var completed, timedOut atomic.Int32
		repo := &dailyResetScannerRepo{
			list: func(_ context.Context, afterID int64, limit int) ([]SubscriptionDailyResetCandidate, error) {
				var candidates []SubscriptionDailyResetCandidate
				for id := afterID + 1; id <= 600 && len(candidates) < limit; id++ {
					candidates = append(candidates, SubscriptionDailyResetCandidate{ID: id})
				}
				return candidates, nil
			},
			state: func(_ context.Context, _, id int64) (*SubscriptionDailyResetState, error) {
				completed.Add(1)
				state := dailyResetScannerState(id)
				state.Subscription.AutoDailyResetEnabled = false
				return state, nil
			},
		}
		svc := &SubscriptionService{dailyResetRepo: repo, userSubRepo: &dailyResetScannerSubscriptionRepo{
			get: func(ctx context.Context, id int64) (*UserSubscription, error) {
				if interrupted.Load() {
					<-ctx.Done()
					if errors.Is(ctx.Err(), context.DeadlineExceeded) {
						timedOut.Add(1)
					}
					return nil, ctx.Err()
				}
				return dailyResetScannerState(id).Subscription, nil
			},
		}}
		cursor := int64(500)
		started := time.Now()
		svc.scanAutoDailyResets(context.Background(), &cursor)
		require.Equal(t, 25*time.Second, time.Since(started), "enforce the scanner's own page timeout")
		require.Positive(t, timedOut.Load())
		require.Zero(t, completed.Load())
		require.Equal(t, int64(500), cursor, "an unfinished page must not be marked processed")

		interrupted.Store(false)
		svc.scanAutoDailyResets(context.Background(), &cursor)
		require.Equal(t, int32(100), completed.Load(), "the next scan must recover every skipped subscription")
		require.Equal(t, int64(600), cursor)
	})
}

func TestDailyResetScannerRechecksEachSubscriptionsCurrentPreference(t *testing.T) {
	var mu sync.Mutex
	var applied []SubscriptionDailyResetCommand
	repo := &dailyResetScannerRepo{
		list: func(context.Context, int64, int) ([]SubscriptionDailyResetCandidate, error) {
			return []SubscriptionDailyResetCandidate{{ID: 1, DailyResetVersion: 2}, {ID: 2, DailyResetVersion: 2}}, nil
		},
		state: func(_ context.Context, userID, id int64) (*SubscriptionDailyResetState, error) {
			state := dailyResetScannerState(id)
			if userID != state.Subscription.UserID {
				return nil, errors.New("wrong subscription owner")
			}
			state.Subscription.AutoDailyResetEnabled = id == 2
			return state, nil
		},
		apply: func(_ context.Context, command *SubscriptionDailyResetCommand) (*SubscriptionDailyResetResult, error) {
			mu.Lock()
			applied = append(applied, *command)
			mu.Unlock()
			return nil, ErrResetStateChanged
		},
	}
	svc := &SubscriptionService{dailyResetRepo: repo, userSubRepo: &dailyResetScannerSubscriptionRepo{
		get: func(_ context.Context, id int64) (*UserSubscription, error) {
			return dailyResetScannerState(id).Subscription, nil
		},
	}}
	var cursor int64
	svc.scanAutoDailyResets(context.Background(), &cursor)
	require.Len(t, applied, 1)
	require.Equal(t, int64(2), applied[0].SubscriptionID)
	require.Equal(t, int64(1002), applied[0].UserID)
	require.Equal(t, int64(9), applied[0].ExpectedVersion, "use the refreshed state, not the scanned version")
}

func TestDailyResetScannerFailedResetDoesNotBlockOthersAndRecovers(t *testing.T) {
	var mu sync.Mutex
	commands := make(map[int64][]SubscriptionDailyResetCommand)
	var scan int
	repo := &dailyResetScannerRepo{
		list: func(context.Context, int64, int) ([]SubscriptionDailyResetCandidate, error) {
			scan++
			if scan == 1 {
				return []SubscriptionDailyResetCandidate{{ID: 1}, {ID: 2}}, nil
			}
			return []SubscriptionDailyResetCandidate{{ID: 1}}, nil
		},
		state: func(_ context.Context, _, id int64) (*SubscriptionDailyResetState, error) {
			return dailyResetScannerState(id), nil
		},
		apply: func(_ context.Context, command *SubscriptionDailyResetCommand) (*SubscriptionDailyResetResult, error) {
			mu.Lock()
			defer mu.Unlock()
			commands[command.SubscriptionID] = append(commands[command.SubscriptionID], *command)
			if command.SubscriptionID == 1 && len(commands[1]) == 1 {
				return nil, errors.New("temporary reset failure")
			}
			return &SubscriptionDailyResetResult{
				State: dailyResetScannerState(command.SubscriptionID), Event: &SubscriptionDailyResetEvent{OperationID: command.OperationID},
			}, nil
		},
	}
	svc := &SubscriptionService{dailyResetRepo: repo, userSubRepo: &dailyResetScannerSubscriptionRepo{
		get: func(_ context.Context, id int64) (*UserSubscription, error) {
			return dailyResetScannerState(id).Subscription, nil
		},
	}}
	var cursor int64
	svc.scanAutoDailyResets(context.Background(), &cursor)
	require.Len(t, commands[1], 1)
	require.Len(t, commands[2], 1, "one failed reset must not abort the other subscriptions")
	svc.scanAutoDailyResets(context.Background(), &cursor)
	require.Len(t, commands[1], 2)
	require.Equal(t, commands[1][0], commands[1][1], "retry must retain the same logical operation")
}

func TestDailyResetConcurrentTriggersShareOneOperationIdentity(t *testing.T) {
	var mu sync.Mutex
	var commands []SubscriptionDailyResetCommand
	repo := &dailyResetScannerRepo{
		state: func(_ context.Context, _, id int64) (*SubscriptionDailyResetState, error) {
			return dailyResetScannerState(id), nil
		},
		apply: func(_ context.Context, command *SubscriptionDailyResetCommand) (*SubscriptionDailyResetResult, error) {
			mu.Lock()
			commands = append(commands, *command)
			mu.Unlock()
			return nil, ErrResetStateChanged
		},
	}
	svc := &SubscriptionService{dailyResetRepo: repo, userSubRepo: &dailyResetScannerSubscriptionRepo{
		get: func(_ context.Context, id int64) (*UserSubscription, error) {
			return dailyResetScannerState(id).Subscription, nil
		},
	}}
	var wg sync.WaitGroup
	errs := make(chan error, 8)
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); errs <- svc.TriggerAutoDailyReset(context.Background(), 1) }()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		require.NoError(t, err, "a competing reset's stale-state rejection is not a technical failure")
	}
	require.Len(t, commands, 8)
	for _, command := range commands {
		require.Equal(t, commands[0], command)
		require.Equal(t, "automatic", command.Source)
		require.Equal(t, "auto:9:"+command.ObservedDate, command.OperationID)
		require.Equal(t, resetFingerprint("automatic", 9, command.ObservedDate), command.RequestFingerprint)
	}
}
