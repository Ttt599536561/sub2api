package handler

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"math"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

const (
	signupAffiliateCookieName     = "__Host-sub2api_affiliate"
	signupAffiliateTTL            = 30 * 24 * time.Hour
	affiliateTicketTTL            = 2 * time.Minute
	affiliateTicketPrefix         = "affiliate:handoff:"
	affiliateCookiePrefix         = "affiliate:browser:"
	affiliateBrowserReadFailedKey = "affiliate.browser-read-failed"
)

type affiliatePromotionBackend interface {
	ResolveAffiliateDomain(context.Context, string) (*service.AffiliatePromotionDomain, error)
	ValidateSignupAffiliateAttribution(context.Context, *service.SignupAffiliateAttribution) error
	IsEnabled(context.Context) bool
}
type affiliateOriginSettings interface{ GetFrontendURL(context.Context) string }

type affiliatePromotionHandler struct {
	settings        affiliateOriginSettings
	backend         affiliatePromotionBackend
	store           service.AffiliateAttributionStore
	originMu        sync.Mutex
	cachedOrigin    *url.URL
	cachedOriginErr error
	originExpiresAt time.Time
}

func newAffiliatePromotionHandler(settings affiliateOriginSettings, backend affiliatePromotionBackend, store service.AffiliateAttributionStore) *affiliatePromotionHandler {
	return &affiliatePromotionHandler{settings: settings, backend: backend, store: store}
}

// AffiliatePromotionMiddleware must run before the embedded SPA middleware.
// Promotion hosts are entry points only; the canonical site owns registration.
func (h *AuthHandler) AffiliatePromotionMiddleware(store service.AffiliateAttributionStore) gin.HandlerFunc {
	if h == nil || h.authService == nil || h.authService.AffiliateService() == nil || h.settingSvc == nil {
		return func(c *gin.Context) { c.Next() }
	}
	h.affiliatePromotion = newAffiliatePromotionHandler(h.settingSvc, h.authService.AffiliateService(), store)
	return h.affiliatePromotion.Middleware()
}

func (p *affiliatePromotionHandler) origin(ctx context.Context) (*url.URL, error) {
	if p == nil || p.settings == nil {
		return nil, errors.New("affiliate origin unavailable")
	}
	p.originMu.Lock()
	defer p.originMu.Unlock()
	if time.Now().Before(p.originExpiresAt) {
		return p.cachedOrigin, p.cachedOriginErr
	}
	p.cachedOrigin, p.cachedOriginErr = service.ParseAffiliateFrontendOrigin(p.settings.GetFrontendURL(ctx))
	ttl := 30 * time.Second
	if p.cachedOriginErr != nil {
		ttl = 2 * time.Second
	}
	p.originExpiresAt = time.Now().Add(ttl)
	return p.cachedOrigin, p.cachedOriginErr
}

// Settings writes invalidate the local origin cache immediately; the bounded
// TTL also refreshes it in other application instances.
func (h *AuthHandler) InvalidateAffiliateOrigin() {
	if h == nil || h.affiliatePromotion == nil {
		return
	}
	p := h.affiliatePromotion
	p.originMu.Lock()
	p.originExpiresAt = time.Time{}
	p.originMu.Unlock()
}

func affiliateRequestHost(raw string) string {
	if !affiliateASCIIAuthority(raw) {
		return ""
	}
	raw = strings.TrimSpace(raw)
	if strings.Contains(raw, ":") {
		host, _, err := net.SplitHostPort(raw)
		if err != nil {
			return ""
		}
		raw = host
	}
	// A DNS absolute name and its non-rooted spelling identify the same host.
	// Normalize before looking up the mapping, but never Unicode-fold input.
	raw = strings.TrimSuffix(raw, ".")
	host, err := service.NormalizeAffiliateHostname(raw)
	if err != nil {
		return ""
	}
	return host
}

func affiliateRequestAuthorityAllowed(raw string) bool {
	raw = strings.TrimSpace(raw)
	if !strings.Contains(raw, ":") {
		return true
	}
	_, port, err := net.SplitHostPort(raw)
	return err == nil && (port == "80" || port == "443")
}

func affiliateASCIIAuthority(raw string) bool {
	for i := 0; i < len(raw); i++ {
		if raw[i] >= 128 {
			return false
		}
	}
	return true
}

func (p *affiliatePromotionHandler) Middleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		if p == nil || p.backend == nil {
			c.Next()
			return
		}
		host := affiliateRequestHost(c.Request.Host)
		origin, originErr := p.origin(c.Request.Context())
		canonical := originErr == nil && host == origin.Hostname() && affiliateRequestAuthorityAllowed(c.Request.Host)
		if canonical {
			if strings.HasPrefix(c.Request.URL.Path, "/api/v1/auth/") {
				p.captureBrowserAttribution(c)
			}
			c.Next()
			return
		}
		// Preserve a locked failure if canonical configuration changes during a
		// registration flow. Existing account login can still complete.
		if strings.HasPrefix(c.Request.URL.Path, "/api/v1/auth/") && len(affiliateCookies(c)) > 0 {
			c.Request = c.Request.WithContext(service.WithSignupAffiliateAttribution(c.Request.Context(), &service.SignupAffiliateAttribution{}))
		}
		if host == "" {
			if !affiliateASCIIAuthority(c.Request.Host) {
				affiliateInvalid(c, "Invalid invitation hostname")
				return
			}
			c.Next()
			return
		}
		domain, err := p.backend.ResolveAffiliateDomain(c.Request.Context(), host)
		if err != nil {
			affiliateUnavailable(c)
			return
		}
		if domain == nil {
			c.Next()
			return
		}
		affiliatePrivateHeaders(c)
		if !affiliateRequestAuthorityAllowed(c.Request.Host) {
			affiliateInvalid(c, "Invalid invitation port")
			return
		}
		if !domain.Enabled || domain.InviterID <= 0 {
			response.ErrorFrom(c, infraerrors.New(http.StatusGone, "AFFILIATE_DOMAIN_INACTIVE", "This invitation entry is no longer available"))
			c.Abort()
			return
		}
		if c.Request.Method != http.MethodGet && c.Request.Method != http.MethodHead {
			c.Header("Allow", "GET, HEAD")
			c.AbortWithStatus(http.StatusMethodNotAllowed)
			return
		}
		if c.Request.URL.Path != "/" && c.Request.URL.Path != "/register" {
			c.AbortWithStatus(http.StatusNotFound)
			return
		}
		if originErr != nil {
			affiliateUnavailable(c)
			return
		}
		if !p.backend.IsEnabled(c.Request.Context()) {
			affiliateInvalid(c, "The invitation program is unavailable")
			return
		}
		if c.Request.Method == http.MethodHead {
			c.AbortWithStatus(http.StatusOK)
			return
		}
		if !p.limit(c, "entry", 30) {
			return
		}
		now := time.Now().UTC()
		attr := service.SignupAffiliateAttribution{InviterID: domain.InviterID, SourceHost: domain.Hostname, HostRevision: domain.Revision, CapturedAt: now, ExpiresAt: now.Add(signupAffiliateTTL)}
		if err := p.backend.ValidateSignupAffiliateAttribution(c.Request.Context(), &attr); err != nil {
			affiliateValidationFailure(c, err)
			return
		}
		ticket, err := p.storeAttribution(c.Request.Context(), affiliateTicketPrefix, &attr, affiliateTicketTTL)
		if err != nil {
			affiliateUnavailable(c)
			return
		}
		destination := *origin
		destination.Path = "/api/v1/auth/affiliate/claim"
		destination.RawQuery = url.Values{"ticket": []string{ticket}}.Encode()
		c.Redirect(http.StatusSeeOther, destination.String())
		c.Abort()
	}
}

func affiliatePrivateHeaders(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	c.Header("Referrer-Policy", "no-referrer")
}
func affiliateInvalid(c *gin.Context, message string) {
	affiliatePrivateHeaders(c)
	response.ErrorFrom(c, infraerrors.BadRequest("AFFILIATE_ATTRIBUTION_INVALID", message))
	c.Abort()
}
func affiliateUnavailable(c *gin.Context) {
	affiliatePrivateHeaders(c)
	response.ErrorFrom(c, infraerrors.ServiceUnavailable("AFFILIATE_ATTRIBUTION_UNAVAILABLE", "Invitation registration is temporarily unavailable; please retry the original invitation link"))
	c.Abort()
}
func affiliateValidationFailure(c *gin.Context, err error) {
	if code := infraerrors.Code(err); code >= http.StatusBadRequest && code < http.StatusInternalServerError {
		affiliatePrivateHeaders(c)
		response.ErrorFrom(c, err)
		c.Abort()
		return
	}
	affiliateUnavailable(c)
}

func validAffiliateToken(token string) bool {
	if len(token) != 43 {
		return false
	}
	decoded, err := base64.RawURLEncoding.DecodeString(token)
	return err == nil && len(decoded) == 32 && base64.RawURLEncoding.EncodeToString(decoded) == token
}
func newAffiliateToken() (string, error) {
	bytes := make([]byte, 32)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(bytes), nil
}
func (p *affiliatePromotionHandler) storeAttribution(ctx context.Context, prefix string, attr *service.SignupAffiliateAttribution, ttl time.Duration) (string, error) {
	if p.store == nil {
		return "", errors.New("affiliate storage unavailable")
	}
	value, err := json.Marshal(attr)
	if err != nil {
		return "", err
	}
	for attempt := 0; attempt < 3; attempt++ {
		token, err := newAffiliateToken()
		if err != nil {
			return "", err
		}
		stored, err := p.store.SetIfAbsent(ctx, prefix+token, value, ttl)
		if err != nil {
			return "", err
		}
		if stored {
			return token, nil
		}
	}
	return "", errors.New("affiliate token allocation failed")
}
func (p *affiliatePromotionHandler) loadAttribution(ctx context.Context, prefix, token string, consume bool) (*service.SignupAffiliateAttribution, error) {
	if !validAffiliateToken(token) {
		return nil, service.ErrAffiliateAttributionMissing
	}
	if p.store == nil {
		return nil, errors.New("affiliate storage unavailable")
	}
	var raw string
	var err error
	if consume {
		raw, err = p.store.Take(ctx, prefix+token)
	} else {
		raw, err = p.store.Get(ctx, prefix+token)
	}
	if err != nil {
		return nil, err
	}
	var attr service.SignupAffiliateAttribution
	if len(raw) > 2048 || json.Unmarshal([]byte(raw), &attr) != nil || attr.InviterID <= 0 || !attr.ExpiresAt.After(time.Now()) {
		return nil, service.ErrAffiliateAttributionMissing
	}
	return &attr, nil
}
func affiliateCookies(c *gin.Context) []*http.Cookie {
	var found []*http.Cookie
	for _, cookie := range c.Request.Cookies() {
		if cookie.Name == signupAffiliateCookieName {
			found = append(found, cookie)
		}
	}
	return found
}
func (p *affiliatePromotionHandler) captureBrowserAttribution(c *gin.Context) {
	cookies := affiliateCookies(c)
	if len(cookies) == 0 {
		return
	}
	attr := &service.SignupAffiliateAttribution{}
	if len(cookies) == 1 {
		stored, err := p.loadAttribution(c.Request.Context(), affiliateCookiePrefix, cookies[0].Value, false)
		if err == nil {
			attr = stored
		} else if !errors.Is(err, service.ErrAffiliateAttributionMissing) {
			// Keep storage unavailability distinct from confirmed expiry or
			// revocation. A later claim must not replace an unread first source.
			c.Set(affiliateBrowserReadFailedKey, true)
		}
	}
	// A non-nil zero attribution is an explicit locked error, not an invitation
	// to fall back to user-controlled aff_code. Creation validates it atomically.
	c.Request = c.Request.WithContext(service.WithSignupAffiliateAttribution(c.Request.Context(), attr))
}

func (p *affiliatePromotionHandler) limit(c *gin.Context, kind string, max int64) bool {
	if p.store == nil {
		affiliateUnavailable(c)
		return false
	}
	digest := sha256.Sum256([]byte(c.ClientIP()))
	key := "affiliate:limit:" + kind + ":" + hex.EncodeToString(digest[:]) + ":" + strconv.FormatInt(time.Now().Unix()/60, 10)
	n, err := p.store.Count(c.Request.Context(), key, 120*time.Second)
	if err != nil {
		affiliateUnavailable(c)
		return false
	}
	if n > max {
		c.Header("Retry-After", "60")
		response.ErrorFrom(c, infraerrors.New(http.StatusTooManyRequests, "AFFILIATE_RATE_LIMITED", "Too many invitation requests; please retry shortly"))
		c.Abort()
		return false
	}
	return true
}

func (p *affiliatePromotionHandler) Claim(c *gin.Context) {
	affiliatePrivateHeaders(c)
	origin, err := p.origin(c.Request.Context())
	if err != nil {
		affiliateUnavailable(c)
		return
	}
	if affiliateRequestHost(c.Request.Host) != origin.Hostname() || !affiliateRequestAuthorityAllowed(c.Request.Host) {
		affiliateInvalid(c, "Invalid invitation destination")
		return
	}
	values := c.Request.URL.Query()["ticket"]
	if len(values) != 1 || !validAffiliateToken(values[0]) {
		affiliateInvalid(c, "This invitation link has expired; reopen the original invitation link")
		return
	}
	if !p.limit(c, "claim", 30) {
		return
	}
	if c.GetBool(affiliateBrowserReadFailedKey) {
		affiliateUnavailable(c)
		return
	}
	attr, err := p.loadAttribution(c.Request.Context(), affiliateTicketPrefix, values[0], true)
	if errors.Is(err, service.ErrAffiliateAttributionMissing) {
		affiliateInvalid(c, "This invitation link has expired; reopen the original invitation link")
		return
	}
	if err != nil {
		affiliateUnavailable(c)
		return
	}
	if err := p.backend.ValidateSignupAffiliateAttribution(c.Request.Context(), attr); err != nil {
		affiliateValidationFailure(c, err)
		return
	}
	if current, ok := service.SignupAffiliateAttributionFromContext(c.Request.Context()); ok {
		currentErr := p.backend.ValidateSignupAffiliateAttribution(c.Request.Context(), current)
		if currentErr == nil {
			c.Redirect(http.StatusSeeOther, "/register")
			return
		}
		if !errors.Is(currentErr, service.ErrAffiliateAttributionInvalid) {
			affiliateValidationFailure(c, currentErr)
			return
		}
	}
	token, err := p.storeAttribution(c.Request.Context(), affiliateCookiePrefix, attr, time.Until(attr.ExpiresAt))
	if err != nil {
		affiliateUnavailable(c)
		return
	}
	http.SetCookie(c.Writer, &http.Cookie{Name: signupAffiliateCookieName, Value: token, Path: "/", MaxAge: int(math.Ceil(time.Until(attr.ExpiresAt).Seconds())), Expires: attr.ExpiresAt, Secure: true, HttpOnly: true, SameSite: http.SameSiteLaxMode})
	c.Redirect(http.StatusSeeOther, "/register")
}
func (p *affiliatePromotionHandler) Status(c *gin.Context) {
	affiliatePrivateHeaders(c)
	// With no browser attribution, alternate ordinary site origins need no
	// promotion configuration and expose no private referral state.
	if len(affiliateCookies(c)) == 0 {
		response.Success(c, gin.H{"locked": false})
		return
	}
	origin, err := p.origin(c.Request.Context())
	if err != nil || affiliateRequestHost(c.Request.Host) != origin.Hostname() {
		affiliateInvalid(c, "Invalid invitation destination")
		return
	}
	attr, locked := service.SignupAffiliateAttributionFromContext(c.Request.Context())
	if c.GetBool(affiliateBrowserReadFailedKey) {
		affiliateUnavailable(c)
		return
	}
	if locked {
		if err := p.backend.ValidateSignupAffiliateAttribution(c.Request.Context(), attr); err != nil {
			affiliateValidationFailure(c, err)
			return
		}
	}
	response.Success(c, gin.H{"locked": locked})
}
func clearSignupAffiliateCookie(c *gin.Context) {
	if c == nil || len(affiliateCookies(c)) == 0 {
		return
	}
	http.SetCookie(c.Writer, &http.Cookie{Name: signupAffiliateCookieName, Value: "", Path: "/", MaxAge: -1, Expires: time.Unix(1, 0), Secure: true, HttpOnly: true, SameSite: http.SameSiteLaxMode})
}
func (h *AuthHandler) ClaimAffiliateAttribution(c *gin.Context) {
	if h == nil || h.affiliatePromotion == nil {
		affiliateUnavailable(c)
		return
	}
	h.affiliatePromotion.Claim(c)
}
func (h *AuthHandler) AffiliateAttributionStatus(c *gin.Context) {
	if h == nil || h.affiliatePromotion == nil {
		affiliatePrivateHeaders(c)
		response.Success(c, gin.H{"locked": false})
		return
	}
	h.affiliatePromotion.Status(c)
}
