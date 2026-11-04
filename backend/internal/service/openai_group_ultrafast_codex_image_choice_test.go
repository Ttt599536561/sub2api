//go:build unit

package service

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/pkg/ctxkey"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestGroupUltrafastCodexBridgePreservesImageOnlyChoice(t *testing.T) {
	for _, tc := range []struct {
		name, model, choice string
		wantImage           bool
	}{
		{name: "image_default_forces_generation", model: "gpt-image-2", wantImage: true},
		{name: "image_explicit_generation", model: "gpt-image-2", choice: `{"type":"image_generation"}`, wantImage: true},
		{name: "image_explicit_auto_can_return_text", model: "gpt-image-2", choice: `"auto"`},
		{name: "image_explicit_none_can_return_text", model: "gpt-image-2", choice: `"none"`},
		{name: "text_default_keeps_optional_tool", model: "gpt-5.4"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			logs := &openAIRecordUsageLogRepoStub{inserted: true}
			users := &openAIRecordUsageUserRepoStub{}
			svc := newOpenAIRecordUsageServiceForTest(logs, users, &openAIRecordUsageSubRepoStub{}, nil)
			svc.resolver = NewModelPricingResolver(nil, svc.billingService)
			svc.cfg.Gateway.CodexImageGenerationBridgeEnabled = true
			price := 0.3
			group := &Group{ID: 9971, Platform: PlatformOpenAI, Hydrated: true, Status: StatusActive,
				RateMultiplier: 0.5, ForceOpenAIUltrafast: true, AllowImageGeneration: true,
				ModelPricing: []ChannelModelPricing{{Models: []string{tc.model}, BillingMode: BillingModePerRequest, PerRequestPrice: &price}}}
			user := &User{ID: 9972}
			key := &APIKey{ID: 9973, GroupID: &group.ID, Group: group, User: user}
			account := &Account{ID: 9974, Platform: PlatformOpenAI, Type: AccountTypeAPIKey,
				Credentials: map[string]any{"api_key": "sk-test", "base_url": "https://example.com"},
				Extra:       map[string]any{"openai_responses_supported": true}}
			response := `{"id":"text_response","output":[{"type":"message","content":[{"type":"output_text","text":"hello"}]}],"usage":{"input_tokens":1,"output_tokens":2}}`
			if tc.wantImage {
				response = `{"id":"image_response","output":[{"type":"image_generation_call","result":"image-bytes"}],"usage":{"input_tokens":1,"output_tokens":2}}`
			}
			upstream := &httpUpstreamRecorder{resp: &http.Response{StatusCode: http.StatusOK,
				Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(strings.NewReader(response))}}
			svc.httpUpstream = upstream
			snap := &inflightSnapshotCacheStub{byBucket: map[string][]Account{inflightBucketKey(group.ID, PlatformOpenAI): {*account}}}
			svc.schedulerSnapshot = NewSchedulerSnapshotService(snap, nil, &inflightCountingAccountRepo{}, nil, svc.cfg)
			ctx := context.WithValue(context.Background(), ctxkey.Group, group)
			c := newOpenAICompactFallbackTestContext(t, "/v1/responses")
			c.Set("api_key", key)
			c.Request = c.Request.WithContext(ctx)
			c.Request.Header.Set("User-Agent", "codex_cli_rs/0.98.0")
			body := `{"model":"` + tc.model + `","input":"hello"`
			if tc.choice != "" {
				body += `,"tool_choice":` + tc.choice
			}
			body += `}`
			result, err := svc.Forward(ctx, c, account, []byte(body))
			require.NoError(t, err)
			require.True(t, gjson.GetBytes(upstream.lastBody, `tools.#(type=="image_generation")`).Exists())
			require.False(t, isOpenAIImageGenerationModel(gjson.GetBytes(upstream.lastBody, "model").String()))
			if tc.wantImage {
				require.Equal(t, "image_generation", gjson.GetBytes(upstream.lastBody, "tool_choice.type").String())
				require.Equal(t, 1, result.ImageCount)
			} else {
				wantChoice := "auto"
				if tc.choice == `"none"` {
					wantChoice = "none"
				}
				require.Equal(t, wantChoice, gjson.GetBytes(upstream.lastBody, "tool_choice").String())
				require.Zero(t, result.ImageCount)
			}
			require.NoError(t, svc.RecordUsage(ctx, &OpenAIRecordUsageInput{APIKey: key, User: user, Account: account, Result: result, OriginalModel: tc.model}))
			estimate, priced := svc.EstimateInflightReservation(ctx, key, InflightEstimateRequest{Model: tc.model,
				BodyBytes: len(body), MaxTokens: 100, ImageOnlyRequest: imageOnlyRequestForTest(tc.model, []byte(body))})
			require.True(t, priced)
			wantCost := 1.2
			if tc.wantImage {
				wantCost = 0.15
			}
			require.InDelta(t, wantCost, users.lastAmount, 1e-12)
			require.InDelta(t, users.lastAmount, estimate, 1e-12)
		})
	}
}
