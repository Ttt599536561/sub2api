//go:build integration

package repository

import (
	"context"
	"fmt"
	"sort"
	"sync"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

// This is a local comparative smoke measurement, not a production capacity
// assertion. Report both single-user contention and multiple-user throughput.
func TestWelfareBillingPerformanceSmoke(t *testing.T) {
	ctx := context.Background()
	client := testEntClient(t)
	type actor struct{ user, key int64 }
	actors := make([]actor, 8)
	for i := range actors {
		u := mustCreateUser(t, client, &service.User{Email: uuid.NewString() + "@performance.invalid", PasswordHash: "hash", Balance: 100})
		key := mustCreateApiKey(t, client, &service.APIKey{UserID: u.ID, Key: "sk-perf-" + uuid.NewString(), Name: "welfare performance"})
		actors[i] = actor{user: u.ID, key: key.ID}
	}
	program := NewWelfareRepository(integrationDB)
	repo := NewUsageBillingRepository(client, integrationDB)
	for _, shared := range []bool{true, false} {
		for _, enabled := range []bool{false, true} {
			_, err := program.UpdateSettings(ctx, enabled)
			require.NoError(t, err)
			latencies := make(chan time.Duration, 160)
			errors := make(chan error, 160)
			var wg sync.WaitGroup
			start := time.Now()
			for worker := 0; worker < 8; worker++ {
				a := actors[worker]
				if shared {
					a = actors[0]
				}
				wg.Add(1)
				go func() {
					defer wg.Done()
					for j := 0; j < 20; j++ {
						began := time.Now()
						result, err := repo.Apply(ctx, &service.UsageBillingCommand{RequestID: uuid.NewString(), APIKeyID: a.key, UserID: a.user, BalanceCost: 0.0001})
						if err == nil && !result.Applied {
							err = fmt.Errorf("new billing operation was not applied")
						}
						errors <- err
						latencies <- time.Since(began)
					}
				}()
			}
			wg.Wait()
			elapsed := time.Since(start)
			close(errors)
			close(latencies)
			for err := range errors {
				require.NoError(t, err)
			}
			var samples []time.Duration
			for d := range latencies {
				samples = append(samples, d)
			}
			sort.Slice(samples, func(i, j int) bool { return samples[i] < samples[j] })
			t.Logf("single_user=%t welfare_enabled=%t operations=%d elapsed=%s throughput=%.1f/s p50=%s p95=%s p99=%s", shared, enabled, len(samples), elapsed, float64(len(samples))/elapsed.Seconds(), samples[len(samples)/2], samples[len(samples)*95/100], samples[len(samples)*99/100])
		}
	}
}
