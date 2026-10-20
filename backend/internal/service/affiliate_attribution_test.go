//go:build unit

package service

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestSignupAffiliateAttributionContextCopiesTrustedSnapshot(t *testing.T) {
	now := time.Now().UTC()
	attr := &SignupAffiliateAttribution{InviterID: 42, SourceHost: "partner.example.test", HostRevision: 2, CapturedAt: now, ExpiresAt: now.Add(time.Hour)}
	ctx := WithSignupAffiliateAttribution(context.Background(), attr)
	attr.InviterID = 99
	got, ok := SignupAffiliateAttributionFromContext(ctx)
	require.True(t, ok)
	require.Equal(t, int64(42), got.InviterID)
	got.InviterID = 100
	again, ok := SignupAffiliateAttributionFromContext(ctx)
	require.True(t, ok)
	require.Equal(t, int64(42), again.InviterID)
}

func TestSignupAffiliateAttributionContextPreservesInvalidLock(t *testing.T) {
	ctx := WithSignupAffiliateAttribution(context.Background(), &SignupAffiliateAttribution{})
	attr, ok := SignupAffiliateAttributionFromContext(ctx)
	require.True(t, ok, "an invalid protected source must not become an ordinary unprotected signup")
	require.Zero(t, attr.InviterID)
	_, ok = SignupAffiliateAttributionFromContext(context.Background())
	require.False(t, ok)
	_, ok = SignupAffiliateAttributionFromContext(WithSignupAffiliateAttribution(ctx, nil))
	require.False(t, ok)
}
