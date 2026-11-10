// Package qqinvite owns the independent NapCat invitation reward ledger.
package qqinvite

import (
	"context"
	"errors"
	"time"
)

var (
	ErrInvalid               = errors.New("qq_invite_invalid")
	ErrConflict              = errors.New("qq_invite_conflict")
	ErrNotFound              = errors.New("qq_invite_not_found")
	ErrDisabled              = errors.New("qq_invite_disabled")
	ErrEncryptionUnavailable = errors.New("qq_invite_encryption_unavailable")
	ErrDeliveryUnknown       = errors.New("qq_invite_delivery_unknown")
)

type Config struct {
	Version               int64  `json:"version"`
	Enabled               bool   `json:"enabled"`
	OneBotURL             string `json:"onebot_url"`
	WebUIURL              string `json:"webui_url"`
	BotQQ                 string `json:"bot_qq"`
	OneBotToken           string `json:"onebot_token,omitempty"`
	WebUIToken            string `json:"webui_token,omitempty"`
	EventSecret           string `json:"event_secret,omitempty"`
	PublicBaseURL         string `json:"public_base_url"`
	OneBotTokenConfigured bool   `json:"onebot_token_configured"`
	WebUITokenConfigured  bool   `json:"webui_token_configured"`
	EventSecretConfigured bool   `json:"event_secret_configured"`
	EncryptionAvailable   bool   `json:"encryption_available"`
}
type Campaign struct {
	ID                      int64     `json:"id"`
	Version                 int64     `json:"version"`
	Name                    string    `json:"name"`
	PlanName                string    `json:"plan_name"`
	Enabled                 bool      `json:"enabled"`
	GroupIDs                []string  `json:"group_ids"`
	SubscriptionGroupID     int64     `json:"subscription_group_id"`
	StartsAt                time.Time `json:"starts_at"`
	EndsAt                  time.Time `json:"ends_at"`
	WelcomeTemplate         string    `json:"welcome_template"`
	CongratulationTemplate  string    `json:"congratulation_template"`
	PendingTemplate         string    `json:"pending_template"`
	DeliveryPendingTemplate string    `json:"delivery_pending_template"`
	PrivateTemplate         string    `json:"private_template"`
	BaselineReady           bool      `json:"baseline_ready"`
	CreatedAt               time.Time `json:"created_at"`
}
type Event struct {
	Kind       string `json:"kind"`
	SelfID     string `json:"self_id"`
	GroupID    string `json:"group_id"`
	UserID     string `json:"user_id"`
	OperatorID string `json:"operator_id"`
	SubType    string `json:"sub_type"`
	Time       int64  `json:"time"`
	EventID    string `json:"event_id,omitempty"`
}
type Record struct {
	ID            int64     `json:"id"`
	CampaignID    int64     `json:"campaign_id"`
	GroupID       string    `json:"group_id"`
	InviteeQQ     string    `json:"invitee_qq"`
	InviterQQ     string    `json:"inviter_qq"`
	QQLevel       *int      `json:"qq_level"`
	Status        string    `json:"status"`
	Reason        string    `json:"reason"`
	RewardID      *int64    `json:"reward_id,omitempty"`
	CodeHint      string    `json:"code_hint,omitempty"`
	PrivateStatus string    `json:"private_status,omitempty"`
	GroupStatus   string    `json:"group_status,omitempty"`
	CreatedAt     time.Time `json:"created_at"`
}
type RecordList struct {
	Items []Record `json:"items"`
	Total int      `json:"total"`
}
type CampaignList struct {
	Items []Campaign `json:"items"`
	Total int        `json:"total"`
}
type PoolCode struct {
	ID         int64     `json:"id"`
	CampaignID int64     `json:"campaign_id"`
	CodeHint   string    `json:"code_hint"`
	State      string    `json:"state"`
	RecordID   *int64    `json:"record_id,omitempty"`
	CreatedAt  time.Time `json:"created_at"`
}
type CodeList struct {
	Items     []PoolCode `json:"items"`
	Total     int        `json:"total"`
	Available int        `json:"available"`
	Assigned  int        `json:"assigned"`
}
type CodePreviewItem struct {
	Index    int    `json:"index"`
	Valid    bool   `json:"valid"`
	Reason   string `json:"reason"`
	CodeHint string `json:"code_hint"`
}
type CodePreview struct {
	ValidCount   int               `json:"valid_count"`
	InvalidCount int               `json:"invalid_count"`
	Items        []CodePreviewItem `json:"items"`
}
type Bot interface {
	GetLevel(context.Context, string) (int, error)
	GroupMembers(context.Context, string) ([]string, error)
	SendGroup(context.Context, string, string, string) (string, error)
	SendPrivate(context.Context, string, string) (string, error)
}
type BotFactory func(Config) (Bot, error)
