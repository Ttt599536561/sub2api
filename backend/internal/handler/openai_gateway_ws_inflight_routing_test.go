package handler

import (
	"context"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

type wsRoutingReservationCache struct {
	*handlerInflightCache
	amounts chan float64
}

func (c *wsRoutingReservationCache) ReserveInflightBalance(ctx context.Context, userID int64, id string, amount, balance float64, ttl time.Duration) (bool, float64, error) {
	c.amounts <- amount
	return c.handlerInflightCache.ReserveInflightBalance(ctx, userID, id, amount, balance, ttl)
}

func TestOpenAIResponsesWebSocket_InflightReservationUsesNativeMapping(t *testing.T) {
	var amounts []float64
	for _, force := range []bool{false, true} {
		cache := &wsRoutingReservationCache{handlerInflightCache: newHandlerInflightCache(100), amounts: make(chan float64, 2)}
		billingCfg := &config.Config{}
		billingCfg.Billing.InflightReservation = config.InflightReservationConfig{Enabled: true, TTLSeconds: 60}
		repo := newWSGroupPricingAPIKeyRepoStub(1)
		repo.group.ForceOpenAIUltrafast = force
		got := runOpenAIResponsesWebSocketUsageLogCase(t, openAIResponsesWSUsageLogCase{
			firstPayload:        `{"type":"response.create","model":"gpt-5.6-sol","max_output_tokens":1024}`,
			ingressMode:         service.OpenAIWSIngressModeCtxPool,
			apiKeyService:       newWSGroupPricingAPIKeyService(repo),
			apiKeyCredential:    wsGroupPricingKey,
			accountModelMapping: map[string]any{"gpt-5.6-sol": "gpt-6.1-sol"},
			billingCache:        cache, billingConfig: billingCfg, useSchedulerSnapshot: true,
			configureAccount: func(account *service.Account, cfg *config.Config) {
				account.Extra["openai_passthrough"] = true
				cfg.Gateway.Scheduling.DbFallbackEnabled = true
			},
		})
		require.Equal(t, "gpt-6.1-sol", gjson.GetBytes(got.upstreamFirstPayload, "model").String())
		if force {
			require.Equal(t, "ultrafast", gjson.GetBytes(got.upstreamFirstPayload, "service_tier").String())
		}
		select {
		case amount := <-cache.amounts:
			require.Positive(t, amount)
			amounts = append(amounts, amount)
		default:
			t.Fatal("WebSocket admission did not reserve balance")
		}
	}
	require.InDelta(t, amounts[0]*8, amounts[1], 1e-12, "reservation must follow the native WS target despite the unrelated HTTP passthrough flag")
}
