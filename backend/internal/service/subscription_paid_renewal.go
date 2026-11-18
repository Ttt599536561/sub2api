package service

import (
	"context"
	"fmt"
	"time"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
)

var (
	ErrPaidMonthlyResetNotAllowed = infraerrors.BadRequest("PAID_MONTHLY_RESET_NOT_ALLOWED", "paid monthly quota and usage cannot be reset")
	errPaidMonthlyQuotaRemoved    = infraerrors.Conflict("PAID_MONTHLY_QUOTA_REMOVED", "the subscription group's monthly quota was removed after checkout")
)

// PaidMonthlyRenewalGrant is recorded with the payment assignment in the same transaction.
type PaidMonthlyRenewalGrant struct {
	PaymentOrderID      int64
	ValidityDays        int
	AmountUSD           float64
	BaseMonthlyLimitUSD float64
	AfterExpiresAt      time.Time
}

type paidMonthlyRenewalWriter interface {
	GrantPaidMonthlyRenewal(context.Context, *UserSubscription, PaidMonthlyRenewalGrant) error
}

type paidMonthlyWindowWriter interface {
	AdvancePaidMonthlyWindow(context.Context, *UserSubscription, time.Time, float64) error
}

func (s *SubscriptionService) assignOrExtendPaidMonthlySubscription(ctx context.Context, input *AssignSubscriptionInput, paymentOrderID int64, monthlyAllowanceUSD float64) (*UserSubscription, bool, error) {
	if input.ValidityDays != 30 || monthlyAllowanceUSD == 0 {
		return s.assignOrExtendSubscription(ctx, input, true)
	}
	if paymentOrderID <= 0 || !finiteNonnegative(monthlyAllowanceUSD) {
		return nil, false, fmt.Errorf("invalid paid monthly allowance")
	}
	return s.assignOrExtendSubscriptionWithGrant(ctx, input, true, &PaidMonthlyRenewalGrant{
		PaymentOrderID: paymentOrderID,
		ValidityDays:   input.ValidityDays,
		AmountUSD:      monthlyAllowanceUSD,
	})
}

func (s *SubscriptionService) recordPaidMonthlyRenewal(ctx context.Context, sub *UserSubscription, grant PaidMonthlyRenewalGrant, now, expiry time.Time) error {
	writer, ok := s.userSubRepo.(paidMonthlyRenewalWriter)
	if !ok {
		return fmt.Errorf("paid monthly renewal storage is unavailable")
	}
	if sub.PaidMonthlyLimitUSD == nil {
		// Settle a natural rollover already due on an unconverted term before
		// locking in its current allowance and carrying usage forward.
		if start, _, due := sub.MonthlyWindowAdvanceAt(now); due {
			if err := s.userSubRepo.ResetMonthlyUsage(ctx, sub.ID, sub.MonthlyWindowStart, start); err != nil {
				return err
			}
			sub.ApplyMonthlyWindowAdvance(start, 0)
		}
	}
	grant.AfterExpiresAt = expiry
	return writer.GrantPaidMonthlyRenewal(ctx, sub, grant)
}

func (s *SubscriptionService) persistMonthlyWindowAdvance(ctx context.Context, sub *UserSubscription, start time.Time, allowance float64) error {
	if sub.PaidMonthlyLimitUSD == nil {
		return s.userSubRepo.ResetMonthlyUsage(ctx, sub.ID, sub.MonthlyWindowStart, start)
	}
	writer, ok := s.userSubRepo.(paidMonthlyWindowWriter)
	if !ok {
		return fmt.Errorf("paid monthly renewal storage is unavailable")
	}
	return writer.AdvancePaidMonthlyWindow(ctx, sub, start, allowance)
}

// extendWithLegacyMonthlyCoverage preserves natural allotments for ordinary
// renewals on a term that also contains prepaid monthly allowances.
func (s *SubscriptionService) extendWithLegacyMonthlyCoverage(ctx context.Context, sub *UserSubscription, expiry time.Time) error {
	if sub.PaidMonthlyLegacyExpiresAt == nil || expiry.Equal(sub.ExpiresAt) {
		return s.userSubRepo.ExtendExpiry(ctx, sub.ID, expiry)
	}
	horizon := sub.PaidMonthlyLegacyExpiresAt.Add(expiry.Sub(sub.ExpiresAt))
	updated := *sub
	updated.ExpiresAt = expiry
	updated.PaidMonthlyLegacyExpiresAt = &horizon
	return s.userSubRepo.Update(ctx, &updated)
}
