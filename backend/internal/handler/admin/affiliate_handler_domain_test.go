//go:build unit

package admin

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type domainHandlerSettings struct{ service.SettingRepository }

func (domainHandlerSettings) GetValue(_ context.Context, key string) (string, error) {
	if key == service.SettingKeyFrontendURL {
		return "https://api.example.com", nil
	}
	return "", service.ErrSettingNotFound
}

type domainHandlerRepo struct {
	service.AffiliateRepository
	userID  int64
	host    string
	calls   int
	cleared []string
	err     error
}

func (r *domainHandlerRepo) ResolveAffiliateDomain(context.Context, string) (*service.AffiliatePromotionDomain, error) {
	return nil, nil
}
func (r *domainHandlerRepo) GetUserAffiliateDomain(context.Context, int64) (*service.AffiliatePromotionDomain, error) {
	return nil, nil
}
func (r *domainHandlerRepo) SetUserAffiliateDomain(_ context.Context, id int64, hostname string) (*service.AffiliatePromotionDomain, error) {
	r.calls++
	r.userID, r.host = id, hostname
	if r.err != nil {
		return nil, r.err
	}
	if hostname == "" {
		r.cleared = append(r.cleared, "domain")
		return nil, nil
	}
	return &service.AffiliatePromotionDomain{Hostname: hostname, InviterID: id, Enabled: true, Revision: 1}, nil
}
func (*domainHandlerRepo) ValidateSignupAffiliateAttribution(context.Context, *service.SignupAffiliateAttribution) error {
	return nil
}
func (*domainHandlerRepo) BindSignupAffiliateAttribution(context.Context, int64, *service.SignupAffiliateAttribution) error {
	return nil
}
func (r *domainHandlerRepo) SetUserRebateRate(context.Context, int64, *float64) error {
	r.cleared = append(r.cleared, "rate")
	return nil
}
func (r *domainHandlerRepo) ResetUserAffCode(context.Context, int64) (string, error) {
	r.cleared = append(r.cleared, "code")
	return "NEWCODE12345", nil
}

func domainHandlerRequest(t *testing.T, repo *domainHandlerRepo, method, id, body string) *httptest.ResponseRecorder {
	t.Helper()
	settings := service.NewSettingService(domainHandlerSettings{}, &config.Config{})
	handler := NewAffiliateHandler(service.NewAffiliateService(repo, settings, nil, nil), nil)
	router := gin.New()
	router.PUT("/users/:user_id/domain", handler.SetUserDomain)
	router.DELETE("/users/:user_id", handler.ClearUserSettings)
	path := "/users/" + id
	if method == http.MethodPut {
		path += "/domain"
	}
	request := httptest.NewRequest(method, path, strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)
	return recorder
}

func TestAffiliateHandlerDomain_RequestContract(t *testing.T) {
	for _, tc := range []struct {
		name string
		id   string
		body string
	}{
		{"missing", "42", `{}`},
		{"null", "42", `{"domain":null}`},
		{"number", "42", `{"domain":12}`},
		{"invalid json", "42", `{`},
		{"zero user", "0", `{"domain":"p.api.example.com"}`},
		{"negative user", "-1", `{"domain":"p.api.example.com"}`},
		{"invalid user", "abc", `{"domain":"p.api.example.com"}`},
		{"parent host", "42", `{"domain":"api.example.com"}`},
		{"external domain", "42", `{"domain":"outside.example.com"}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			repo := &domainHandlerRepo{}
			response := domainHandlerRequest(t, repo, http.MethodPut, tc.id, tc.body)
			require.Equal(t, http.StatusBadRequest, response.Code, response.Body.String())
			require.Zero(t, repo.calls)
		})
	}
}

func TestAffiliateHandlerDomain_SetAndClearResponses(t *testing.T) {
	for _, raw := range []string{" Partner.Api.Example.com ", ""} {
		t.Run(raw, func(t *testing.T) {
			repo := &domainHandlerRepo{}
			body, err := json.Marshal(map[string]string{"domain": raw})
			require.NoError(t, err)
			response := domainHandlerRequest(t, repo, http.MethodPut, "42", string(body))
			require.Equal(t, http.StatusOK, response.Code, response.Body.String())
			require.Equal(t, int64(42), repo.userID)
			var envelope struct {
				Data struct {
					Domain string `json:"affiliate_domain"`
					URL    string `json:"referral_url"`
				} `json:"data"`
			}
			require.NoError(t, json.Unmarshal(response.Body.Bytes(), &envelope))
			if raw == "" {
				require.Empty(t, envelope.Data.Domain)
				require.Empty(t, envelope.Data.URL)
				require.Empty(t, repo.host)
			} else {
				require.Equal(t, "partner.api.example.com", envelope.Data.Domain)
				require.Equal(t, "https://partner.api.example.com/", envelope.Data.URL)
				require.Equal(t, envelope.Data.Domain, repo.host)
			}
		})
	}
}

func TestAffiliateHandlerDomain_ConflictAndClearAll(t *testing.T) {
	repo := &domainHandlerRepo{err: service.ErrAffiliateDomainTaken}
	response := domainHandlerRequest(t, repo, http.MethodPut, "42", `{"domain":"p.api.example.com"}`)
	require.Equal(t, http.StatusConflict, response.Code)
	repo = &domainHandlerRepo{}
	response = domainHandlerRequest(t, repo, http.MethodDelete, "42", "")
	require.Equal(t, http.StatusOK, response.Code, response.Body.String())
	require.Equal(t, []string{"domain", "rate", "code"}, repo.cleared)
}
