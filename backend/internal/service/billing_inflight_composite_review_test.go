//go:build unit

package service

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestReviewInflightCompositeRouteUsesConcreteModel(t *testing.T) {
	groupID := int64(703)
	apiKey := &APIKey{User: &User{ID: 7}, GroupID: &groupID, Group: &Group{ID: groupID, Platform: PlatformComposite, RateMultiplier: 1}}
	gateway := newInflightEstimateGateway(t, nil)
	openai := &OpenAIGatewayService{cfg: gateway.cfg, billingService: gateway.billingService, resolver: gateway.resolver}
	ctx := WithCompositeRouteDecision(context.Background(), CompositeRouteDecision{
		Matched: true, GroupID: groupID, PublicModel: "public-alias", TargetPlatform: PlatformOpenAI, UpstreamModel: "gpt-5.4", Source: CompositeRouteSourceExplicit,
	})
	req := InflightEstimateRequest{Model: "public-alias", BodyBytes: 4000, MaxTokens: 1000}
	direct := req
	direct.Model = "gpt-5.4"
	expected, priced := gateway.EstimateInflightReservation(ctx, apiKey, direct)
	require.True(t, priced)
	require.Positive(t, expected)
	for name, estimate := range map[string]func(context.Context, *APIKey, InflightEstimateRequest) (float64, bool){
		"gateway": gateway.EstimateInflightReservation,
		"openai":  openai.EstimateInflightReservation,
	} {
		t.Run(name, func(t *testing.T) {
			got, ok := estimate(ctx, apiKey, req)
			require.True(t, ok, "a resolved, billable composite alias must not bypass reservations or fail closed as unpriced")
			require.InDelta(t, expected, got, 1e-12)
		})
	}
}

func TestReviewInflightCompositeRouteUsesChannelMapping(t *testing.T) {
	groupID := int64(704)
	cs := newTestChannelService(makeStandardRepo(Channel{
		ID: 1, Status: StatusActive, GroupIDs: []int64{groupID},
		ModelMapping: map[string]map[string]string{"openai": {"route-target": "gpt-5.4"}},
	}, map[int64]string{groupID: PlatformComposite}))
	gateway := newInflightEstimateGateway(t, cs)
	apiKey := &APIKey{User: &User{ID: 7}, GroupID: &groupID, Group: &Group{ID: groupID, Platform: PlatformComposite, RateMultiplier: 1}}
	ctx := WithCompositeRouteDecision(context.Background(), CompositeRouteDecision{
		Matched: true, GroupID: groupID, PublicModel: "public-alias", TargetPlatform: PlatformOpenAI, UpstreamModel: "route-target", Source: CompositeRouteSourceExplicit,
	})
	req := InflightEstimateRequest{Model: "public-alias", BodyBytes: 4000, MaxTokens: 1000}
	got, ok := gateway.EstimateInflightReservation(ctx, apiKey, req)
	require.True(t, ok)
	req.Model = "gpt-5.4"
	expected, priced := gateway.EstimateInflightReservation(context.Background(), apiKey, req)
	require.True(t, priced)
	require.InDelta(t, expected, got, 1e-12)
}

func TestReviewInflightOpenAICompositeMappingUsesTargetPlatform(t *testing.T) {
	groupID := int64(705)
	gateway := newInflightEstimateGateway(t, nil)
	snap := &inflightSnapshotCacheStub{byBucket: map[string][]Account{
		inflightBucketKey(groupID, PlatformGrok):   {{ID: 7, Platform: PlatformGrok, Credentials: map[string]any{"model_mapping": map[string]any{"route-target": "grok-4.3"}}}},
		inflightBucketKey(groupID, PlatformOpenAI): {{ID: 8, Platform: PlatformOpenAI, Credentials: map[string]any{"model_mapping": map[string]any{"route-target": "gpt-5.4-pro"}}}},
	}}
	attachInflightSnapshot(gateway, snap)
	openai := &OpenAIGatewayService{cfg: gateway.cfg, billingService: gateway.billingService, resolver: gateway.resolver, schedulerSnapshot: gateway.schedulerSnapshot}
	apiKey := &APIKey{User: &User{ID: 7}, GroupID: &groupID, Group: &Group{ID: groupID, Platform: PlatformComposite, RateMultiplier: 1}}
	ctx := WithCompositeRouteDecision(context.Background(), CompositeRouteDecision{
		Matched: true, GroupID: groupID, PublicModel: "public-alias", TargetPlatform: PlatformGrok, UpstreamModel: "route-target", Source: CompositeRouteSourceExplicit,
	})
	req := InflightEstimateRequest{Model: "public-alias", BodyBytes: 4000, MaxTokens: 1000}
	got, ok := openai.EstimateInflightReservation(ctx, apiKey, req)
	require.True(t, ok)
	req.Model = "grok-4.3"
	expected, priced := openai.EstimateInflightReservation(context.Background(), apiKey, req)
	require.True(t, priced)
	require.InDelta(t, expected, got, 1e-12, "the unrelated OpenAI pool must not price a Grok route")
}

func TestReviewInflightCompositeRouteDoesNotReuseUnrelatedDecision(t *testing.T) {
	groupID := int64(706)
	gateway := newInflightEstimateGateway(t, nil)
	apiKey := &APIKey{User: &User{ID: 7}, GroupID: &groupID, Group: &Group{ID: groupID, Platform: PlatformComposite, RateMultiplier: 1}}
	req := InflightEstimateRequest{Model: "public-alias", BodyBytes: 4000, MaxTokens: 1000}
	_, priced := gateway.EstimateInflightReservation(context.Background(), apiKey, req)
	require.False(t, priced, "estimation must not invent a route for an unresolved alias")
	ctx := WithCompositeRouteDecision(context.Background(), CompositeRouteDecision{
		Matched: true, PublicModel: "another-alias", TargetPlatform: PlatformOpenAI, UpstreamModel: "gpt-5.4",
	})
	_, priced = gateway.EstimateInflightReservation(ctx, apiKey, req)
	require.False(t, priced, "a different model must not inherit an earlier route")
	ctx = WithCompositeRouteDecision(context.Background(), CompositeRouteDecision{
		Matched: true, PublicModel: req.Model, TargetPlatform: PlatformOpenAI, UpstreamModel: "gpt-5.4",
	})
	apiKey.Group.Platform = PlatformAnthropic
	_, priced = gateway.EstimateInflightReservation(ctx, apiKey, req)
	require.False(t, priced, "a non-composite fallback group must not inherit the original route")
}
