//go:build unit

package handler

import (
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestGeminiCachedContentsAbsoluteExpiration_CreateCapsDeadline(t *testing.T) {
	for _, field := range []string{"expireTime", "expire_time"} {
		t.Run(field, func(t *testing.T) {
			env := newCachedBillingEnv(t, 500, 900)
			deadline := time.Now().Add(2 * time.Hour).Truncate(time.Second)
			// Missing upstream expiry must fall back to the requested timestamp.
			env.upstream.createBody = `{"name":"cachedContents/up1","usageMetadata":{"totalTokenCount":1000000}}`
			rec := env.serve(http.MethodPost, "/v1beta/cachedContents", fmt.Sprintf(
				`{"model":"gemini-2.5-pro",%q:%q}`, field, deadline.Format(time.RFC3339Nano)), nil, env.h.GeminiCachedContentsCreate)
			require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
			require.Len(t, env.repo.records, 1)
			for _, record := range env.repo.records {
				require.Equal(t, deadline.UTC(), record.ExpireTime.UTC(), "response and billing must not extend the client's deadline")
			}
			require.Len(t, env.waitForUsage(t, 1), 1)
		})
	}
}

func TestGeminiCachedContentsAbsoluteExpiration_UnchangedPatchIsFree(t *testing.T) {
	for _, balance := range []float64{500, 0} {
		t.Run(fmt.Sprintf("balance=%g", balance), func(t *testing.T) {
			env := newCachedBillingEnv(t, balance, 900)
			deadline := time.Now().Add(time.Hour).Truncate(time.Second)
			env.seed(deadline)
			rec := env.serve(http.MethodPatch, "/v1beta/cachedContents/abc?updateMask=expireTime", fmt.Sprintf(
				`{"expireTime":%q}`, deadline.Format(time.RFC3339Nano)), gin.Params{{Key: "cacheID", Value: "abc"}}, env.h.GeminiCachedContentsPatch)
			require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
			require.Equal(t, deadline.UTC(), env.repo.records["abc"].ExpireTime.UTC())
			require.Empty(t, env.usage.totals(), "resubmitting the current expiration is not a paid extension")
			require.Equal(t, 1, env.upstream.methodCalls(http.MethodPatch))
		})
	}
}

func TestGeminiCachedContentsAbsoluteExpiration_PersistencePrecision(t *testing.T) {
	for _, tc := range []struct {
		name       string
		delta      time.Duration
		balance    float64
		wantStatus int
		charged    bool
	}{
		{name: "submicrosecond replay with balance", delta: 123 * time.Nanosecond, balance: 500, wantStatus: http.StatusOK},
		{name: "submicrosecond replay without balance", delta: 123 * time.Nanosecond, wantStatus: http.StatusOK},
		{name: "one microsecond extension with balance", delta: time.Microsecond, balance: 500, wantStatus: http.StatusOK, charged: true},
		{name: "one microsecond extension without balance", delta: time.Microsecond, wantStatus: http.StatusForbidden},
	} {
		t.Run(tc.name, func(t *testing.T) {
			env := newCachedBillingEnv(t, tc.balance, 900)
			// PostgreSQL persists microseconds, while a create/patch response can
			// return the original nanosecond deadline from the in-memory record.
			storedDeadline := time.Now().Add(time.Hour).Truncate(time.Microsecond)
			deadline := storedDeadline.Add(tc.delta)
			env.seed(storedDeadline)
			rec := env.serve(http.MethodPatch, "/v1beta/cachedContents/abc?updateMask=expireTime", fmt.Sprintf(
				`{"expireTime":%q}`, deadline.Format(time.RFC3339Nano)), gin.Params{{Key: "cacheID", Value: "abc"}}, env.h.GeminiCachedContentsPatch)
			require.Equal(t, tc.wantStatus, rec.Code, rec.Body.String())
			if tc.wantStatus != http.StatusOK {
				require.Zero(t, env.upstream.methodCalls(http.MethodPatch))
				require.Empty(t, env.usage.totals())
				return
			}
			require.Equal(t, deadline.UTC(), env.repo.records["abc"].ExpireTime.UTC(), "the upstream deadline keeps its nanosecond precision")
			if tc.charged {
				totals := env.waitForUsage(t, 1)
				require.Len(t, totals, 1)
				require.InDelta(t, cachedBillingTokens*cachedBillingStoragePrice*tc.delta.Hours(), totals[0], 1e-16)
			} else {
				require.Empty(t, env.usage.totals(), "submicrosecond database precision differences are not paid extensions")
			}
		})
	}
}
