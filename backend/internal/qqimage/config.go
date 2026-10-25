package qqimage

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"database/sql"
	"encoding/json"
	"io"
	"net"
	"net/url"
	"strings"
	"time"
)

func (s *Store) encrypt(value string) ([]byte, error) {
	if len(s.key) != 32 {
		return nil, ErrEncryptionUnavailable
	}
	block, e := aes.NewCipher(s.key)
	if e != nil {
		return nil, e
	}
	a, e := cipher.NewGCM(block)
	if e != nil {
		return nil, e
	}
	nonce := make([]byte, a.NonceSize())
	if _, e = io.ReadFull(rand.Reader, nonce); e != nil {
		return nil, e
	}
	return a.Seal(nonce, nonce, []byte(value), []byte("qq-image-credential-v1")), nil
}
func (s *Store) decrypt(value []byte) (string, error) {
	if len(value) == 0 {
		return "", nil
	}
	if len(s.key) != 32 {
		return "", ErrEncryptionUnavailable
	}
	block, e := aes.NewCipher(s.key)
	if e != nil {
		return "", e
	}
	a, e := cipher.NewGCM(block)
	if e != nil {
		return "", e
	}
	if len(value) < a.NonceSize() {
		return "", ErrEncryptionUnavailable
	}
	plain, e := a.Open(nil, value[:a.NonceSize()], value[a.NonceSize():], []byte("qq-image-credential-v1"))
	if e != nil {
		return "", ErrEncryptionUnavailable
	}
	return string(plain), nil
}
func validHTTPS(raw string, originOnly bool) bool {
	u, e := url.Parse(raw)
	if e != nil || u.Scheme != "https" || u.Hostname() == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return false
	}
	host := strings.ToLower(u.Hostname())
	if host == "localhost" || strings.HasSuffix(host, ".localhost") || strings.HasSuffix(host, ".local") {
		return false
	}
	if ip := net.ParseIP(host); ip != nil && (ip.IsPrivate() || ip.IsLoopback() || ip.IsUnspecified() || ip.IsLinkLocalUnicast() || ip.IsMulticast()) {
		return false
	}
	return !originOnly || u.Path == "" || u.Path == "/"
}

func validImageHost(host string) bool {
	if host == "" || len(host) > 253 || !strings.Contains(host, ".") || !validHTTPS("https://"+host, true) {
		return false
	}
	u, e := url.Parse("https://" + host)
	if e != nil || u.Hostname() != host || u.Port() != "" {
		return false
	}
	for _, part := range strings.Split(host, ".") {
		if len(part) == 0 || len(part) > 63 || part[0] == '-' || part[len(part)-1] == '-' {
			return false
		}
		for _, r := range part {
			if (r < 'a' || r > 'z') && (r < '0' || r > '9') && r != '-' {
				return false
			}
		}
	}
	return true
}
func (s *Store) configFrom(row interface{ Scan(...any) error }) (*Config, []byte, []byte, error) {
	var id int64
	var b, bot, key []byte
	if e := row.Scan(&id, &b, &bot, &key); e != nil {
		return nil, nil, nil, normalizeError(e)
	}
	var c Config
	if e := json.Unmarshal(b, &c); e != nil {
		return nil, nil, nil, e
	}
	c.ReceiveMode = receiveMode(c.ReceiveMode)
	c.ID = id
	c.BotSecret = ""
	c.APIKey = ""
	c.ClearBotSecret = false
	c.ClearAPIKey = false
	c.BotSecretConfigured = len(bot) > 0
	c.APIKeyConfigured = len(key) > 0
	c.EncryptionAvailable = len(s.key) == 32
	return &c, bot, key, nil
}
func (s *Store) GetConfig(ctx context.Context) (*Config, error) {
	c, _, _, e := s.configFrom(s.queryer(ctx).QueryRowContext(ctx, `SELECT id,config,bot_secret,api_key FROM qq_image_configs ORDER BY id DESC LIMIT 1`))
	if e == ErrNotFound {
		v := DefaultConfig()
		v.EncryptionAvailable = len(s.key) == 32
		return &v, nil
	}
	return c, e
}
func (s *Store) RuntimeConfig(ctx context.Context, id int64) (*RuntimeConfig, error) {
	c, bot, key, e := s.configFrom(s.queryer(ctx).QueryRowContext(ctx, `SELECT id,config,bot_secret,api_key FROM qq_image_configs WHERE id=$1`, id))
	if e != nil {
		return nil, e
	}
	var botRevoked, apiRevoked bool
	if e = s.queryer(ctx).QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM qq_image_credential_revocations WHERE config_id=$1 AND kind='bot'),EXISTS(SELECT 1 FROM qq_image_credential_revocations WHERE config_id=$1 AND kind='api')`, id).Scan(&botRevoked, &apiRevoked); e != nil {
		return nil, e
	}
	if apiRevoked {
		return nil, ErrEncryptionUnavailable
	}
	if botRevoked {
		bot = nil
	}
	bs, e := s.decrypt(bot)
	if e != nil {
		return nil, e
	}
	ak, e := s.decrypt(key)
	if e != nil {
		return nil, e
	}
	return &RuntimeConfig{Config: *c, BotSecret: bs, APIKey: ak}, nil
}

// DeliveryConfig loads only the original bot credential for an already stored
// image. Revoking the provider key must not revoke an independent QQ delivery.
func (s *Store) DeliveryConfig(ctx context.Context, id int64) (*RuntimeConfig, error) {
	c, bot, _, e := s.configFrom(s.queryer(ctx).QueryRowContext(ctx, `SELECT id,config,bot_secret,''::bytea FROM qq_image_configs WHERE id=$1`, id))
	if e != nil {
		return nil, e
	}
	var revoked bool
	if e = s.queryer(ctx).QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM qq_image_credential_revocations WHERE config_id=$1 AND kind='bot')`, id).Scan(&revoked); e != nil {
		return nil, e
	}
	if revoked || len(bot) == 0 {
		return nil, ErrDisabled
	}
	secret, e := s.decrypt(bot)
	if e != nil {
		return nil, e
	}
	return &RuntimeConfig{Config: *c, BotSecret: secret}, nil
}

func (s *Store) SaveConfig(ctx context.Context, c Config, actorID int64) (*Config, error) {
	c.ReceiveMode = receiveMode(c.ReceiveMode)
	if c.ReceiveMode != ReceiveModeWebhook && c.ReceiveMode != ReceiveModeWebSocket {
		return nil, invalidConfig("receive_mode", "invalid_format")
	}
	c.AppID = strings.TrimSpace(c.AppID)
	if len(c.AppID) > 256 || strings.ContainsAny(c.AppID, " \t\r\n") {
		return nil, invalidConfig("app_id", "invalid_format")
	}
	if len(c.BotSecret) > 4096 {
		return nil, invalidConfig("bot_secret", "invalid_format")
	}
	if !c.Enabled {
		defaults := DefaultConfig()
		if c.UserDailyLimit == 0 {
			c.UserDailyLimit = defaults.UserDailyLimit
		}
		if c.GroupConcurrency == 0 {
			c.GroupConcurrency = defaults.GroupConcurrency
		}
		if c.GlobalConcurrency == 0 {
			c.GlobalConcurrency = defaults.GlobalConcurrency
		}
		if c.QueueLimit == 0 {
			c.QueueLimit = defaults.QueueLimit
		}
		if c.QueueTimeoutSeconds == 0 {
			c.QueueTimeoutSeconds = defaults.QueueTimeoutSeconds
		}
		if c.DeadlineSeconds == 0 {
			c.DeadlineSeconds = defaults.DeadlineSeconds
		}
		if c.RetentionDays == 0 {
			c.RetentionDays = defaults.RetentionDays
		}
		if c.PromptMaxChars == 0 {
			c.PromptMaxChars = defaults.PromptMaxChars
		}
	}
	if len(c.ImageHosts) > 20 {
		return nil, invalidConfig("image_hosts", "too_many")
	}
	imageHosts := make([]string, 0, len(c.ImageHosts))
	for _, host := range c.ImageHosts {
		host = strings.ToLower(strings.TrimSpace(host))
		if !validImageHost(host) {
			return nil, invalidConfig("image_hosts", "invalid_format")
		}
		if !contains(imageHosts, host) {
			imageHosts = append(imageHosts, host)
		}
	}
	c.ImageHosts = imageHosts
	clearBot, clearAPI := c.ClearBotSecret, c.ClearAPIKey
	if clearBot && c.BotSecret != "" {
		return nil, invalidConfig("bot_secret", "conflicting_clear")
	}
	if clearAPI && c.APIKey != "" {
		return nil, invalidConfig("api_key", "conflicting_clear")
	}
	if clearBot || clearAPI {
		c.Enabled = false
	}
	if strings.HasSuffix(strings.TrimRight(c.APIURL, "/"), "/images/generations") {
		return nil, invalidConfig("api_url", "generation_endpoint")
	}
	if c.Enabled || c.BotSecret != "" || c.APIKey != "" {
		if len(s.key) != 32 {
			return nil, ErrEncryptionUnavailable
		}
	}
	if c.Price != "" {
		p, e := ParseAmount(c.Price)
		if e != nil {
			return nil, invalidConfig("price", "invalid_amount")
		}
		c.Price = p
	}
	if c.UserDailyAmount != "" {
		p, e := ParseAmount(c.UserDailyAmount)
		if e != nil {
			return nil, invalidConfig("user_daily_amount", "invalid_amount")
		}
		c.UserDailyAmount = p
	}
	if c.APIURL != "" && !validHTTPS(c.APIURL, false) {
		return nil, invalidConfig("api_url", "invalid_url")
	}
	if c.PublicBaseURL != "" && !validHTTPS(c.PublicBaseURL, true) {
		return nil, invalidConfig("public_base_url", "invalid_url")
	}
	if c.Enabled {
		for _, required := range []struct{ field, value string }{
			{"app_id", c.AppID}, {"api_url", c.APIURL}, {"model", c.Model}, {"size", c.Size},
			{"price", c.Price}, {"public_base_url", c.PublicBaseURL}, {"user_daily_amount", c.UserDailyAmount},
		} {
			if required.value == "" {
				return nil, invalidConfig(required.field, "required")
			}
		}
		if len(c.AllowedGroups) == 0 {
			return nil, invalidConfig("allowed_groups", "required")
		}
	}
	for _, limit := range []struct {
		field          string
		value, maximum int
	}{
		{"user_daily_limit", c.UserDailyLimit, 10000}, {"group_concurrency", c.GroupConcurrency, 100},
		{"global_concurrency", c.GlobalConcurrency, 100}, {"queue_limit", c.QueueLimit, 10000},
		{"queue_timeout_seconds", c.QueueTimeoutSeconds, 3600}, {"deadline_seconds", c.DeadlineSeconds, 7200},
		{"retention_days", c.RetentionDays, 30}, {"prompt_max_chars", c.PromptMaxChars, 2000},
	} {
		if limit.value < 1 || limit.value > limit.maximum {
			return nil, invalidConfig(limit.field, "out_of_range")
		}
	}
	for _, g := range c.AllowedGroups {
		if strings.TrimSpace(g) == "" || len(g) > 256 {
			return nil, invalidConfig("allowed_groups", "invalid_format")
		}
	}
	var result *Config
	e := s.transaction(ctx, func(tx *sql.Tx, _ time.Time) error {
		var bot, key []byte
		var prior int64
		e := tx.QueryRowContext(ctx, `SELECT id,bot_secret,api_key FROM qq_image_configs ORDER BY id DESC LIMIT 1`).Scan(&prior, &bot, &key)
		if e != nil && e != sql.ErrNoRows {
			return e
		}
		if c.ID != 0 && c.ID != prior {
			return ErrConflict
		}
		if c.BotSecret != "" {
			bot, e = s.encrypt(c.BotSecret)
			if e != nil {
				return e
			}
		}
		if c.APIKey != "" {
			key, e = s.encrypt(c.APIKey)
			if e != nil {
				return e
			}
		}
		if clearBot {
			bot = []byte{}
			if _, e = tx.ExecContext(ctx, `INSERT INTO qq_image_credential_revocations(config_id,kind,actor_id) SELECT id,'bot',$1 FROM qq_image_configs ON CONFLICT DO NOTHING`, actorID); e != nil {
				return e
			}
		}
		if clearAPI {
			key = []byte{}
			if _, e = tx.ExecContext(ctx, `INSERT INTO qq_image_credential_revocations(config_id,kind,actor_id) SELECT id,'api',$1 FROM qq_image_configs ON CONFLICT DO NOTHING`, actorID); e != nil {
				return e
			}
		}
		if c.Enabled && len(bot) == 0 {
			return invalidConfig("bot_secret", "missing_credential")
		}
		if c.Enabled && len(key) == 0 {
			return invalidConfig("api_key", "missing_credential")
		}
		if c.Enabled {
			if _, e = s.decrypt(bot); e != nil {
				return e
			}
			if _, e = s.decrypt(key); e != nil {
				return e
			}
		}
		c.BotSecret = ""
		c.APIKey = ""
		c.ID = 0
		c.BotSecretConfigured = false
		c.APIKeyConfigured = false
		c.EncryptionAvailable = false
		c.ClearAPIKey = false
		c.ClearBotSecret = false
		if bot == nil {
			bot = []byte{}
		}
		if key == nil {
			key = []byte{}
		}
		var id int64
		if e = tx.QueryRowContext(ctx, `INSERT INTO qq_image_configs(config,bot_secret,api_key) VALUES($1,$2,$3) RETURNING id`, jsonBytes(c), bot, key).Scan(&id); e != nil {
			return e
		}
		c.ID = id
		c.BotSecretConfigured = len(bot) > 0
		c.APIKeyConfigured = len(key) > 0
		c.EncryptionAvailable = len(s.key) == 32
		result = &c
		if clearBot {
			if e = audit(ctx, tx, actorID, "config.bot_credentials_revoked", stringID(id), "all_previous_versions"); e != nil {
				return e
			}
		}
		if clearAPI {
			if e = audit(ctx, tx, actorID, "config.api_credentials_revoked", stringID(id), "all_previous_versions"); e != nil {
				return e
			}
		}
		if _, e = tx.ExecContext(ctx, `UPDATE qq_image_discoveries SET status='closed' WHERE status IN ('waiting','found')`); e != nil {
			return e
		}
		return audit(ctx, tx, actorID, "config.save", stringID(id), "")
	})
	return result, e
}
