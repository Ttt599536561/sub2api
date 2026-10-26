package qqimage

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
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
			if body["msg_type"] != float64(7) || body["content"] != " " || body["msg_seq"] != float64(2) || body["msg_id"] != "message1" || media["file_info"] != "opaque-media" {
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

func TestQQClientHTTPFailureDiagnosticsIdentifyStageAndCode(t *testing.T) {
	for _, tt := range []struct {
		name, path, reason string
		calls              int
	}{
		{"token", "/app/getAppAccessToken", "qq_token_http_400_code_40034004", 1},
		{"upload", "/v2/groups/group-1/files", "qq_media_upload_http_400_code_40034004", 2},
		{"send", "/v2/groups/group-1/messages", "qq_message_send_http_400_code_40034004", 3},
	} {
		t.Run(tt.name, func(t *testing.T) {
			calls := 0
			c := qqClientServer(t, func(w http.ResponseWriter, r *http.Request) {
				calls++
				if r.URL.Path == tt.path {
					w.WriteHeader(http.StatusBadRequest)
					_, _ = io.WriteString(w, `{"code":40034004,"message":"secret details","file_info":"secret media","url":"https://site.example.com/?token=private"}`)
					return
				}
				if r.URL.Path == "/app/getAppAccessToken" {
					_, _ = io.WriteString(w, `{"access_token":"token","expires_in":7200}`)
					return
				}
				_, _ = io.WriteString(w, `{"file_info":"media","ttl":60}`)
			})
			result := c.SendDelivery(context.Background(), qqConfig(), qqDelivery(), "https://site.example.com/api/v1/qq-image/media?token=opaque")
			if result.Status != "site_only" || result.Reason != tt.reason || calls != tt.calls {
				t.Fatalf("result=%+v calls=%d", result, calls)
			}
		})
	}
}

func TestQQClientHTTPErrorBodyIsBoundedAndPrivate(t *testing.T) {
	valid := `{"code":40034004,"message":"secret details"}`
	for _, tt := range []struct {
		name, body, suffix string
	}{
		{"html", "<html>secret details</html>", ""},
		{"truncated", `{"code":40034004,"message":"secret`, ""},
		{"string_code", `{"code":"secret details"}`, ""},
		{"object_code", `{"code":{"secret":"details"}}`, ""},
		{"fraction_code", `{"code":400.5}`, ""},
		{"exponent_code", `{"code":4e7}`, ""},
		{"overflow_code", `{"code":9223372036854775808}`, ""},
		{"null_code", `{"code":null}`, ""},
		{"missing_code", `{"message":"secret details"}`, ""},
		{"zero_code", `{"code":0}`, "_code_0"},
		{"at_limit", valid + strings.Repeat(" ", (64<<10)-len(valid)), "_code_40034004"},
		{"over_limit", valid + strings.Repeat(" ", (64<<10)+1-len(valid)), ""},
	} {
		t.Run(tt.name, func(t *testing.T) {
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
				w.WriteHeader(http.StatusBadRequest)
				_, _ = io.WriteString(w, tt.body)
			})
			result := c.SendDelivery(context.Background(), qqConfig(), qqDelivery(), "https://site.example.com/api/v1/qq-image/media?token=opaque")
			if result.Status != "site_only" || result.Reason != "qq_message_send_http_400"+tt.suffix || calls != 3 {
				t.Fatalf("result=%+v calls=%d", result, calls)
			}
		})
	}
}

func TestQQClientHTTPDiagnosticsPreserveUnknownDelivery(t *testing.T) {
	for _, tt := range []struct {
		name, reason string
		status       int
	}{
		{"server_error", "qq_message_send_http_503_code_40034004", http.StatusServiceUnavailable},
		{"request_timeout", "qq_message_send_http_408_code_40034004", http.StatusRequestTimeout},
		{"redirect", "qq_message_send_http_302_code_40034004", http.StatusFound},
		{"network_error", "qq_result_unknown", 0},
	} {
		t.Run(tt.name, func(t *testing.T) {
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
				w.Header().Set("Location", "https://untrusted.example/secret")
				w.WriteHeader(tt.status)
				_, _ = io.WriteString(w, `{"code":40034004,"message":"secret details"}`)
			})
			if tt.status == 0 {
				original := c.client.Transport
				c.client.Transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
					if strings.HasSuffix(r.URL.Path, "/messages") {
						calls++
						return nil, errors.New("secret transport details")
					}
					return original.RoundTrip(r)
				})
			}
			result := c.SendDelivery(context.Background(), qqConfig(), qqDelivery(), "https://site.example.com/api/v1/qq-image/media?token=opaque")
			if result.Status != "unknown" || result.Reason != tt.reason || calls != 3 {
				t.Fatalf("uncertain delivery was changed or retried: result=%+v calls=%d", result, calls)
			}
		})
	}
}

func TestQQClientTextHTTPDiagnosticsPreserveCustomContent(t *testing.T) {
	calls := 0
	const content = "用户自定义文案"
	c := qqClientServer(t, func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.URL.Path == "/app/getAppAccessToken" {
			_, _ = io.WriteString(w, `{"access_token":"token","expires_in":7200}`)
			return
		}
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		if body["content"] != content || body["msg_seq"] != float64(1) || body["msg_type"] != float64(0) {
			t.Errorf("custom text changed: %+v", body)
		}
		w.WriteHeader(http.StatusBadRequest)
		_, _ = io.WriteString(w, `{"code":40034004,"message":"secret details"}`)
	})
	event := &QQEvent{MessageID: "msg", Identity: qqDelivery().Order.Identity, CreatedAt: time.Now()}
	result := c.SendText(context.Background(), qqConfig(), event, content, 1)
	if result.Status != "failed" || result.Reason != "qq_message_send_http_400_code_40034004" || calls != 2 {
		t.Fatalf("result=%+v calls=%d", result, calls)
	}
}

func TestQQClientSlowHTTPErrorBodyHonorsTimeout(t *testing.T) {
	for _, tt := range []struct {
		name, status, reason string
		httpStatus           int
		clientTimeout        bool
	}{
		{"client_400", "site_only", "qq_message_send_http_400", http.StatusBadRequest, true},
		{"client_503", "unknown", "qq_message_send_http_503", http.StatusServiceUnavailable, true},
		{"context_400", "site_only", "qq_message_send_http_400", http.StatusBadRequest, false},
		{"context_503", "unknown", "qq_message_send_http_503", http.StatusServiceUnavailable, false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			var calls, messages atomic.Int32
			finished := make(chan struct{}, 1)
			c := qqClientServer(t, func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				if r.URL.Path == "/app/getAppAccessToken" {
					_, _ = io.WriteString(w, `{"access_token":"token","expires_in":7200}`)
					return
				}
				if strings.HasSuffix(r.URL.Path, "/files") {
					_, _ = io.WriteString(w, `{"file_info":"media","ttl":60}`)
					return
				}
				messages.Add(1)
				flusher, ok := w.(http.Flusher)
				if !ok {
					t.Error("test response writer does not support flushing")
					return
				}
				w.WriteHeader(tt.httpStatus)
				flusher.Flush()
				_, _ = io.WriteString(w, `{"code":40034004,"message":"secret unfinished body`)
				flusher.Flush()
				<-r.Context().Done()
				select {
				case finished <- struct{}{}:
				default:
				}
			})
			// Warm the local TLS connection and token before timing body cancellation.
			if _, reason := c.token(context.Background(), qqConfig()); reason != "" {
				t.Fatalf("token setup failed: %s", reason)
			}
			const shortTimeout = 250 * time.Millisecond
			const outerTimeout = 2 * time.Second
			deadline := shortTimeout
			c.client.Timeout = outerTimeout
			if tt.clientTimeout {
				c.client.Timeout = shortTimeout
				deadline = outerTimeout
			}
			ctx, cancel := context.WithTimeout(context.Background(), deadline)
			defer cancel()
			started := time.Now()
			result := c.SendDelivery(ctx, qqConfig(), qqDelivery(), "https://site.example.com/api/v1/qq-image/media?token=opaque")
			elapsed := time.Since(started)
			if result.Status != tt.status || result.Reason != tt.reason || calls.Load() != 3 || messages.Load() != 1 {
				t.Fatalf("slow error body changed classification or retried: result=%+v calls=%d messages=%d", result, calls.Load(), messages.Load())
			}
			if elapsed >= outerTimeout {
				t.Fatalf("error body exceeded bounded timeout: %v", elapsed)
			}
			select {
			case <-finished:
			case <-time.After(time.Second):
				t.Fatal("server request did not finish after cancellation")
			}
		})
	}
}
