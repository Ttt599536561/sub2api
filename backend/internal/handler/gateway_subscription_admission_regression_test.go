//go:build unit

package handler

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/ctxkey"
	"github.com/Wei-Shaw/sub2api/internal/pkg/tlsfingerprint"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type gatewaySubscriptionAdmissionSequence struct {
	sub      *service.UserSubscription
	group    *service.Group
	checks   int
	rejectAt int
	err      error
}

type gatewaySubscriptionGroupRepo struct {
	service.GroupRepository
	groups map[int64]*service.Group
}

func (r *gatewaySubscriptionGroupRepo) GetByID(_ context.Context, id int64) (*service.Group, error) {
	return r.groups[id], nil
}

func (r *gatewaySubscriptionGroupRepo) GetByIDLite(ctx context.Context, id int64) (*service.Group, error) {
	return r.GetByID(ctx, id)
}

type gatewaySubscriptionSchedulerCache struct {
	*fakeSchedulerCache
	groups map[int64][]*service.Account
}

func (s *gatewaySubscriptionSchedulerCache) GetSnapshot(_ context.Context, bucket service.SchedulerBucket) ([]*service.Account, bool, error) {
	return s.groups[bucket.GroupID], true, nil
}

type gatewaySubscriptionBalanceCache struct{ service.BillingCache }

func (*gatewaySubscriptionBalanceCache) GetUserBalance(context.Context, int64) (float64, error) {
	return 100, nil
}

type gatewayFallbackSubscriptionAdmission struct {
	sub            *service.UserSubscription
	group          *service.Group
	fallbackGroup  *service.Group
	expired        bool
	originalChecks int
}

func (s *gatewayFallbackSubscriptionAdmission) GetSubscriptionForAdmission(_ context.Context, _, groupID int64) (*service.UserSubscription, *service.Group, error) {
	if groupID == s.fallbackGroup.ID {
		return nil, s.fallbackGroup, nil
	}
	s.originalChecks++
	if s.expired {
		return nil, s.group, service.ErrSubscriptionExpired
	}
	sub := *s.sub
	return &sub, s.group, nil
}

func (*gatewayFallbackSubscriptionAdmission) TriggerAutoDailyReset(context.Context, int64) error {
	return nil
}

type gatewaySubscriptionUpstream struct {
	respond func(int64) (*http.Response, error)
}

func (u *gatewaySubscriptionUpstream) Do(_ *http.Request, _ string, accountID int64, _ int) (*http.Response, error) {
	return u.respond(accountID)
}

func (u *gatewaySubscriptionUpstream) DoWithTLS(req *http.Request, proxy string, accountID int64, concurrency int, _ *tlsfingerprint.Profile) (*http.Response, error) {
	return u.Do(req, proxy, accountID, concurrency)
}

func TestMessagesFallbackUsesCurrentAttemptSubscription(t *testing.T) {
	gin.SetMode(gin.TestMode)
	fallbackID := int64(9301)
	group := &service.Group{ID: 9300, Hydrated: true, Status: service.StatusActive, Platform: service.PlatformAnthropic, SubscriptionType: service.SubscriptionTypeSubscription, FallbackGroupIDOnInvalidRequest: &fallbackID}
	fallback := &service.Group{ID: fallbackID, Hydrated: true, Status: service.StatusActive, Platform: service.PlatformAnthropic, SubscriptionType: service.SubscriptionTypeStandard}
	primaryAccount := &service.Account{
		ID: 9302, Platform: service.PlatformAntigravity, Type: service.AccountTypeOAuth,
		Status: service.StatusActive, Schedulable: true, Concurrency: 1,
		Credentials:   map[string]any{"access_token": "test-token", "project_id": "test-project"},
		Extra:         map[string]any{"mixed_scheduling": true},
		AccountGroups: []service.AccountGroup{{AccountID: 9302, GroupID: group.ID}},
	}
	fallbackAccount := &service.Account{
		ID: 9303, Platform: service.PlatformAnthropic, Type: service.AccountTypeAPIKey,
		Status: service.StatusActive, Schedulable: true, Concurrency: 1,
		Credentials:   map[string]any{"api_key": "test-key"},
		AccountGroups: []service.AccountGroup{{AccountID: 9303, GroupID: fallback.ID}},
	}
	scheduler := service.NewSchedulerSnapshotService(&gatewaySubscriptionSchedulerCache{
		fakeSchedulerCache: &fakeSchedulerCache{accounts: []*service.Account{primaryAccount, fallbackAccount}},
		groups:             map[int64][]*service.Account{group.ID: {primaryAccount}, fallback.ID: {fallbackAccount}},
	}, nil, nil, nil, nil)
	sub := &service.UserSubscription{ID: 9304, UserID: 9305, GroupID: group.ID}
	admission := &gatewayFallbackSubscriptionAdmission{sub: sub, group: group, fallbackGroup: fallback}
	cfg := &config.Config{}
	billing := service.NewBillingCacheService(&gatewaySubscriptionBalanceCache{}, nil, nil, nil, nil, nil, cfg, nil)
	billing.SetSubscriptionService(admission)
	t.Cleanup(billing.Stop)
	var upstreamAccounts []int64
	upstream := &gatewaySubscriptionUpstream{respond: func(accountID int64) (*http.Response, error) {
		upstreamAccounts = append(upstreamAccounts, accountID)
		// A terminal fallback response keeps this routing test independent of usage billing.
		message := `{"error":{"message":"fallback reached"}}`
		if accountID == primaryAccount.ID {
			admission.expired = true
			message = `{"error":{"message":"Prompt is too long"}}`
		}
		return &http.Response{StatusCode: http.StatusBadRequest, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(bytes.NewBufferString(message))}, nil
	}}
	groupRepo := &gatewaySubscriptionGroupRepo{groups: map[int64]*service.Group{group.ID: group, fallback.ID: fallback}}
	settings := service.NewSettingService(&oauthCaptchaSettingRepo{}, cfg)
	gateway := service.NewGatewayService(
		nil, groupRepo, nil, nil, nil, nil, nil, nil, cfg, scheduler,
		nil, nil, nil, nil, nil, upstream, nil, nil, nil, nil, nil, settings, nil, nil, nil, nil, nil, nil,
	)
	h := &GatewayHandler{
		gatewayService:            gateway,
		antigravityGatewayService: service.NewAntigravityGatewayService(nil, nil, scheduler, &service.AntigravityTokenProvider{}, nil, upstream, settings, nil),
		billingCacheService:       billing,
		concurrencyHelper:         NewConcurrencyHelper(service.NewConcurrencyService(&fakeConcurrencyCache{}), SSEPingFormatClaude, 0),
		maxAccountSwitches:        1,
	}
	apiKey := &service.APIKey{ID: 9306, UserID: sub.UserID, GroupID: &group.ID, Group: group, User: &service.User{ID: sub.UserID, Concurrency: 10, Balance: 100}}
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", bytes.NewBufferString(`{"model":"claude-opus-4-6","messages":[{"role":"user","content":"hello"}],"max_tokens":10,"stream":false}`))
	c.Request.Header.Set("Content-Type", "application/json")
	c.Request = c.Request.WithContext(context.WithValue(c.Request.Context(), ctxkey.Group, group))
	c.Set(string(middleware.ContextKeyAPIKey), apiKey)
	c.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: sub.UserID, Concurrency: 10})
	c.Set(string(middleware.ContextKeySubscription), sub)

	h.Messages(c)

	require.Equal(t, []int64{primaryAccount.ID, fallbackAccount.ID}, upstreamAccounts, w.Body.String())
	require.Equal(t, http.StatusBadRequest, w.Code)
	require.Contains(t, w.Body.String(), "fallback reached")
	require.Equal(t, 2, admission.originalChecks, "the balance-funded fallback must not revalidate the original subscription")
}

func (s *gatewaySubscriptionAdmissionSequence) GetSubscriptionForAdmission(context.Context, int64, int64) (*service.UserSubscription, *service.Group, error) {
	s.checks++
	if s.checks >= s.rejectAt {
		return nil, s.group, s.err
	}
	sub := *s.sub
	return &sub, s.group, nil
}

func (*gatewaySubscriptionAdmissionSequence) TriggerAutoDailyReset(context.Context, int64) error {
	return nil
}

func TestWebSearchSubscriptionRejectionAfterAccountAcquisition(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, test := range []struct {
		name     string
		err      error
		rejectAt int
	}{
		{name: "expired", err: service.ErrSubscriptionExpired, rejectAt: 2},
		{name: "daily quota exhausted", err: service.ErrDailyLimitExceeded, rejectAt: 2},
		{name: "weekly quota exhausted", err: service.ErrWeeklyLimitExceeded, rejectAt: 2},
		{name: "expired during failover", err: service.ErrSubscriptionExpired, rejectAt: 3},
		{name: "daily quota exhausted during failover", err: service.ErrDailyLimitExceeded, rejectAt: 3},
	} {
		t.Run(test.name, func(t *testing.T) {
			groupID := int64(9200)
			group := &service.Group{ID: groupID, Hydrated: true, Status: service.StatusActive, Platform: service.PlatformGrok, SubscriptionType: service.SubscriptionTypeSubscription}
			account := &service.Account{
				ID: 9201, Platform: service.PlatformGrok, Type: service.AccountTypeAPIKey,
				Status: service.StatusActive, Schedulable: true, Concurrency: 1,
				Credentials:   map[string]any{"api_key": "test-key"},
				AccountGroups: []service.AccountGroup{{AccountID: 9201, GroupID: groupID}},
			}
			secondAccount := *account
			secondAccount.ID = 9211
			secondAccount.AccountGroups = []service.AccountGroup{{AccountID: secondAccount.ID, GroupID: groupID}}
			scheduler := service.NewSchedulerSnapshotService(&fakeSchedulerCache{accounts: []*service.Account{account, &secondAccount}}, nil, nil, nil, nil)
			cache := &profitCountingConcurrencyCache{}
			concurrency := service.NewConcurrencyService(cache)
			upstreamCalls := 0
			upstream := &gatewaySubscriptionUpstream{respond: func(int64) (*http.Response, error) {
				upstreamCalls++
				return &http.Response{StatusCode: http.StatusServiceUnavailable, Header: http.Header{}, Body: io.NopCloser(bytes.NewBufferString(`{"error":{"message":"upstream unavailable"}}`))}, nil
			}}
			gateway := service.NewGatewayService(
				nil, &fakeGroupRepo{group: group}, nil, nil, nil, nil, nil, nil, nil, scheduler,
				concurrency, nil, nil, nil, nil, upstream, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil,
			)
			sub := &service.UserSubscription{ID: 9202, UserID: 9203, GroupID: groupID}
			admission := &gatewaySubscriptionAdmissionSequence{sub: sub, group: group, rejectAt: test.rejectAt, err: test.err}
			billing := service.NewBillingCacheService(nil, nil, nil, nil, nil, nil, &config.Config{}, nil)
			billing.SetSubscriptionService(admission)
			t.Cleanup(billing.Stop)
			h := &GatewayHandler{gatewayService: gateway, billingCacheService: billing, concurrencyHelper: NewConcurrencyHelper(concurrency, SSEPingFormatClaude, 0)}
			apiKey := &service.APIKey{ID: 9204, UserID: sub.UserID, GroupID: &groupID, Group: group, User: &service.User{ID: sub.UserID, Concurrency: 10}}
			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/web_search", bytes.NewBufferString(`{"query":"subscription billing"}`))
			c.Request.Header.Set("Content-Type", "application/json")
			c.Request = c.Request.WithContext(context.WithValue(c.Request.Context(), ctxkey.Group, group))
			c.Set(string(middleware.ContextKeyAPIKey), apiKey)
			c.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: sub.UserID, Concurrency: 10})
			c.Set(string(middleware.ContextKeySubscription), sub)

			h.WebSearch(c)

			require.Equal(t, http.StatusForbidden, w.Code, w.Body.String())
			_, code, message, _ := billingErrorDetails(test.err)
			require.JSONEq(t, `{"error":{"type":`+strconv.Quote(code)+`,"message":`+strconv.Quote(message)+`}}`, w.Body.String())
			require.Equal(t, test.rejectAt, admission.checks, "billing rejection must stop account failover")
			require.Equal(t, test.rejectAt-2, upstreamCalls, "the rejected attempt must not reach the upstream")
			require.Equal(t, int64(test.rejectAt-1), cache.accountReleases.Load(), "every acquired slot must be released")
		})
	}
}
