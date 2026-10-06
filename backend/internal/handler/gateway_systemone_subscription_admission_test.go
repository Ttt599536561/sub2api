//go:build unit

package handler

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/ctxkey"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestSystemOneSubscriptionRejectionAfterAccountAcquisition(t *testing.T) {
	for _, test := range []struct {
		name     string
		err      error
		rejectAt int
	}{
		{name: "expired", err: service.ErrSubscriptionExpired, rejectAt: 2},
		{name: "daily quota exhausted", err: service.ErrDailyLimitExceeded, rejectAt: 2},
		{name: "weekly quota exhausted", err: service.ErrWeeklyLimitExceeded, rejectAt: 2},
		{name: "group disabled", err: service.ErrSubscriptionInvalid, rejectAt: 2},
		{name: "expired during failover", err: service.ErrSubscriptionExpired, rejectAt: 3},
		{name: "daily quota exhausted during failover", err: service.ErrDailyLimitExceeded, rejectAt: 3},
	} {
		t.Run(test.name, func(t *testing.T) {
			groupID := int64(9400)
			group := &service.Group{ID: groupID, Hydrated: true, Status: service.StatusActive, Platform: service.PlatformTypeSafe, SubscriptionType: service.SubscriptionTypeSubscription}
			account := &service.Account{
				ID: 9401, Platform: service.PlatformTypeSafe, Type: service.AccountTypeAPIKey,
				Status: service.StatusActive, Schedulable: true, Concurrency: 1,
				Credentials:   map[string]any{"api_key": "test-key"},
				AccountGroups: []service.AccountGroup{{AccountID: 9401, GroupID: groupID}},
			}
			secondAccount := *account
			secondAccount.ID = 9411
			secondAccount.AccountGroups = []service.AccountGroup{{AccountID: secondAccount.ID, GroupID: groupID}}
			scheduler := service.NewSchedulerSnapshotService(&fakeSchedulerCache{accounts: []*service.Account{account, &secondAccount}}, nil, nil, nil, nil)
			cache := &profitCountingConcurrencyCache{}
			concurrency := service.NewConcurrencyService(cache)
			upstreamCalls := 0
			upstream := &gatewaySubscriptionUpstream{respond: func(int64) (*http.Response, error) {
				upstreamCalls++
				// A first retryable response exercises the next account attempt;
				// terminal errors avoid unrelated usage billing in this admission test.
				status := http.StatusBadRequest
				if test.rejectAt == 3 && upstreamCalls == 1 {
					status = http.StatusServiceUnavailable
				}
				return &http.Response{StatusCode: status, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(`{"error":{"message":"upstream rejected request"}}`))}, nil
			}}
			cfg := &config.Config{Gateway: config.GatewayConfig{MaxBodySize: 1 << 20}}
			gateway := service.NewGatewayService(
				nil, &fakeGroupRepo{group: group}, nil, nil, nil, nil, nil, nil, cfg, scheduler,
				concurrency, nil, nil, nil, nil, upstream, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil,
			)
			sub := &service.UserSubscription{ID: 9402, UserID: 9403, GroupID: groupID}
			admission := &gatewaySubscriptionAdmissionSequence{sub: sub, group: group, rejectAt: test.rejectAt, err: test.err}
			billing := service.NewBillingCacheService(nil, nil, nil, nil, nil, nil, cfg, nil)
			billing.SetSubscriptionService(admission)
			t.Cleanup(billing.Stop)
			h := &GatewayHandler{cfg: cfg, gatewayService: gateway, billingCacheService: billing, concurrencyHelper: NewConcurrencyHelper(concurrency, SSEPingFormatClaude, 0), maxAccountSwitches: 2}
			apiKey := &service.APIKey{ID: 9404, UserID: sub.UserID, GroupID: &groupID, Group: group, User: &service.User{ID: sub.UserID, Concurrency: 10}}
			c, recorder := newSystemOneHandlerContext(validSystemOneHandlerBody)
			c.Request = c.Request.WithContext(context.WithValue(c.Request.Context(), ctxkey.Group, group))
			c.Set(string(middleware.ContextKeyAPIKey), apiKey)
			c.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: sub.UserID, Concurrency: 10})
			c.Set(string(middleware.ContextKeySubscription), sub)

			h.SystemOne(c)

			status, code, message, _ := billingErrorDetails(test.err)
			require.Equal(t, status, recorder.Code, recorder.Body.String())
			require.Contains(t, recorder.Body.String(), code)
			require.Contains(t, recorder.Body.String(), message)
			require.Equal(t, test.rejectAt, admission.checks, "billing rejection must stop account failover")
			require.Equal(t, test.rejectAt-2, upstreamCalls, "the rejected attempt must not reach the upstream")
			require.Equal(t, int64(test.rejectAt-1), cache.accountReleases.Load(), "every acquired slot must be released")
		})
	}
}
