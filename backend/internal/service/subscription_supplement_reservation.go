package service

import (
	"context"
	"time"
)

type SubscriptionSupplementReservation struct {
	ID                              string
	SubscriptionID, UserID, GroupID int64
	TermStartsAt                    time.Time
	AmountUSD                       float64
	TTL                             time.Duration
}

type SubscriptionSupplementReservationRepository interface {
	ReserveSubscriptionSupplement(context.Context, *SubscriptionSupplementReservation) error
	RenewSubscriptionSupplementReservation(context.Context, string, time.Duration) (bool, error)
	ReleaseSubscriptionSupplementReservation(context.Context, string) error
	TransferSubscriptionSupplementReservation(context.Context, string, string, time.Duration) error
}

type subscriptionSupplementHoldContextKey struct{}

func WithSubscriptionSupplementHoldID(ctx context.Context, id string) context.Context {
	if id == "" {
		return ctx
	}
	return context.WithValue(ctx, subscriptionSupplementHoldContextKey{}, id)
}
func SubscriptionSupplementHoldIDFromContext(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	id, _ := ctx.Value(subscriptionSupplementHoldContextKey{}).(string)
	return id
}

func (r *InflightReservation) SupplementHoldID() string {
	if r == nil || r.supplementHoldRepo == nil {
		return ""
	}
	return r.requestID
}

// Once upstream accepted a task, even failed persistence must not release its
// existing hold. The original TTL provides conservative orphan recovery.
func (r *InflightReservation) TransferSupplementHold(ctx context.Context, taskKey string, ttl time.Duration) error {
	if r == nil || r.supplementHoldRepo == nil {
		return nil
	}
	r.supplementTransferred.Store(true)
	return r.supplementHoldRepo.TransferSubscriptionSupplementReservation(ctx, r.requestID, taskKey, ttl)
}

func (s *BillingCacheService) ReleaseSubscriptionSupplementHold(ctx context.Context, id string) error {
	if id == "" {
		return nil
	}
	if s == nil {
		return ErrBillingServiceUnavailable
	}
	repo, ok := s.subRepo.(SubscriptionSupplementReservationRepository)
	if !ok {
		return ErrBillingServiceUnavailable
	}
	return repo.ReleaseSubscriptionSupplementReservation(ctx, id)
}
