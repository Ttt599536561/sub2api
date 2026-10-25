//go:build unit

package service

import (
	"context"
	"errors"
	"net/mail"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestSendQQImageEmailCode_ExistingNonWhitelistUserReachesEmailService(t *testing.T) {
	user := &User{ID: 42, Email: "owner@custom.example", Status: StatusActive}
	authService := newOAuthEmailFlowAuthService(
		&userRepoStub{user: user, domainCounts: map[string]int{"custom.example": 1}},
		nil, nil,
		map[string]string{
			SettingKeyRegistrationEnabled:                 "false",
			SettingKeyRegistrationEmailSuffixWhitelist:    `["@example.com"]`,
			SettingKeyRegistrationEmailDomainQuotaEnabled: "true",
		},
		&emailCacheStub{}, nil,
	)

	// SMTP is intentionally absent: reaching this error proves the existing
	// account can request verification without attempting a real delivery.
	result, err := authService.SendQQImageEmailCode(context.Background(), user.ID)
	require.Nil(t, result)
	require.ErrorIs(t, err, ErrEmailNotConfigured)
}

func TestSendQQImageEmailCode_ExistingNonWhitelistUserIgnoresRegistrationPolicy(t *testing.T) {
	user := &User{ID: 42, Email: "owner@custom.example", Status: StatusActive}
	authService := newOAuthEmailFlowAuthService(
		&userRepoStub{user: user}, nil, nil,
		map[string]string{
			SettingKeyRegistrationEnabled:              "false",
			SettingKeyRegistrationEmailSuffixWhitelist: `["@example.com"]`,
		},
		&emailCacheStub{}, nil,
	)

	result, err := authService.SendQQImageEmailCode(context.Background(), user.ID)
	require.Nil(t, result)
	require.ErrorIs(t, err, ErrEmailNotConfigured)
}

func TestSendQQImageEmailCode_MissingSMTPDoesNotCreateCooldown(t *testing.T) {
	for _, test := range []struct {
		name string
		data *VerificationCodeData
	}{
		{name: "no previous code"},
		{name: "existing cooldown", data: &VerificationCodeData{
			Code: "123456", Attempts: 2, CreatedAt: time.Now(), ExpiresAt: time.Now().Add(verifyCodeTTL),
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			cache := &qqImageEmailCache{emailCacheStub: &emailCacheStub{data: test.data}}
			authService := newOAuthEmailFlowAuthService(
				&userRepoStub{user: &User{ID: 42, Email: "owner@example.com", Status: StatusActive}},
				nil, nil, nil, cache, nil,
			)

			for attempt := 0; attempt < 2; attempt++ {
				result, err := authService.SendQQImageEmailCode(context.Background(), 42)
				require.Nil(t, result)
				require.ErrorIs(t, err, ErrEmailNotConfigured)
				require.Empty(t, cache.savedEmail, "configuration failures must not create a verification code")
				require.Equal(t, test.data, cache.data, "configuration failures must preserve any existing code")
			}
			require.Empty(t, cache.readEmails, "SMTP availability must be checked before verification cooldown")
		})
	}
}

func TestSendQQImageEmailCode_RejectsInvalidDomainBeforeSMTP(t *testing.T) {
	for _, email := range []string{
		"qq-test-admin@local.invalid",
		"owner@invalid",
		"owner@nested.local.invalid",
		"  OWNER@LOCAL.INVALID  ",
	} {
		t.Run(email, func(t *testing.T) {
			cache := &qqImageEmailCache{emailCacheStub: &emailCacheStub{}}
			authService := newOAuthEmailFlowAuthService(
				&userRepoStub{user: &User{ID: 42, Email: email, Status: StatusActive}},
				nil, nil, nil, cache, nil,
			)

			result, err := authService.SendQQImageEmailCode(context.Background(), 42)
			require.Nil(t, result)
			require.ErrorIs(t, err, ErrEmailReserved, "placeholder addresses must be rejected before missing SMTP")
			require.Empty(t, cache.readEmails)
			require.Empty(t, cache.savedEmail)
		})
	}
}

func TestSendQQImageEmailCode_DoesNotRejectDomainsContainingInvalid(t *testing.T) {
	for _, email := range []string{"owner@invalid.example.com", "owner@notinvalid.com"} {
		t.Run(email, func(t *testing.T) {
			authService := newOAuthEmailFlowAuthService(
				&userRepoStub{user: &User{ID: 42, Email: email, Status: StatusActive}},
				nil, nil, nil, &emailCacheStub{}, nil,
			)

			result, err := authService.SendQQImageEmailCode(context.Background(), 42)
			require.Nil(t, result)
			require.ErrorIs(t, err, ErrEmailNotConfigured)
		})
	}
}

type qqImageEmailCache struct {
	*emailCacheStub
	readEmails []string
	savedEmail string
}

func (s *qqImageEmailCache) GetVerificationCode(ctx context.Context, email string) (*VerificationCodeData, error) {
	s.readEmails = append(s.readEmails, email)
	return s.emailCacheStub.GetVerificationCode(ctx, email)
}

func (s *qqImageEmailCache) SetVerificationCode(_ context.Context, email string, data *VerificationCodeData, _ time.Duration) error {
	s.savedEmail = email
	s.data = data
	return nil
}

func (s *qqImageEmailCache) DeleteVerificationCode(context.Context, string) error {
	s.data = nil
	return nil
}

func TestSendQQImageEmailCode_UsesCurrentUserAndSiteName(t *testing.T) {
	// The fixture only accepts SMTP on loopback and stores messages in memory.
	smtpServer := startNotificationEmailTestSMTPServer(t)
	settings := smtpServer.settings()
	settings[SettingKeySiteName] = "QQ Test Site"
	settings[SettingKeyRegistrationEnabled] = "false"
	settings[SettingKeyRegistrationEmailSuffixWhitelist] = `["@example.com"]`
	settings[SettingKeyRegistrationEmailDomainQuotaEnabled] = "true"
	cache := &qqImageEmailCache{emailCacheStub: &emailCacheStub{}}
	authService := newOAuthEmailFlowAuthService(
		&userRepoStub{domainCounts: map[string]int{"custom.example": 1}, usersByID: map[int64]*User{
			42: {ID: 42, Email: "  Owner@CUSTOM.EXAMPLE  ", Status: StatusActive},
			43: {ID: 43, Email: "other@example.com", Status: StatusActive},
		}},
		nil, nil, settings, cache, nil,
	)

	result, err := authService.SendQQImageEmailCode(context.Background(), 42, "en")
	require.NoError(t, err)
	require.Equal(t, int(verifyCodeCooldown/time.Second), result.Countdown)
	require.Equal(t, "owner@custom.example", cache.savedEmail)
	require.Equal(t, int64(1), smtpServer.messageCount())
	message, err := mail.ReadMessage(strings.NewReader(smtpServer.lastMessage()))
	require.NoError(t, err)
	require.Contains(t, message.Header.Get("To"), "owner@custom.example")
	require.Contains(t, smtpServer.lastMessageBody(t), "QQ Test Site")

	code := cache.data.Code
	require.NoError(t, authService.VerifyOAuthEmailCode(context.Background(), "owner@custom.example", code))
	require.ErrorIs(t, authService.VerifyOAuthEmailCode(context.Background(), "owner@custom.example", code), ErrInvalidVerifyCode)
}

func TestSendQQImageEmailCode_RejectsUnavailableUserOrEmail(t *testing.T) {
	repoErr := errors.New("user lookup failed")
	for _, test := range []struct {
		name string
		repo *userRepoStub
		want error
	}{
		{"missing user", &userRepoStub{}, ErrUserNotFound},
		{"lookup failed", &userRepoStub{getErr: repoErr}, repoErr},
		{"inactive user", &userRepoStub{user: &User{ID: 42, Email: "owner@example.com", Status: StatusDisabled}}, ErrUserNotActive},
		{"missing email", &userRepoStub{user: &User{ID: 42, Status: StatusActive}}, ErrEmailVerifyRequired},
		{"malformed email", &userRepoStub{user: &User{ID: 42, Email: "invalid-address", Status: StatusActive}}, ErrEmailVerifyRequired},
		{"display name", &userRepoStub{user: &User{ID: 42, Email: "Owner <owner@example.com>", Status: StatusActive}}, ErrEmailVerifyRequired},
		{"reserved email", &userRepoStub{user: &User{ID: 42, Email: "owner" + LinuxDoConnectSyntheticEmailDomain, Status: StatusActive}}, ErrEmailReserved},
	} {
		t.Run(test.name, func(t *testing.T) {
			cache := &qqImageEmailCache{emailCacheStub: &emailCacheStub{}}
			authService := newOAuthEmailFlowAuthService(test.repo, nil, nil, nil, cache, nil)

			result, err := authService.SendQQImageEmailCode(context.Background(), 42)
			require.Nil(t, result)
			require.ErrorIs(t, err, test.want)
			require.Empty(t, cache.readEmails)
			require.Empty(t, cache.savedEmail)
		})
	}
}

func TestSendQQImageEmailCode_MissingDependencies(t *testing.T) {
	for _, authService := range []*AuthService{nil, {}, {userRepo: &userRepoStub{}}, {emailService: &EmailService{}}} {
		result, err := authService.SendQQImageEmailCode(context.Background(), 42)
		require.Nil(t, result)
		require.ErrorIs(t, err, ErrServiceUnavailable)
	}
}

func TestSendQQImageEmailCode_PreservesVerificationCooldownAndAttempts(t *testing.T) {
	cache := &qqImageEmailCache{emailCacheStub: &emailCacheStub{data: &VerificationCodeData{
		Code: "123456", CreatedAt: time.Now(), ExpiresAt: time.Now().Add(verifyCodeTTL),
	}}}
	authService := newOAuthEmailFlowAuthService(
		&userRepoStub{user: &User{ID: 42, Email: "owner@example.com", Status: StatusActive}},
		nil, nil, map[string]string{SettingKeySMTPHost: "smtp.example.test"}, cache, nil,
	)

	result, err := authService.SendQQImageEmailCode(context.Background(), 42)
	require.Nil(t, result)
	require.ErrorIs(t, err, ErrVerifyCodeTooFrequent)
	require.Empty(t, cache.savedEmail)
	for attempt := 1; attempt < maxVerifyCodeAttempts; attempt++ {
		require.ErrorIs(t, authService.VerifyOAuthEmailCode(context.Background(), "owner@example.com", "000000"), ErrInvalidVerifyCode)
	}
	require.ErrorIs(t, authService.VerifyOAuthEmailCode(context.Background(), "owner@example.com", "000000"), ErrVerifyCodeMaxAttempts)
	require.ErrorIs(t, authService.VerifyOAuthEmailCode(context.Background(), "owner@example.com", "123456"), ErrVerifyCodeMaxAttempts)
}
