package service

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestGPT55RetirementHTTPPassthroughIgnoresUnusedReplacementMapping(t *testing.T) {
	for _, accountType := range []string{AccountTypeOAuth, AccountTypeSetupToken, AccountTypeAPIKey} {
		t.Run(accountType, func(t *testing.T) {
			account := &Account{
				Platform: PlatformOpenAI, Type: accountType,
				Credentials: map[string]any{"model_mapping": map[string]any{"gpt-5.5": "gpt-6.1-sol"}},
				Extra:       map[string]any{"openai_passthrough": true},
			}
			require.Equal(t, "gpt-5.5", resolveOpenAIAccountUpstreamModelForRequest(account, "gpt-5.5", false))
			require.Equal(t, accountType == AccountTypeAPIKey, account.IsModelSupported("gpt-5.5"))
			account.Extra["openai_passthrough"] = false
			require.True(t, account.IsModelSupported("gpt-5.5"), "native HTTP replacement remains supported")
		})
	}
}
