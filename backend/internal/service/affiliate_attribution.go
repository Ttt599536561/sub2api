package service

import (
	"context"
	"time"
)

// SignupAffiliateAttribution is a server-verified referral snapshot. A non-nil
// zero value deliberately represents an invalid protected source, not absence.
type SignupAffiliateAttribution struct {
	InviterID    int64     `json:"inviter_id"`
	SourceHost   string    `json:"source_host"`
	HostRevision int64     `json:"host_revision"`
	CapturedAt   time.Time `json:"captured_at"`
	ExpiresAt    time.Time `json:"expires_at"`
}

type signupAffiliateAttributionContextKey struct{}

// WithSignupAffiliateAttribution stores a copy so that one OAuth flow cannot
// mutate another flow's captured source. Passing nil explicitly removes it.
func WithSignupAffiliateAttribution(ctx context.Context, attr *SignupAffiliateAttribution) context.Context {
	var snapshot *SignupAffiliateAttribution
	if attr != nil {
		value := *attr
		snapshot = &value
	}
	return context.WithValue(ctx, signupAffiliateAttributionContextKey{}, snapshot)
}

func SignupAffiliateAttributionFromContext(ctx context.Context) (*SignupAffiliateAttribution, bool) {
	attr, ok := ctx.Value(signupAffiliateAttributionContextKey{}).(*SignupAffiliateAttribution)
	if !ok || attr == nil {
		return nil, false
	}
	snapshot := *attr
	return &snapshot, true
}
