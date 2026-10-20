package service

import (
	"context"
	"net"
	"net/url"
	"strings"
	"time"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
)

var (
	ErrAffiliateDomainInvalid      = infraerrors.BadRequest("AFFILIATE_DOMAIN_INVALID", "affiliate domain must be a strict subdomain of the HTTPS frontend origin")
	ErrAffiliateDomainTaken        = infraerrors.Conflict("AFFILIATE_DOMAIN_TAKEN", "affiliate domain is already assigned to another user")
	ErrAffiliateFrontendInvalid    = infraerrors.BadRequest("AFFILIATE_FRONTEND_INVALID", "frontend URL must be an HTTPS origin without credentials, port, path, query or fragment")
	ErrAffiliateAttributionInvalid = infraerrors.BadRequest("AFFILIATE_ATTRIBUTION_INVALID", "affiliate attribution is expired, revoked or invalid; open a valid promotion link again")
	ErrAffiliateDisabled           = infraerrors.Forbidden("AFFILIATE_DISABLED", "affiliate promotion is disabled")
)

// AffiliatePromotionDomain retains disabled hosts and deleted owners so an old
// promotion hostname cannot silently become an ordinary registration entry.
type AffiliatePromotionDomain struct {
	Hostname  string `json:"hostname"`
	InviterID int64  `json:"inviter_id"`
	Enabled   bool   `json:"enabled"`
	Revision  int64  `json:"revision"`
}

// AffiliateDomainRepository is optional for older AffiliateRepository test
// doubles. Production affiliateRepository implements these authority checks.
type AffiliateDomainRepository interface {
	ResolveAffiliateDomain(context.Context, string) (*AffiliatePromotionDomain, error)
	GetUserAffiliateDomain(context.Context, int64) (*AffiliatePromotionDomain, error)
	SetUserAffiliateDomain(context.Context, int64, string) (*AffiliatePromotionDomain, error)
	ValidateSignupAffiliateAttribution(context.Context, *SignupAffiliateAttribution) error
	BindSignupAffiliateAttribution(context.Context, int64, *SignupAffiliateAttribution) error
}

func NormalizeAffiliateHostname(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	for i := 0; i < len(raw); i++ {
		// Reject Unicode before case folding: Kelvin-sign and dotted-I can
		// otherwise turn into valid ASCII hostname characters.
		if raw[i] > 127 {
			return "", ErrAffiliateDomainInvalid
		}
	}
	host := strings.ToLower(raw)
	if len(host) == 0 || len(host) > 253 || net.ParseIP(host) != nil || !strings.Contains(host, ".") {
		return "", ErrAffiliateDomainInvalid
	}
	for _, label := range strings.Split(host, ".") {
		if len(label) == 0 || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return "", ErrAffiliateDomainInvalid
		}
		for i := 0; i < len(label); i++ {
			c := label[i]
			if (c < 'a' || c > 'z') && (c < '0' || c > '9') && c != '-' {
				return "", ErrAffiliateDomainInvalid
			}
		}
	}
	return host, nil
}

// ParseAffiliateFrontendOrigin deliberately accepts no alternate ports:
// cookies are scoped by hostname, and promotion origins always use HTTPS/443.
func ParseAffiliateFrontendOrigin(raw string) (*url.URL, error) {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || parsed.Scheme != "https" || parsed.Opaque != "" || parsed.User != nil ||
		parsed.Port() != "" || strings.Contains(parsed.Host, ":") ||
		(parsed.Path != "" && parsed.Path != "/") || parsed.RawPath != "" ||
		parsed.RawQuery != "" || parsed.ForceQuery || parsed.Fragment != "" || strings.Contains(raw, "#") {
		return nil, ErrAffiliateFrontendInvalid
	}
	host, err := NormalizeAffiliateHostname(parsed.Host)
	if err != nil {
		return nil, ErrAffiliateFrontendInvalid
	}
	return &url.URL{Scheme: "https", Host: host}, nil
}

func (s *AffiliateService) domainRepository() (AffiliateDomainRepository, error) {
	if s != nil {
		if repo, ok := s.repo.(AffiliateDomainRepository); ok {
			return repo, nil
		}
	}
	return nil, infraerrors.ServiceUnavailable("AFFILIATE_DOMAIN_UNAVAILABLE", "affiliate domain service unavailable")
}

func (s *AffiliateService) ResolveAffiliateDomain(ctx context.Context, hostname string) (*AffiliatePromotionDomain, error) {
	host, err := NormalizeAffiliateHostname(hostname)
	if err != nil {
		return nil, err
	}
	repo, err := s.domainRepository()
	if err != nil {
		return nil, err
	}
	return repo.ResolveAffiliateDomain(ctx, host)
}

func (s *AffiliateService) SetUserAffiliateDomain(ctx context.Context, userID int64, domain string) (*AffiliatePromotionDomain, error) {
	if userID <= 0 {
		return nil, ErrUserNotFound
	}
	repo, err := s.domainRepository()
	if err != nil {
		return nil, err
	}
	domain = strings.TrimSpace(domain)
	if domain == "" {
		return repo.SetUserAffiliateDomain(ctx, userID, "")
	}
	host, err := NormalizeAffiliateHostname(domain)
	if err != nil {
		return nil, err
	}
	if s.settingService == nil {
		return nil, ErrAffiliateFrontendInvalid
	}
	origin, err := ParseAffiliateFrontendOrigin(s.settingService.GetFrontendURL(ctx))
	if err != nil {
		return nil, err
	}
	if !strings.HasSuffix(host, "."+origin.Hostname()) {
		return nil, ErrAffiliateDomainInvalid
	}
	return repo.SetUserAffiliateDomain(ctx, userID, host)
}

// ValidateSignupAffiliateAttributionValue checks the immutable server snapshot
// both before storage reads and after locks have been acquired by the repository.
func ValidateSignupAffiliateAttributionValue(attr *SignupAffiliateAttribution, now time.Time) error {
	if attr == nil || attr.InviterID <= 0 || attr.HostRevision <= 0 || attr.CapturedAt.IsZero() ||
		attr.CapturedAt.After(now) || !attr.ExpiresAt.After(now) || !attr.ExpiresAt.After(attr.CapturedAt) ||
		attr.ExpiresAt.Sub(attr.CapturedAt) > 30*24*time.Hour {
		return ErrAffiliateAttributionInvalid
	}
	host, err := NormalizeAffiliateHostname(attr.SourceHost)
	if err != nil || host != attr.SourceHost {
		return ErrAffiliateAttributionInvalid
	}
	return nil
}

func (s *AffiliateService) ValidateSignupAffiliateAttribution(ctx context.Context, attr *SignupAffiliateAttribution) error {
	if err := ValidateSignupAffiliateAttributionValue(attr, time.Now()); err != nil {
		return err
	}
	if !s.IsEnabled(ctx) {
		return ErrAffiliateDisabled
	}
	repo, err := s.domainRepository()
	if err != nil {
		return err
	}
	return repo.ValidateSignupAffiliateAttribution(ctx, attr)
}

func (s *AffiliateService) BindSignupAffiliateAttribution(ctx context.Context, userID int64, attr *SignupAffiliateAttribution) error {
	if err := ValidateSignupAffiliateAttributionValue(attr, time.Now()); err != nil {
		return err
	}
	if userID <= 0 || userID == attr.InviterID {
		return ErrAffiliateAttributionInvalid
	}
	if !s.IsEnabled(ctx) {
		return ErrAffiliateDisabled
	}
	repo, err := s.domainRepository()
	if err != nil {
		return err
	}
	return repo.BindSignupAffiliateAttribution(ctx, userID, attr)
}

func affiliateDomainURL(domain *AffiliatePromotionDomain) (string, string) {
	if domain == nil || !domain.Enabled || domain.InviterID <= 0 {
		return "", ""
	}
	return domain.Hostname, "https://" + domain.Hostname + "/"
}
