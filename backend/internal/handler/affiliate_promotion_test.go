//go:build unit

package handler

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type promotionOriginStub string

func (s promotionOriginStub) GetFrontendURL(context.Context) string { return string(s) }

type promotionBackendStub struct {
	validate func(*service.SignupAffiliateAttribution) error
	domain   *service.AffiliatePromotionDomain
	enabled  bool
	invalid  bool
	err      error
}

func (s *promotionBackendStub) IsEnabled(context.Context) bool { return s.enabled }
func (s *promotionBackendStub) ResolveAffiliateDomain(_ context.Context, host string) (*service.AffiliatePromotionDomain, error) {
	if s.err != nil {
		return nil, s.err
	}
	if s.domain != nil && host == s.domain.Hostname {
		copy := *s.domain
		return &copy, nil
	}
	return nil, nil
}
func (s *promotionBackendStub) ValidateSignupAffiliateAttribution(_ context.Context, a *service.SignupAffiliateAttribution) error {
	if s.validate != nil {
		return s.validate(a)
	}
	if s.err != nil {
		return s.err
	}
	if s.invalid || !s.enabled || a == nil || a.InviterID <= 0 || s.domain == nil || !s.domain.Enabled || a.SourceHost != s.domain.Hostname || a.HostRevision != s.domain.Revision || a.InviterID != s.domain.InviterID || !a.ExpiresAt.After(time.Now()) {
		return service.ErrAffiliateAttributionInvalid
	}
	return nil
}

func newPromotionTestRouter(t *testing.T) (*gin.Engine, *affiliatePromotionHandler, *promotionBackendStub, *promotionMemoryStore) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	mr := newPromotionMemoryStore()
	backend := &promotionBackendStub{enabled: true, domain: &service.AffiliatePromotionDomain{Hostname: "partner.example.test", InviterID: 42, Enabled: true, Revision: 1}}
	p := newAffiliatePromotionHandler(promotionOriginStub("https://example.test"), backend, mr)
	router := gin.New()
	router.Use(p.Middleware())
	router.GET("/api/v1/auth/affiliate/claim", p.Claim)
	router.GET("/api/v1/auth/affiliate-attribution", p.Status)
	router.GET("/register", func(c *gin.Context) { c.Status(http.StatusOK) })
	router.POST("/api/v1/auth/marker", func(c *gin.Context) {
		_, locked := service.SignupAffiliateAttributionFromContext(c.Request.Context())
		c.JSON(200, gin.H{"locked": locked})
	})
	return router, p, backend, mr
}

func promotionRequest(router *gin.Engine, method, target string, cookies ...*http.Cookie) *httptest.ResponseRecorder {
	request := httptest.NewRequest(method, target, nil)
	for _, cookie := range cookies {
		request.AddCookie(cookie)
	}
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)
	return recorder
}
func promotionTicket(t *testing.T, router *gin.Engine) string {
	t.Helper()
	res := promotionRequest(router, http.MethodGet, "https://partner.example.test/")
	require.Equal(t, http.StatusSeeOther, res.Code, res.Body.String())
	target, err := url.Parse(res.Header().Get("Location"))
	require.NoError(t, err)
	require.Equal(t, "https", target.Scheme)
	require.Equal(t, "example.test", target.Host)
	require.Equal(t, "/api/v1/auth/affiliate/claim", target.Path)
	require.NotEmpty(t, target.Query().Get("ticket"))
	require.Equal(t, "no-store", res.Header().Get("Cache-Control"))
	require.Equal(t, "no-referrer", res.Header().Get("Referrer-Policy"))
	return target.String()
}

func TestAffiliatePromotionClaimOpaqueCookieAndReplay(t *testing.T) {
	router, _, _, _ := newPromotionTestRouter(t)
	target := promotionTicket(t, router)
	response := promotionRequest(router, http.MethodGet, target)
	require.Equal(t, http.StatusSeeOther, response.Code, response.Body.String())
	require.Equal(t, "/register", response.Header().Get("Location"))
	cookies := response.Result().Cookies()
	require.Len(t, cookies, 1)
	cookie := cookies[0]
	require.True(t, strings.HasPrefix(cookie.Name, "__Host-"))
	require.True(t, cookie.Secure)
	require.True(t, cookie.HttpOnly)
	require.Empty(t, cookie.Domain)
	require.Equal(t, "/", cookie.Path)
	require.Equal(t, http.SameSiteLaxMode, cookie.SameSite)
	require.Equal(t, 30*24*60*60, cookie.MaxAge)
	require.Len(t, cookie.Value, 43)
	require.NotEqual(t, urlMustParse(t, target).Query().Get("ticket"), cookie.Value)
	status := promotionRequest(router, http.MethodGet, "https://example.test/api/v1/auth/affiliate-attribution", cookie)
	require.Equal(t, 200, status.Code)
	require.Contains(t, status.Body.String(), `"locked":true`)
	require.NotContains(t, status.Body.String(), "42")
	require.Equal(t, "no-store", status.Header().Get("Cache-Control"))
	replay := promotionRequest(router, http.MethodGet, target)
	require.Equal(t, http.StatusBadRequest, replay.Code)
}
func urlMustParse(t *testing.T, raw string) *url.URL {
	t.Helper()
	u, e := url.Parse(raw)
	require.NoError(t, e)
	return u
}

func TestAffiliatePromotionHostIsolation(t *testing.T) {
	router, _, _, mr := newPromotionTestRouter(t)
	req := httptest.NewRequest(http.MethodGet, "https://example.test/", nil)
	req.Header.Set("X-Forwarded-Host", "partner.example.test")
	req.Header.Set("Forwarded", "host=partner.example.test")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	require.Equal(t, 404, rec.Code)
	require.Empty(t, mr.Keys())
	for _, path := range []string{"/api/v1/auth/register", "/login", "/s/anything"} {
		res := promotionRequest(router, http.MethodGet, "https://partner.example.test"+path)
		require.Equal(t, http.StatusNotFound, res.Code, path)
	}
	res := promotionRequest(router, http.MethodPost, "https://partner.example.test/")
	require.Equal(t, http.StatusMethodNotAllowed, res.Code)
	target := promotionTicket(t, router)
	host := urlMustParse(t, target)
	host.Host = "other.example.test"
	wrongHost := promotionRequest(router, http.MethodGet, host.String())
	require.Equal(t, http.StatusBadRequest, wrongHost.Code)
	require.Equal(t, http.StatusSeeOther, promotionRequest(router, http.MethodGet, target).Code)
}

func TestAffiliatePromotionExpiredAndRevokedTickets(t *testing.T) {
	t.Run("expired", func(t *testing.T) {
		router, _, _, mr := newPromotionTestRouter(t)
		target := promotionTicket(t, router)
		mr.FastForward(3 * time.Minute)
		require.Equal(t, http.StatusBadRequest, promotionRequest(router, http.MethodGet, target).Code)
	})
	t.Run("revision", func(t *testing.T) {
		router, _, backend, _ := newPromotionTestRouter(t)
		target := promotionTicket(t, router)
		backend.domain.Revision++
		response := promotionRequest(router, http.MethodGet, target)
		require.Equal(t, http.StatusBadRequest, response.Code)
		require.Empty(t, response.Result().Cookies())
	})
	t.Run("disabled", func(t *testing.T) {
		router, _, backend, _ := newPromotionTestRouter(t)
		backend.domain.Enabled = false
		require.Equal(t, http.StatusGone, promotionRequest(router, http.MethodGet, "https://partner.example.test/").Code)
	})
}
func TestAffiliatePromotionExistingCookieNotReplaced(t *testing.T) {
	router, _, _, mr := newPromotionTestRouter(t)
	first := promotionRequest(router, http.MethodGet, promotionTicket(t, router)).Result().Cookies()[0]
	mr.FastForward(time.Hour)
	response := promotionRequest(router, http.MethodGet, promotionTicket(t, router), first)
	require.Equal(t, http.StatusSeeOther, response.Code)
	require.Empty(t, response.Result().Cookies())
	require.Equal(t, http.StatusOK, promotionRequest(router, http.MethodGet, "https://example.test/api/v1/auth/affiliate-attribution", first).Code)
}
func TestAffiliatePromotionInvalidCookieKeepsCreationLocked(t *testing.T) {
	router, _, _, _ := newPromotionTestRouter(t)
	bad := &http.Cookie{Name: signupAffiliateCookieName, Value: strings.Repeat("x", 43)}
	response := promotionRequest(router, http.MethodPost, "https://example.test/api/v1/auth/marker", bad)
	require.Equal(t, 200, response.Code)
	require.Contains(t, response.Body.String(), `"locked":true`)
	status := promotionRequest(router, http.MethodGet, "https://example.test/api/v1/auth/affiliate-attribution", bad)
	require.Equal(t, http.StatusBadRequest, status.Code)
}
func TestAffiliatePromotionStoreFailureDoesNotSilentlyRegister(t *testing.T) {
	router, _, _, mr := newPromotionTestRouter(t)
	target := promotionTicket(t, router)
	mr.SetError("service unavailable")
	response := promotionRequest(router, http.MethodGet, target)
	require.Equal(t, http.StatusServiceUnavailable, response.Code)
	require.Empty(t, response.Result().Cookies())
}

type countingPromotionOrigin struct {
	calls int
	raw   string
}

func (s *countingPromotionOrigin) GetFrontendURL(context.Context) string { s.calls++; return s.raw }

func TestAffiliatePromotionCanonicalTrafficDoesNotReadSettingsPerRequest(t *testing.T) {
	router, p, _, _ := newPromotionTestRouter(t)
	settings := &countingPromotionOrigin{raw: "https://example.test"}
	p.settings = settings
	for i := 0; i < 10; i++ {
		require.Equal(t, http.StatusOK, promotionRequest(router, http.MethodGet, "https://example.test/api/v1/auth/affiliate-attribution").Code)
	}
	require.Equal(t, 1, settings.calls, "the gateway middleware must not add a settings database read to every request")
}

func TestAffiliatePromotionClaimCookieDoesNotExtendOriginalExpiry(t *testing.T) {
	router, p, _, _ := newPromotionTestRouter(t)
	now := time.Now().UTC()
	attr := &service.SignupAffiliateAttribution{InviterID: 42, SourceHost: "partner.example.test", HostRevision: 1, CapturedAt: now.Add(-time.Hour), ExpiresAt: now.Add(signupAffiliateTTL - time.Hour)}
	ticket, err := p.storeAttribution(context.Background(), affiliateTicketPrefix, attr, affiliateTicketTTL)
	require.NoError(t, err)
	response := promotionRequest(router, http.MethodGet, "https://example.test/api/v1/auth/affiliate/claim?ticket="+ticket)
	require.Equal(t, http.StatusSeeOther, response.Code)
	cookies := response.Result().Cookies()
	require.Len(t, cookies, 1)
	require.InDelta(t, int((signupAffiliateTTL-time.Hour)/time.Second), cookies[0].MaxAge, 1)
}

func TestAffiliatePromotionTransientFailureCannotReplaceExistingSource(t *testing.T) {
	for _, failure := range []string{"cookie_read", "authority_read"} {
		t.Run(failure, func(t *testing.T) {
			router, p, backend, store := newPromotionTestRouter(t)
			old := promotionRequest(router, http.MethodGet, promotionTicket(t, router)).Result().Cookies()[0]
			now := time.Now().UTC()
			replacement := &service.SignupAffiliateAttribution{InviterID: 99, SourceHost: "other.example.test", HostRevision: 1, CapturedAt: now, ExpiresAt: now.Add(signupAffiliateTTL)}
			ticket, err := p.storeAttribution(context.Background(), affiliateTicketPrefix, replacement, affiliateTicketTTL)
			require.NoError(t, err)
			backend.validate = func(attr *service.SignupAffiliateAttribution) error {
				if attr != nil && attr.InviterID == 99 {
					return nil
				}
				if failure == "authority_read" {
					return errors.New("temporary database failure")
				}
				if attr == nil || attr.InviterID == 0 {
					return service.ErrAffiliateAttributionInvalid
				}
				return nil
			}
			if failure == "cookie_read" {
				store.failBrowserRead.Store(true)
			}
			res := promotionRequest(router, http.MethodGet, "https://example.test/api/v1/auth/affiliate/claim?ticket="+ticket, old)
			require.Equal(t, http.StatusServiceUnavailable, res.Code, res.Body.String())
			require.Empty(t, res.Result().Cookies(), "a transient read failure must never replace the first source")
			preserved, err := p.loadAttribution(context.Background(), affiliateCookiePrefix, old.Value, false)
			require.NoError(t, err)
			require.Equal(t, int64(42), preserved.InviterID)
		})
	}
}
func TestAffiliatePromotionHostVariantsRemainIsolated(t *testing.T) {
	for _, authority := range []string{"partner.example.test.", "partner.example.test:8443"} {
		t.Run(authority, func(t *testing.T) {
			router, _, _, _ := newPromotionTestRouter(t)
			router.GET("/api/v1/settings/public", func(c *gin.Context) { c.Status(http.StatusOK) })
			res := promotionRequest(router, http.MethodGet, "https://"+authority+"/api/v1/settings/public")
			require.NotEqual(t, http.StatusOK, res.Code, "a configured promotion host cannot become a full API origin through a hostname spelling change")
		})
	}
	t.Run("unicode", func(t *testing.T) {
		router, _, backend, _ := newPromotionTestRouter(t)
		backend.domain.Hostname = "k.example.test"
		res := promotionRequest(router, http.MethodGet, "https://\u212a.example.test/")
		require.Equal(t, http.StatusBadRequest, res.Code)
		require.Empty(t, res.Header().Get("Location"))
	})
}
func TestAffiliatePromotionInvalidCanonicalOriginDoesNotUnlockKnownHost(t *testing.T) {
	router, p, _, _ := newPromotionTestRouter(t)
	p.settings = promotionOriginStub("")
	router.GET("/api/v1/settings/public", func(c *gin.Context) { c.Status(http.StatusOK) })
	res := promotionRequest(router, http.MethodGet, "https://partner.example.test/api/v1/settings/public")
	require.Equal(t, http.StatusNotFound, res.Code)
}

func TestAffiliatePromotionRejectsRawNonASCIIWhitespace(t *testing.T) {
	for _, authority := range []string{"\u00a0partner.example.test", "partner.example.test\u00a0"} {
		t.Run(authority, func(t *testing.T) {
			router, _, _, _ := newPromotionTestRouter(t)
			request := httptest.NewRequest(http.MethodGet, "https://partner.example.test/", nil)
			request.Host = authority
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, request)
			require.Equal(t, http.StatusBadRequest, rec.Code)
			require.Empty(t, rec.Header().Get("Location"))
		})
	}
}
