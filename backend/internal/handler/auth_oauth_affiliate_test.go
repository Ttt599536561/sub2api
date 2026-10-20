//go:build unit

package handler

import (
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func affiliateOAuthContext(t *testing.T, attr *service.SignupAffiliateAttribution) *gin.Context {
	t.Helper()
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest("GET", "https://example.test/api/v1/auth/oauth/github/start", nil)
	c.Request = c.Request.WithContext(service.WithSignupAffiliateAttribution(c.Request.Context(), attr))
	return c
}
func TestAffiliateOAuthSnapshotFrozenToProviderAndState(t *testing.T) {
	_, p, _, _ := newPromotionTestRouter(t)
	h := &AuthHandler{affiliatePromotion: p}
	now := time.Now().UTC()
	original := &service.SignupAffiliateAttribution{InviterID: 42, SourceHost: "partner.example.test", HostRevision: 1, CapturedAt: now, ExpiresAt: now.Add(time.Hour)}
	start := affiliateOAuthContext(t, original)
	h.freezeSignupAffiliate(start, "github", "unique-state")
	changed := *original
	changed.InviterID = 99
	callback := affiliateOAuthContext(t, &changed)
	h.restoreSignupAffiliate(callback, "github", "unique-state")
	actual, ok := service.SignupAffiliateAttributionFromContext(callback.Request.Context())
	require.True(t, ok)
	require.Equal(t, int64(42), actual.InviterID)
	wrong := affiliateOAuthContext(t, &changed)
	h.restoreSignupAffiliate(wrong, "google", "unique-state")
	actual, ok = service.SignupAffiliateAttributionFromContext(wrong.Request.Context())
	require.True(t, ok)
	require.Zero(t, actual.InviterID)
	replay := affiliateOAuthContext(t, &changed)
	h.restoreSignupAffiliate(replay, "github", "unique-state")
	actual, ok = service.SignupAffiliateAttributionFromContext(replay.Request.Context())
	require.True(t, ok)
	require.Zero(t, actual.InviterID)
}
func TestAffiliateOAuthSnapshotAbsentCannotAdoptLaterCookie(t *testing.T) {
	_, p, _, _ := newPromotionTestRouter(t)
	h := &AuthHandler{affiliatePromotion: p}
	h.freezeSignupAffiliate(affiliateOAuthContext(t, nil), "linuxdo", "no-source-state")
	callback := affiliateOAuthContext(t, &service.SignupAffiliateAttribution{InviterID: 99})
	h.restoreSignupAffiliate(callback, "linuxdo", "no-source-state")
	_, ok := service.SignupAffiliateAttributionFromContext(callback.Request.Context())
	require.False(t, ok)
}
func TestAffiliateOAuthSnapshotExpiredAndStorageFailureStayLocked(t *testing.T) {
	for _, failure := range []string{"expiry", "storage"} {
		t.Run(failure, func(t *testing.T) {
			_, p, _, mr := newPromotionTestRouter(t)
			h := &AuthHandler{affiliatePromotion: p}
			h.freezeSignupAffiliate(affiliateOAuthContext(t, &service.SignupAffiliateAttribution{InviterID: 42}), "oidc", "state")
			if failure == "expiry" {
				mr.FastForward(16 * time.Minute)
			} else {
				mr.SetError("unavailable")
			}
			callback := affiliateOAuthContext(t, nil)
			h.restoreSignupAffiliate(callback, "oidc", "state")
			attr, ok := service.SignupAffiliateAttributionFromContext(callback.Request.Context())
			require.True(t, ok)
			require.Zero(t, attr.InviterID)
		})
	}
}
