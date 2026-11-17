package service

import "time"

// GrokVideoBillingBinding preserves only billing identity, never credentials or
// full user/group objects, across deferred video polling. A non-nil binding with
// SubscriptionID=0 explicitly represents a balance-mode create request.
type GrokVideoBillingBinding struct {
	SupplementHoldID string    `json:"supplement_hold_id,omitempty"`
	UserID           int64     `json:"user_id"`
	GroupID          int64     `json:"group_id"`
	SubscriptionID   int64     `json:"subscription_id"`
	TermStartsAt     time.Time `json:"term_starts_at"`
	EntitlementKind  string    `json:"entitlement_kind"`
}

func CaptureGrokVideoBillingBinding(userID int64, groupID *int64, subscription *UserSubscription) *GrokVideoBillingBinding {
	binding := &GrokVideoBillingBinding{UserID: userID}
	if groupID != nil {
		binding.GroupID = *groupID
	}
	if subscription != nil {
		binding.SubscriptionID = subscription.ID
		binding.TermStartsAt = subscription.StartsAt
		binding.EntitlementKind = SubscriptionEntitlementStandard
		if subscription.IsWelfare() {
			binding.EntitlementKind = SubscriptionEntitlementWelfare
		}
	}
	return binding
}

// RestoreSubscription deliberately accepts an expired original term: generation
// was admitted at create-time, and its completed usage still has to be settled.
// Legacy jobs without a binding retain their prior polling-time behavior.
func (b *GrokVideoBillingBinding) RestoreSubscription(userID int64, groupID *int64, current *UserSubscription) (*UserSubscription, error) {
	if b == nil {
		return current, nil
	}
	var group int64
	if groupID != nil {
		group = *groupID
	}
	if b.UserID != userID || b.GroupID != group {
		return nil, ErrSubscriptionInvalid
	}
	if b.SubscriptionID == 0 {
		return nil, nil
	}
	if b.SubscriptionID < 0 || b.TermStartsAt.IsZero() || (b.EntitlementKind != SubscriptionEntitlementStandard && b.EntitlementKind != SubscriptionEntitlementWelfare) {
		return nil, ErrSubscriptionInvalid
	}
	return &UserSubscription{ID: b.SubscriptionID, UserID: userID, GroupID: group,
		StartsAt: b.TermStartsAt, EntitlementKind: b.EntitlementKind}, nil
}
