//go:build unit

package service

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestTrustedSignupRejectsUnavailableAttributionBeforeCreatingUser(t *testing.T) {
	for _, mode := range []string{"email", "legacy_oauth", "oauth_pair", "verified_email", "email_auto"} {
		t.Run(mode, func(t *testing.T) {
			repo := &userRepoStub{nextID: 42}
			svc := newAuthService(repo, map[string]string{SettingKeyRegistrationEnabled: "true"}, nil, nil)
			svc.refreshTokenCache = &refreshTokenCacheStub{}
			ctx := WithSignupAffiliateAttribution(context.Background(), &SignupAffiliateAttribution{})
			var err error
			switch mode {
			case "email":
				_, _, err = svc.RegisterWithVerification(ctx, "signup@example.com", "password", "", "", "", "OTHER")
			case "legacy_oauth":
				_, _, err = svc.LoginOrRegisterOAuth(ctx, "signup@example.com", "signup")
			case "oauth_pair":
				_, _, err = svc.LoginOrRegisterOAuthWithTokenPair(ctx, "signup@example.com", "signup", "", "OTHER", "linuxdo")
			case "verified_email":
				_, _, err = svc.RegisterVerifiedOAuthEmailAccount(ctx, "signup@example.com", "password", "", "github")
			case "email_auto":
				_, err = svc.createEmailOAuthUser(ctx, "signup@example.com", "signup", "github", "", "OTHER")
			}
			require.Error(t, err, "a protected source must never silently become an unbound signup")
			require.Empty(t, repo.created, "validate protected attribution before creating any user")
		})
	}
}

func TestInvalidSignupAttributionDoesNotBlockExistingOAuthLogin(t *testing.T) {
	repo := &userRepoStub{user: &User{ID: 42, Email: "existing@example.com", Username: "existing", Status: StatusActive, Role: RoleUser}}
	svc := newAuthService(repo, map[string]string{SettingKeyRegistrationEnabled: "true"}, nil, nil)
	svc.refreshTokenCache = &refreshTokenCacheStub{}
	ctx := WithSignupAffiliateAttribution(context.Background(), &SignupAffiliateAttribution{})
	token, user, err := svc.LoginOrRegisterOAuth(ctx, repo.user.Email, "existing")
	require.NoError(t, err)
	require.NotEmpty(t, token)
	require.Equal(t, int64(42), user.ID)
	tokenPair, user, err := svc.LoginOrRegisterOAuthWithTokenPair(ctx, repo.user.Email, "existing", "", "OTHER", "linuxdo")
	require.NoError(t, err)
	require.NotNil(t, tokenPair)
	require.Equal(t, int64(42), user.ID)
	require.Empty(t, repo.created)
}
