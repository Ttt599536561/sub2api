package service

import (
	"context"
	"math"
	"strconv"
	"time"

	"github.com/google/uuid"
)

func inflightSubscriptionTermScope(sub *UserSubscription) string {
	return inflightSubscriptionScope(sub.ID) + ":" + strconv.FormatInt(sub.StartsAt.UnixNano(), 10)
}

// Register before the authoritative database read. Settlement retains the hold
// until its transaction completes, so a concurrent reader observes either that
// hold or the committed charge (temporarily both is conservative).
func (s *BillingCacheService) reserveSubscriptionSupplement(ctx context.Context, user *User, group *Group, selected *UserSubscription, amount float64) (*InflightReservation, error) {
	if user == nil || group == nil || selected == nil || selected.UserID != user.ID || selected.GroupID != group.ID || selected.StartsAt.IsZero() {
		return nil, ErrSubscriptionInvalid
	}
	if math.IsNaN(amount) || math.IsInf(amount, 0) || amount < 0 {
		return nil, ErrSubscriptionInvalid
	}
	if amount == 0 {
		return nil, nil
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	durable, hasDurable := s.subRepo.(SubscriptionSupplementReservationRepository)
	scopeCache, hasScope := s.cache.(InflightScopeReservationCache)
	renewer, hasRenewal := s.cache.(InflightScopeReservationRenewer)
	if !hasDurable {
		return nil, ErrBillingServiceUnavailable
	}
	ttl := defaultInflightReservationTTL
	if s.cfg != nil && s.cfg.Billing.InflightReservation.TTLSeconds > 0 {
		ttl = time.Duration(s.cfg.Billing.InflightReservation.TTLSeconds) * time.Second
	}
	scope := inflightSubscriptionTermScope(selected)
	res := &InflightReservation{userID: user.ID, requestID: uuid.NewString(), amount: amount, ttl: ttl, renewUntilReleased: true}
	res.refs.Store(1)
	var err error
	if hasScope {
		res.scopeCache = scopeCache
		res.scopes = []string{scope}
		reserveCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), inflightReservationReserveTimeout)
		_, _ = scopeCache.ReserveInflightScope(reserveCtx, scope, res.requestID, amount, ttl)
		cancel()
	}
	var fresh *UserSubscription
	if s.subscriptionService != nil {
		fresh, group, err = s.getSubscriptionForRequestAdmission(ctx, user.ID, group.ID, selected)
	} else if s.subRepo != nil {
		fresh, err = s.subRepo.GetByID(ctx, selected.ID)
		if err == nil {
			err = loadSubscriptionSupplement(ctx, s.subRepo, fresh)
		}
		if fresh != nil && fresh.Group != nil {
			group = fresh.Group
		}
	} else {
		err = ErrBillingServiceUnavailable
	}
	if err != nil {
		res.Release()
		return nil, err
	}
	if fresh == nil || fresh.ID != selected.ID || fresh.UserID != user.ID || fresh.GroupID != selected.GroupID || !fresh.StartsAt.Equal(selected.StartsAt) {
		res.Release()
		return nil, ErrSubscriptionInvalid
	}
	group = fresh.EffectiveGroup(group)
	if group == nil || !group.IsActive() || !group.IsSubscriptionType() {
		res.Release()
		return nil, ErrSubscriptionInvalid
	}
	if err = checkAuthoritativeSubscriptionLimits(fresh, group, time.Now()); err == nil {
		res.supplementHoldRepo = durable
		err = durable.ReserveSubscriptionSupplement(ctx, &SubscriptionSupplementReservation{ID: res.requestID, SubscriptionID: fresh.ID, UserID: user.ID, GroupID: fresh.GroupID, TermStartsAt: fresh.StartsAt, AmountUSD: amount, TTL: ttl})
	}
	if err != nil {
		res.Release()
		return nil, err
	}
	res.startRenewalCallback(func(ctx context.Context) (bool, error) {
		alive, err := durable.RenewSubscriptionSupplementReservation(ctx, res.requestID, ttl)
		if err != nil || !alive {
			return alive, err
		}
		if hasRenewal {
			_, _ = renewer.RenewInflightScope(ctx, scope, res.requestID, ttl)
		}
		return true, nil
	})
	return res, nil
}
