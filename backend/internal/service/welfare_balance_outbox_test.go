package service

import (
	"context"
	"errors"
	"github.com/stretchr/testify/require"
	"testing"
	"time"
)

type welfareOutboxTestRepo struct {
	events             []WelfareBalanceOutboxEvent
	completed, retried int
}

func (r *welfareOutboxTestRepo) Claim(context.Context, int, time.Duration) ([]WelfareBalanceOutboxEvent, error) {
	return r.events, nil
}
func (r *welfareOutboxTestRepo) Complete(context.Context, WelfareBalanceOutboxEvent) error {
	r.completed++
	return nil
}
func (r *welfareOutboxTestRepo) Retry(context.Context, WelfareBalanceOutboxEvent, string, time.Duration) error {
	r.retried++
	return nil
}

type welfareBalanceTestInvalidator struct {
	err   error
	calls int
}

func (c *welfareBalanceTestInvalidator) InvalidateUserBalance(context.Context, int64) error {
	c.calls++
	return c.err
}

type welfareAuthTestInvalidator struct {
	err   error
	calls int
}

func (c *welfareAuthTestInvalidator) InvalidateAuthCacheByUserIDReliable(context.Context, int64) error {
	c.calls++
	return c.err
}

func TestWelfareBalanceOutboxRequiresBothInvalidations(t *testing.T) {
	for _, failing := range []string{"balance", "auth", "none"} {
		t.Run(failing, func(t *testing.T) {
			repo := &welfareOutboxTestRepo{events: []WelfareBalanceOutboxEvent{{ID: 1, UserID: 7, LeaseToken: "owner"}}}
			balance, auth := &welfareBalanceTestInvalidator{}, &welfareAuthTestInvalidator{}
			if failing == "balance" {
				balance.err = errors.New("redis unavailable")
			}
			if failing == "auth" {
				auth.err = errors.New("publish unavailable")
			}
			worker := NewWelfareBalanceOutboxWorker(repo, balance, auth)
			require.NoError(t, worker.ProcessPending(context.Background()))
			require.Equal(t, 1, balance.calls)
			require.Equal(t, 1, auth.calls)
			if failing == "none" {
				require.Equal(t, 1, repo.completed)
				require.Zero(t, repo.retried)
			} else {
				require.Zero(t, repo.completed)
				require.Equal(t, 1, repo.retried)
			}
		})
	}
}

func TestWelfareBalanceOutboxWakeIsBoundedAndStopSafe(t *testing.T) {
	w := NewWelfareBalanceOutboxWorker(&welfareOutboxTestRepo{}, &welfareBalanceTestInvalidator{}, &welfareAuthTestInvalidator{})
	for i := 0; i < 10000; i++ {
		w.Wake()
	}
	require.Equal(t, 1, len(w.wake))
	w.Start()
	w.Stop()
	w.Stop()
	w.Wake()
}
