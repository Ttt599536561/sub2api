package handler

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"github.com/Wei-Shaw/sub2api/internal/qqinvite"
	"strconv"
	"strings"
	"time"
)

// Only explicitly supported OneBot messages enter the reward ledger. The caller
// verifies the exact original body before this normalization.
func parseQQInviteEvent(body []byte) (qqinvite.Event, bool, error) {
	var wire struct {
		PostType    string          `json:"post_type"`
		NoticeType  string          `json:"notice_type"`
		MessageType string          `json:"message_type"`
		SubType     string          `json:"sub_type"`
		SelfID      json.RawMessage `json:"self_id"`
		GroupID     json.RawMessage `json:"group_id"`
		UserID      json.RawMessage `json:"user_id"`
		OperatorID  json.RawMessage `json:"operator_id"`
		MessageID   json.RawMessage `json:"message_id"`
		Time        int64           `json:"time"`
		RawMessage  string          `json:"raw_message"`
	}
	if json.Unmarshal(body, &wire) != nil {
		return qqinvite.Event{}, false, qqinvite.ErrInvalid
	}
	kind := ""
	switch {
	case wire.PostType == "notice" && wire.NoticeType == "group_increase":
		kind = "group_increase"
	case wire.PostType == "notice" && wire.NoticeType == "group_decrease":
		if wire.SubType != "leave" && wire.SubType != "kick" && wire.SubType != "kick_me" {
			return qqinvite.Event{}, false, nil
		}
		kind = "group_decrease"
	case wire.PostType == "message" && wire.MessageType == "private" && (strings.TrimSpace(wire.RawMessage) == "/领取奖励" || strings.TrimSpace(wire.RawMessage) == "领取奖励"):
		kind = "private_claim"
	default:
		return qqinvite.Event{}, false, nil
	}
	if wire.Time <= 0 || wire.Time > time.Now().Add(5*time.Minute).Unix() {
		return qqinvite.Event{}, false, qqinvite.ErrInvalid
	}
	e := qqinvite.Event{Kind: kind, SubType: wire.SubType, Time: wire.Time}
	var err error
	if e.SelfID, err = qqInviteID(wire.SelfID, false); err != nil {
		return e, false, err
	}
	if e.UserID, err = qqInviteID(wire.UserID, false); err != nil {
		return e, false, err
	}
	if kind == "group_increase" || kind == "group_decrease" {
		if e.GroupID, err = qqInviteID(wire.GroupID, false); err != nil {
			return e, false, err
		}
		if e.OperatorID, err = qqInviteID(wire.OperatorID, true); err != nil {
			return e, false, err
		}
		if kind == "group_increase" && e.SubType != "invite" && e.SubType != "approve" {
			e.SubType = "unknown"
		}
	}
	// Source callback delivery times/JSON ordering must not create new rewards.
	key := strings.Join([]string{e.SelfID, e.Kind, e.GroupID, e.UserID, e.OperatorID, e.SubType, strconv.FormatInt(e.Time, 10)}, "|")
	if kind == "private_claim" {
		var msg string
		if json.Unmarshal(wire.MessageID, &msg) != nil {
			msg = string(wire.MessageID)
		}
		if len(msg) > 128 {
			return e, false, qqinvite.ErrInvalid
		}
		key += "|" + msg
	}
	hash := sha256.Sum256([]byte(key))
	e.EventID = hex.EncodeToString(hash[:])
	return e, true, nil
}
func qqInviteID(raw json.RawMessage, optional bool) (string, error) {
	if len(raw) == 0 || string(raw) == "null" {
		if optional {
			return "", nil
		}
		return "", qqinvite.ErrInvalid
	}
	var value string
	if raw[0] == '"' {
		if json.Unmarshal(raw, &value) != nil {
			return "", qqinvite.ErrInvalid
		}
	} else {
		value = string(raw)
	}
	if optional && value == "0" {
		return "", nil
	}
	n, err := strconv.ParseUint(value, 10, 64)
	if err != nil || n == 0 || strconv.FormatUint(n, 10) != value || len(value) > 20 {
		return "", qqinvite.ErrInvalid
	}
	return value, nil
}
