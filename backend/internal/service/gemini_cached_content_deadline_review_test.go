//go:build unit

package service

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestGeminiCachedContentAbsoluteExpiration_PreservesPrecision(t *testing.T) {
	now := time.Date(2026, 10, 15, 8, 0, 0, 123456789, time.UTC)
	deadline := now.Add(time.Hour + 200*time.Millisecond)
	for _, field := range []string{"expireTime", "expire_time"} {
		t.Run(field, func(t *testing.T) {
			body := []byte(fmt.Sprintf(`{"model":"gemini-2.5-pro",%q:%q}`, field, deadline.Format(time.RFC3339Nano)))
			req, err := ParseGeminiCachedContentCreateRequest(body, now, 24*time.Hour)
			require.NoError(t, err)
			require.Equal(t, deadline, now.Add(req.TTL), "an absolute deadline must not be rounded into extra storage")
			body = []byte(fmt.Sprintf(`{%q:%q}`, field, deadline.Format(time.RFC3339Nano)))
			ttl, err := ParseGeminiCachedContentPatchRequest(body, field, now, 24*time.Hour)
			require.NoError(t, err)
			require.Equal(t, deadline, now.Add(ttl))
		})
	}
}

func TestGeminiCachedContentAbsoluteExpiration_CreateSurvivesQueueDelay(t *testing.T) {
	now := time.Date(2026, 10, 15, 8, 0, 0, 123456789, time.UTC)
	deadline := now.Add(time.Hour + 200*time.Millisecond)
	for _, field := range []string{"expireTime", "expire_time"} {
		t.Run(field, func(t *testing.T) {
			svc, upstream := newGeminiCachedContentServiceForTest(t, 200,
				`{"name":"cachedContents/up1","usageMetadata":{"totalTokenCount":2000}}`)
			req, err := ParseGeminiCachedContentCreateRequest([]byte(fmt.Sprintf(
				`{"model":"gemini-2.5-pro",%q:%q}`, field, deadline.Format(time.RFC3339Nano))), now, 24*time.Hour)
			require.NoError(t, err)
			// Account and concurrency admission take five minutes after parsing.
			svc.now = func() time.Time { return now.Add(5 * time.Minute) }
			account := &Account{ID: 9, Platform: PlatformGemini, Type: AccountTypeAPIKey, Credentials: map[string]any{"api_key": "secret"}}
			ttl, allowed := req.TTLForAccount(account)
			require.True(t, allowed)
			_, err = svc.CreateUpstream(context.Background(), account, req, req.Model, "pub1", ttl)
			require.NoError(t, err)
			var payload map[string]any
			require.NoError(t, json.NewDecoder(upstream.lastReq.Body).Decode(&payload))
			require.Equal(t, deadline.Format(time.RFC3339Nano), payload["expireTime"], "forward the client deadline, including its fractional seconds")
			require.NotContains(t, payload, "ttl", "a relative TTL restarts the lifetime after queueing")
		})
	}
}

func TestGeminiCachedContentStorageExtensionPrecision(t *testing.T) {
	for _, tc := range []struct {
		name           string
		baselineOffset time.Duration
		extension      time.Duration
		want           time.Duration
	}{
		{name: "unchanged"},
		{name: "shortening", extension: -time.Nanosecond},
		{name: "999 nanoseconds", extension: 999 * time.Nanosecond},
		{name: "1000 nanoseconds", extension: time.Microsecond, want: time.Microsecond},
		{name: "999 nanoseconds from fractional baseline", baselineOffset: 999 * time.Nanosecond, extension: 999 * time.Nanosecond},
		{name: "1000 nanoseconds from fractional baseline", baselineOffset: 999 * time.Nanosecond, extension: time.Microsecond, want: time.Microsecond},
	} {
		t.Run(tc.name, func(t *testing.T) {
			baseline := time.Date(2026, 10, 15, 8, 0, 0, 0, time.UTC).Add(tc.baselineOffset)
			require.Equal(t, tc.want, GeminiCachedContentStorageExtension(baseline, baseline.Add(tc.extension)))
		})
	}
}
