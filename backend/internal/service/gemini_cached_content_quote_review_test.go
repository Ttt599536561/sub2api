//go:build unit

package service

import (
	"context"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestMainAuditCachedChargeKeepsQuotedPricing(t *testing.T) {
	svc, logs := newGeminiCacheBillingGatewayForTest(t)
	gid := int64(33)
	key := &APIKey{ID: 821, GroupID: &gid, User: &User{ID: 621}, Group: &Group{ID: gid, Platform: PlatformGemini, RateMultiplier: 1, Hydrated: true}}
	input := &RecordUsageInput{Result: &ForwardResult{RequestID: GeminiCachedContentPatchUsageRequestID("audit"), Model: "gemini-3.8-flash", CacheStorageTokenHours: 20000}, APIKey: key, User: key.User, Account: &Account{ID: 713, Platform: PlatformGemini}}
	quoted := svc.QuoteUsageCost(context.Background(), input)
	pricing := svc.billingService.pricingService
	pricing.mu.Lock()
	price := *pricing.pricingData["gemini-3.8-flash"]
	price.CacheStorageCostPerTokenPerHour *= 10
	pricing.pricingData["gemini-3.8-flash"] = &price
	pricing.mu.Unlock()
	require.NoError(t, svc.RecordUsage(context.Background(), input))
	require.InDelta(t, quoted, logs.lastLog.ActualCost, 1e-12, "settlement must keep the exact storage quote reserved before upstream latency or worker queuing")
}

func TestMainAuditCachedQuoteCanReduceStorageWithoutChangingInput(t *testing.T) {
	svc, logs := newGeminiCacheBillingGatewayForTest(t)
	gid := int64(33)
	key := &APIKey{ID: 821, GroupID: &gid, User: &User{ID: 621}, Group: &Group{ID: gid, Platform: PlatformGemini, RateMultiplier: 2, Hydrated: true}}
	for _, hours := range []float64{0, 10000, 20000} {
		input := &RecordUsageInput{Result: &ForwardResult{RequestID: GeminiCachedContentPatchUsageRequestID("reduced"), Model: "gemini-3.8-flash", Usage: ClaudeUsage{InputTokens: 1000}, CacheStorageTokenHours: 20000}, APIKey: key, User: key.User, Account: &Account{ID: 713, Platform: PlatformGemini}}
		svc.QuoteUsageCost(context.Background(), input)
		original := input.CostQuote
		input.CostQuote = original.WithCacheStorageTokenHours(hours)
		input.Result.CacheStorageTokenHours = hours
		require.NoError(t, svc.RecordUsage(context.Background(), input))
		require.InDelta(t, (1000*0.00000075+hours*0.0000005)*2, logs.lastLog.ActualCost, 1e-12)
		require.Equal(t, 20000.0, original.storageTokenHours)
	}
}
