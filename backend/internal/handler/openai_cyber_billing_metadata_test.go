package handler

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/timezone"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestRecordCyberPolicyIfMarked_PreservesBillingSnapshot(t *testing.T) {
	for _, peak := range []bool{false, true} {
		t.Run(fmt.Sprintf("peak=%t", peak), func(t *testing.T) {
			cfg := &config.Config{}
			cfg.RunMode = config.RunModeSimple
			cfg.Default.RateMultiplier = 1
			repo := &openAIWSUsageHandlerUsageLogRepoStub{created: make(chan *service.UsageLog, 2)}
			billing := service.NewBillingService(cfg, nil)
			gw := service.NewOpenAIGatewayService(nil, repo, nil, nil, nil, nil, nil, cfg, nil, nil, billing, nil, nil, nil, &service.DeferredService{}, nil, nil, nil, nil, nil, nil, nil)
			h := &OpenAIGatewayHandler{gatewayService: gw}
			group := &service.Group{ID: 11, Platform: service.PlatformOpenAI, Status: service.StatusActive, Hydrated: true, RateMultiplier: 1, ForceOpenAIUltrafast: true}
			var sub *service.UserSubscription
			pricingAt := time.Time{}
			wantRate := 1.0
			if peak {
				hour := (time.Now().In(timezone.Location()).Hour() + 12) % 23
				group.SubscriptionType = service.SubscriptionTypeSubscription
				group.PeakRateEnabled = true
				group.PeakStart = fmt.Sprintf("%02d:00", hour)
				group.PeakEnd = fmt.Sprintf("%02d:00", hour+1)
				group.PeakRateMultiplier = 3
				pricingAt = time.Date(2020, 1, 1, hour, 30, 0, 0, timezone.Location())
				sub = &service.UserSubscription{ID: 99}
				wantRate = 3
				require.Equal(t, 3.0, group.PeakMultiplierAt(pricingAt))
				require.Equal(t, 1.0, group.PeakMultiplierAt(time.Now()))
			}
			key := &service.APIKey{ID: 12, GroupID: &group.ID, Group: group, User: &service.User{ID: 13}}
			account := &service.Account{ID: 14, Platform: service.PlatformOpenAI, Type: service.AccountTypeAPIKey}
			c := newTestGinContext()
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
			c.Writer.Header().Set("X-Request-Id", fmt.Sprintf("cyber-handler-%t", peak))
			metadata := service.OpenAICyberBillingMetadata{BillingModel: "gpt-5.4", UpstreamModel: "gpt-6-astra", ServiceTier: "ultrafast", UpstreamResponseModel: "gpt-6-astra", UpstreamResponseServiceTier: "ultrafast", Usage: service.OpenAIUsage{InputTokens: 1200, OutputTokens: 300, CacheReadInputTokens: 200}}
			service.MarkOpsCyberPolicy(c, service.CyberPolicyMark{Message: "cyber policy", UpstreamStatus: 200, UpstreamInTok: 1200, UpstreamOutTok: 300, Billing: metadata})
			h.recordCyberPolicyIfMarked(c, key, account, sub, "client-alias", true, nil, service.ChannelUsageFields{}, "", pricingAt)
			// The async task must not reread a later turn's Gin metadata.
			service.ClearOpsCyberPolicy(c)
			service.MarkOpsCyberPolicy(c, service.CyberPolicyMark{Billing: service.OpenAICyberBillingMetadata{BillingModel: "gpt-5.4", UpstreamModel: "gpt-5.4", ServiceTier: "flex"}})
			select {
			case got := <-repo.created:
				require.NotNil(t, got.ServiceTier)
				require.Equal(t, "ultrafast", *got.ServiceTier)
				require.NotNil(t, got.UpstreamModel)
				require.Equal(t, "gpt-6-astra", *got.UpstreamModel)
				require.Equal(t, 1000, got.InputTokens)
				require.Equal(t, 200, got.CacheReadTokens)
				require.InDelta(t, wantRate, got.RateMultiplier, 1e-12)
				cost, err := billing.CalculateCost("gpt-5.4", service.UsageTokens{InputTokens: 1000, OutputTokens: 300, CacheReadTokens: 200}, wantRate)
				require.NoError(t, err)
				require.InDelta(t, cost.ActualCost*8, got.ActualCost, 1e-12)
			case <-time.After(3 * time.Second):
				t.Fatal("cyber usage task did not record the failed request")
			}
		})
	}
}
