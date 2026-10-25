package service

import (
	"context"
	"net/mail"
	"strings"
	"time"
)

// SendQQImageEmailCode verifies an existing account's current email. Registration
// domain admission and quotas do not apply to this ownership check.
func (s *AuthService) SendQQImageEmailCode(ctx context.Context, userID int64, locale ...string) (*SendVerifyCodeResult, error) {
	if s == nil || s.userRepo == nil || s.emailService == nil {
		return nil, ErrServiceUnavailable
	}
	user, err := s.userRepo.GetByID(ctx, userID)
	if err != nil {
		return nil, err
	}
	if user == nil {
		return nil, ErrUserNotFound
	}
	if !user.IsActive() {
		return nil, ErrUserNotActive
	}

	email := strings.ToLower(strings.TrimSpace(user.Email))
	address, err := mail.ParseAddress(email)
	if err != nil || len(email) > 255 || address.Address != email {
		return nil, ErrEmailVerifyRequired
	}
	domain := email[strings.LastIndexByte(email, '@')+1:]
	if isReservedEmail(email) || domain == "invalid" || strings.HasSuffix(domain, ".invalid") {
		return nil, ErrEmailReserved
	}
	// Check delivery configuration before SendVerifyCode stores a code and starts
	// its cooldown, including when a previous failed request left a cached code.
	if _, err := s.emailService.GetSMTPConfig(ctx); err != nil {
		return nil, err
	}

	siteName := "Sub2API"
	if s.settingService != nil {
		siteName = s.settingService.GetSiteName(ctx)
	}
	if err := s.emailService.SendVerifyCode(ctx, email, siteName, firstEmailLocale(locale)); err != nil {
		return nil, err
	}
	return &SendVerifyCodeResult{Countdown: int(verifyCodeCooldown / time.Second)}, nil
}
