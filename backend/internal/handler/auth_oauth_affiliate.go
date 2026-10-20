package handler

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"log/slog"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

type affiliateOAuthSnapshot struct {
	Attribution *service.SignupAffiliateAttribution `json:"attribution"`
}

func affiliateOAuthSnapshotKey(provider, state string) string {
	digest := sha256.Sum256([]byte(provider + "\x00" + state))
	return "affiliate:oauth:" + hex.EncodeToString(digest[:])
}

// Freeze only server-owned context, including explicit absence. A later visit
// to another invitation entry cannot change an already-started OAuth flow.
func (h *AuthHandler) freezeSignupAffiliate(c *gin.Context, provider, state string) {
	if h == nil || h.affiliatePromotion == nil {
		return
	}
	p := h.affiliatePromotion
	if p.store == nil || strings.TrimSpace(provider) == "" || state == "" {
		return
	}
	attr, _ := service.SignupAffiliateAttributionFromContext(c.Request.Context())
	raw, err := json.Marshal(affiliateOAuthSnapshot{Attribution: attr})
	if err == nil {
		err = p.store.Set(c.Request.Context(), affiliateOAuthSnapshotKey(provider, state), raw, 15*time.Minute)
	}
	if err != nil {
		// Do not print state, tokens, or the private attribution. A missing
		// snapshot is restored as locked-invalid: existing users may log in,
		// while new-account creation cannot silently lose an invitation.
		slog.Warn("failed to preserve affiliate OAuth state", "provider", provider)
	}
}

// Call only after the provider's existing state/browser checks have succeeded.
// Consumption is atomic and scoped to both provider and state.
func (h *AuthHandler) restoreSignupAffiliate(c *gin.Context, provider, state string) {
	if h == nil || h.affiliatePromotion == nil {
		return
	}
	attr := &service.SignupAffiliateAttribution{}
	if h.affiliatePromotion.store != nil && state != "" {
		raw, err := h.affiliatePromotion.store.Take(c.Request.Context(), affiliateOAuthSnapshotKey(provider, state))
		if err == nil && len(raw) <= 4096 {
			var snapshot affiliateOAuthSnapshot
			if json.Unmarshal([]byte(raw), &snapshot) == nil {
				attr = snapshot.Attribution
			}
		}
	}
	c.Request = c.Request.WithContext(service.WithSignupAffiliateAttribution(c.Request.Context(), attr))
}
