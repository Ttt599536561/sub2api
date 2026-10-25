package handler

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/qqimage"
)

type qqImageOutbox interface {
	PendingBalanceInvalidations(context.Context, int) ([]qqimage.CacheInvalidation, error)
	AckBalanceInvalidation(context.Context, int64) error
}
type qqImageBalanceInvalidator interface {
	InvalidateUserBalance(context.Context, int64) error
}
type qqImageAuthInvalidator interface {
	InvalidateAuthCacheByUserIDReliable(context.Context, int64) error
}

// QQImageRuntime owns the worker and the durable wallet-cache outbox. Database
// money changes are authoritative; a cache outage cannot acknowledge an event.
type qqImageGateway interface {
	Run(context.Context)
	Status() qqimage.GatewayStatus
}

type QQImageRuntime struct {
	gateway     qqImageGateway
	gatewayDone chan struct{}
	core        *qqimage.Store
	commands    *qqimage.QQCommandHandler
	worker      *qqimage.Worker
	outbox      qqImageOutbox
	balance     qqImageBalanceInvalidator
	auth        qqImageAuthInvalidator
	start       sync.Once
	cancel      context.CancelFunc
	done        chan struct{}
}

func (r *QQImageRuntime) Start() {
	r.start.Do(func() {
		ctx, cancel := context.WithCancel(context.Background())
		r.cancel = cancel
		r.done = make(chan struct{})
		if r.gateway != nil {
			r.gatewayDone = make(chan struct{})
			go func() { defer close(r.gatewayDone); r.gateway.Run(ctx) }()
		}
		if r.worker != nil {
			r.worker.Start()
		}
		go func() {
			defer close(r.done)
			ticker := time.NewTicker(time.Second)
			defer ticker.Stop()
			for {
				if ctx.Err() != nil {
					return
				}
				op, stop := context.WithTimeout(ctx, 5*time.Second)
				err := r.ProcessInvalidations(op)
				stop()
				if err != nil && ctx.Err() == nil {
					slog.Warn("QQ image wallet cache invalidation pending; durable event retained")
				}
				select {
				case <-ctx.Done():
					return
				case <-ticker.C:
				}
			}
		}()
	})
}
func (r *QQImageRuntime) Stop(ctx context.Context) error {
	if r.cancel != nil {
		r.cancel()
	}
	var err error
	if r.worker != nil {
		err = r.worker.Stop(ctx)
	}
	if r.done != nil {
		select {
		case <-r.done:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	if r.gatewayDone != nil {
		select {
		case <-r.gatewayDone:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return err
}
func (r *QQImageRuntime) ProcessInvalidations(ctx context.Context) error {
	if r.outbox == nil || r.balance == nil || r.auth == nil {
		return errors.New("qq_image_cache_dependencies_unavailable")
	}
	events, err := r.outbox.PendingBalanceInvalidations(ctx, 100)
	if err != nil {
		return err
	}
	for _, event := range events {
		if err = r.balance.InvalidateUserBalance(ctx, event.UserID); err != nil {
			return err
		}
		if err = r.auth.InvalidateAuthCacheByUserIDReliable(ctx, event.UserID); err != nil {
			return err
		}
		if err = r.outbox.AckBalanceInvalidation(ctx, event.ID); err != nil {
			return err
		}
	}
	return nil
}
