package handler

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/ent/authidentity"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type trustedOAuthDomainRepo struct {
	service.AffiliateRepository
}

func (*trustedOAuthDomainRepo) ResolveAffiliateDomain(context.Context, string) (*service.AffiliatePromotionDomain, error) {
	return nil, nil
}
func (*trustedOAuthDomainRepo) GetUserAffiliateDomain(context.Context, int64) (*service.AffiliatePromotionDomain, error) {
	return nil, nil
}
func (*trustedOAuthDomainRepo) SetUserAffiliateDomain(context.Context, int64, string) (*service.AffiliatePromotionDomain, error) {
	return nil, nil
}
func (*trustedOAuthDomainRepo) ValidateSignupAffiliateAttribution(context.Context, *service.SignupAffiliateAttribution) error {
	return nil
}
func (*trustedOAuthDomainRepo) BindSignupAffiliateAttribution(ctx context.Context, userID int64, attr *service.SignupAffiliateAttribution) error {
	tx := dbent.TxFromContext(ctx)
	if tx == nil {
		return errors.New("binding escaped the signup transaction")
	}
	_, err := tx.Client().ExecContext(ctx, "INSERT INTO test_oauth_bindings (user_id, inviter_id) VALUES (?, ?)", userID, attr.InviterID)
	return err
}

func TestTrustedOAuthPendingCompletionIsAtomicAcrossProviders(t *testing.T) {
	for _, flow := range []string{"linuxdo", "oidc", "wechat", "dingtalk", "github", "google", "oidc_email"} {
		for _, failBeforeCommit := range []bool{false, true} {
			name := flow + "/success"
			if failBeforeCommit {
				name = flow + "/rollback"
			}
			t.Run(name, func(t *testing.T) {
				provider := flow
				if provider == "oidc_email" {
					provider = "oidc"
				}
				handler, client := newOAuthPendingFlowTestHandlerWithDependencies(t, oauthPendingFlowTestHandlerOptions{
					invitationEnabled:  true,
					emailVerifyEnabled: flow == "oidc_email",
					emailCache: &oauthPendingFlowEmailCacheStub{verificationCodes: map[string]*service.VerificationCodeData{
						"trusted@example.com": {Code: "246810", ExpiresAt: time.Now().Add(time.Hour)},
					}},
					settingValues: map[string]string{service.SettingKeyAffiliateEnabled: "true"},
					affiliateFactory: func(client *dbent.Client, settings *service.SettingService) *service.AffiliateService {
						_, err := client.ExecContext(context.Background(), "CREATE TABLE test_oauth_bindings (user_id INTEGER PRIMARY KEY, inviter_id INTEGER NOT NULL)")
						require.NoError(t, err)
						return service.NewAffiliateService(&trustedOAuthDomainRepo{}, settings, nil, nil)
					},
				})
				ctx := context.Background()
				invitation, err := client.RedeemCode.Create().SetCode("TRUSTED").SetType(service.RedeemTypeInvitation).
					SetStatus(service.StatusUnused).SetValue(0).Save(ctx)
				require.NoError(t, err)
				now := time.Now().UTC()
				attr := &service.SignupAffiliateAttribution{InviterID: 700, SourceHost: "partner.example.test", HostRevision: 1, CapturedAt: now, ExpiresAt: now.Add(time.Hour)}
				session, err := client.PendingAuthSession.Create().SetSessionToken("trusted-provider-session").
					SetIntent("login").SetProviderType(provider).SetProviderKey(provider).SetProviderSubject("subject").
					SetResolvedEmail("trusted@example.com").SetBrowserSessionKey("trusted-browser").
					SetUpstreamIdentityClaims(map[string]any{"username": "trusted", "email_verified": true}).
					SetLocalFlowState(map[string]any{
						"step": oauthPendingChoiceStep, "error": "invitation_required", "signup_affiliate_attribution": attr,
					}).
					SetExpiresAt(now.Add(10 * time.Minute)).Save(ctx)
				require.NoError(t, err)
				if failBeforeCommit {
					original := pendingOAuthCreateAccountPreCommitHook
					pendingOAuthCreateAccountPreCommitHook = func(context.Context, *dbent.PendingAuthSession) error {
						return errors.New("injected completion failure")
					}
					t.Cleanup(func() { pendingOAuthCreateAccountPreCommitHook = original })
				}
				recorder := httptest.NewRecorder()
				c, _ := gin.CreateTestContext(recorder)
				c.Request = httptest.NewRequest(http.MethodPost, "/api/v1/auth/oauth/"+provider+"/complete-registration",
					strings.NewReader(`{"email":"trusted@example.com","verify_code":"246810","password":"secret-123","invitation_code":"TRUSTED","aff_code":"ATTACKER"}`))
				otherSource := *attr
				otherSource.InviterID = 800
				c.Request = c.Request.WithContext(service.WithSignupAffiliateAttribution(ctx, &otherSource))
				c.Request.Header.Set("Content-Type", "application/json")
				c.Request.AddCookie(&http.Cookie{Name: oauthPendingSessionCookieName, Value: encodeCookieValue(session.SessionToken)})
				c.Request.AddCookie(&http.Cookie{Name: oauthPendingBrowserCookieName, Value: encodeCookieValue("trusted-browser")})
				switch flow {
				case "linuxdo":
					handler.CompleteLinuxDoOAuthRegistration(c)
				case "oidc":
					handler.CompleteOIDCOAuthRegistration(c)
				case "wechat":
					handler.CompleteWeChatOAuthRegistration(c)
				case "dingtalk":
					handler.CompleteDingTalkOAuthRegistration(c)
				case "github", "google":
					handler.completeEmailOAuthRegistration(c, provider)
				case "oidc_email":
					handler.CreateOIDCOAuthAccount(c)
				}
				users, err := client.User.Query().Count(ctx)
				require.NoError(t, err)
				identities, err := client.AuthIdentity.Query().Count(ctx)
				require.NoError(t, err)
				providerIdentities, err := client.AuthIdentity.Query().Where(authidentity.ProviderTypeEQ(provider)).Count(ctx)
				require.NoError(t, err)
				storedSession, err := client.PendingAuthSession.Get(ctx, session.ID)
				require.NoError(t, err)
				storedInvitation, err := client.RedeemCode.Get(ctx, invitation.ID)
				require.NoError(t, err)
				rows, err := client.QueryContext(ctx, "SELECT COUNT(*), COALESCE(MAX(inviter_id), 0) FROM test_oauth_bindings")
				require.NoError(t, err)
				defer func() { _ = rows.Close() }()
				require.True(t, rows.Next())
				var bindings, inviterID int
				require.NoError(t, rows.Scan(&bindings, &inviterID))
				if failBeforeCommit {
					require.GreaterOrEqual(t, recorder.Code, 400, recorder.Body.String())
					require.Zero(t, users)
					require.Zero(t, identities)
					require.Zero(t, bindings)
					require.Nil(t, storedSession.ConsumedAt)
					require.Equal(t, service.StatusUnused, storedInvitation.Status)
				} else {
					require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
					require.Equal(t, 1, users)
					require.Equal(t, 1, providerIdentities)
					require.Equal(t, 1, bindings)
					require.Equal(t, 700, inviterID)
					require.NotNil(t, storedSession.ConsumedAt)
					require.Equal(t, service.StatusUsed, storedInvitation.Status)
					require.Contains(t, recorder.Body.String(), "access_token")
				}
			})
		}
	}
}

func TestTrustedOAuthPendingCapturesProtectedSourceInLocalState(t *testing.T) {
	handler, client := newOAuthPendingFlowTestHandler(t, false)
	now := time.Now().UTC()
	attr := &service.SignupAffiliateAttribution{InviterID: 7, SourceHost: "partner.example.test", HostRevision: 3, CapturedAt: now, ExpiresAt: now.Add(time.Hour)}
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/v1/auth/oauth/oidc/callback", nil).
		WithContext(service.WithSignupAffiliateAttribution(context.Background(), attr))
	err := handler.createOAuthPendingSession(c, oauthPendingSessionPayload{
		Intent: "login", Identity: service.PendingAuthIdentityKey{ProviderType: "oidc", ProviderKey: "issuer", ProviderSubject: "subject"},
		BrowserSessionKey: "browser",
	})
	require.NoError(t, err)
	session, err := client.PendingAuthSession.Query().Only(context.Background())
	require.NoError(t, err)
	source, ok := session.LocalFlowState["signup_affiliate_attribution"].(map[string]any)
	require.True(t, ok, "server attribution belongs in local flow state")
	require.EqualValues(t, 7, source["inviter_id"])
	require.NotContains(t, session.UpstreamIdentityClaims, "signup_affiliate_attribution")
}

func TestTrustedOAuthPendingInvalidSnapshotCannotFallBackToRequestCode(t *testing.T) {
	handler, client := newOAuthPendingFlowTestHandlerWithEmailVerification(t, false, "protected@example.com", "246810")
	session, err := client.PendingAuthSession.Create().
		SetSessionToken("protected-pending").
		SetIntent("login").SetProviderType("oidc").SetProviderKey("issuer").SetProviderSubject("subject").
		SetBrowserSessionKey("protected-browser").
		SetLocalFlowState(map[string]any{"signup_affiliate_attribution": map[string]any{}}).
		SetExpiresAt(time.Now().UTC().Add(10 * time.Minute)).
		Save(context.Background())
	require.NoError(t, err)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/v1/auth/oauth/oidc/create-account", strings.NewReader(`{"email":"protected@example.com","verify_code":"246810","password":"secret-123","aff_code":"OTHER"}`))
	c.Request.Header.Set("Content-Type", "application/json")
	c.Request.AddCookie(&http.Cookie{Name: oauthPendingSessionCookieName, Value: encodeCookieValue(session.SessionToken)})
	c.Request.AddCookie(&http.Cookie{Name: oauthPendingBrowserCookieName, Value: encodeCookieValue("protected-browser")})
	handler.CreateOIDCOAuthAccount(c)
	require.GreaterOrEqual(t, recorder.Code, 400, recorder.Body.String())
	count, err := client.User.Query().Count(context.Background())
	require.NoError(t, err)
	require.Zero(t, count)
	stored, err := client.PendingAuthSession.Get(context.Background(), session.ID)
	require.NoError(t, err)
	require.Nil(t, stored.ConsumedAt)
}

func TestTrustedOAuthPendingPreservesAbsentSourceOverLaterCookie(t *testing.T) {
	handler, client := newOAuthPendingFlowTestHandler(t, false)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodGet, "/api/v1/auth/oauth/oidc/callback", nil).
		WithContext(service.WithSignupAffiliateAttribution(context.Background(), nil))
	require.NoError(t, handler.createOAuthPendingSession(c, oauthPendingSessionPayload{
		Intent: "login", Identity: service.PendingAuthIdentityKey{ProviderType: "google", ProviderKey: "google", ProviderSubject: "absent"},
		BrowserSessionKey: "browser", ResolvedEmail: "absent@example.com",
	}))
	session, err := client.PendingAuthSession.Query().Only(context.Background())
	require.NoError(t, err)
	_, recorded := session.LocalFlowState["signup_affiliate_attribution"]
	require.True(t, recorded, "absence captured at OAuth start must remain an explicit snapshot")
	laterSource := service.WithSignupAffiliateAttribution(context.Background(), &service.SignupAffiliateAttribution{InviterID: 99})
	_, protected := service.SignupAffiliateAttributionFromContext(pendingSignupAttributionContext(laterSource, session))
	require.False(t, protected, "a later promotional visit cannot change this already-started flow")
	recorder := httptest.NewRecorder()
	completeCtx, _ := gin.CreateTestContext(recorder)
	completeCtx.Request = httptest.NewRequest(http.MethodPost, "/api/v1/auth/oauth/google/complete-registration", strings.NewReader(`{"password":"secret-123"}`)).
		WithContext(laterSource)
	completeCtx.Request.Header.Set("Content-Type", "application/json")
	completeCtx.Request.AddCookie(&http.Cookie{Name: oauthPendingSessionCookieName, Value: encodeCookieValue(session.SessionToken)})
	completeCtx.Request.AddCookie(&http.Cookie{Name: oauthPendingBrowserCookieName, Value: encodeCookieValue("browser")})
	handler.completeEmailOAuthRegistration(completeCtx, "google")
	require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
}

func TestTrustedOAuthPendingTypedNilMeansExplicitAbsence(t *testing.T) {
	var absent *service.SignupAffiliateAttribution
	session := &dbent.PendingAuthSession{LocalFlowState: map[string]any{"signup_affiliate_attribution": absent}}
	laterSource := service.WithSignupAffiliateAttribution(context.Background(), &service.SignupAffiliateAttribution{InviterID: 99})
	_, protected := service.SignupAffiliateAttributionFromContext(pendingSignupAttributionContext(laterSource, session))
	require.False(t, protected)
}

func TestTrustedOAuthPendingLegacyMissingSnapshotUsesCookie(t *testing.T) {
	session := &dbent.PendingAuthSession{LocalFlowState: map[string]any{}}
	cookieCtx := service.WithSignupAffiliateAttribution(context.Background(), &service.SignupAffiliateAttribution{InviterID: 99})
	attr, protected := service.SignupAffiliateAttributionFromContext(pendingSignupAttributionContext(cookieCtx, session))
	require.True(t, protected)
	require.Equal(t, int64(99), attr.InviterID)
}
