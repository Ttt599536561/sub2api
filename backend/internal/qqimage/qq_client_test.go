package qqimage

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func qqClientServer(t *testing.T, h http.HandlerFunc) *QQClient {
	t.Helper()
	s := httptest.NewTLSServer(h)
	t.Cleanup(s.Close)
	client := s.Client()
	original := client.Transport
	client.Transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
		copy := r.Clone(r.Context())
		target := *r.URL
		target.Host = strings.TrimPrefix(s.URL, "https://")
		copy.URL = &target
		return original.RoundTrip(copy)
	})
	return &QQClient{client: client, now: time.Now}
}
func qqConfig() *RuntimeConfig {
	return &RuntimeConfig{Config: Config{ID: 42, Enabled: true, AppID: "123", AllowedGroups: []string{"group-1"}, PublicBaseURL: "https://site.example.com"}, BotSecret: "secret"}
}
func qqDelivery() *Delivery {
	return &Delivery{ID: "delivery1", Order: &Order{ID: "order1", Identity: Identity{Platform: "qq", AppID: "123", SubjectType: "group_member", SubjectID: "member1", ScopeType: "group", ScopeID: "group-1", GroupID: "group-1"}}, ReplyMessageID: "message1", ReplySequence: 2, ReplyCreatedAt: time.Now()}
}
func TestQQClientUploadsThenSendsImageWithPersistentSequence(t *testing.T) {
	requests := []string{}
	c := qqClientServer(t, func(w http.ResponseWriter, r *http.Request) {
		requests = append(requests, r.URL.Path)
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		switch r.URL.Path {
		case "/app/getAppAccessToken":
			if body["appId"] != "123" || body["clientSecret"] != "secret" {
				t.Errorf("token request invalid")
			}
			_, _ = io.WriteString(w, `{"access_token":"token","expires_in":"7200"}`)
		case "/v2/groups/group-1/files":
			if r.Header.Get("Authorization") != "QQBot token" || body["file_type"] != float64(1) || body["srv_send_msg"] != false || body["url"] != "https://site.example.com/api/v1/qq-image/media?token=opaque" {
				t.Errorf("invalid media request: %+v", body)
			}
			_, _ = io.WriteString(w, `{"file_info":"opaque-media","ttl":60}`)
		case "/v2/groups/group-1/messages":
			media, _ := body["media"].(map[string]any)
			if body["msg_type"] != float64(7) || body["msg_seq"] != float64(2) || body["msg_id"] != "message1" || media["file_info"] != "opaque-media" {
				t.Errorf("invalid image message: %+v", body)
			}
			_, _ = io.WriteString(w, `{"id":"receipt1"}`)
		default:
			t.Errorf("unexpected request %s", r.URL.Path)
		}
	})
	result := c.SendDelivery(context.Background(), qqConfig(), qqDelivery(), "https://site.example.com/api/v1/qq-image/media?token=opaque")
	if result.Status != "sent" || len(requests) != 3 {
		t.Fatalf("result=%+v requests=%v", result, requests)
	}
}
func TestQQClientNoBlindRetryUnknownMessage(t *testing.T) {
	calls := 0
	c := qqClientServer(t, func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.URL.Path == "/app/getAppAccessToken" {
			_, _ = io.WriteString(w, `{"access_token":"token","expires_in":7200}`)
			return
		}
		if strings.HasSuffix(r.URL.Path, "/files") {
			_, _ = io.WriteString(w, `{"file_info":"media","ttl":60}`)
			return
		}
		w.WriteHeader(503)
	})
	result := c.SendDelivery(context.Background(), qqConfig(), qqDelivery(), "https://site.example.com/api/v1/qq-image/media?token=opaque")
	if result.Status != "unknown" || calls != 3 {
		t.Fatalf("unknown result retried: %+v calls=%d", result, calls)
	}
}
func TestQQClientKnownMediaFailureFallsBackToSite(t *testing.T) {
	calls := 0
	c := qqClientServer(t, func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.URL.Path == "/app/getAppAccessToken" {
			_, _ = io.WriteString(w, `{"access_token":"token","expires_in":7200}`)
			return
		}
		_, _ = io.WriteString(w, `{"code":40034004,"message":"secret details"}`)
	})
	result := c.SendDelivery(context.Background(), qqConfig(), qqDelivery(), "https://site.example.com/api/v1/qq-image/media?token=opaque")
	if result.Status != "site_only" || calls != 2 || strings.Contains(result.Reason, "secret") {
		t.Fatalf("bad fallback: %+v calls=%d", result, calls)
	}
}
func TestQQClientRefusesExpiredAndWrongAppReply(t *testing.T) {
	calls := 0
	c := qqClientServer(t, func(w http.ResponseWriter, r *http.Request) { calls++ })
	for _, kind := range []string{"expired", "wrongapp", "badsequence"} {
		d := qqDelivery()
		switch kind {
		case "expired":
			d.ReplyCreatedAt = time.Now().Add(-6 * time.Minute)
		case "wrongapp":
			d.Order.Identity.AppID = "other"
		case "badsequence":
			d.ReplySequence = 6
		}
		if result := c.SendDelivery(context.Background(), qqConfig(), d, "https://site.example.com/a"); result.Status != "site_only" {
			t.Errorf("unsafe send: %+v", result)
		}
	}
	if calls != 0 {
		t.Fatal("ineligible reply made network request")
	}
}
func TestQQClientTextTokenReuseAndHTTP200Error(t *testing.T) {
	tokens := 0
	messages := 0
	c := qqClientServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/app/getAppAccessToken" {
			tokens++
			_, _ = io.WriteString(w, `{"access_token":"token","expires_in":7200}`)
			return
		}
		messages++
		_, _ = io.WriteString(w, `{"code":40034004,"message":"permission denied"}`)
	})
	event := &QQEvent{MessageID: "msg", Identity: qqDelivery().Order.Identity, CreatedAt: time.Now()}
	for i := 0; i < 2; i++ {
		result := c.SendText(context.Background(), qqConfig(), event, "Please use the website", 1)
		if result.Status != "failed" {
			t.Fatalf("HTTP200 business error accepted: %+v", result)
		}
	}
	if tokens != 1 || messages != 2 {
		t.Fatalf("token cache not reused %d %d", tokens, messages)
	}
}

func TestQQClientDisabledEntranceStillAllowsExistingOrderReply(t *testing.T) {
	c := qqClientServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/app/getAppAccessToken" {
			_, _ = io.WriteString(w, `{"access_token":"token","expires_in":7200}`)
			return
		}
		_, _ = io.WriteString(w, `{"id":"reply"}`)
	})
	config := qqConfig()
	config.Enabled = false
	event := &QQEvent{MessageID: "msg", Identity: qqDelivery().Order.Identity, CreatedAt: time.Now()}
	if result := c.SendText(context.Background(), config, event, "已有订单仍可领取", 1); result.Status != "sent" {
		t.Fatalf("closing new orders blocked an existing order reply: %+v", result)
	}
}

func TestQQClientOversizeImageRemainsPrivateSiteOnly(t *testing.T) {
	calls := 0
	c := qqClientServer(t, func(w http.ResponseWriter, r *http.Request) { calls++ })
	d := qqDelivery()
	d.ArtifactBytes = 5*1024*1024 + 1
	result := c.SendDelivery(context.Background(), qqConfig(), d, "https://site.example.com/api/v1/qq-image/media?token=opaque")
	if result.Status != "site_only" || result.Reason != "qq_media_too_large" || calls != 0 {
		t.Fatalf("oversize QQ upload attempted: %+v calls=%d", result, calls)
	}
}
