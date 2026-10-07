package handler

import (
	"context"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestReviewInflightFreeMediaStrictAdmission(t *testing.T) {
	cfg := &config.Config{}
	cfg.Billing.InflightReservation = config.InflightReservationConfig{Enabled: true, FailClosedOnUnpriced: true}
	cache := newHandlerInflightCache(10)
	billingCache := service.NewBillingCacheService(cache, nil, nil, nil, nil, nil, cfg, nil)
	t.Cleanup(billingCache.Stop)
	billing := service.NewBillingService(cfg, nil)
	gateway := service.NewGatewayService(
		nil, nil, nil, nil, nil, nil, nil, nil, cfg, nil, nil, billing, nil, nil,
		nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, service.NewModelPricingResolver(nil, billing), nil, nil, nil,
	)
	zero := 0.0
	group := &service.Group{
		ID: 100, Platform: service.PlatformGrok, RateMultiplier: 1,
		AudioTTSPricePerMillionChars: &zero, AudioSTTPricePerHour: &zero,
		AudioRealtimePricePerMin: &zero, SearchPricePer1k: &zero,
		VideoPrice480P: &zero, ImagePrice1K: &zero, ImagePrice2K: &zero, ImagePrice4K: &zero,
	}
	key := &service.APIKey{User: &service.User{ID: 7}, GroupID: &group.ID, Group: group}
	for _, tc := range []struct {
		name    string
		request service.InflightEstimateRequest
		reject  bool
	}{
		{"free tts", grokVoiceInflightEstimate("tts", []byte(`{"input":"hello"}`)), false},
		{"free stt", grokVoiceInflightEstimate("stt", []byte("audio content")), false},
		{"free realtime", service.InflightEstimateRequest{Model: "grok-voice", Kind: service.InflightEstimateAudio, AudioMode: "realtime", AudioUnits: 1}, false},
		{"free search", service.InflightEstimateRequest{Model: "grok-web-search", Kind: service.InflightEstimatePerRequest, SearchCalls: 1}, false},
		{"free video", service.InflightEstimateRequest{Model: "grok-imagine-video", Kind: service.InflightEstimateVideo, VideoResolution: "480p", VideoDurationSeconds: 5}, false},
		{"free image", service.InflightEstimateRequest{Model: "grok-imagine-image", Kind: service.InflightEstimateImage, Units: 1}, false},
		{"unknown token", service.InflightEstimateRequest{Model: "unknown-review-model", Kind: service.InflightEstimateToken, BodyBytes: 40, MaxTokens: 10}, true},
		{"unknown token with free search", service.InflightEstimateRequest{Model: "unknown-review-model", Kind: service.InflightEstimateToken, BodyBytes: 40, MaxTokens: 10, SearchCalls: 1}, true},
		{"unknown audio mode", service.InflightEstimateRequest{Model: "unknown-review-model", Kind: service.InflightEstimateAudio, AudioMode: "unknown", AudioUnits: 1}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, done, err := reserveInflightBalanceCtx(context.Background(), billingCache, gateway, key, nil, tc.request)
			defer done()
			if tc.reject {
				require.ErrorIs(t, err, service.ErrInsufficientBalance)
			} else {
				require.NoError(t, err, "explicit free requests must pass strict unpriced admission")
				require.Nil(t, service.InflightReservationFromContext(ctx))
			}
			require.Zero(t, cache.count())
		})
	}
}
