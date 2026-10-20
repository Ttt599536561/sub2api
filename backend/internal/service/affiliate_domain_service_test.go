//go:build unit

package service

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

type affiliateDomainRepoStub struct {
	AffiliateRepository
	domain       *AffiliatePromotionDomain
	err          error
	setUserID    int64
	setHost      string
	validateCall int
	bindUserID   int64
}

func (r *affiliateDomainRepoStub) EnsureUserAffiliate(context.Context, int64) (*AffiliateSummary, error) {
	return &AffiliateSummary{UserID: 7, AffCode: "CODE1234"}, nil
}
func (r *affiliateDomainRepoStub) ThawFrozenQuota(context.Context, int64) (float64, error) {
	return 0, nil
}
func (r *affiliateDomainRepoStub) ListInvitees(context.Context, int64, int) ([]AffiliateInvitee, error) {
	return nil, nil
}
func (r *affiliateDomainRepoStub) ResolveAffiliateDomain(context.Context, string) (*AffiliatePromotionDomain, error) {
	return r.domain, r.err
}
func (r *affiliateDomainRepoStub) GetUserAffiliateDomain(context.Context, int64) (*AffiliatePromotionDomain, error) {
	return r.domain, r.err
}
func (r *affiliateDomainRepoStub) SetUserAffiliateDomain(_ context.Context, userID int64, host string) (*AffiliatePromotionDomain, error) {
	r.setUserID, r.setHost = userID, host
	return r.domain, r.err
}
func (r *affiliateDomainRepoStub) ValidateSignupAffiliateAttribution(context.Context, *SignupAffiliateAttribution) error {
	r.validateCall++
	return r.err
}
func (r *affiliateDomainRepoStub) BindSignupAffiliateAttribution(_ context.Context, userID int64, _ *SignupAffiliateAttribution) error {
	r.bindUserID = userID
	return r.err
}

func domainTestService(repo *affiliateDomainRepoStub, origin string, enabled bool) *AffiliateService {
	values := map[string]string{SettingKeyFrontendURL: origin, SettingKeyAffiliateEnabled: "false"}
	if enabled {
		values[SettingKeyAffiliateEnabled] = "true"
	}
	settings := NewSettingService(&settingRepoStub{values: values}, &config.Config{})
	return NewAffiliateService(repo, settings, nil, nil)
}

func TestAffiliateDomain_NormalizeHostname(t *testing.T) {
	for _, raw := range []string{"Partner.Api.Example.com", " partner.api.example.com "} {
		host, err := NormalizeAffiliateHostname(raw)
		require.NoError(t, err)
		require.Equal(t, "partner.api.example.com", host)
	}
	for _, raw := range []string{"", "localhost", "127.0.0.1", "[::1]", "*.api.example.com", "foo:443",
		"https://partner.api.example.com", "a.api.example.com/", "a..example.com", "-a.example.com",
		"a-.example.com", "a_b.example.com", "é.example.com", "K.example.com", "İ.example.com",
		"a.example.com.", strings.Repeat("a", 64) + ".example.com"} {
		t.Run(raw, func(t *testing.T) {
			_, err := NormalizeAffiliateHostname(raw)
			require.Error(t, err)
		})
	}
}

func TestAffiliateDomain_FrontendOrigin(t *testing.T) {
	for _, raw := range []string{"https://api.example.com", " https://API.example.com/ "} {
		origin, err := ParseAffiliateFrontendOrigin(raw)
		require.NoError(t, err)
		require.Equal(t, "https://api.example.com", origin.String())
	}
	for _, raw := range []string{"", "http://api.example.com", "https://api.example.com/path",
		"https://api.example.com/?", "https://api.example.com/?a=b", "https://api.example.com/#x",
		"https://u:p@api.example.com", "https://api.example.com:443", "https://127.0.0.1",
		"https://api.example.com.", "https://api.example.com\\evil"} {
		t.Run(raw, func(t *testing.T) {
			_, err := ParseAffiliateFrontendOrigin(raw)
			require.Error(t, err)
		})
	}
}

func TestAffiliateDomain_SetRequiresStrictSubdomain(t *testing.T) {
	repo := &affiliateDomainRepoStub{}
	svc := domainTestService(repo, "https://api.example.com", true)
	_, err := svc.SetUserAffiliateDomain(context.Background(), 7, " Partner.Api.Example.com ")
	require.NoError(t, err)
	require.Equal(t, int64(7), repo.setUserID)
	require.Equal(t, "partner.api.example.com", repo.setHost)
	for _, host := range []string{"api.example.com", "evilapi.example.com", "api.example.com.evil.test", "example.com"} {
		_, err = svc.SetUserAffiliateDomain(context.Background(), 7, host)
		require.Error(t, err)
	}
	_, err = svc.SetUserAffiliateDomain(context.Background(), 0, "partner.api.example.com")
	require.Error(t, err)
}

func TestAffiliateDomain_ClearWorksWithBrokenFrontendAndDisabledFeature(t *testing.T) {
	repo := &affiliateDomainRepoStub{}
	svc := domainTestService(repo, "not a url", false)
	_, err := svc.SetUserAffiliateDomain(context.Background(), 7, "")
	require.NoError(t, err)
	require.Equal(t, int64(7), repo.setUserID)
	require.Empty(t, repo.setHost)
}

func TestAffiliateDomain_ResolvePreservesTombstonesAndErrors(t *testing.T) {
	repo := &affiliateDomainRepoStub{domain: &AffiliatePromotionDomain{Hostname: "p.api.example.com", Revision: 5}}
	svc := domainTestService(repo, "https://api.example.com", true)
	domain, err := svc.ResolveAffiliateDomain(context.Background(), "P.api.example.com")
	require.NoError(t, err)
	require.Same(t, repo.domain, domain)
	repo.err = errors.New("database unavailable")
	_, err = svc.ResolveAffiliateDomain(context.Background(), "p.api.example.com")
	require.ErrorIs(t, err, repo.err)
}

func TestAffiliateDomain_UserDetailUsesActiveDomainAndPropagatesReadFailure(t *testing.T) {
	repo := &affiliateDomainRepoStub{domain: &AffiliatePromotionDomain{
		Hostname: "partner.api.example.com", InviterID: 7, Enabled: true, Revision: 1,
	}}
	svc := domainTestService(repo, "https://api.example.com", true)
	detail, err := svc.GetAffiliateDetail(context.Background(), 7)
	require.NoError(t, err)
	require.Equal(t, "partner.api.example.com", detail.AffiliateDomain)
	require.Equal(t, "https://partner.api.example.com/", detail.ReferralURL)
	repo.domain.Enabled = false
	detail, err = svc.GetAffiliateDetail(context.Background(), 7)
	require.NoError(t, err)
	require.Empty(t, detail.AffiliateDomain)
	require.Empty(t, detail.ReferralURL)
	repo.err = errors.New("domain read failed")
	_, err = svc.GetAffiliateDetail(context.Background(), 7)
	require.ErrorIs(t, err, repo.err)
}

func TestAffiliateDomain_TrustedAttributionValidation(t *testing.T) {
	now := time.Now().UTC()
	valid := SignupAffiliateAttribution{InviterID: 7, SourceHost: "p.api.example.com", HostRevision: 2, CapturedAt: now.Add(-time.Minute), ExpiresAt: now.Add(time.Hour)}
	for _, tc := range []struct {
		name string
		edit func(*SignupAffiliateAttribution)
	}{
		{"missing inviter", func(a *SignupAffiliateAttribution) { a.InviterID = 0 }},
		{"invalid host", func(a *SignupAffiliateAttribution) { a.SourceHost = "https://p.api.example.com" }},
		{"noncanonical host", func(a *SignupAffiliateAttribution) { a.SourceHost = "P.api.example.com" }},
		{"missing revision", func(a *SignupAffiliateAttribution) { a.HostRevision = 0 }},
		{"expired", func(a *SignupAffiliateAttribution) { a.ExpiresAt = now.Add(-time.Second) }},
		{"missing capture", func(a *SignupAffiliateAttribution) { a.CapturedAt = time.Time{} }},
		{"future capture", func(a *SignupAffiliateAttribution) { a.CapturedAt = now.Add(time.Hour) }},
		{"invalid interval", func(a *SignupAffiliateAttribution) { a.ExpiresAt = a.CapturedAt.Add(-time.Second) }},
		{"lifetime exceeds thirty days", func(a *SignupAffiliateAttribution) { a.ExpiresAt = a.CapturedAt.Add(31 * 24 * time.Hour) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			repo := &affiliateDomainRepoStub{}
			svc := domainTestService(repo, "https://api.example.com", true)
			attr := valid
			tc.edit(&attr)
			require.Error(t, svc.ValidateSignupAffiliateAttribution(context.Background(), &attr))
			require.Zero(t, repo.validateCall)
		})
	}
	repo := &affiliateDomainRepoStub{}
	svc := domainTestService(repo, "https://api.example.com", true)
	require.NoError(t, svc.ValidateSignupAffiliateAttribution(context.Background(), &valid))
	require.Equal(t, 1, repo.validateCall)
	require.NoError(t, svc.BindSignupAffiliateAttribution(context.Background(), 9, &valid))
	require.Equal(t, int64(9), repo.bindUserID)
	require.Error(t, svc.BindSignupAffiliateAttribution(context.Background(), 7, &valid))
	repo.err = errors.New("failed authority check")
	require.ErrorIs(t, svc.ValidateSignupAffiliateAttribution(context.Background(), &valid), repo.err)
	svc = domainTestService(repo, "https://api.example.com", false)
	require.Error(t, svc.ValidateSignupAffiliateAttribution(context.Background(), &valid))
}
