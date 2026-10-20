//go:build unit

package repository

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
)

func newAffiliateAttributionStoreTest(t *testing.T) (service.AffiliateAttributionStore, *miniredis.Miniredis) {
	t.Helper()
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr(), MaxRetries: -1})
	t.Cleanup(func() { require.NoError(t, client.Close()) })
	return NewAffiliateAttributionStore(client), server
}

func TestAffiliateAttributionStoreSetAndReplace(t *testing.T) {
	store, server := newAffiliateAttributionStoreTest(t)
	ctx := context.Background()
	require.NoError(t, store.Set(ctx, "snapshot", []byte("first"), time.Minute))
	value, err := store.Get(ctx, "snapshot")
	require.NoError(t, err)
	require.Equal(t, "first", value)
	require.Equal(t, time.Minute, server.TTL("snapshot"))

	server.FastForward(10 * time.Second)
	require.NoError(t, store.Set(ctx, "snapshot", []byte("second\x00value"), 2*time.Minute))
	value, err = store.Get(ctx, "snapshot")
	require.NoError(t, err)
	require.Equal(t, "second\x00value", value)
	require.Equal(t, 2*time.Minute, server.TTL("snapshot"))
}

func TestAffiliateAttributionStoreSetIfAbsentKeepsValueAndTTL(t *testing.T) {
	store, server := newAffiliateAttributionStoreTest(t)
	ctx := context.Background()
	created, err := store.SetIfAbsent(ctx, "browser", []byte("first-source"), time.Minute)
	require.NoError(t, err)
	require.True(t, created)
	server.FastForward(10 * time.Second)

	created, err = store.SetIfAbsent(ctx, "browser", []byte("later-source"), 5*time.Minute)
	require.NoError(t, err)
	require.False(t, created)
	value, err := store.Get(ctx, "browser")
	require.NoError(t, err)
	require.Equal(t, "first-source", value)
	require.Equal(t, 50*time.Second, server.TTL("browser"))
}

func TestAffiliateAttributionStoreMissingAndExpired(t *testing.T) {
	for _, operation := range []string{"get", "take"} {
		t.Run(operation, func(t *testing.T) {
			store, server := newAffiliateAttributionStoreTest(t)
			ctx := context.Background()
			read := store.Get
			if operation == "take" {
				read = store.Take
			}
			value, err := read(ctx, "ticket")
			require.Empty(t, value)
			require.ErrorIs(t, err, service.ErrAffiliateAttributionMissing)
			require.NotErrorIs(t, err, redis.Nil)

			require.NoError(t, store.Set(ctx, "ticket", []byte("expired"), time.Minute))
			server.FastForward(2 * time.Minute)
			value, err = read(ctx, "ticket")
			require.Empty(t, value)
			require.ErrorIs(t, err, service.ErrAffiliateAttributionMissing)
		})
	}
}

func TestAffiliateAttributionStoreTakeHasOneConcurrentWinner(t *testing.T) {
	store, _ := newAffiliateAttributionStoreTest(t)
	ctx := context.Background()
	require.NoError(t, store.Set(ctx, "ticket", []byte("one-use"), time.Minute))
	// Reading a ticket must not consume it before Take atomically claims it.
	value, err := store.Get(ctx, "ticket")
	require.NoError(t, err)
	require.Equal(t, "one-use", value)

	const attempts = 24
	type result struct {
		value string
		err   error
	}
	results := make(chan result, attempts)
	start := make(chan struct{})
	var workers sync.WaitGroup
	for i := 0; i < attempts; i++ {
		workers.Add(1)
		go func() {
			defer workers.Done()
			<-start
			value, err := store.Take(ctx, "ticket")
			results <- result{value: value, err: err}
		}()
	}
	close(start)
	workers.Wait()
	close(results)
	winners := 0
	for result := range results {
		if result.err == nil {
			winners++
			require.Equal(t, "one-use", result.value)
		} else {
			require.ErrorIs(t, result.err, service.ErrAffiliateAttributionMissing)
		}
	}
	require.Equal(t, 1, winners)
	_, err = store.Get(ctx, "ticket")
	require.ErrorIs(t, err, service.ErrAffiliateAttributionMissing)
}

func TestAffiliateAttributionStoreCountDoesNotRefreshTTL(t *testing.T) {
	store, server := newAffiliateAttributionStoreTest(t)
	ctx := context.Background()
	count, err := store.Count(ctx, "limit", 120*time.Second)
	require.NoError(t, err)
	require.EqualValues(t, 1, count)
	require.Equal(t, 120*time.Second, server.TTL("limit"))
	server.FastForward(30 * time.Second)

	count, err = store.Count(ctx, "limit", 5*time.Minute)
	require.NoError(t, err)
	require.EqualValues(t, 2, count)
	require.Equal(t, 90*time.Second, server.TTL("limit"))
	server.FastForward(91 * time.Second)

	count, err = store.Count(ctx, "limit", 120*time.Second)
	require.NoError(t, err)
	require.EqualValues(t, 1, count)
	require.Equal(t, 120*time.Second, server.TTL("limit"))
}

func TestAffiliateAttributionStoreCountIsAtomic(t *testing.T) {
	store, server := newAffiliateAttributionStoreTest(t)
	const attempts = 24
	type result struct {
		count int64
		err   error
	}
	results := make(chan result, attempts)
	start := make(chan struct{})
	var workers sync.WaitGroup
	for i := 0; i < attempts; i++ {
		workers.Add(1)
		go func() {
			defer workers.Done()
			<-start
			count, err := store.Count(context.Background(), "limit", 120*time.Second)
			results <- result{count: count, err: err}
		}()
	}
	close(start)
	workers.Wait()
	close(results)
	seen := make(map[int64]bool)
	for result := range results {
		require.NoError(t, result.err)
		require.False(t, seen[result.count])
		seen[result.count] = true
	}
	for i := int64(1); i <= attempts; i++ {
		require.True(t, seen[i])
	}
	require.Equal(t, 120*time.Second, server.TTL("limit"))
}

func TestAffiliateAttributionStoreCountRequiresPositiveTTL(t *testing.T) {
	store, server := newAffiliateAttributionStoreTest(t)
	for _, ttl := range []time.Duration{0, -time.Second} {
		count, err := store.Count(context.Background(), "limit", ttl)
		require.Zero(t, count)
		require.Error(t, err)
		require.Empty(t, server.Keys())
	}
	count, err := store.Count(context.Background(), "limit", time.Millisecond)
	require.NoError(t, err)
	require.EqualValues(t, 1, count)
	require.Equal(t, time.Second, server.TTL("limit"))
}

func TestAffiliateAttributionStoreErrorsRemainFailures(t *testing.T) {
	for _, failure := range []string{"redis", "nil_client"} {
		t.Run(failure, func(t *testing.T) {
			var store service.AffiliateAttributionStore
			if failure == "nil_client" {
				store = NewAffiliateAttributionStore(nil)
			} else {
				var server *miniredis.Miniredis
				store, server = newAffiliateAttributionStoreTest(t)
				server.SetError("injected storage failure")
			}
			ctx := context.Background()
			operations := []struct {
				name string
				call func(*testing.T) error
			}{
				{"set", func(*testing.T) error { return store.Set(ctx, "key", []byte("value"), time.Minute) }},
				{"set_if_absent", func(t *testing.T) error {
					created, err := store.SetIfAbsent(ctx, "key", []byte("value"), time.Minute)
					require.False(t, created)
					return err
				}},
				{"get", func(t *testing.T) error {
					value, err := store.Get(ctx, "key")
					require.Empty(t, value)
					return err
				}},
				{"take", func(t *testing.T) error {
					value, err := store.Take(ctx, "key")
					require.Empty(t, value)
					return err
				}},
				{"count", func(t *testing.T) error {
					count, err := store.Count(ctx, "key", time.Minute)
					require.Zero(t, count)
					return err
				}},
			}
			for _, operation := range operations {
				t.Run(operation.name, func(t *testing.T) {
					var err error
					require.NotPanics(t, func() { err = operation.call(t) })
					require.Error(t, err)
					require.NotErrorIs(t, err, service.ErrAffiliateAttributionMissing)
				})
			}
		})
	}
}
