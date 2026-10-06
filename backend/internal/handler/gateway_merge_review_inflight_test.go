//go:build unit

package handler

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/ctxkey"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestMergeReviewMessagesFallbackReservesBalance(t *testing.T) {
	for _, tt := range []struct {
		name         string
		subscription bool
		heldFraction float64
		wantRejected bool
	}{
		{name: "subscription rejects occupied balance", subscription: true, heldFraction: 1.05, wantRejected: true},
		{name: "subscription acquires balance reservation", subscription: true},
		{name: "balance replaces lower original estimate without self rejection"},
		{name: "balance rejects more expensive fallback", heldFraction: 1.0, wantRejected: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			testMergeReviewMessagesFallbackReservation(t, tt.subscription, tt.heldFraction, tt.wantRejected)
		})
	}
}

type mergeReviewFallbackAdmission struct {
	primary, fallback *service.Group
	sub               *service.UserSubscription
}

func (a *mergeReviewFallbackAdmission) GetSubscriptionForAdmission(_ context.Context, _, groupID int64) (*service.UserSubscription, *service.Group, error) {
	if groupID == a.fallback.ID {
		return nil, a.fallback, nil
	}
	if a.sub == nil {
		return nil, a.primary, nil
	}
	copy := *a.sub
	return &copy, a.primary, nil
}

func (*mergeReviewFallbackAdmission) TriggerAutoDailyReset(context.Context, int64) error { return nil }

func testMergeReviewMessagesFallbackReservation(t *testing.T, subscription bool, heldFraction float64, wantRejected bool) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	fallbackID := int64(9301)
	group := &service.Group{ID: 9300, Hydrated: true, Status: service.StatusActive, Platform: service.PlatformAnthropic, SubscriptionType: service.SubscriptionTypeStandard, RateMultiplier: 0.01, FallbackGroupIDOnInvalidRequest: &fallbackID}
	if subscription {
		group.SubscriptionType = service.SubscriptionTypeSubscription
	}
	fallback := &service.Group{ID: fallbackID, Hydrated: true, Status: service.StatusActive, Platform: service.PlatformAnthropic, SubscriptionType: service.SubscriptionTypeStandard, RateMultiplier: 1}
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
	const userID = int64(9305)
	var sub *service.UserSubscription
	if subscription {
		sub = &service.UserSubscription{ID: 9304, UserID: userID, GroupID: group.ID}
	}
	admission := &mergeReviewFallbackAdmission{sub: sub, primary: group, fallback: fallback}
	cfg := &config.Config{}
	cfg.Billing.InflightReservation = config.InflightReservationConfig{Enabled: true, TTLSeconds: 60}
	cache := newHandlerInflightCache(1)
	pricing := service.NewBillingService(cfg, nil)
	billing := service.NewBillingCacheService(cache, nil, nil, nil, nil, nil, cfg, nil)
	billing.SetSubscriptionService(admission)
	t.Cleanup(billing.Stop)
	var upstreamAccounts []int64
	var fallbackEstimate float64
	otherReservations := 0
	upstream := &gatewaySubscriptionUpstream{respond: func(accountID int64) (*http.Response, error) {
		upstreamAccounts = append(upstreamAccounts, accountID)
		// A terminal fallback response keeps this routing test independent of usage billing.
		message := `{"error":{"message":"fallback reached"}}`
		if accountID == primaryAccount.ID {
			// Primary request fails before consuming any billable tokens.
			message = `{"error":{"message":"Prompt is too long"}}`
		} else if !wantRejected {
			require.Equal(t, otherReservations+1, cache.count(), "the fallback must own exactly one reservation, replacing the failed attempt")
			cache.mu.Lock()
			amount := 0.0
			for _, v := range cache.res {
				amount += v
			}
			cache.mu.Unlock()
			require.InDelta(t, fallbackEstimate*(1+heldFraction), amount, 1e-12, "fallback group pricing must determine the held amount")
		}
		return &http.Response{StatusCode: http.StatusBadRequest, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(bytes.NewBufferString(message))}, nil
	}}
	groupRepo := &gatewaySubscriptionGroupRepo{groups: map[int64]*service.Group{group.ID: group, fallback.ID: fallback}}
	settings := service.NewSettingService(&oauthCaptchaSettingRepo{}, cfg)
	gateway := service.NewGatewayService(
		nil, groupRepo, nil, nil, nil, nil, nil, nil, cfg, scheduler,
		nil, pricing, nil, nil, nil, upstream, nil, nil, nil, nil, nil, settings, nil, nil, service.NewModelPricingResolver(nil, pricing), nil, nil, nil,
	)
	h := &GatewayHandler{
		gatewayService:            gateway,
		antigravityGatewayService: service.NewAntigravityGatewayService(nil, nil, scheduler, &service.AntigravityTokenProvider{}, nil, upstream, settings, nil),
		billingCacheService:       billing,
		concurrencyHelper:         NewConcurrencyHelper(service.NewConcurrencyService(&fakeConcurrencyCache{}), SSEPingFormatClaude, 0),
		maxAccountSwitches:        1,
	}
	apiKey := &service.APIKey{ID: 9306, UserID: userID, GroupID: &group.ID, Group: group, User: &service.User{ID: userID, Concurrency: 10, Balance: 100}}
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	body := []byte(`{"model":"claude-opus-4-6","messages":[{"role":"user","content":"hello"}],"max_tokens":10,"stream":false}`)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", bytes.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")
	c.Request = c.Request.WithContext(context.WithValue(c.Request.Context(), ctxkey.Group, group))
	c.Set(string(middleware.ContextKeyAPIKey), apiKey)
	c.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: userID, Concurrency: 10})
	if sub != nil {
		c.Set(string(middleware.ContextKeySubscription), sub)
	}

	estimate, priced := gateway.EstimateInflightReservation(c.Request.Context(), cloneAPIKeyWithGroup(apiKey, fallback), tokenInflightEstimate("claude-opus-4-6", body))
	require.True(t, priced)
	require.Positive(t, estimate)
	fallbackEstimate = estimate
	cache.balance = estimate * 1.05
	if heldFraction == 0 {
		cache.balance = estimate * 1.005
	}
	if heldFraction > 0 {
		held, err := billing.ReserveInflight(context.Background(), apiKey.User, fallback, nil, estimate*heldFraction)
		require.NoError(t, err)
		require.NotNil(t, held)
		defer held.HandlerDone()
		otherReservations = 1
	}
	h.Messages(c)

	if wantRejected {
		require.Equal(t, []int64{primaryAccount.ID}, upstreamAccounts, "fallback must not reach upstream when the available balance cannot cover the new estimate")
		require.Contains(t, w.Body.String(), "balance")
	} else {
		require.Equal(t, []int64{primaryAccount.ID, fallbackAccount.ID}, upstreamAccounts, w.Body.String())
		require.Contains(t, w.Body.String(), "fallback reached")
	}
	require.Equal(t, otherReservations, cache.count(), "completed request must not leak a reservation or release another request's hold")
}
