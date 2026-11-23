package repository

import (
	"context"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
)

func TestCompareAndDeleteSessionAccountID(t *testing.T) {
	mr := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = client.Close() })
	cache := NewGatewayCache(client)
	ctx := context.Background()
	const owner int64 = 9007199254740993
	require.NoError(t, cache.SetSessionAccountID(ctx, 1, "sess", owner, time.Minute))
	for _, group := range []int64{1, 2} {
		removed, err := cache.CompareAndDeleteSessionAccountID(ctx, group, "sess", owner-1)
		require.NoError(t, err)
		require.False(t, removed)
	}
	bound, err := cache.GetSessionAccountID(ctx, 1, "sess")
	require.NoError(t, err)
	require.Equal(t, owner, bound)
	removed, err := cache.CompareAndDeleteSessionAccountID(ctx, 1, "sess", owner)
	require.NoError(t, err)
	require.True(t, removed)
	removed, err = cache.CompareAndDeleteSessionAccountID(ctx, 1, "sess", owner)
	require.NoError(t, err)
	require.False(t, removed)
}
