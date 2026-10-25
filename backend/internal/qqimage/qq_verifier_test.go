package qqimage

import (
	"crypto/ed25519"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"
)

func signQQ(t *testing.T, secret, stamp string, body []byte) http.Header {
	t.Helper()
	seed := strings.Repeat(secret, 32/len(secret)+1)[:32]
	key := ed25519.NewKeyFromSeed([]byte(seed))
	h := http.Header{}
	h.Set("X-Signature-Timestamp", stamp)
	h.Set("X-Signature-Ed25519", hex.EncodeToString(ed25519.Sign(key, append([]byte(stamp), body...))))
	return h
}
func qqBody() []byte {
	return []byte(`{"op":0,"id":"evt-1","t":"GROUP_AT_MESSAGE_CREATE","d":{"id":"msg-1","group_openid":"group-1","author":{"member_openid":"member-1","user_id":"evil"},"content":"<@!123> /生图 a tree","timestamp":"2026-10-23T10:00:00+08:00"}}`)
}
func TestQQVerifyTrustedGroupEvent(t *testing.T) {
	now := time.Date(2026, 10, 23, 2, 0, 0, 0, time.UTC)
	raw := qqBody()
	h := signQQ(t, "secret", strconv.FormatInt(now.Unix(), 10), raw)
	v := NewQQVerifier("123", "secret")
	v.now = func() time.Time { return now }
	event, challenge, err := v.Verify(h, raw)
	if err != nil || challenge != nil || event.Identity.SubjectID != "member-1" || event.Identity.ScopeID != "group-1" || event.Identity.AppID != "123" || event.MessageID != "msg-1" || event.Content != "/生图 a tree" {
		t.Fatalf("unexpected trusted event: %+v %v", event, err)
	}
}
func TestQQRejectsSignatureTamperAndReplay(t *testing.T) {
	now := time.Now()
	v := NewQQVerifier("123", "secret")
	v.now = func() time.Time { return now }
	raw := qqBody()
	for _, tc := range []struct {
		name, stamp string
		change      bool
	}{{"old", strconv.FormatInt(now.Add(-6*time.Minute).Unix(), 10), false}, {"future", strconv.FormatInt(now.Add(6*time.Minute).Unix(), 10), false}, {"invalid", "1e10", false}, {"tampered", strconv.FormatInt(now.Unix(), 10), true}} {
		t.Run(tc.name, func(t *testing.T) {
			h := signQQ(t, "secret", tc.stamp, raw)
			body := raw
			if tc.change {
				body = append(append([]byte{}, raw...), byte(' '))
			}
			if _, _, err := v.Verify(h, body); err == nil {
				t.Fatal("untrusted event accepted")
			}
		})
	}
	h := signQQ(t, "othersecret", strconv.FormatInt(now.Unix(), 10), raw)
	if _, _, err := v.Verify(h, raw); err == nil {
		t.Fatal("other app secret accepted")
	}
}
func TestQQRejectsUntrustedScopeAndMissingIDs(t *testing.T) {
	now := time.Now()
	v := NewQQVerifier("123", "secret")
	v.now = func() time.Time { return now }
	for _, body := range []string{strings.Replace(string(qqBody()), "GROUP_AT_MESSAGE_CREATE", "C2C_MESSAGE_CREATE", 1), strings.Replace(string(qqBody()), `"member_openid":"member-1"`, `"user_openid":"member-1"`, 1), strings.Replace(string(qqBody()), `"id":"evt-1"`, `"id":""`, 1), strings.Replace(string(qqBody()), `<@!123>`, `<@!999>`, 1)} {
		raw := []byte(body)
		if _, _, err := v.Verify(signQQ(t, "secret", strconv.FormatInt(now.Unix(), 10), raw), raw); err == nil {
			t.Errorf("invalid scope accepted %s", body)
		}
	}
}
func TestQQChallenge(t *testing.T) {
	now := time.Now()
	v := NewQQVerifier("123", "secret")
	v.now = func() time.Time { return now }
	stamp := strconv.FormatInt(now.Unix(), 10)
	raw := []byte(fmt.Sprintf(`{"op":13,"d":{"plain_token":"plain-token","event_ts":"%s"}}`, stamp))
	event, response, err := v.Verify(http.Header{}, raw)
	if err != nil || event != nil || response == nil || response.PlainToken != "plain-token" {
		t.Fatalf("challenge: %+v %v", response, err)
	}
	seed := strings.Repeat("secret", 6)[:32]
	key := ed25519.NewKeyFromSeed([]byte(seed))
	signature, _ := hex.DecodeString(response.Signature)
	if !ed25519.Verify(key.Public().(ed25519.PublicKey), []byte(stamp+"plain-token"), signature) {
		t.Fatal("challenge signature invalid")
	}
	raw = []byte(`{"op":13,"d":{"plain_token":"x","event_ts":"1"}}`)
	if _, _, err := v.Verify(http.Header{}, raw); err == nil {
		t.Fatal("stale challenge accepted")
	}
}
func TestQQUnsignedBusinessAlwaysRejected(t *testing.T) {
	v := NewQQVerifier("123", "secret")
	if _, _, err := v.Verify(http.Header{}, qqBody()); err == nil {
		t.Fatal("unsigned business event accepted")
	}
	var payload map[string]any
	_ = json.Unmarshal(qqBody(), &payload)
	payload["op"] = 13
	raw, _ := json.Marshal(payload)
	if _, _, err := v.Verify(http.Header{}, raw); err == nil {
		t.Fatal("business event disguised as challenge accepted")
	}
}

func TestQQChallengeCannotSignBusinessEvent(t *testing.T) {
	now := time.Date(2026, 10, 23, 2, 0, 0, 0, time.UTC)
	verifier := NewQQVerifier("123", "secret")
	verifier.now = func() time.Time { return now }
	stamp := strconv.FormatInt(now.Unix(), 10)
	business := qqBody()
	attack, _ := json.Marshal(map[string]any{"op": 13, "d": map[string]string{"plain_token": string(business), "event_ts": stamp}})
	_, challenge, err := verifier.Verify(http.Header{}, attack)
	if err != nil {
		return
	}
	if challenge == nil {
		t.Fatal("invalid challenge returned no error")
	}
	replay := http.Header{}
	replay.Set("X-Signature-Timestamp", stamp)
	replay.Set("X-Signature-Ed25519", challenge.Signature)
	forged, _, replayErr := verifier.Verify(replay, business)
	if replayErr == nil && forged != nil {
		t.Fatalf("unsigned challenge minted a valid signature for forged business event %s", forged.EventID)
	}
	t.Fatal("unsigned challenge accepted business JSON as signing input")
}
func TestQQChallengeRejectsUnsafeTokensAndExtraFields(t *testing.T) {
	now := time.Now()
	verifier := NewQQVerifier("123", "secret")
	verifier.now = func() time.Time { return now }
	stamp := strconv.FormatInt(now.Unix(), 10)
	for _, token := range []string{`{}`, `"token"`, `[1]`, "a\\b", "a b", "a\nb", "a\tb", "令牌", "a\x00b", "a/b", "a=b", strings.Repeat("a", 257)} {
		raw, _ := json.Marshal(map[string]any{"op": 13, "d": map[string]string{"plain_token": token, "event_ts": stamp}})
		if _, challenge, err := verifier.Verify(http.Header{}, raw); err == nil || challenge != nil {
			t.Errorf("unsafe token accepted: %q", token)
		}
	}
	for _, extra := range []string{`"id":"",`, `"t":"",`, `"content":"/生图 tree",`, `"s":1,`} {
		raw := []byte(fmt.Sprintf(`{"op":13,%s"d":{"plain_token":"safe-token_123","event_ts":%q}}`, extra, stamp))
		if _, challenge, err := verifier.Verify(http.Header{}, raw); err == nil || challenge != nil {
			t.Errorf("extra top-level challenge field accepted: %s", extra)
		}
	}
}
func TestQQChallengeSafeTokenBoundary(t *testing.T) {
	now := time.Now()
	verifier := NewQQVerifier("123", "secret")
	verifier.now = func() time.Time { return now }
	stamp := strconv.FormatInt(now.Unix(), 10)
	for _, token := range []string{"a", strings.Repeat("A0_-", 64)} {
		raw, _ := json.Marshal(map[string]any{"op": 13, "d": map[string]string{"plain_token": token, "event_ts": stamp}})
		if _, challenge, err := verifier.Verify(http.Header{}, raw); err != nil || challenge == nil || challenge.PlainToken != token {
			t.Errorf("safe challenge rejected: %v", err)
		}
	}
}
