//go:build unit

package repository

import (
	"context"
	"github.com/stretchr/testify/require"
	"testing"
	"time"
)

func TestBalanceGenerationFencesInflightRefill(t *testing.T) {
	c, mr := newMiniRedisCache(t)
	ctx := context.Background()
	generation, err := c.UserBalanceGeneration(ctx, 7)
	require.NoError(t, err)
	require.NoError(t, c.InvalidateUserBalance(ctx, 7))
	stored, err := c.SetUserBalanceIfGeneration(ctx, 7, 1, generation)
	require.NoError(t, err)
	require.False(t, stored, "pre-credit database read must not repopulate the cache")
	_, err = c.GetUserBalance(ctx, 7)
	require.Error(t, err)
	current, err := c.UserBalanceGeneration(ctx, 7)
	require.NoError(t, err)
	stored, err = c.SetUserBalanceIfGeneration(ctx, 7, 20, current)
	require.NoError(t, err)
	require.True(t, stored)
	balance, err := c.GetUserBalance(ctx, 7)
	require.NoError(t, err)
	require.Equal(t, 20.0, balance)
	// Generation must not expire and return to zero while a refiller is queued.
	mr.FastForward(24 * time.Hour)
	stored, err = c.SetUserBalanceIfGeneration(ctx, 7, 1, generation)
	require.NoError(t, err)
	require.False(t, stored)
}
