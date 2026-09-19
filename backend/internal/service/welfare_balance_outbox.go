package service

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"
)

type WelfareBalanceOutboxEvent struct {
	ID         int64
	UserID     int64
	Version    int64
	Attempts   int
	LeaseToken string
}

type WelfareBalanceOutboxRepository interface {
	Claim(context.Context, int, time.Duration) ([]WelfareBalanceOutboxEvent, error)
	Complete(context.Context, WelfareBalanceOutboxEvent) error
	Retry(context.Context, WelfareBalanceOutboxEvent, string, time.Duration) error
}
type WelfareBalanceInvalidator interface {
	InvalidateUserBalance(context.Context, int64) error
}
type WelfareAuthInvalidator interface {
	InvalidateAuthCacheByUserIDReliable(context.Context, int64) error
}

// WelfareBalanceOutboxWorker has a single bounded consumer per instance. Leases
// survive crashes; token-conditional acknowledgement prevents expired workers
// from completing another instance's retry.
type WelfareBalanceOutboxWorker struct {
	repo    WelfareBalanceOutboxRepository
	balance WelfareBalanceInvalidator
	auth    WelfareAuthInvalidator
	wake    chan struct{}
	ctx     context.Context
	cancel  context.CancelFunc
	start   sync.Once
	stop    sync.Once
	wg      sync.WaitGroup
}

func NewWelfareBalanceOutboxWorker(repo WelfareBalanceOutboxRepository, balance WelfareBalanceInvalidator, auth WelfareAuthInvalidator) *WelfareBalanceOutboxWorker {
	ctx, cancel := context.WithCancel(context.Background())
	return &WelfareBalanceOutboxWorker{repo: repo, balance: balance, auth: auth, wake: make(chan struct{}, 1), ctx: ctx, cancel: cancel}
}
func (w *WelfareBalanceOutboxWorker) Start() {
	w.start.Do(func() {
		w.wg.Add(1)
		go func() {
			defer w.wg.Done()
			ticker := time.NewTicker(2 * time.Second)
			defer ticker.Stop()
			for {
				if w.ctx.Err() != nil {
					return
				}
				if err := w.ProcessPending(w.ctx); err != nil && w.ctx.Err() == nil {
					slog.Warn("welfare balance cache outbox retry", "error", err)
				}
				select {
				case <-w.ctx.Done():
					return
				case <-ticker.C:
				case <-w.wake:
				}
			}
		}()
	})
}
func (w *WelfareBalanceOutboxWorker) Stop() { w.stop.Do(func() { w.cancel(); w.wg.Wait() }) }

// Wake is nonblocking and coalesces bursts of redemptions. The durable table is
// the queue, so dropping a duplicate wake notification cannot lose an event.
func (w *WelfareBalanceOutboxWorker) Wake() {
	select {
	case w.wake <- struct{}{}:
	default:
	}
}

func (w *WelfareBalanceOutboxWorker) ProcessPending(ctx context.Context) error {
	if w.repo == nil || w.balance == nil || w.auth == nil {
		return errors.New("welfare outbox dependencies unavailable")
	}
	claimCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	events, err := w.repo.Claim(claimCtx, 16, time.Minute)
	cancel()
	if err != nil {
		return err
	}
	for _, event := range events {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		eventCtx, eventCancel := context.WithTimeout(ctx, 2*time.Second)
		balanceErr := w.balance.InvalidateUserBalance(eventCtx, event.UserID)
		authErr := w.auth.InvalidateAuthCacheByUserIDReliable(eventCtx, event.UserID)
		eventCancel()
		writeCtx, writeCancel := context.WithTimeout(ctx, 2*time.Second)
		invalidationErr := errors.Join(balanceErr, authErr)
		if invalidationErr == nil {
			err = w.repo.Complete(writeCtx, event)
		} else {
			delay := time.Duration(1<<min(event.Attempts, 8)) * time.Second
			err = w.repo.Retry(writeCtx, event, invalidationErr.Error(), delay)
		}
		writeCancel()
		if err != nil {
			return fmt.Errorf("welfare balance outbox event %d: %w", event.ID, err)
		}
	}
	return nil
}
