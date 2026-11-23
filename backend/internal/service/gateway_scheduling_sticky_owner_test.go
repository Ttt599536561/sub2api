package service

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestSchedulerStickyCleanupPreservesReboundPrimaryAndClearsOldLegacy(t *testing.T) {
	ctx := withOpenAILegacySessionHash(context.Background(), "old")
	cache := &stubGatewayCache{sessionBindings: map[string]int64{"openai:current": 202, "openai:old": 101}}
	scheduler := defaultOpenAIAccountScheduler{service: &OpenAIGatewayService{
		cache: cache, accountRepo: schedulerTestOpenAIAccountRepo{},
	}}
	_, _, err := scheduler.selectBySessionHash(ctx, OpenAIAccountScheduleRequest{SessionHash: "current", StickyAccountID: 101})
	require.NoError(t, err)
	require.Equal(t, map[string]int64{"openai:current": 202}, cache.sessionBindings)
}

func TestResponseCleanupPreservesReboundLocalAndRemoteOwners(t *testing.T) {
	ctx := context.Background()
	cache := &stubGatewayCache{}
	first, second := NewOpenAIWSStateStore(cache), NewOpenAIWSStateStore(cache)
	require.NoError(t, first.BindResponseAccount(ctx, 1, "resp_x", 101, time.Minute))
	require.NoError(t, second.BindResponseAccount(ctx, 1, "resp_x", 202, time.Minute))
	for _, store := range []OpenAIWSStateStore{first, second} {
		require.NoError(t, store.DeleteResponseAccountIfBound(ctx, 1, "resp_x", 101))
		id, err := store.GetResponseAccount(ctx, 1, "resp_x")
		require.NoError(t, err)
		require.Equal(t, int64(202), id)
	}
	require.NoError(t, second.DeleteResponseAccountIfBound(ctx, 1, "resp_x", 202))
	id, err := second.GetResponseAccount(ctx, 1, "resp_x")
	require.NoError(t, err)
	require.Zero(t, id)
}
