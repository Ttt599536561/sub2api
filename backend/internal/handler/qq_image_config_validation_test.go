package handler

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/qqimage"
	"github.com/stretchr/testify/require"
)

type qqConfigValidationHTTPStore struct {
	qqImageStore
	calls int
}

func (s *qqConfigValidationHTTPStore) SaveConfig(ctx context.Context, c qqimage.Config, actor int64) (*qqimage.Config, error) {
	s.calls++
	return qqimage.NewStore(nil, nil).SaveConfig(ctx, c, actor)
}

func TestQQImageHTTPConfigValidationDetails(t *testing.T) {
	tests := []struct {
		name, field, issue string
		mutate             func(*qqimage.Config)
	}{
		{"origin", "public_base_url", "invalid_url", func(c *qqimage.Config) { c.PublicBaseURL = "https://site.example.test/admin?private-marker" }},
		{"generation endpoint while disabled", "api_url", "generation_endpoint", func(c *qqimage.Config) { c.APIURL = "https://images.example.test/v1/images/generations" }},
		{"amount while disabled", "price", "invalid_amount", func(c *qqimage.Config) { c.Price = "private-marker" }},
		{"retention", "retention_days", "out_of_range", func(c *qqimage.Config) { c.RetentionDays = 31 }},
		{"prompt limit", "prompt_max_chars", "out_of_range", func(c *qqimage.Config) { c.PromptMaxChars = 2001 }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := &qqConfigValidationHTTPStore{}
			h := &QQImageHandler{store: s, cfg: &config.Config{Server: config.ServerConfig{FrontendURL: "https://site.example.test"}}}
			c := qqimage.DefaultConfig()
			tt.mutate(&c)
			body, err := json.Marshal(c)
			require.NoError(t, err)
			w := qqDiscoveryRequest(h.SaveAdminConfig, 42, "admin", "https://site.example.test", string(body), "PUT")
			require.Equal(t, 400, w.Code)
			var got struct {
				Reason   string            `json:"reason"`
				Metadata map[string]string `json:"metadata"`
			}
			require.NoError(t, json.Unmarshal(w.Body.Bytes(), &got))
			require.Equal(t, "QQ_IMAGE_CONFIG_INVALID", got.Reason)
			require.Equal(t, map[string]string{"field": tt.field, "issue": tt.issue}, got.Metadata)
			require.NotContains(t, w.Body.String(), "private-marker")
			require.Equal(t, 1, s.calls)
		})
	}
}

func TestQQImageHTTPConfigDecodeDetailsNeverEchoInput(t *testing.T) {
	for _, body := range []string{`{"private-marker":"secret-marker"}`, `{"api_key":"secret-marker"} trailing`, `{"enabled":"secret-marker"}`} {
		s := &qqConfigValidationHTTPStore{}
		h := &QQImageHandler{store: s, cfg: &config.Config{Server: config.ServerConfig{FrontendURL: "https://site.example.test"}}}
		w := qqDiscoveryRequest(h.SaveAdminConfig, 42, "admin", "https://site.example.test", body, "PUT")
		require.Equal(t, 400, w.Code)
		var got struct {
			Reason   string            `json:"reason"`
			Metadata map[string]string `json:"metadata"`
		}
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &got))
		require.Equal(t, "QQ_IMAGE_CONFIG_INVALID", got.Reason)
		require.Equal(t, map[string]string{"field": "request", "issue": "invalid_request"}, got.Metadata)
		require.NotContains(t, w.Body.String(), "private-marker")
		require.NotContains(t, w.Body.String(), "secret-marker")
		require.Zero(t, s.calls)
	}
}

type qqConfigResultHTTPStore struct {
	qqImageStore
	result *qqimage.Config
	err    error
	calls  int
}

func (s *qqConfigResultHTTPStore) SaveConfig(context.Context, qqimage.Config, int64) (*qqimage.Config, error) {
	s.calls++
	return s.result, s.err
}
func TestQQImageHTTPConfigMetadataSafetyAndOtherErrors(t *testing.T) {
	for _, tt := range []struct {
		name         string
		err          error
		status       int
		reason       string
		field, issue string
	}{
		{"known", &qqimage.ConfigValidationError{Field: "api_key", Issue: "missing_credential"}, 400, "QQ_IMAGE_CONFIG_INVALID", "api_key", "missing_credential"},
		{"unknown field", &qqimage.ConfigValidationError{Field: "secret-marker", Issue: "required"}, 400, "QQ_IMAGE_CONFIG_INVALID", "request", "invalid_request"},
		{"unknown pair", &qqimage.ConfigValidationError{Field: "request", Issue: "missing_credential"}, 400, "QQ_IMAGE_CONFIG_INVALID", "request", "invalid_request"},
		{"encryption", qqimage.ErrEncryptionUnavailable, 503, "QQ_IMAGE_ENCRYPTION_UNAVAILABLE", "", ""},
		{"conflict", qqimage.ErrConflict, 409, "QQ_IMAGE_CONFLICT", "", ""},
	} {
		t.Run(tt.name, func(t *testing.T) {
			s := &qqConfigResultHTTPStore{err: tt.err}
			h := &QQImageHandler{store: s, cfg: &config.Config{Server: config.ServerConfig{FrontendURL: "https://site.example.test"}}}
			w := qqDiscoveryRequest(h.SaveAdminConfig, 42, "admin", "https://site.example.test", `{"api_key":"secret-marker"}`, "PUT")
			require.Equal(t, tt.status, w.Code)
			var got struct {
				Reason   string            `json:"reason"`
				Metadata map[string]string `json:"metadata"`
			}
			require.NoError(t, json.Unmarshal(w.Body.Bytes(), &got))
			require.Equal(t, tt.reason, got.Reason)
			if tt.field != "" {
				require.Equal(t, map[string]string{"field": tt.field, "issue": tt.issue}, got.Metadata)
			} else {
				require.Empty(t, got.Metadata)
			}
			require.NotContains(t, w.Body.String(), "secret-marker")
		})
	}
}
func TestQQImageHTTPConfigGuardsAndSuccessRedaction(t *testing.T) {
	for _, tt := range []struct {
		name         string
		uid          int64
		role, origin string
		simple       bool
		status       int
	}{
		{"anonymous", 0, "", "https://site.example.test", false, 401},
		{"non-admin", 42, "user", "https://site.example.test", false, 403},
		{"cross-origin", 42, "admin", "https://other.example.test", false, 403},
		{"simple mode", 42, "admin", "https://site.example.test", true, 403},
	} {
		t.Run(tt.name, func(t *testing.T) {
			s := &qqConfigResultHTTPStore{}
			cfg := &config.Config{Server: config.ServerConfig{FrontendURL: "https://site.example.test"}}
			if tt.simple {
				cfg.RunMode = config.RunModeSimple
			}
			h := &QQImageHandler{store: s, cfg: cfg}
			w := qqDiscoveryRequest(h.SaveAdminConfig, tt.uid, tt.role, tt.origin, `{"enabled":true,"api_key":"secret-marker"}`, "PUT")
			require.Equal(t, tt.status, w.Code)
			require.Zero(t, s.calls)
			require.NotContains(t, w.Body.String(), "secret-marker")
		})
	}
	s := &qqConfigResultHTTPStore{result: &qqimage.Config{Enabled: true, BotSecret: "bot-marker", APIKey: "secret-marker"}}
	h := &QQImageHandler{store: s, cfg: &config.Config{Server: config.ServerConfig{FrontendURL: "https://site.example.test"}}}
	w := qqDiscoveryRequest(h.SaveAdminConfig, 42, "admin", "https://site.example.test", `{}`, "PUT")
	require.Equal(t, 200, w.Code)
	require.Equal(t, 1, s.calls)
	require.NotContains(t, w.Body.String(), "secret-marker")
	require.NotContains(t, w.Body.String(), "bot-marker")
	require.Equal(t, "secret-marker", s.result.APIKey, "response redaction must not mutate stored configuration")
}
