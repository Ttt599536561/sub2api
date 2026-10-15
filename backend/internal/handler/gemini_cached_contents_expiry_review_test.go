//go:build unit

package handler

import (
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestGeminiCachedContentsCreate_ActualExpiry(t *testing.T) {
	for _, tc := range []struct {
		name        string
		balance     float64
		upstreamTTL time.Duration
		wantHours   float64
	}{
		{name: "shorter expiry", balance: 500, upstreamTTL: 12 * time.Hour, wantHours: 12},
		{name: "balance covers actual duration only", balance: 60, upstreamTTL: 12 * time.Hour, wantHours: 12},
		{name: "longer expiry capped by requested TTL", balance: 500, upstreamTTL: 100 * time.Hour, wantHours: 24},
		{name: "missing expiry uses requested TTL", balance: 500, wantHours: 24},
		{name: "past expiry never charges storage", balance: 500, upstreamTTL: -time.Minute},
	} {
		t.Run(tc.name, func(t *testing.T) {
			env := newCachedBillingEnv(t, tc.balance, 900)
			expiry := time.Time{}
			if tc.upstreamTTL != 0 {
				expiry = time.Now().Add(tc.upstreamTTL)
			}
			env.upstream.createBody = cachedBillingCreateBody(expiry)
			if tc.upstreamTTL == 0 {
				env.upstream.createBody = `{"name":"cachedContents/up1","usageMetadata":{"totalTokenCount":1000000}}`
			}
			rec := env.create()
			require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
			totals := env.waitForUsage(t, 1)
			expected := cachedBillingTokens*cachedBillingInputPrice + cachedBillingTokens*cachedBillingStoragePrice*tc.wantHours
			require.InDelta(t, expected, totals[0], 0.01, "admission and settlement must charge only delivered storage duration")
			require.Len(t, env.repo.records, 1)
			for _, record := range env.repo.records {
				if tc.upstreamTTL > 0 && tc.upstreamTTL < 24*time.Hour {
					require.Equal(t, expiry.UTC(), record.ExpireTime.UTC())
				}
				require.False(t, record.ExpireTime.After(time.Now().Add(24*time.Hour)))
			}
			require.Zero(t, env.upstream.methodCalls(http.MethodDelete))
		})
	}
}
