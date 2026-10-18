//go:build unit

package handler

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type inflightCompactionMetadataEstimator struct {
	ctx      context.Context
	apiKey   *service.APIKey
	requests []service.InflightEstimateRequest
}

func (e *inflightCompactionMetadataEstimator) EstimateInflightReservation(ctx context.Context, apiKey *service.APIKey, req service.InflightEstimateRequest) (float64, bool) {
	e.ctx = ctx
	e.apiKey = apiKey
	e.requests = append(e.requests, req)
	return 0, true
}

func newInflightCompactionMetadataBilling(t *testing.T) *service.BillingCacheService {
	t.Helper()
	cfg := &config.Config{}
	cfg.Billing.InflightReservation.Enabled = true
	billing := service.NewBillingCacheService(nil, nil, nil, nil, nil, nil, cfg, nil)
	t.Cleanup(billing.Stop)
	return billing
}

func TestReserveInflightBalance_OpenAICompactionMetadata(t *testing.T) {
	previousMode := gin.Mode()
	gin.SetMode(gin.TestMode)
	t.Cleanup(func() { gin.SetMode(previousMode) })

	for _, tc := range []struct {
		name         string
		path         string
		body         string
		wantCompact  bool
		wantFallback bool
	}{
		{name: "legacy_compact", path: "/v1/responses/compact", body: `{"model":"gpt-6-astra","max_output_tokens":37,"input":"hello"}`, wantCompact: true},
		{name: "legacy_compact_without_v1", path: "/responses/compact", body: `{"model":"gpt-6-astra","max_output_tokens":37,"input":"hello"}`, wantCompact: true},
		{name: "legacy_compact_subpath", path: "/v1/responses/compact/status", body: `{"model":"gpt-6-astra","max_output_tokens":37,"input":"hello"}`, wantCompact: true},
		{name: "ordinary_responses", path: "/v1/responses", body: `{"model":"gpt-6-astra","max_output_tokens":37,"input":"hello"}`},
		{name: "native_compaction_trigger", path: "/v1/responses", body: `{"model":"gpt-6-astra","max_output_tokens":37,"input":[{"type":"compaction_trigger"}]}`, wantFallback: true},
		{name: "legacy_path_with_trigger", path: "/v1/responses/compact", body: `{"model":"gpt-6-astra","max_output_tokens":37,"input":[{"type":"compaction_trigger"}]}`, wantCompact: true, wantFallback: true},
		{name: "trigger_text_is_not_an_input_item", path: "/v1/responses", body: `{"model":"gpt-6-astra","max_output_tokens":37,"input":"compaction_trigger"}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			billing := newInflightCompactionMetadataBilling(t)
			estimator := &inflightCompactionMetadataEstimator{}
			key := &service.APIKey{User: &service.User{ID: 1}}
			router := gin.New()
			router.POST(tc.path, func(c *gin.Context) {
				body, err := io.ReadAll(c.Request.Body)
				require.NoError(t, err)
				done, err := reserveInflightBalance(c, billing, estimator, key, nil, tokenInflightEstimate("gpt-6-astra", body))
				require.NoError(t, err)
				defer done()
				c.Status(http.StatusNoContent)
			})
			request := httptest.NewRequest(http.MethodPost, tc.path, strings.NewReader(tc.body))
			request.Header.Set("Content-Type", "application/json")
			recorder := httptest.NewRecorder()
			router.ServeHTTP(recorder, request)

			require.Equal(t, http.StatusNoContent, recorder.Code)
			require.Len(t, estimator.requests, 1)
			require.Same(t, key, estimator.apiKey)
			require.Equal(t, service.InflightEstimateRequest{
				Model: "gpt-6-astra", BodyBytes: len(tc.body), MaxTokens: 37, Kind: service.InflightEstimateToken,
				OpenAICompactPath: tc.wantCompact, OpenAICompactionFallback: tc.wantFallback,
			}, estimator.requests[0])
		})
	}
}

func TestReserveInflightBalanceCtx_OpenAICompactionMetadata(t *testing.T) {
	for _, tc := range []struct {
		name         string
		body         string
		compactPath  bool
		wantFallback bool
	}{
		{name: "ws_body_without_trigger", body: `{"type":"response.create","model":"gpt-6-astra","max_output_tokens":47,"input":"hello"}`},
		{name: "ws_body_with_trigger", body: `{"type":"response.create","model":"gpt-6-astra","max_output_tokens":47,"input":[{"type":"compaction_trigger"}]}`, wantFallback: true},
		{name: "caller_supplied_compact_path_is_preserved", body: `{"type":"response.create","model":"gpt-6-astra","max_output_tokens":47,"input":"hello"}`, compactPath: true},
		{name: "caller_supplied_path_and_body_signal_are_independent", body: `{"type":"response.create","model":"gpt-6-astra","max_output_tokens":47,"input":[{"type":"compaction_trigger"}]}`, compactPath: true, wantFallback: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			billing := newInflightCompactionMetadataBilling(t)
			estimator := &inflightCompactionMetadataEstimator{}
			key := &service.APIKey{User: &service.User{ID: 1}}
			type contextKey struct{}
			ctx := context.WithValue(context.Background(), contextKey{}, tc.name)
			req := tokenInflightEstimate("gpt-6-astra", []byte(tc.body))
			req.OpenAICompactPath = tc.compactPath
			req.SearchCalls = 2
			nextCtx, done, err := reserveInflightBalanceCtx(ctx, billing, estimator, key, nil, req)
			require.NoError(t, err)
			defer done()

			require.Same(t, ctx, nextCtx, "a zero-cost estimate must retain the caller's context")
			require.Same(t, ctx, estimator.ctx)
			require.Same(t, key, estimator.apiKey)
			require.Len(t, estimator.requests, 1)
			require.Equal(t, service.InflightEstimateRequest{
				Model: "gpt-6-astra", BodyBytes: len(tc.body), MaxTokens: 47, Kind: service.InflightEstimateToken, SearchCalls: 2,
				OpenAICompactPath: tc.compactPath, OpenAICompactionFallback: tc.wantFallback,
			}, estimator.requests[0])
		})
	}
}

func TestReserveInflightBalance_OpenAICompactionMetadataPreservesRequest(t *testing.T) {
	previousMode := gin.Mode()
	gin.SetMode(gin.TestMode)
	t.Cleanup(func() { gin.SetMode(previousMode) })

	for _, tc := range []struct {
		name        string
		path        string
		wantCompact bool
	}{
		{name: "actual_legacy_path_overrides_false", path: "/v1/responses/compact", wantCompact: true},
		{name: "ordinary_path_clears_stale_true", path: "/v1/responses"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			billing := newInflightCompactionMetadataBilling(t)
			estimator := &inflightCompactionMetadataEstimator{}
			key := &service.APIKey{User: &service.User{ID: 1}}
			type contextKey struct{}
			ctx := context.WithValue(context.Background(), contextKey{}, tc.name)
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest(http.MethodPost, tc.path, nil).WithContext(ctx)
			req := service.InflightEstimateRequest{
				Model: "preserved-model", BodyBytes: 123, MaxTokens: 456, Kind: service.InflightEstimateVideo,
				Units: 2, SearchCalls: 3, VideoResolution: "720p", VideoDurationSeconds: 4,
				AudioMode: "tts", AudioUnits: 5.5,
				OpenAICompactPath: !tc.wantCompact, OpenAICompactionFallback: true,
			}
			original := req
			want := req
			want.OpenAICompactPath = tc.wantCompact
			done, err := reserveInflightBalance(c, billing, estimator, key, nil, req)
			require.NoError(t, err)
			defer done()

			require.Len(t, estimator.requests, 1)
			require.Equal(t, want, estimator.requests[0], "Gin may change only the path-derived field")
			require.Equal(t, original, req, "the caller's request value must not be mutated")
			require.Same(t, ctx, estimator.ctx)
			require.Same(t, ctx, c.Request.Context())
		})
	}
}
