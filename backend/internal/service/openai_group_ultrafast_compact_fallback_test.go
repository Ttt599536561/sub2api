package service

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/ctxkey"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestForward_GroupUltrafastCompactFallbackReappliesTierForActualModel(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, passthrough := range []bool{false, true} {
		mode := "native"
		if passthrough {
			mode = "passthrough"
		}
		for _, tc := range []struct {
			name          string
			requestModel  string
			fallbackModel string
			inputTier     string
			wantTier      string
			policyAction  string
			groupDisabled bool
			wantBlocked   bool
		}{
			{name: "non_target_to_astra", requestModel: "gpt-5.5", fallbackModel: "gpt-6-astra", wantTier: OpenAIFastTierUltrafast},
			{name: "flex_to_sol", requestModel: "gpt-5.5", fallbackModel: "gpt-6.1-sol", inputTier: OpenAIFastTierFlex, wantTier: OpenAIFastTierUltrafast},
			{name: "target_to_non_target_restores_missing", requestModel: "gpt-6-astra", fallbackModel: "gpt-5.4"},
			{name: "target_to_non_target_restores_flex", requestModel: "gpt-6.1-sol", fallbackModel: "gpt-5.4", inputTier: OpenAIFastTierFlex, wantTier: OpenAIFastTierFlex},
			{name: "explicit_ultrafast_is_preserved", requestModel: "gpt-6-astra", fallbackModel: "gpt-5.4", inputTier: OpenAIFastTierUltrafast, wantTier: OpenAIFastTierUltrafast},
			{name: "target_to_target", requestModel: "gpt-6-astra", fallbackModel: "gpt-6.1-sol", wantTier: OpenAIFastTierUltrafast},
			{name: "global_filter_cannot_downgrade_fallback", requestModel: "gpt-5.5", fallbackModel: "gpt-6-astra", wantTier: OpenAIFastTierUltrafast, policyAction: BetaPolicyActionFilter},
			{name: "global_force_priority_cannot_downgrade_fallback", requestModel: "gpt-5.5", fallbackModel: "gpt-6-astra", wantTier: OpenAIFastTierUltrafast, policyAction: OpenAIFastPolicyActionForcePriority},
			{name: "global_block_rejects_fallback_before_upstream", requestModel: "gpt-5.5", fallbackModel: "gpt-6-astra", policyAction: BetaPolicyActionBlock, wantBlocked: true},
			{name: "disabled_group_retains_existing_behavior", requestModel: "gpt-5.5", fallbackModel: "gpt-6-astra", inputTier: OpenAIFastTierPriority, wantTier: OpenAIFastTierPriority, groupDisabled: true},
		} {
			t.Run(mode+"/"+tc.name, func(t *testing.T) {
				request := map[string]any{
					"model": tc.requestModel, "stream": false, "instructions": "compact fallback tier test",
					"input": []any{map[string]any{"type": "message", "role": "user", "content": "hello"}, map[string]any{"type": "compaction_trigger"}},
				}
				if tc.inputTier != "" {
					request["service_tier"] = tc.inputTier
				}
				body, err := json.Marshal(request)
				require.NoError(t, err)
				c := newOpenAICompactFallbackTestContext(t, "/v1/responses")
				c.Request.Body = io.NopCloser(bytes.NewReader(body))
				c.Request.Header.Set("Content-Type", "application/json")
				MarkOpenAINativeCompactionV2(c)
				groupCtx := context.WithValue(context.Background(), ctxkey.Group, &Group{
					ID: 81, Platform: PlatformOpenAI, Status: StatusActive, Hydrated: true,
					ForceOpenAIUltrafast: !tc.groupDisabled,
				})
				c.Request = c.Request.WithContext(groupCtx)
				upstream := &httpUpstreamRecorder{responses: []*http.Response{
					{
						StatusCode: http.StatusBadRequest,
						Header:     http.Header{"Content-Type": []string{"application/json"}},
						Body:       io.NopCloser(strings.NewReader(`{"error":{"code":"context_length_exceeded","message":"context window exceeded"}}`)),
					},
					{
						StatusCode: http.StatusOK,
						Header:     http.Header{"Content-Type": []string{"application/json"}},
						Body:       io.NopCloser(strings.NewReader(`{"id":"resp_compact_tier","status":"completed","model":"` + tc.fallbackModel + `","output":[],"usage":{"input_tokens":1,"output_tokens":1}}`)),
					},
				}}
				svc := &OpenAIGatewayService{
					cfg:          &config.Config{Gateway: config.GatewayConfig{OpenAICompactModel: tc.fallbackModel}},
					httpUpstream: upstream,
				}
				if tc.policyAction != "" {
					policySvc := newOpenAIGatewayServiceWithSettings(t, &OpenAIFastPolicySettings{Rules: []OpenAIFastPolicyRule{{
						ServiceTier: OpenAIFastTierUltrafast, Action: tc.policyAction, Scope: BetaPolicyScopeAll,
					}}})
					svc.settingService = policySvc.settingService
				}
				account := &Account{
					ID: 81, Name: "compact-tier-key", Platform: PlatformOpenAI, Type: AccountTypeAPIKey,
					Concurrency: 1, Credentials: map[string]any{"api_key": "sk-test"},
					Extra:  map[string]any{"openai_responses_supported": true, "openai_passthrough": passthrough},
					Status: StatusActive, Schedulable: true,
				}

				result, err := svc.Forward(groupCtx, c, account, body)

				if tc.wantBlocked {
					var blocked *OpenAIFastBlockedError
					require.ErrorAs(t, err, &blocked)
					require.Nil(t, result)
					require.Len(t, upstream.bodies, 1, "a denied fallback must never reach upstream")
					return
				}
				require.NoError(t, err)
				require.NotNil(t, result)
				require.Len(t, upstream.bodies, 2)
				require.Equal(t, tc.fallbackModel, gjson.GetBytes(upstream.bodies[1], "model").String())
				require.Equal(t, tc.wantTier, gjson.GetBytes(upstream.bodies[1], "service_tier").String())
				if tc.wantTier == "" {
					require.False(t, gjson.GetBytes(upstream.bodies[1], "service_tier").Exists())
					require.Nil(t, result.ServiceTier)
				} else {
					require.NotNil(t, result.ServiceTier)
					require.Equal(t, tc.wantTier, *result.ServiceTier)
				}
			})
		}
	}
}
