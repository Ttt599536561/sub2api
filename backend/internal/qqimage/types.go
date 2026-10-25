// Package qqimage implements separately authorized QQ image orders against the existing wallet.
package qqimage

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"regexp"
	"strings"
	"time"

	"github.com/shopspring/decimal"
)

var (
	ErrNotFound              = errors.New("qq_image_not_found")
	ErrForbidden             = errors.New("qq_image_forbidden")
	ErrConflict              = errors.New("qq_image_conflict")
	ErrExpired               = errors.New("qq_image_expired")
	ErrDisabled              = errors.New("qq_image_disabled")
	ErrInvalid               = errors.New("qq_image_invalid")
	ErrInsufficientBalance   = errors.New("qq_image_insufficient_balance")
	ErrLimit                 = errors.New("qq_image_limit")
	ErrEncryptionUnavailable = errors.New("qq_image_encryption_unavailable")
)

var amountPattern = regexp.MustCompile(`^[0-9]{1,12}(\.[0-9]{1,8})?$`)

// ParseAmount rejects exponent notation and precision the wallet cannot represent.
func ParseAmount(v string) (string, error) {
	if !amountPattern.MatchString(v) {
		return "", ErrInvalid
	}
	d, err := decimal.NewFromString(v)
	if err != nil || !d.IsPositive() || d.GreaterThanOrEqual(decimal.New(1, 12)) {
		return "", ErrInvalid
	}
	return d.StringFixed(8), nil
}

type Identity struct {
	Platform    string `json:"platform"`
	AppID       string `json:"app_id"`
	SubjectType string `json:"subject_type"`
	SubjectID   string `json:"subject_id"`
	ScopeType   string `json:"scope_type"`
	ScopeID     string `json:"scope_id"`
	GroupID     string `json:"group_id"`
}

func (i Identity) Validate() error {
	if i.Platform != "qq" || i.SubjectType != "group_member" || i.ScopeType != "group" || i.GroupID != i.ScopeID {
		return ErrInvalid
	}
	for _, v := range []string{i.AppID, i.SubjectID, i.ScopeID} {
		if strings.TrimSpace(v) == "" || len(v) > 256 {
			return ErrInvalid
		}
	}
	return nil
}
func (i Identity) Key() string {
	b, _ := json.Marshal(i)
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}

const (
	ReceiveModeWebhook   = "webhook"
	ReceiveModeWebSocket = "websocket"
)

func receiveMode(mode string) string {
	if mode == "" {
		return ReceiveModeWebhook
	}
	return mode
}

type Config struct {
	ReceiveMode         string   `json:"receive_mode"`
	ID                  int64    `json:"id"`
	Enabled             bool     `json:"enabled"`
	AppID               string   `json:"app_id"`
	BotSecret           string   `json:"bot_secret,omitempty"`
	ClearBotSecret      bool     `json:"clear_bot_secret,omitempty"`
	APIURL              string   `json:"api_url"`
	ImageHosts          []string `json:"image_hosts"`
	APIKey              string   `json:"api_key,omitempty"`
	ClearAPIKey         bool     `json:"clear_api_key,omitempty"`
	Model               string   `json:"model"`
	Size                string   `json:"size"`
	Price               string   `json:"price"`
	AllowedGroups       []string `json:"allowed_groups"`
	PublicBaseURL       string   `json:"public_base_url"`
	UserDailyLimit      int      `json:"user_daily_limit"`
	UserDailyAmount     string   `json:"user_daily_amount"`
	GroupConcurrency    int      `json:"group_concurrency"`
	GlobalConcurrency   int      `json:"global_concurrency"`
	QueueLimit          int      `json:"queue_limit"`
	QueueTimeoutSeconds int      `json:"queue_timeout_seconds"`
	DeadlineSeconds     int      `json:"deadline_seconds"`
	RetentionDays       int      `json:"retention_days"`
	PromptMaxChars      int      `json:"prompt_max_chars"`
	BotSecretConfigured bool     `json:"bot_secret_configured"`
	APIKeyConfigured    bool     `json:"api_key_configured"`
	EncryptionAvailable bool     `json:"encryption_available"`
}
type RuntimeConfig struct {
	Config
	BotSecret string
	APIKey    string
}

func DefaultConfig() Config {
	return Config{ReceiveMode: ReceiveModeWebhook, AllowedGroups: []string{}, UserDailyLimit: 20, GroupConcurrency: 2, GlobalConcurrency: 5, QueueLimit: 50, QueueTimeoutSeconds: 300, DeadlineSeconds: 7200, RetentionDays: 7, PromptMaxChars: 2000}
}

type BindingRequest struct {
	ID          string    `json:"id"`
	Status      string    `json:"status"`
	GroupID     string    `json:"group_id"`
	SubjectHint string    `json:"subject_hint"`
	ExpiresAt   time.Time `json:"expires_at"`
	Revision    int64     `json:"revision"`
	Identity    Identity  `json:"-"`
}
type BindingAuthorization struct {
	ConfirmationCode string    `json:"confirmation_code"`
	ExpiresAt        time.Time `json:"expires_at"`
}
type Grant struct {
	ID        string     `json:"id"`
	UserID    int64      `json:"user_id"`
	Identity  Identity   `json:"identity"`
	GroupID   string     `json:"group_id"`
	Status    string     `json:"status"`
	Revision  int64      `json:"revision"`
	CreatedAt time.Time  `json:"created_at"`
	RevokedAt *time.Time `json:"revoked_at,omitempty"`
}
type Order struct {
	ID                 string     `json:"id"`
	UserID             int64      `json:"user_id"`
	GrantID            string     `json:"grant_id"`
	GrantRevision      int64      `json:"grant_revision"`
	ConfigID           int64      `json:"config_id"`
	Identity           Identity   `json:"-"`
	EventID            string     `json:"-"`
	ReplyMessageID     string     `json:"-"`
	Prompt             string     `json:"prompt"`
	Price              string     `json:"price"`
	Currency           string     `json:"currency"`
	ProviderCost       *string    `json:"provider_cost"`
	ProviderCurrency   *string    `json:"provider_currency"`
	ProviderCostStatus string     `json:"provider_cost_status"`
	Model              string     `json:"model"`
	Size               string     `json:"size"`
	Status             string     `json:"status"`
	FundsStatus        string     `json:"funds_status"`
	DeliveryStatus     string     `json:"delivery_status"`
	WebsiteStatus      string     `json:"website_status"`
	CreatedAt          time.Time  `json:"created_at"`
	Deadline           time.Time  `json:"deadline"`
	QueueDeadline      time.Time  `json:"queue_deadline"`
	ArtifactExpiresAt  *time.Time `json:"artifact_expires_at,omitempty"`
	Error              string     `json:"error"`
	LeaseToken         string     `json:"-"`
	LeaseUntil         *time.Time `json:"-"`
	Artifact           *Artifact  `json:"-"`
}
type Artifact struct {
	Data   []byte
	MIME   string
	Width  int
	Height int
	SHA256 string
}
type Delivery struct {
	ID             string
	Order          *Order
	LeaseToken     string
	ReplyMessageID string
	ReplySequence  int
	ReplyCreatedAt time.Time
	ArtifactBytes  int64
}
type Audit struct {
	ID        int64     `json:"id"`
	ActorID   int64     `json:"actor_id"`
	Action    string    `json:"action"`
	ObjectID  string    `json:"object_id"`
	Reason    string    `json:"reason"`
	CreatedAt time.Time `json:"created_at"`
}
type CacheInvalidation struct {
	ID     int64
	UserID int64
}
