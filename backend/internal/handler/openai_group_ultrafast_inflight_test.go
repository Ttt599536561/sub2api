//go:build unit

package handler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	middleware "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// Stop at admission so the test exercises each real handler and estimator,
// without making an upstream request or writing a usage record.
type ultrafastEndpointAdmissionCache struct {
	*handlerInflightCache
	estimated float64
}

func (c *ultrafastEndpointAdmissionCache) ReserveInflightBalance(_ context.Context, _ int64, _ string, amount, _ float64, _ time.Duration) (bool, float64, error) {
	c.estimated = amount
	return false, 0, nil
}

func TestOpenAIGroupUltrafastDedicatedEndpointAdmission(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, endpoint := range []string{"embeddings", "alpha_search"} {
		t.Run(endpoint, func(t *testing.T) {
			cfg := &config.Config{}
			cfg.Billing.InflightReservation = config.InflightReservationConfig{Enabled: true, TTLSeconds: 60}
			cache := &ultrafastEndpointAdmissionCache{handlerInflightCache: newHandlerInflightCache(10)}
			billingCache := service.NewBillingCacheService(cache, nil, nil, nil, nil, nil, cfg, nil)
			t.Cleanup(billingCache.Stop)
			billing := service.NewBillingService(cfg, nil)
			concurrency := service.NewConcurrencyService(&fakeConcurrencyCache{})
			gateway := service.NewOpenAIGatewayService(nil, nil, nil, nil, nil, nil, nil, cfg, nil, concurrency, billing,
				nil, billingCache, nil, nil, nil, nil, service.NewModelPricingResolver(nil, billing), nil, nil, nil, nil)
			h := &OpenAIGatewayHandler{gatewayService: gateway, billingCacheService: billingCache, apiKeyService: &service.APIKeyService{},
				concurrencyHelper: NewConcurrencyHelper(concurrency, SSEPingFormatClaude, 0)}
			requestPrice, searchPrice := 0.3, 0.0
			group := &service.Group{ID: 8801, Platform: service.PlatformOpenAI, RateMultiplier: 0.5, Hydrated: true, Status: service.StatusActive,
				ForceOpenAIUltrafast: true, SearchPricePer1k: &searchPrice,
				ModelPricing: []service.ChannelModelPricing{{Models: []string{"custom-dedicated-model"}, BillingMode: service.BillingModePerRequest, PerRequestPrice: &requestPrice}},
			}
			key := &service.APIKey{ID: 8802, GroupID: &group.ID, Group: group, User: &service.User{ID: 8803, Balance: 10}}
			rec := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(rec)
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/"+endpoint, strings.NewReader(`{"model":"custom-dedicated-model","input":"hello","query":"hello","max_tokens":100}`))
			c.Request.Header.Set("Content-Type", "application/json")
			c.Set(string(middleware.ContextKeyAPIKey), key)
			c.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: key.User.ID, Concurrency: 10})
			if endpoint == "embeddings" {
				h.Embeddings(c)
			} else {
				h.AlphaSearch(c)
			}
			require.InDelta(t, 0.15, cache.estimated, 1e-12, "dedicated endpoint must keep its original estimate: %s", rec.Body.String())
		})
	}
}
