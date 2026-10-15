//go:build unit

package service

import (
	"context"
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
