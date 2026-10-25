package handler

import (
	"context"
	"errors"
	"github.com/Wei-Shaw/sub2api/internal/qqimage"
	"github.com/stretchr/testify/require"
	"testing"
)

type qqOutboxStub struct{ acked []int64 }

func (s *qqOutboxStub) PendingBalanceInvalidations(context.Context, int) ([]qqimage.CacheInvalidation, error) {
	return []qqimage.CacheInvalidation{{ID: 1, UserID: 42}}, nil
}
func (s *qqOutboxStub) AckBalanceInvalidation(_ context.Context, id int64) error {
	s.acked = append(s.acked, id)
	return nil
}

type qqInvalidatorStub struct {
	err   error
	calls int
	id    int64
}

func (s *qqInvalidatorStub) InvalidateUserBalance(_ context.Context, id int64) error {
	s.calls++
	s.id = id
	return s.err
}
func (s *qqInvalidatorStub) InvalidateAuthCacheByUserIDReliable(_ context.Context, id int64) error {
	s.calls++
	s.id = id
	return s.err
}
func TestQQImageCacheOutboxRetainsFailureUntilBothCachesInvalidate(t *testing.T) {
	out := &qqOutboxStub{}
	bal := &qqInvalidatorStub{err: errors.New("redis unavailable")}
	auth := &qqInvalidatorStub{}
	r := &QQImageRuntime{outbox: out, balance: bal, auth: auth}
	require.Error(t, r.ProcessInvalidations(context.Background()))
	require.Empty(t, out.acked)
	require.Zero(t, auth.calls)
	bal.err = nil
	auth.err = errors.New("auth cache unavailable")
	require.Error(t, r.ProcessInvalidations(context.Background()))
	require.Empty(t, out.acked)
	auth.err = nil
	require.NoError(t, r.ProcessInvalidations(context.Background()))
	require.Equal(t, []int64{1}, out.acked)
	require.Equal(t, int64(42), auth.id)
}
