package qqimage

import (
	"context"
	"crypto/sha256"
	"errors"
	"reflect"
	"strings"
	"testing"
)

func configValidationFixture() Config {
	c := DefaultConfig()
	c.Enabled = true
	c.AppID = "fake-app"
	c.APIURL = "https://images.example.test/v1"
	c.Model = "fake-model"
	c.Size = "1024x1024"
	c.Price = "0.04"
	c.PublicBaseURL = "https://qq-test.tangguo.xin"
	c.UserDailyAmount = "20"
	c.AllowedGroups = []string{"fake-group"}
	return c
}
func requireConfigValidation(t *testing.T, err error, field, issue string) {
	t.Helper()
	if !errors.Is(err, ErrInvalid) {
		t.Fatalf("expected invalid configuration, got %T", err)
	}
	var detail *ConfigValidationError
	if !errors.As(err, &detail) {
		t.Fatalf("missing field diagnostics: %T", err)
	}
	gotField, gotIssue := detail.Details()
	if gotField != field || gotIssue != issue {
		t.Fatalf("got %s/%s, want %s/%s", gotField, gotIssue, field, issue)
	}
	if err.Error() != "qq_image_config_invalid" {
		t.Fatal("error string must remain value-free")
	}
}
func TestConfigValidationStaticFields(t *testing.T) {
	tests := []struct {
		field, issue string
		change       func(*Config)
	}{
		{"receive_mode", "invalid_format", func(c *Config) { c.ReceiveMode = "wrong" }},
		{"app_id", "invalid_format", func(c *Config) { c.AppID = "two words" }},
		{"app_id", "invalid_format", func(c *Config) { c.AppID = strings.Repeat("界", 86) }},
		{"bot_secret", "invalid_format", func(c *Config) { c.BotSecret = strings.Repeat("x", 4097) }},
		{"image_hosts", "too_many", func(c *Config) { c.ImageHosts = make([]string, 21) }},
		{"image_hosts", "invalid_format", func(c *Config) { c.ImageHosts = []string{"https://cdn.example.test"} }},
		{"bot_secret", "conflicting_clear", func(c *Config) { c.BotSecret = "fake"; c.ClearBotSecret = true }},
		{"api_key", "conflicting_clear", func(c *Config) { c.APIKey = "fake"; c.ClearAPIKey = true }},
		{"api_url", "generation_endpoint", func(c *Config) { c.Enabled = false; c.APIURL += "/images/generations/" }},
		{"price", "invalid_amount", func(c *Config) { c.Enabled = false; c.Price = "0" }},
		{"user_daily_amount", "invalid_amount", func(c *Config) { c.Enabled = false; c.UserDailyAmount = "1e2" }},
		{"api_url", "invalid_url", func(c *Config) { c.Enabled = false; c.APIURL = "http://images.example.test/v1" }},
		{"public_base_url", "invalid_url", func(c *Config) { c.Enabled = false; c.PublicBaseURL += "/admin" }},
		{"app_id", "required", func(c *Config) { c.AppID = "" }},
		{"api_url", "required", func(c *Config) { c.APIURL = "" }},
		{"model", "required", func(c *Config) { c.Model = "" }},
		{"size", "required", func(c *Config) { c.Size = "" }},
		{"price", "required", func(c *Config) { c.Price = "" }},
		{"public_base_url", "required", func(c *Config) { c.PublicBaseURL = "" }},
		{"user_daily_amount", "required", func(c *Config) { c.UserDailyAmount = "" }},
		{"allowed_groups", "required", func(c *Config) { c.AllowedGroups = nil }},
		{"allowed_groups", "invalid_format", func(c *Config) { c.AllowedGroups = []string{"   "} }},
		{"allowed_groups", "invalid_format", func(c *Config) { c.AllowedGroups = []string{strings.Repeat("界", 86)} }},
		{"user_daily_limit", "out_of_range", func(c *Config) { c.UserDailyLimit = 10001 }},
		{"group_concurrency", "out_of_range", func(c *Config) { c.GroupConcurrency = 101 }},
		{"global_concurrency", "out_of_range", func(c *Config) { c.GlobalConcurrency = 101 }},
		{"queue_limit", "out_of_range", func(c *Config) { c.QueueLimit = 10001 }},
		{"queue_timeout_seconds", "out_of_range", func(c *Config) { c.QueueTimeoutSeconds = 3601 }},
		{"deadline_seconds", "out_of_range", func(c *Config) { c.DeadlineSeconds = 7201 }},
		{"retention_days", "out_of_range", func(c *Config) { c.RetentionDays = 31 }},
		{"prompt_max_chars", "out_of_range", func(c *Config) { c.PromptMaxChars = 2001 }},
		{"retention_days", "out_of_range", func(c *Config) { c.RetentionDays = 0 }},
	}
	for _, tt := range tests {
		t.Run(tt.field+"_"+tt.issue, func(t *testing.T) {
			c := configValidationFixture()
			tt.change(&c)
			_, err := NewStore(nil, []byte(strings.Repeat("k", 32))).SaveConfig(context.Background(), c, 42)
			requireConfigValidation(t, err, tt.field, tt.issue)
		})
	}
}
func TestConfigValidationDetailsOnlyKnownPairs(t *testing.T) {
	for _, e := range []*ConfigValidationError{nil, {Field: "secret-marker", Issue: "required"}, {Field: "api_url", Issue: "secret-marker"}, {Field: "request", Issue: "missing_credential"}} {
		field, issue := e.Details()
		if field != "request" || issue != "invalid_request" {
			t.Fatal("unknown metadata reflected")
		}
	}
}
func configValidationSnapshot(t *testing.T, s *Store) map[string][32]byte {
	t.Helper()
	snapshot := map[string][32]byte{}
	for _, table := range []string{"qq_image_configs", "qq_image_audit", "qq_image_discoveries", "qq_image_credential_revocations", "qq_image_orders", "qq_image_grants", "users"} {
		var value string
		if err := s.db.QueryRow(`SELECT COALESCE(jsonb_agg(to_jsonb(t) ORDER BY to_jsonb(t)::text),'[]'::jsonb)::text FROM ` + table + ` t`).Scan(&value); err != nil {
			t.Fatal(err)
		}
		snapshot[table] = sha256.Sum256([]byte(value))
	}
	return snapshot
}
func TestConfigValidationRejectsWithoutWritingAndRetainsEncryptedBot(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	current, err := s.GetConfig(ctx)
	if err != nil {
		t.Fatal(err)
	}
	current.ClearAPIKey = true
	disabled, err := s.SaveConfig(ctx, *current, 42)
	if err != nil {
		t.Fatal(err)
	}
	var botBefore []byte
	if err = s.db.QueryRow(`SELECT bot_secret FROM qq_image_configs WHERE id=$1`, disabled.ID).Scan(&botBefore); err != nil {
		t.Fatal(err)
	}
	before := configValidationSnapshot(t, s)
	for _, tt := range []struct {
		field, issue string
		change       func(*Config)
	}{
		{"api_key", "missing_credential", func(c *Config) { c.Enabled = true }},
		{"api_url", "generation_endpoint", func(c *Config) { c.APIURL += "/images/generations" }},
		{"public_base_url", "invalid_url", func(c *Config) { c.PublicBaseURL += "/admin" }},
		{"retention_days", "out_of_range", func(c *Config) { c.RetentionDays = 31 }},
		{"prompt_max_chars", "out_of_range", func(c *Config) { c.PromptMaxChars = 2001 }},
	} {
		t.Run(tt.field+"_"+tt.issue, func(t *testing.T) {
			candidate := *disabled
			tt.change(&candidate)
			_, err := s.SaveConfig(ctx, candidate, 42)
			requireConfigValidation(t, err, tt.field, tt.issue)
			if !reflect.DeepEqual(before, configValidationSnapshot(t, s)) {
				t.Fatal("rejected save changed stored configuration or business data")
			}
		})
	}
	next := *disabled
	next.Enabled = true
	next.APIKey = "fake-new-key"
	next.PublicBaseURL = "https://qq-test.tangguo.xin"
	saved, err := s.SaveConfig(ctx, next, 42)
	if err != nil {
		t.Fatal(err)
	}
	if !saved.Enabled || !saved.BotSecretConfigured || !saved.APIKeyConfigured || saved.BotSecret != "" || saved.APIKey != "" {
		t.Fatal("enable did not retain/redact credentials")
	}
	runtime, err := s.RuntimeConfig(ctx, saved.ID)
	if err != nil {
		t.Fatal(err)
	}
	if runtime.BotSecret != "qq-secret" || runtime.APIKey != "fake-new-key" {
		t.Fatal("credential preservation failed")
	}
	var botAfter []byte
	if err = s.db.QueryRow(`SELECT bot_secret FROM qq_image_configs WHERE id=$1`, saved.ID).Scan(&botAfter); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(botBefore, botAfter) {
		t.Fatal("retained encrypted bot secret changed")
	}
	var auditCount int
	if err = s.db.QueryRow(`SELECT count(*) FROM qq_image_audit WHERE action='config.save' AND object_id=$1`, stringID(saved.ID)).Scan(&auditCount); err != nil {
		t.Fatal(err)
	}
	if auditCount != 1 {
		t.Fatal("successful save must have one audit record")
	}
}
func TestConfigValidationMissingBotCredentialPreservesState(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	c, err := s.GetConfig(ctx)
	if err != nil {
		t.Fatal(err)
	}
	c.ClearBotSecret = true
	disabled, err := s.SaveConfig(ctx, *c, 42)
	if err != nil {
		t.Fatal(err)
	}
	before := configValidationSnapshot(t, s)
	disabled.Enabled = true
	_, err = s.SaveConfig(ctx, *disabled, 42)
	requireConfigValidation(t, err, "bot_secret", "missing_credential")
	if !reflect.DeepEqual(before, configValidationSnapshot(t, s)) {
		t.Fatal("failed enable changed stored state")
	}
}

func TestConfigValidationAuditFailureRollsBackCredentialsAndDiscovery(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	if _, err := s.StartDiscovery(ctx, 42); err != nil {
		t.Fatal(err)
	}
	c, err := s.GetConfig(ctx)
	if err != nil {
		t.Fatal(err)
	}
	before := configValidationSnapshot(t, s)
	_, err = s.db.Exec(`CREATE FUNCTION reject_config_audit() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.action='config.save' THEN RAISE EXCEPTION 'fake audit unavailable'; END IF; RETURN NEW; END $$; CREATE TRIGGER reject_config_audit BEFORE INSERT ON qq_image_audit FOR EACH ROW EXECUTE FUNCTION reject_config_audit()`)
	if err != nil {
		t.Fatal(err)
	}
	c.ClearAPIKey = true
	_, err = s.SaveConfig(ctx, *c, 42)
	if err == nil || errors.Is(err, ErrInvalid) {
		t.Fatal("audit failure should remain an internal transaction failure")
	}
	if !reflect.DeepEqual(before, configValidationSnapshot(t, s)) {
		t.Fatal("failed audit left partial credential, discovery, or configuration changes")
	}
	runtime, err := s.RuntimeConfig(ctx, c.ID)
	if err != nil {
		t.Fatal(err)
	}
	if runtime.APIKey != "upstream-secret" {
		t.Fatal("failed save revoked prior credentials")
	}
}
