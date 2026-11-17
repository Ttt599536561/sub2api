package handler

import (
	"context"
	"sync"

	"github.com/Wei-Shaw/sub2api/internal/service"
)

type openAIWSTurnReservationGateway interface {
	inflightReservationEstimator
	WithOpenAIWSModelRouting(context.Context, []byte) context.Context
}

type openAIWSTurnReservation struct {
	ctx  context.Context
	done func()
}

// openAIWSTurnReservations owns only handler references. take transfers that
// reference to AfterTurn, which acquires any async usage reference before done.
// Each upstream attempt drains unfinished turns before retrying or closing.
type openAIWSTurnReservations struct {
	mu    sync.Mutex
	turns map[int]openAIWSTurnReservation
}

func (r *openAIWSTurnReservations) reserve(ctx context.Context, turn int, billing *service.BillingCacheService, gateway openAIWSTurnReservationGateway, apiKey *service.APIKey, subscription *service.UserSubscription, model string, payload []byte) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if previous, ok := r.turns[turn]; ok {
		delete(r.turns, turn)
		previous.done()
	}
	// Credit can be redeemed while the connection is idle. Refresh the selected
	// ID and term, leaving the original async settlement snapshot untouched.
	fresh, err := billing.RefreshSubscriptionForInflight(ctx, subscription)
	if err != nil {
		return err
	}
	if fresh != nil && fresh.Group != nil && apiKey != nil {
		keyCopy := *apiKey
		keyCopy.Group = fresh.Group
		apiKey = &keyCopy
	}
	// Recompute transport/account mapping from this payload, never the first frame.
	ctx = gateway.WithOpenAIWSModelRouting(ctx, payload)
	reservationCtx, done, err := reserveInflightBalanceCtx(ctx, billing, gateway, apiKey, fresh, tokenInflightEstimate(model, payload))
	if err != nil {
		return err
	}
	if r.turns == nil {
		r.turns = make(map[int]openAIWSTurnReservation)
	}
	r.turns[turn] = openAIWSTurnReservation{ctx: reservationCtx, done: done}
	return nil
}

func (r *openAIWSTurnReservations) take(turn int, fallback context.Context) (context.Context, func()) {
	r.mu.Lock()
	defer r.mu.Unlock()
	entry, ok := r.turns[turn]
	if !ok {
		return fallback, inflightNoop
	}
	delete(r.turns, turn)
	return entry.ctx, entry.done
}

func (r *openAIWSTurnReservations) releaseAll() {
	r.mu.Lock()
	entries := r.turns
	r.turns = nil
	r.mu.Unlock()
	for _, entry := range entries {
		entry.done()
	}
}
