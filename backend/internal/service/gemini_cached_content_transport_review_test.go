//go:build unit

package service

import (
	"context"
	"errors"
	"math"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestGeminiCachedContentCreate_LocalFailuresAreNotSent(t *testing.T) {
	for _, scenario := range []string{"invalid model resource", "invalid payload", "invalid base URL"} {
		t.Run(scenario, func(t *testing.T) {
			svc, upstream := newGeminiCachedContentServiceForTest(t, http.StatusOK, `{}`)
			account := &Account{ID: 900, Platform: PlatformGemini, Type: AccountTypeAPIKey, Credentials: map[string]any{"api_key": "secret"}}
			req, err := ParseGeminiCachedContentCreateRequest([]byte(`{"model":"gemini-2.5-pro"}`), time.Now(), time.Hour)
			require.NoError(t, err)
			switch scenario {
			case "invalid model resource":
				account.Type = AccountTypeServiceAccount
			case "invalid payload":
				req.payload["contents"] = math.NaN()
			case "invalid base URL":
				account.Credentials["base_url"] = "http://insecure.example.com"
			}
			_, err = svc.CreateUpstream(context.Background(), account, req, req.Model, "cache1", time.Hour)
			require.ErrorIs(t, err, ErrGeminiCachedContentRequestNotSent)
			require.Nil(t, upstream.lastReq)
		})
	}
}

func TestGeminiCachedContentCreate_UnknownOutcomeMarkerScope(t *testing.T) {
	for _, tc := range []struct {
		name     string
		method   string
		upstream bool
		status   int
		marker   string
		unknown  bool
	}{
		{"declared create", http.MethodPost, true, http.StatusBadGateway, "unknown", true},
		{"official create", http.MethodPost, false, http.StatusBadGateway, "unknown", false},
		{"declared patch", http.MethodPatch, true, http.StatusBadGateway, "unknown", false},
		{"declared delete", http.MethodDelete, true, http.StatusBadGateway, "unknown", false},
		{"declared get", http.MethodGet, true, http.StatusBadGateway, "unknown", false},
		{"ordinary rejection", http.MethodPost, true, http.StatusServiceUnavailable, "", false},
		{"unrecognized marker", http.MethodPost, true, http.StatusBadGateway, "other", false},
		{"success", http.MethodPost, true, http.StatusOK, "unknown", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			svc, upstream := newGeminiCachedContentServiceForTest(t, tc.status, `{}`)
			upstream.response.Header.Set("X-Sub2api-Cached-Content-Outcome", tc.marker)
			account := &Account{ID: 900, Platform: PlatformGemini, Type: AccountTypeAPIKey, Credentials: map[string]any{"api_key": "secret", GeminiExplicitCacheUpstreamCredentialKey: tc.upstream}}
			_, err := svc.doUpstream(context.Background(), account, tc.method, "https://upstream-gateway.example.com/v1beta/cachedContents", nil, time.Second)
			if tc.status == http.StatusOK {
				require.NoError(t, err)
				return
			}
			require.Error(t, err)
			var rejected *GeminiCachedContentUpstreamError
			if tc.unknown {
				require.False(t, errors.As(err, &rejected), "an unknown result must not become a retryable HTTP rejection")
				require.ErrorIs(t, err, ErrGeminiCachedContentCreateOutcomeUnknown)
			} else {
				require.ErrorAs(t, err, &rejected)
				require.True(t, rejected.RetryableOnOtherAccount())
			}
		})
	}
}
