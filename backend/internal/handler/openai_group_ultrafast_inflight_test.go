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

func TestTokenInflightEstimateImageOnlyClassification(t *testing.T) {
	for _, tc := range []struct {
		name  string
		model string
		body  string
		image bool
	}{
		{name: "image_model", model: "gpt-image-2", body: `{"model":"gpt-image-2","input":"draw","max_output_tokens":128}`, image: true},
		{name: "image_model_forced_choice", model: "gpt-image-2", body: `{"model":"gpt-image-2","input":"draw","tool_choice":{"type":"image_generation"}}`, image: true},
		{name: "image_model_none_choice", model: "gpt-image-2", body: `{"model":"gpt-image-2","input":"draw","tool_choice":"none"}`},
		{name: "image_model_auto_choice", model: "gpt-image-2", body: `{"model":"gpt-image-2","input":"draw","tool_choice":"auto"}`},
		{name: "image_model_null_choice", model: "gpt-image-2", body: `{"model":"gpt-image-2","input":"draw","tool_choice":null}`},
		{name: "image_model_function_choice", model: "gpt-image-2", body: `{"model":"gpt-image-2","input":"draw","tool_choice":{"type":"function","name":"image_generation"}}`},
		{name: "image_model_messages_shape", model: "gpt-image-2", body: `{"model":"gpt-image-2","messages":[{"role":"user","content":"draw"}]}`},
		{name: "duplicate_model", model: "gpt-image-2", body: `{"model":"gpt-image-2","input":"draw","model":"custom-text"}`},
		{name: "duplicate_input", model: "gpt-image-2", body: `{"model":"gpt-image-2","input":"draw","input":"hello"}`},
		{name: "duplicate_choice", model: "gpt-image-2", body: `{"model":"gpt-image-2","input":"draw","tool_choice":{"type":"image_generation"},"tool_choice":"none"}`},
		{name: "duplicate_choice_type", model: "gpt-image-2", body: `{"model":"gpt-image-2","input":"draw","tool_choice":{"type":"image_generation","type":"function","name":"lookup"}}`},
		{name: "native_forced_tool", model: "custom-text", body: `{"input":"draw","tools":[{"type":"image_generation"}],"tool_choice":{"type":"image_generation"}}`},
		{name: "plain_text", model: "custom-text", body: `{"input":"hello"}`},
		{name: "native_tool_only", model: "custom-text", body: `{"tools":[{"type":"image_generation"}]}`},
		{name: "native_tool_auto", model: "custom-text", body: `{"tools":[{"type":"image_generation"}],"tool_choice":"auto"}`},
		{name: "image_input", model: "custom-text", body: `{"input":[{"role":"user","content":[{"type":"input_image","image_url":"https://example.com/image.png"}]}]}`},
		{name: "chat_image_input", model: "custom-text", body: `{"messages":[{"role":"user","content":[{"type":"image_url","image_url":{"url":"https://example.com/image.png"}}]}]}`},
		{name: "same_named_function", model: "custom-text", body: `{"tools":[{"type":"function","function":{"name":"image_generation","parameters":{"type":"object"}}}],"tool_choice":{"type":"function","function":{"name":"image_generation"}}}`},
		{name: "namespace_declaration", model: "custom-text", body: `{"tools":[{"type":"namespace","name":"image_gen"}]}`},
		{name: "namespace_choice", model: "custom-text", body: `{"tools":[{"type":"namespace","name":"image_gen"}],"tool_choice":{"type":"namespace","name":"image_gen"}}`},
		{name: "namespace_function_choice", model: "custom-text", body: `{"tool_choice":{"type":"function","name":"image_gen.imagegen"}}`},
		{name: "responses_lite_tools", model: "custom-text", body: `{"input":[{"type":"additional_tools","tools":[{"type":"namespace","name":"image_gen"}]}]}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := responsesInflightEstimate(tc.model, []byte(tc.body))
			require.Equal(t, tc.image, req.ImageOnlyRequest)
			require.Equal(t, service.InflightEstimateToken, req.Kind, "classification must not change existing image-count billing semantics")
			require.False(t, req.SkipGroupUltrafast, "token pricing must still use the group multiplier")
			require.Equal(t, len(tc.body), req.BodyBytes)
			require.Equal(t, tc.model, req.Model)
		})
	}
}

func TestTokenInflightEstimateExplicitImageToolModel(t *testing.T) {
	for _, tc := range []struct {
		name, fields, wantModel string
		image                   bool
	}{
		{"explicit", `"tools":[{"type":"function","name":"lookup"},{"type":"image_generation","model":" gpt-image-1 " }]`, "gpt-image-1", true},
		{"implicit", `"tools":[{"type":"image_generation"}]`, "", true},
		{"unrelated", `"tools":[{"type":"function","name":"image_generation","model":"other"}]`, "", true},
		{"duplicate_tools", `"tools":[],"tools":[{"type":"image_generation","model":"other"}]`, "", false},
		{"duplicate_model", `"tools":[{"type":"image_generation","model":"one","mo\u0064el":"two"}]`, "", false},
		{"duplicate_type", `"tools":[{"type":"function","type":"image_generation","model":"other"}]`, "", false},
		{"duplicate_choice_type", `"tool_choice":{"type":"function","type":"image_generation"}`, "", false},
		{"multiple_native", `"tools":[{"type":"image_generation"},{"type":"image_generation","model":"other"}]`, "", false},
		{"invalid_model", `"tools":[{"type":"image_generation","model":123}]`, "", false},
		{"invalid_list", `"tools":{}`, "", false},
		{"invalid_entry", `"tools":[null]`, "", false},
		{"invalid_type", `"tools":[{"type":null}]`, "", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			body := []byte(`{"model":"gpt-image-2","input":"draw",` + tc.fields + `}`)
			req := responsesInflightEstimate("gpt-image-2", body)
			require.Equal(t, tc.image, req.ImageOnlyRequest)
			require.Equal(t, tc.wantModel, req.ImageToolModel)
			generic := tokenInflightEstimate("gpt-image-2", body) // WS ingress and non-Responses handlers
			require.False(t, generic.ImageOnlyRequest)
			require.Empty(t, generic.ImageToolModel)
			require.Equal(t, service.InflightEstimateToken, req.Kind)
		})
	}
}

func newUltrafastImageAdmissionFixture(t *testing.T, cache service.BillingCache) (*OpenAIGatewayHandler, *service.APIKey) {
	t.Helper()
	cfg := &config.Config{}
	cfg.Billing.InflightReservation = config.InflightReservationConfig{Enabled: true, TTLSeconds: 60}
	billingCache := service.NewBillingCacheService(cache, nil, nil, nil, nil, nil, cfg, nil)
	t.Cleanup(billingCache.Stop)
	billing := service.NewBillingService(cfg, nil)
	concurrency := service.NewConcurrencyService(&fakeConcurrencyCache{})
	account := &service.Account{ID: 8824, Platform: service.PlatformOpenAI, Type: service.AccountTypeAPIKey,
		Credentials: map[string]any{"api_key": "sk-test", "base_url": "https://example.com"}, Extra: map[string]any{"openai_responses_supported": true}}
	snapshot := service.NewSchedulerSnapshotService(&fakeSchedulerCache{accounts: []*service.Account{account}}, nil, nil, nil, cfg)
	gateway := service.NewOpenAIGatewayService(nil, nil, nil, nil, nil, nil, nil, cfg, snapshot, concurrency, billing,
		nil, billingCache, nil, nil, nil, nil, service.NewModelPricingResolver(nil, billing), nil, nil, nil, nil)
	h := &OpenAIGatewayHandler{gatewayService: gateway, billingCacheService: billingCache, apiKeyService: &service.APIKeyService{},
		concurrencyHelper: NewConcurrencyHelper(concurrency, SSEPingFormatClaude, 0)}
	price, explicitPrice := 0.3, 1.6
	group := &service.Group{ID: 8821, Platform: service.PlatformOpenAI, Hydrated: true, Status: service.StatusActive,
		RateMultiplier: 0.5, ForceOpenAIUltrafast: true, AllowImageGeneration: true, AllowMessagesDispatch: true,
		ModelPricing: []service.ChannelModelPricing{{Models: []string{"gpt-image-2", "custom-text"}, BillingMode: service.BillingModePerRequest, PerRequestPrice: &price}, {Models: []string{"gpt-image-1"}, BillingMode: service.BillingModePerRequest, PerRequestPrice: &explicitPrice}}}
	key := &service.APIKey{ID: 8822, GroupID: &group.ID, Group: group, User: &service.User{ID: 8823, Balance: 0.5}}
	return h, key
}

func TestOpenAIGroupUltrafastImageHandlerAdmission(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, tc := range []struct {
		name string
		path string
		body string
		want float64
	}{
		{name: "responses_image_model", path: "/v1/responses", body: `{"model":"gpt-image-2","input":"draw"}`, want: 0.15},
		{name: "responses_explicit_tool_price", path: "/v1/responses", body: `{"model":"gpt-image-2","input":"draw","tools":[{"type":"image_generation","model":"gpt-image-1"}]}`, want: 0.8},
		{name: "chat_with_responses_fields", path: "/v1/chat/completions", body: `{"model":"gpt-image-2","input":"draw","messages":[{"role":"user","content":"draw"}],"max_tokens":128}`, want: 0},
		{name: "messages_with_responses_fields", path: "/v1/messages", body: `{"model":"gpt-image-2","input":"draw","messages":[{"role":"user","content":"draw"}],"max_tokens":128}`, want: 1.2},
		{name: "responses_image_prompt", path: "/v1/responses", body: `{"model":"gpt-image-2","prompt":"draw a cat"}`, want: 0.15},
		{name: "responses_duplicate_prompt", path: "/v1/responses", body: `{"model":"gpt-image-2","prompt":"draw","prompt":"other"}`, want: 1.2},
		{name: "responses_image_model_forced_choice", path: "/v1/responses", body: `{"model":"gpt-image-2","input":"draw","tool_choice":{"type":"image_generation"}}`, want: 0.15},
		{name: "responses_image_model_none_choice", path: "/v1/responses", body: `{"model":"gpt-image-2","input":"draw","tool_choice":"none"}`, want: 1.2},
		{name: "responses_image_model_auto_choice", path: "/v1/responses", body: `{"model":"gpt-image-2","input":"draw","tool_choice":"auto"}`, want: 1.2},
		{name: "responses_image_model_null_choice", path: "/v1/responses", body: `{"model":"gpt-image-2","input":"draw","tool_choice":null}`, want: 1.2},
		{name: "responses_image_duplicate_model", path: "/v1/responses", body: `{"model":"gpt-image-2","input":"draw","model":"custom-text"}`, want: 1.2},
		{name: "responses_image_duplicate_input", path: "/v1/responses", body: `{"model":"gpt-image-2","input":"draw","input":"hello"}`, want: 1.2},
		{name: "responses_image_duplicate_choice", path: "/v1/responses", body: `{"model":"gpt-image-2","input":"draw","tool_choice":{"type":"image_generation"},"tool_choice":"none"}`, want: 1.2},
		{name: "responses_forced_image_tool", path: "/v1/responses", body: `{"model":"custom-text","input":"draw","tools":[{"type":"image_generation"}],"tool_choice":{"type":"image_generation"}}`, want: 1.2},
		{name: "chat_forced_image_tool", path: "/v1/chat/completions", body: `{"model":"custom-text","messages":[{"role":"user","content":"draw"}],"tools":[{"type":"image_generation"}],"tool_choice":{"type":"image_generation"}}`, want: 1.2},
		{name: "messages_image_model", path: "/v1/messages", body: `{"model":"gpt-image-2","messages":[{"role":"user","content":"draw"}],"max_tokens":128}`, want: 1.2},
		{name: "responses_optional_image_tool", path: "/v1/responses", body: `{"model":"custom-text","input":"hello","tools":[{"type":"image_generation"}],"tool_choice":"auto"}`, want: 1.2},
		{name: "chat_text", path: "/v1/chat/completions", body: `{"model":"custom-text","messages":[{"role":"user","content":"hello"}]}`, want: 1.2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cache := &ultrafastEndpointAdmissionCache{handlerInflightCache: newHandlerInflightCache(0.5)}
			h, key := newUltrafastImageAdmissionFixture(t, cache)
			rec := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(rec)
			c.Request = httptest.NewRequest(http.MethodPost, tc.path, strings.NewReader(tc.body))
			c.Request.Header.Set("Content-Type", "application/json")
			c.Set(string(middleware.ContextKeyAPIKey), key)
			c.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: key.User.ID, Concurrency: 10})
			switch tc.path {
			case "/v1/messages":
				h.Messages(c)
			case "/v1/chat/completions":
				h.ChatCompletions(c)
			default:
				h.Responses(c)
			}
			if tc.name == "chat_with_responses_fields" {
				require.Equal(t, http.StatusBadRequest, rec.Code)
				require.Contains(t, rec.Body.String(), "This model is not supported on the Chat Completions endpoint")
			}
			require.InDelta(t, tc.want, cache.estimated, 1e-12, "handler must pass the request classification to admission: %s", rec.Body.String())
		})
	}
}

func TestOpenAIGroupUltrafastImageOverlappingReservations(t *testing.T) {
	for _, tc := range []struct {
		name     string
		model    string
		body     string
		want     float64
		admitTwo bool
	}{
		{name: "image_model", model: "gpt-image-2", body: `{"model":"gpt-image-2","input":"draw"}`, want: 0.15, admitTwo: true},
		{name: "image_model_forced_choice", model: "gpt-image-2", body: `{"model":"gpt-image-2","input":"draw","tool_choice":{"type":"image_generation"}}`, want: 0.15, admitTwo: true},
		{name: "forced_image_tool", model: "custom-text", body: `{"model":"custom-text","input":"draw","tools":[{"type":"image_generation"}],"tool_choice":{"type":"image_generation"}}`, want: 1.2},
		{name: "messages_image_model", model: "gpt-image-2", body: `{"model":"gpt-image-2","messages":[{"role":"user","content":"draw"}]}`, want: 1.2},
		{name: "text", model: "custom-text", body: `{"model":"custom-text","input":"hello"}`, want: 1.2},
		{name: "optional_image_tool", model: "custom-text", body: `{"model":"custom-text","tools":[{"type":"image_generation"}],"tool_choice":"auto"}`, want: 1.2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cache := newHandlerInflightCache(0.5)
			h, key := newUltrafastImageAdmissionFixture(t, cache)
			req := responsesInflightEstimate(tc.model, []byte(tc.body))
			estimate, priced := h.gatewayService.EstimateInflightReservation(context.Background(), key, req)
			require.True(t, priced)
			require.InDelta(t, tc.want, estimate, 1e-12)
			firstDone, err := reserveInflightBalance(newInflightTestGinContext(), h.billingCacheService, h.gatewayService, key, nil, req)
			require.NoError(t, err)
			defer firstDone()
			secondDone, err := reserveInflightBalance(newInflightTestGinContext(), h.billingCacheService, h.gatewayService, key, nil, req)
			if tc.admitTwo {
				require.NoError(t, err, "balance 0.5 must cover overlapping 0.15 image requests")
				defer secondDone()
				require.Equal(t, 2, cache.count())
			} else {
				require.ErrorIs(t, err, service.ErrInsufficientBalance, "text still reserves Standard times eight")
				require.Equal(t, 1, cache.count())
			}
		})
	}
}
