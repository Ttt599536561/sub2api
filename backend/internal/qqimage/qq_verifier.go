package qqimage

import (
	"crypto/ed25519"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"
)

const maxQQEventBytes = 64 << 10

var ErrQQAuthentication = errors.New("qq_event_authentication_failed")
var ErrQQUnsupportedEvent = errors.New("qq_event_not_supported")

type QQEvent struct {
	EventID   string
	MessageID string
	Identity  Identity
	Content   string
	CreatedAt time.Time
}
type QQChallenge struct {
	PlainToken string `json:"plain_token"`
	Signature  string `json:"signature"`
}
type QQVerifier struct {
	appID  string
	secret string
	now    func() time.Time
}

func NewQQVerifier(appID, secret string) *QQVerifier {
	return &QQVerifier{appID: appID, secret: secret, now: time.Now}
}
func qqPrivateKey(secret string) (ed25519.PrivateKey, error) {
	if len(secret) == 0 {
		return nil, ErrQQAuthentication
	}
	seed := secret
	for len(seed) < ed25519.SeedSize {
		seed += secret
	}
	return ed25519.NewKeyFromSeed([]byte(seed[:ed25519.SeedSize])), nil
}
func freshQQTimestamp(raw string, now time.Time) bool {
	if raw == "" || len(raw) > 12 {
		return false
	}
	for _, c := range raw {
		if c < '0' || c > '9' {
			return false
		}
	}
	stamp, err := strconv.ParseInt(raw, 10, 64)
	if err != nil {
		return false
	}
	delta := now.Sub(time.Unix(stamp, 0))
	return delta >= -5*time.Minute && delta <= 5*time.Minute
}

// QQ uses the same Ed25519 key for callback verification and challenge signing.
// An unsigned challenge must therefore never sign arbitrary text: allowing a
// JSON body here would mint a signature that authenticates a forged callback.
// This deliberately narrow token alphabet cannot contain a JSON object. Any
// other platform token format requires explicit verification before support.
func safeQQChallengeToken(token string) bool {
	if len(token) == 0 || len(token) > 256 {
		return false
	}
	for _, c := range []byte(token) {
		valid := (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '_' || c == '-'
		if !valid {
			return false
		}
	}
	return true
}

// Verify preserves the exact bytes of the signed timestamp and body. The only
// unsigned payload accepted is QQ's narrowly shaped op=13 endpoint challenge.
// Challenge traffic never constructs an identity or reaches business logic.
func (v *QQVerifier) Verify(headers http.Header, raw []byte) (*QQEvent, *QQChallenge, error) {
	if len(raw) == 0 || len(raw) > maxQQEventBytes || v.appID == "" {
		return nil, nil, ErrQQAuthentication
	}
	key, err := qqPrivateKey(v.secret)
	if err != nil {
		return nil, nil, err
	}
	var envelope struct {
		Op   int             `json:"op"`
		ID   string          `json:"id"`
		Type string          `json:"t"`
		Data json.RawMessage `json:"d"`
	}
	if json.Unmarshal(raw, &envelope) != nil {
		return nil, nil, ErrQQAuthentication
	}
	now := v.now()
	if id := headers.Get("X-Bot-Appid"); id != "" && id != v.appID {
		return nil, nil, ErrQQAuthentication
	}
	if envelope.Op == 13 {
		var challengeEnvelope struct {
			Op   int `json:"op"`
			Data struct {
				PlainToken string `json:"plain_token"`
				EventTS    string `json:"event_ts"`
			} `json:"d"`
		}
		decoder := json.NewDecoder(strings.NewReader(string(raw)))
		decoder.DisallowUnknownFields()
		if decoder.Decode(&challengeEnvelope) != nil {
			return nil, nil, ErrQQAuthentication
		}
		challenge := challengeEnvelope.Data
		if !safeQQChallengeToken(challenge.PlainToken) || !freshQQTimestamp(challenge.EventTS, now) {
			return nil, nil, ErrQQAuthentication
		}
		// Some QQ challenge deliveries do not include signature headers. If supplied,
		// both must validate; unsigned acceptance never extends to op=0 events.
		if headers.Get("X-Signature-Ed25519") != "" || headers.Get("X-Signature-Timestamp") != "" {
			if !v.validSignature(headers, raw, key, now) {
				return nil, nil, ErrQQAuthentication
			}
		}
		sig := ed25519.Sign(key, []byte(challenge.EventTS+challenge.PlainToken))
		return nil, &QQChallenge{PlainToken: challenge.PlainToken, Signature: hex.EncodeToString(sig)}, nil
	}
	if !v.validSignature(headers, raw, key, now) {
		return nil, nil, ErrQQAuthentication
	}
	event, err := parseQQGroupEvent(v.appID, raw, false)
	return event, nil, err
}

// parseQQGroupEvent is called only after authenticating the transport. The
// gateway can omit its envelope ID; the platform message ID is then stable.
func parseQQGroupEvent(appID string, raw []byte, gateway bool) (*QQEvent, error) {
	if len(raw) == 0 || len(raw) > maxQQEventBytes || appID == "" {
		return nil, ErrQQAuthentication
	}
	var envelope struct {
		Op   int             `json:"op"`
		ID   string          `json:"id"`
		Type string          `json:"t"`
		Data json.RawMessage `json:"d"`
	}
	if json.Unmarshal(raw, &envelope) != nil {
		return nil, ErrQQAuthentication
	}
	if envelope.Op != 0 || envelope.Type != "GROUP_AT_MESSAGE_CREATE" {
		return nil, ErrQQUnsupportedEvent
	}
	var data struct {
		ID        string    `json:"id"`
		GroupID   string    `json:"group_openid"`
		Content   string    `json:"content"`
		Timestamp time.Time `json:"timestamp"`
		Author    struct {
			MemberID string `json:"member_openid"`
		} `json:"author"`
	}
	if json.Unmarshal(envelope.Data, &data) != nil || len(envelope.ID) > 256 || data.ID == "" || len(data.ID) > 256 || data.Timestamp.IsZero() {
		return nil, ErrQQAuthentication
	}
	if envelope.ID == "" {
		if !gateway {
			return nil, ErrQQAuthentication
		}
		envelope.ID = data.ID
	}
	identity := Identity{Platform: "qq", AppID: appID, SubjectType: "group_member", SubjectID: data.Author.MemberID, ScopeType: "group", ScopeID: data.GroupID, GroupID: data.GroupID}
	if identity.Validate() != nil {
		return nil, ErrQQAuthentication
	}
	content := strings.TrimSpace(data.Content)
	if strings.HasPrefix(content, "<@") {
		prefixes := []string{"<@!" + appID + ">", "<@" + appID + ">"}
		matched := false
		for _, prefix := range prefixes {
			if strings.HasPrefix(content, prefix) {
				content = strings.TrimSpace(strings.TrimPrefix(content, prefix))
				matched = true
				break
			}
		}
		if !matched {
			return nil, ErrQQAuthentication
		}
	}
	return &QQEvent{EventID: envelope.ID, MessageID: data.ID, Identity: identity, Content: content, CreatedAt: data.Timestamp}, nil
}
func (v *QQVerifier) validSignature(headers http.Header, raw []byte, key ed25519.PrivateKey, now time.Time) bool {
	timestamp := headers.Get("X-Signature-Timestamp")
	if !freshQQTimestamp(timestamp, now) {
		return false
	}
	signature, err := hex.DecodeString(headers.Get("X-Signature-Ed25519"))
	if err != nil || len(signature) != ed25519.SignatureSize {
		return false
	}
	signed := make([]byte, 0, len(timestamp)+len(raw))
	signed = append(signed, timestamp...)
	signed = append(signed, raw...)
	publicKey, ok := key.Public().(ed25519.PublicKey)
	return ok && ed25519.Verify(publicKey, signed, signature)
}
