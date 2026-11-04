//go:build unit

package service

import (
	"context"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/ctxkey"
	"github.com/stretchr/testify/require"
)

func TestInflightEstimate_GroupUltrafastActualPlatform(t *testing.T) {
	for _, tc := range []struct {
		name, forcePlatform, resolvedPlatform, groupPlatform string
		noGroup, disabled                                    bool
		wantUltrafast                                        bool
	}{
		{name: "openai_group_default", wantUltrafast: true},
		{name: "forced_openai", forcePlatform: PlatformOpenAI, wantUltrafast: true},
		{name: "resolved_openai", resolvedPlatform: PlatformOpenAI, wantUltrafast: true},
		{name: "forced_antigravity", forcePlatform: PlatformAntigravity},
		{name: "resolved_antigravity", resolvedPlatform: PlatformAntigravity},
		{name: "force_precedes_resolved_openai", forcePlatform: PlatformAntigravity, resolvedPlatform: PlatformOpenAI},
		{name: "force_openai_precedes_resolved", forcePlatform: PlatformOpenAI, resolvedPlatform: PlatformAntigravity, wantUltrafast: true},
		{name: "empty_force_uses_resolved", resolvedPlatform: PlatformAntigravity},
		{name: "compatible_platform_is_not_openai", resolvedPlatform: PlatformGrok},
		{name: "force_requires_exact_platform", forcePlatform: "OPENAI"},
		{name: "resolved_requires_exact_platform", resolvedPlatform: "OPENAI"},
		{name: "other_group_cannot_force_ultrafast", groupPlatform: PlatformAnthropic, forcePlatform: PlatformOpenAI},
		{name: "composite_group_cannot_force_ultrafast", groupPlatform: PlatformComposite, resolvedPlatform: PlatformOpenAI},
		{name: "disabled_group", disabled: true, forcePlatform: PlatformOpenAI},
		{name: "no_group", noGroup: true},
		{name: "no_group_forced_openai", noGroup: true, forcePlatform: PlatformOpenAI},
		{name: "no_group_resolved_openai", noGroup: true, resolvedPlatform: PlatformOpenAI},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			if tc.forcePlatform != "" || tc.name == "empty_force_uses_resolved" {
				ctx = context.WithValue(ctx, ctxkey.ForcePlatform, tc.forcePlatform)
			}
			ctx = WithResolvedTargetPlatform(ctx, tc.resolvedPlatform)
			gateway := newInflightEstimateGateway(t, nil)
			openai := &OpenAIGatewayService{cfg: gateway.cfg, billingService: gateway.billingService, resolver: gateway.resolver}
			model := "gpt-5.4"
			price := 0.001
			platform := tc.groupPlatform
			if platform == "" {
				platform = PlatformOpenAI
			}
			group := &Group{
				ID: 887, Platform: platform, RateMultiplier: 1, ForceOpenAIUltrafast: !tc.disabled,
				ModelPricing: []ChannelModelPricing{{Models: []string{model}, BillingMode: BillingModeToken, InputPrice: &price, OutputPrice: &price}},
			}
			key := &APIKey{GroupID: &group.ID, Group: group, User: &User{ID: 2887}}
			want := 0.2
			if tc.noGroup {
				key.GroupID, key.Group = nil, nil
				standard, priced := EstimateInflightReservationCost(gateway.billingService, gateway.cfg.Billing.InflightReservation, model, 400, 100, 1)
				require.True(t, priced)
				want = standard
			}
			if tc.wantUltrafast {
				want *= 8
			}
			for name, estimate := range map[string]func(context.Context, *APIKey, InflightEstimateRequest) (float64, bool){
				"gateway": gateway.EstimateInflightReservation,
				"openai":  openai.EstimateInflightReservation,
			} {
				t.Run(name, func(t *testing.T) {
					got, priced := estimate(ctx, key, InflightEstimateRequest{Model: model, Kind: InflightEstimateToken, BodyBytes: 400, MaxTokens: 100})
					require.True(t, priced)
					require.InDelta(t, want, got, 1e-12)
				})
			}
		})
	}
}

func TestInflightEstimate_GroupUltrafastAntigravityMatchesSettlementAndAdmission(t *testing.T) {
	for _, forced := range []bool{false, true} {
		name := "resolved_target_platform"
		ctx := WithResolvedTargetPlatform(context.Background(), PlatformAntigravity)
		if forced {
			name = "force_platform"
			ctx = context.WithValue(context.Background(), ctxkey.ForcePlatform, PlatformAntigravity)
		}
		t.Run(name, func(t *testing.T) {
			gateway := newInflightEstimateGateway(t, nil)
			const model = "claude-sonnet-4-5"
			price := 0.001
			group := &Group{
				ID: 888, Platform: PlatformOpenAI, RateMultiplier: 1, ForceOpenAIUltrafast: true,
				ModelPricing: []ChannelModelPricing{{Models: []string{model}, BillingMode: BillingModeToken, InputPrice: &price, OutputPrice: &price}},
			}
			user := &User{ID: 2888}
			key := &APIKey{GroupID: &group.ID, Group: group, User: user}
			settled := gateway.calculateRecordUsageCost(ctx, &ForwardResult{
				Model: model, Usage: ClaudeUsage{InputTokens: 100, OutputTokens: 100},
			}, key, model, 1, 1, time.Time{})
			require.NotNil(t, settled)
			require.InDelta(t, 0.2, settled.ActualCost, 1e-12)
			estimate, priced := gateway.EstimateInflightReservation(ctx, key, InflightEstimateRequest{
				Model: model, Kind: InflightEstimateToken, BodyBytes: 400, MaxTokens: 100,
			})
			require.True(t, priced)
			t.Run("quotation", func(t *testing.T) {
				require.InDelta(t, settled.ActualCost, estimate, 1e-12)
			})
			t.Run("concurrent_admission", func(t *testing.T) {
				cache := newMemInflightCache(0.5)
				reservationSvc := NewBillingCacheService(cache, nil, nil, nil, nil, nil, gateway.cfg, nil)
				t.Cleanup(reservationSvc.Stop)
				first, err := reservationSvc.ReserveInflight(ctx, user, group, nil, estimate)
				require.NoError(t, err)
				require.NotNil(t, first)
				defer first.HandlerDone()
				second, err := reservationSvc.ReserveInflight(ctx, user, group, nil, estimate)
				if second != nil {
					defer second.HandlerDone()
				}
				require.NoError(t, err, "two actual $0.20 requests must fit a $0.50 balance")
				require.NotNil(t, second)
			})
		})
	}
}
