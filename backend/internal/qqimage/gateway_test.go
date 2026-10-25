package qqimage

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/gorilla/websocket"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

const gatewayTestBootstrap = `{"url":"wss://api.sgroup.qq.com/websocket","shards":1,"session_start_limit":{"total":1000,"remaining":999,"reset_after":86400000,"max_concurrency":1}}`

func gatewayTestClient(t *testing.T, body string) *QQClient {
	t.Helper()
	return qqClientServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/app/getAppAccessToken" {
			_, _ = io.WriteString(w, `{"access_token":"private-access-token","expires_in":"7200"}`)
			return
		}
		if r.URL.Path != "/gateway/bot" || r.Method != http.MethodGet || r.Header.Get("Authorization") != "QQBot private-access-token" {
			t.Error("unexpected bootstrap request")
		}
		_, _ = io.WriteString(w, body)
	})
}
func gatewayFake(t *testing.T, handler func(*websocket.Conn, int), dispatch func(context.Context, []byte) error, options ...func(*Gateway)) (*Gateway, context.CancelFunc, <-chan struct{}) {
	t.Helper()
	var count atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, e := (&websocket.Upgrader{}).Upgrade(w, r, nil)
		if e != nil {
			return
		}
		defer c.Close()
		handler(c, int(count.Add(1)))
	}))
	t.Cleanup(server.Close)
	g := NewGateway(gatewayTestClient(t, gatewayTestBootstrap), qqConfig(), dispatch)
	g.dial = func(ctx context.Context, _ string) (*websocket.Conn, *http.Response, error) {
		return websocket.DefaultDialer.DialContext(ctx, "ws"+strings.TrimPrefix(server.URL, "http"), nil)
	}
	g.wait = func(ctx context.Context, _ time.Duration) bool { return ctx.Err() == nil }
	for _, option := range options {
		option(g)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { defer close(done); g.Run(ctx) }()
	t.Cleanup(func() {
		cancel()
		select {
		case <-done:
		case <-time.After(3 * time.Second):
			t.Error("gateway failed to stop")
		}
	})
	return g, cancel, done
}
func gatewaySend(t *testing.T, c *websocket.Conn, value string) bool {
	t.Helper()
	return c.WriteMessage(websocket.TextMessage, []byte(value)) == nil
}
func gatewayRead(c *websocket.Conn) map[string]json.RawMessage {
	_ = c.SetReadDeadline(time.Now().Add(2 * time.Second))
	_, b, e := c.ReadMessage()
	if e != nil {
		return nil
	}
	var d map[string]json.RawMessage
	_ = json.Unmarshal(b, &d)
	return d
}
func gatewayHello(t *testing.T, c *websocket.Conn, interval int) map[string]json.RawMessage {
	gatewaySend(t, c, fmt.Sprintf(`{"op":10,"d":{"heartbeat_interval":%d}}`, interval))
	return gatewayRead(c)
}
func gatewayEventually(t *testing.T, f func() bool) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if f() {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("gateway condition not reached")
}

func TestGatewayIdentifyHeartbeatOnlyGroupAndStop(t *testing.T) {
	var events atomic.Int32
	heartbeat := make(chan json.RawMessage, 2)
	g, cancel, done := gatewayFake(t, func(c *websocket.Conn, n int) {
		auth := gatewayHello(t, c, 25)
		if string(auth["op"]) != "2" {
			t.Error("expected identify")
		}
		var d struct {
			Token   string
			Intents int
			Shard   []int
		}
		_ = json.Unmarshal(auth["d"], &d)
		if d.Token != "QQBot private-access-token" || d.Intents != 1<<25 || len(d.Shard) != 2 || d.Shard[0] != 0 || d.Shard[1] != 1 {
			t.Error("invalid identify")
		}
		gatewaySend(t, c, `{"op":0,"t":"READY","s":1,"d":{"session_id":"private-session"}}`)
		gatewaySend(t, c, `{"op":0,"t":"C2C_MESSAGE_CREATE","s":2,"d":{}}`)
		gatewaySend(t, c, `{"op":0,"t":"GROUP_AT_MESSAGE_CREATE","s":3,"d":{},"id":"event"}`)
		for {
			m := gatewayRead(c)
			if m == nil {
				return
			}
			if string(m["op"]) == "1" {
				gatewaySend(t, c, `{"op":11}`)
				select {
				case heartbeat <- m["d"]:
				default:
				}
			}
		}
	}, func(_ context.Context, b []byte) error {
		if !strings.Contains(string(b), "GROUP_AT_MESSAGE_CREATE") {
			t.Error("private event dispatched")
		}
		events.Add(1)
		return nil
	})
	select {
	case seq := <-heartbeat:
		if string(seq) != "3" {
			t.Errorf("heartbeat checkpoint %s", seq)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("missing heartbeat")
	}
	gatewayEventually(t, func() bool {
		s := g.Status()
		return events.Load() == 1 && s.State == "connected" && s.LastHeartbeatAt != nil && s.LastConnectedAt != nil && s.LastEventAt != nil
	})
	encoded, _ := json.Marshal(g.Status())
	if strings.Contains(string(encoded), "private-") {
		t.Fatal("status leaked secret")
	}
	snapshot := g.Status()
	original := *snapshot.LastConnectedAt
	*snapshot.LastConnectedAt = time.Time{}
	if !g.Status().LastConnectedAt.Equal(original) {
		t.Fatal("mutable timestamp exposed")
	}
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("stop blocked")
	}
	if g.Status().State != "stopped" {
		t.Fatal("not stopped")
	}
}
func TestGatewayTransientFailureResumesPriorCheckpoint(t *testing.T) {
	var handled atomic.Int32
	resumed := make(chan int64, 1)
	release := make(chan struct{})
	g, cancel, _ := gatewayFake(t, func(c *websocket.Conn, n int) {
		auth := gatewayHello(t, c, 100)
		if n == 1 {
			gatewaySend(t, c, `{"op":0,"t":"READY","s":10,"d":{"session_id":"private-session"}}`)
			gatewaySend(t, c, `{"op":0,"t":"GROUP_AT_MESSAGE_CREATE","s":11,"d":{}}`)
			gatewaySend(t, c, `{"op":0,"t":"C2C_MESSAGE_CREATE","s":12,"d":{}}`)
			for gatewayRead(c) != nil {
			}
			return
		}
		if string(auth["op"]) != "6" {
			t.Errorf("expected resume got %s", auth["op"])
		}
		var d struct {
			Seq       int64
			Token     string
			SessionID string `json:"session_id"`
		}
		_ = json.Unmarshal(auth["d"], &d)
		if d.Token != "QQBot private-access-token" || d.SessionID != "private-session" {
			t.Error("invalid resume")
		}
		select {
		case resumed <- d.Seq:
		default:
		}
		gatewaySend(t, c, `{"op":0,"t":"RESUMED","s":10,"d":{}}`)
		gatewaySend(t, c, `{"op":0,"t":"GROUP_AT_MESSAGE_CREATE","s":11,"d":{}}`)
		<-release
	}, func(context.Context, []byte) error {
		if handled.Add(1) == 1 {
			return errors.New("database password=secret unavailable")
		}
		return nil
	})
	defer close(release)
	select {
	case seq := <-resumed:
		if seq != 10 {
			t.Fatalf("lost failed event: %d", seq)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("no resume")
	}
	gatewayEventually(t, func() bool { return handled.Load() == 2 && g.Status().LastEventAt != nil })
	cancel()
}
func TestGatewayKnownRejectionAdvancesCheckpoint(t *testing.T) {
	for _, domain := range []error{ErrInvalid, ErrForbidden, ErrExpired, ErrDisabled, ErrConflict, ErrNotFound, ErrQQAuthentication, ErrQQUnsupportedEvent, ErrInsufficientBalance, ErrLimit} {
		t.Run(domain.Error(), func(t *testing.T) {
			done := make(chan struct{}, 1)
			_, cancel, _ := gatewayFake(t, func(c *websocket.Conn, n int) {
				gatewayHello(t, c, 15)
				gatewaySend(t, c, `{"op":0,"t":"READY","s":1,"d":{"session_id":"session"}}`)
				gatewaySend(t, c, `{"op":0,"t":"GROUP_AT_MESSAGE_CREATE","s":2,"d":{}}`)
				for {
					m := gatewayRead(c)
					if m == nil {
						return
					}
					if string(m["op"]) == "1" {
						gatewaySend(t, c, `{"op":11}`)
						if string(m["d"]) == "2" {
							select {
							case done <- struct{}{}:
							default:
							}
						}
					}
				}
			}, func(context.Context, []byte) error { return fmt.Errorf("wrapped: %w", domain) })
			select {
			case <-done:
			case <-time.After(time.Second):
				t.Fatal("rejection not checkpointed")
			}
			cancel()
		})
	}
}
func TestGatewayInvalidSessionAndReconnect(t *testing.T) {
	for _, resumable := range []bool{false, true} {
		t.Run(fmt.Sprint(resumable), func(t *testing.T) {
			next := make(chan int, 1)
			hold := make(chan struct{})
			_, cancel, _ := gatewayFake(t, func(c *websocket.Conn, n int) {
				auth := gatewayHello(t, c, 20)
				if n == 1 {
					gatewaySend(t, c, `{"op":0,"t":"READY","s":4,"d":{"session_id":"session"}}`)
					for {
						m := gatewayRead(c)
						if m == nil {
							return
						}
						if string(m["op"]) == "1" {
							gatewaySend(t, c, fmt.Sprintf(`{"op":9,"d":%v}`, resumable))
							return
						}
					}
				}
				var op int
				_ = json.Unmarshal(auth["op"], &op)
				select {
				case next <- op:
				default:
				}
				<-hold
			}, func(context.Context, []byte) error { return nil })
			defer close(hold)
			select {
			case op := <-next:
				want := 2
				if resumable {
					want = 6
				}
				if op != want {
					t.Fatalf("got op %d want %d", op, want)
				}
			case <-time.After(2 * time.Second):
				t.Fatal("missing reconnect")
			}
			cancel()
		})
	}
}
func TestGatewayMissingACKReconnectsAndFatalCloseStops(t *testing.T) {
	second := make(chan struct{}, 1)
	g, _, done := gatewayFake(t, func(c *websocket.Conn, n int) {
		gatewayHello(t, c, 15)
		if n == 1 {
			gatewaySend(t, c, `{"op":0,"t":"READY","s":1,"d":{"session_id":"session"}}`)
			for gatewayRead(c) != nil {
			}
			return
		}
		select {
		case second <- struct{}{}:
		default:
		}
		_ = c.WriteControl(websocket.CloseMessage, websocket.FormatCloseMessage(4914, "private-session private-access-token"), time.Now().Add(time.Second))
	}, func(context.Context, []byte) error { return nil })
	select {
	case <-second:
	case <-time.After(time.Second):
		t.Fatal("missing ack did not reconnect")
	}
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("fatal close did not stop")
	}
	if s := g.Status(); s.State != "error" || s.ErrorCode != "qq_gateway_fatal_close" {
		t.Fatalf("unexpected status %+v", s)
	}
}
func TestGatewayBootstrapRejectsUnsafeResponses(t *testing.T) {
	bodies := []string{`{}`, `null`, strings.Repeat("x", 65537), strings.Replace(gatewayTestBootstrap, `"shards":1`, `"shards":2`, 1), strings.Replace(gatewayTestBootstrap, `"remaining":999`, `"remaining":-1`, 1), strings.Replace(gatewayTestBootstrap, `"max_concurrency":1`, `"max_concurrency":0`, 1), strings.Replace(gatewayTestBootstrap, `"reset_after":86400000`, `"reset_after":0`, 1)}
	for _, raw := range []string{"ws://api.sgroup.qq.com/websocket", "wss://api.sgroup.qq.com.evil.test/websocket", "wss://127.0.0.1/websocket", "wss://api.sgroup.qq.com:444/websocket", "wss://user:secret@api.sgroup.qq.com/websocket", "wss://api.sgroup.qq.com/websocket#secret", "wss://api.sgroup.qq.com/other", "wss://api.sgroup.qq.com/websocket?token=secret"} {
		bodies = append(bodies, strings.Replace(gatewayTestBootstrap, "wss://api.sgroup.qq.com/websocket", raw, 1))
	}
	for i, body := range bodies {
		t.Run(fmt.Sprint(i), func(t *testing.T) {
			g := NewGateway(gatewayTestClient(t, body), qqConfig(), func(context.Context, []byte) error { return nil })
			var dials atomic.Int32
			g.dial = func(context.Context, string) (*websocket.Conn, *http.Response, error) {
				dials.Add(1)
				return nil, nil, errors.New("should not dial")
			}
			g.wait = func(context.Context, time.Duration) bool { return false }
			g.Run(context.Background())
			if dials.Load() != 0 {
				t.Fatal("unsafe bootstrap reached dial")
			}
			if g.Status().ErrorCode == "" {
				t.Fatal("missing safe error")
			}
		})
	}
}
func TestGatewayClosePolicyAndBackoff(t *testing.T) {
	for _, code := range []int{4001, 4002, 4010, 4011, 4012, 4013, 4014, 4914, 4915} {
		if !gatewayCloseResult(code).terminal {
			t.Errorf("code %d must stop", code)
		}
	}
	for _, code := range []int{4006, 4007, 4900, 4903, 4913} {
		if !gatewayCloseResult(code).clearSession {
			t.Errorf("code %d must clear", code)
		}
	}
	if !gatewayCloseResult(4004).refreshToken || gatewayCloseResult(4009).clearSession || gatewayCloseResult(4008).delay < time.Minute {
		t.Fatal("incorrect token/rate/resume policy")
	}
	var previous time.Duration
	for i := 0; i < 20; i++ {
		d := gatewayBackoff(i)
		if d < time.Second || d > time.Minute || d < previous {
			t.Fatalf("unsafe backoff %s", d)
		}
		previous = d
	}
}

func TestGatewayResumeAllowsReplayBeforeResumed(t *testing.T) {
	received := make(chan struct{}, 1)
	hold := make(chan struct{})
	_, cancel, _ := gatewayFake(t, func(c *websocket.Conn, n int) {
		gatewayHello(t, c, 15)
		if n == 1 {
			gatewaySend(t, c, `{"op":0,"t":"READY","s":1,"d":{"session_id":"session"}}`)
			gatewayRead(c)
			gatewaySend(t, c, `{"op":7}`)
			return
		}
		gatewaySend(t, c, `{"op":0,"t":"GROUP_AT_MESSAGE_CREATE","s":2,"d":{}}`)
		gatewaySend(t, c, `{"op":0,"t":"RESUMED","s":3,"d":{}}`)
		<-hold
	}, func(context.Context, []byte) error {
		select {
		case received <- struct{}{}:
		default:
		}
		return nil
	})
	defer close(hold)
	select {
	case <-received:
	case <-time.After(time.Second):
		t.Fatal("resume replay before RESUMED was lost")
	}
	cancel()
}
func TestGatewaySlowBusinessDoesNotBlockHeartbeatAndCancellationJoins(t *testing.T) {
	entered := make(chan struct{})
	exited := make(chan struct{})
	heartbeats := make(chan struct{}, 5)
	_, cancel, done := gatewayFake(t, func(c *websocket.Conn, n int) {
		gatewayHello(t, c, 15)
		gatewaySend(t, c, `{"op":0,"t":"READY","s":1,"d":{"session_id":"session"}}`)
		gatewaySend(t, c, `{"op":0,"t":"GROUP_AT_MESSAGE_CREATE","s":2,"d":{}}`)
		for {
			m := gatewayRead(c)
			if m == nil {
				return
			}
			if string(m["op"]) == "1" {
				if string(m["d"]) != "2" {
					t.Error("heartbeat must report the latest received sequence while business is pending")
				}
				gatewaySend(t, c, `{"op":11}`)
				select {
				case heartbeats <- struct{}{}:
				default:
				}
			}
		}
	}, func(ctx context.Context, _ []byte) error {
		close(entered)
		defer close(exited)
		<-ctx.Done()
		return ctx.Err()
	})
	<-entered
	for i := 0; i < 3; i++ {
		select {
		case <-heartbeats:
		case <-time.After(time.Second):
			t.Fatal("business blocked heartbeat")
		}
	}
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("cancellation blocked")
	}
	select {
	case <-exited:
	default:
		t.Fatal("Run returned before dispatch joined")
	}
}
func TestGatewayQueueFullResumesAndJoinsInFlightDispatch(t *testing.T) {
	entered := make(chan struct{})
	exited := make(chan struct{})
	next := make(chan int64, 1)
	hold := make(chan struct{})
	_, cancel, _ := gatewayFake(t, func(c *websocket.Conn, n int) {
		auth := gatewayHello(t, c, 20)
		if n == 1 {
			gatewaySend(t, c, `{"op":0,"t":"READY","s":1,"d":{"session_id":"session"}}`)
			gatewayRead(c)
			gatewaySend(t, c, `{"op":11}`)
			gatewaySend(t, c, `{"op":0,"t":"GROUP_AT_MESSAGE_CREATE","s":2,"d":{}}`)
			<-entered
			gatewaySend(t, c, `{"op":0,"t":"C2C_MESSAGE_CREATE","s":3,"d":{}}`)
			gatewaySend(t, c, `{"op":0,"t":"C2C_MESSAGE_CREATE","s":4,"d":{}}`)
			for gatewayRead(c) != nil {
			}
			return
		}
		select {
		case <-exited:
		default:
			t.Error("reconnected before dispatch joined")
		}
		var d struct{ Seq int64 }
		_ = json.Unmarshal(auth["d"], &d)
		select {
		case next <- d.Seq:
		default:
		}
		<-hold
	}, func(ctx context.Context, _ []byte) error {
		close(entered)
		defer close(exited)
		<-ctx.Done()
		return ctx.Err()
	}, func(g *Gateway) { g.queueSize = 1; g.drainTimeout = 20 * time.Millisecond })
	defer close(hold)
	select {
	case seq := <-next:
		if seq != 1 {
			t.Fatalf("queue overflow checkpoint %d", seq)
		}
	case <-time.After(time.Second):
		t.Fatal("queue overflow did not resume")
	}
	cancel()
}
func TestGatewayHeartbeatBeforeReadyUsesNull(t *testing.T) {
	heartbeat := make(chan string, 1)
	hold := make(chan struct{})
	_, cancel, _ := gatewayFake(t, func(c *websocket.Conn, n int) {
		gatewayHello(t, c, 15)
		m := gatewayRead(c)
		heartbeat <- string(m["d"])
		gatewaySend(t, c, `{"op":11}`)
		<-hold
	}, func(context.Context, []byte) error { return nil })
	defer close(hold)
	select {
	case seq := <-heartbeat:
		if seq != "null" {
			t.Fatalf("initial heartbeat %q", seq)
		}
	case <-time.After(time.Second):
		t.Fatal("missing initial heartbeat")
	}
	cancel()
}
func TestGatewaySessionStartBudgetAndHTTPRedirect(t *testing.T) {
	t.Run("exhausted", func(t *testing.T) {
		g := NewGateway(gatewayTestClient(t, strings.Replace(gatewayTestBootstrap, `"remaining":999`, `"remaining":0`, 1)), qqConfig(), func(context.Context, []byte) error { return nil })
		var waited time.Duration
		g.wait = func(_ context.Context, d time.Duration) bool { waited = d; return false }
		g.dial = func(context.Context, string) (*websocket.Conn, *http.Response, error) {
			t.Error("dialed with exhausted identify budget")
			return nil, nil, errors.New("unexpected dial")
		}
		g.Run(context.Background())
		if waited < 23*time.Hour || g.Status().ErrorCode != "qq_gateway_session_limit" {
			t.Fatal("session limit not honored")
		}
	})
	t.Run("redirect", func(t *testing.T) {
		var redirected atomic.Int32
		target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { redirected.Add(1) }))
		defer target.Close()
		c := qqClientServer(t, func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, target.URL, http.StatusFound) })
		g := NewGateway(c, qqConfig(), func(context.Context, []byte) error { return nil })
		_, result := g.endpoint(context.Background(), "token")
		if !result.terminal || redirected.Load() != 0 {
			t.Fatal("followed bootstrap redirect")
		}
	})
}
func TestGatewayRefreshesOnlyOwnTokenAfter4004(t *testing.T) {
	var tokens atomic.Int32
	second := make(chan string, 1)
	client := qqClientServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/app/getAppAccessToken" {
			_, _ = fmt.Fprintf(w, `{"access_token":"token-%d","expires_in":"7200"}`, tokens.Add(1))
			return
		}
		_, _ = io.WriteString(w, gatewayTestBootstrap)
	})
	_, _, done := gatewayFake(t, func(c *websocket.Conn, n int) {
		auth := gatewayHello(t, c, 20)
		if n == 1 {
			_ = c.WriteControl(websocket.CloseMessage, websocket.FormatCloseMessage(4004, "expired"), time.Now().Add(time.Second))
			return
		}
		var d struct{ Token string }
		_ = json.Unmarshal(auth["d"], &d)
		second <- d.Token
		_ = c.WriteControl(websocket.CloseMessage, websocket.FormatCloseMessage(4915, "stop"), time.Now().Add(time.Second))
	}, func(context.Context, []byte) error { return nil }, func(g *Gateway) { g.client = client })
	select {
	case value := <-second:
		if value != "QQBot token-2" {
			t.Fatalf("token not refreshed %q", value)
		}
	case <-time.After(time.Second):
		t.Fatal("no refresh")
	}
	<-done
	if tokens.Load() != 2 {
		t.Fatal("unexpected token requests")
	}
}

func TestGatewayImmediateReconnectRetainsReadySession(t *testing.T) {
	for i := 0; i < 20; i++ {
		t.Run(fmt.Sprint(i), func(t *testing.T) {
			next := make(chan int, 1)
			hold := make(chan struct{})
			_, cancel, _ := gatewayFake(t, func(c *websocket.Conn, n int) {
				auth := gatewayHello(t, c, 100)
				if n == 1 {
					gatewaySend(t, c, `{"op":0,"t":"READY","s":1,"d":{"session_id":"session"}}`)
					gatewaySend(t, c, `{"op":7}`)
					return
				}
				var op int
				_ = json.Unmarshal(auth["op"], &op)
				select {
				case next <- op:
				default:
				}
				<-hold
			}, func(context.Context, []byte) error { return nil })
			defer close(hold)
			select {
			case op := <-next:
				if op != 6 {
					t.Fatalf("READY was lost before reconnect: op %d", op)
				}
			case <-time.After(time.Second):
				t.Fatal("no reconnect")
			}
			cancel()
		})
	}
}

func TestGatewayRepeatedReplayBurstMakesProgress(t *testing.T) {
	const finalSequence = 130
	var completed atomic.Int32
	var attempts atomic.Int32
	finished := make(chan struct{}, 1)
	resumes := make(chan int64, 16)
	_, cancel, done := gatewayFake(t, func(c *websocket.Conn, n int) {
		attempts.Add(1)
		auth := gatewayHello(t, c, 100)
		start := int64(1)
		if n == 1 {
			gatewaySend(t, c, `{"op":0,"t":"READY","s":1,"d":{"session_id":"session"}}`)
			gatewayRead(c)
			gatewaySend(t, c, `{"op":11}`)
		} else {
			var d struct{ Seq int64 }
			_ = json.Unmarshal(auth["d"], &d)
			start = d.Seq
			select {
			case resumes <- start:
			default:
			}
		}
		if n > 8 {
			_ = c.WriteControl(websocket.CloseMessage, websocket.FormatCloseMessage(4915, "test limit"), time.Now().Add(time.Second))
			return
		}
		for sequence := start + 1; sequence <= finalSequence; sequence++ {
			if !gatewaySend(t, c, fmt.Sprintf(`{"op":0,"t":"GROUP_AT_MESSAGE_CREATE","s":%d,"d":{}}`, sequence)) {
				break
			}
		}
		if n > 1 {
			gatewaySend(t, c, fmt.Sprintf(`{"op":0,"t":"RESUMED","s":%d,"d":{}}`, finalSequence+1))
		}
		for {
			m := gatewayRead(c)
			if m == nil {
				return
			}
			if string(m["op"]) == "1" {
				gatewaySend(t, c, `{"op":11}`)
			}
		}
	}, func(ctx context.Context, _ []byte) error {
		timer := time.NewTimer(3 * time.Millisecond)
		defer timer.Stop()
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-timer.C:
		}
		if completed.Add(1) == finalSequence-1 {
			select {
			case finished <- struct{}{}:
			default:
			}
		}
		return nil
	})
	select {
	case <-finished:
	case <-done:
		t.Fatalf("replay made no progress: completed=%d attempts=%d", completed.Load(), attempts.Load())
	case <-time.After(3 * time.Second):
		t.Fatalf("replay stalled: completed=%d", completed.Load())
	}
	cancel()
	if completed.Load() != finalSequence-1 {
		t.Fatalf("replay completed %d events", completed.Load())
	}
	first := <-resumes
	if first <= 1 {
		t.Fatalf("first overflow did not advance checkpoint: %d", first)
	}
	if attempts.Load() > 3 {
		t.Fatalf("replay required %d attempts", attempts.Load())
	}
}

func TestGatewayParentCancellationDoesNotDrainOverflow(t *testing.T) {
	entered := make(chan struct{})
	socketClosed := make(chan struct{})
	var called atomic.Int32
	_, cancel, done := gatewayFake(t, func(c *websocket.Conn, n int) {
		gatewayHello(t, c, 100)
		gatewaySend(t, c, `{"op":0,"t":"READY","s":1,"d":{"session_id":"session"}}`)
		gatewayRead(c)
		gatewaySend(t, c, `{"op":11}`)
		gatewaySend(t, c, `{"op":0,"t":"GROUP_AT_MESSAGE_CREATE","s":2,"d":{}}`)
		<-entered
		gatewaySend(t, c, `{"op":0,"t":"GROUP_AT_MESSAGE_CREATE","s":3,"d":{}}`)
		gatewaySend(t, c, `{"op":0,"t":"GROUP_AT_MESSAGE_CREATE","s":4,"d":{}}`)
		for gatewayRead(c) != nil {
		}
		close(socketClosed)
	}, func(ctx context.Context, _ []byte) error {
		called.Add(1)
		close(entered)
		<-ctx.Done()
		return ctx.Err()
	}, func(g *Gateway) { g.queueSize = 1; g.drainTimeout = 30 * time.Second })
	<-entered
	<-socketClosed
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("parent cancellation waited for drain deadline")
	}
	if called.Load() != 1 {
		t.Fatal("processed queued events after parent cancellation")
	}
}

func TestGatewayWireResumedDoesNotWaitForBusinessAuthentication(t *testing.T) {
	completed := make(chan error, 1)
	_, cancel, _ := gatewayFake(t, func(c *websocket.Conn, n int) {
		gatewayHello(t, c, 50)
		if n == 1 {
			gatewaySend(t, c, `{"op":0,"t":"READY","s":1,"d":{"session_id":"session"}}`)
			gatewayRead(c)
			gatewaySend(t, c, `{"op":7}`)
			return
		}
		gatewaySend(t, c, `{"op":0,"t":"GROUP_AT_MESSAGE_CREATE","s":2,"d":{}}`)
		gatewaySend(t, c, `{"op":0,"t":"RESUMED","s":3,"d":{}}`)
		for {
			m := gatewayRead(c)
			if m == nil {
				return
			}
			if string(m["op"]) == "1" {
				gatewaySend(t, c, `{"op":11}`)
			}
		}
	}, func(ctx context.Context, _ []byte) error {
		timer := time.NewTimer(120 * time.Millisecond)
		defer timer.Stop()
		var err error
		select {
		case <-ctx.Done():
			err = errors.New("authentication timer canceled pending business")
		case <-timer.C:
		}
		select {
		case completed <- err:
		default:
		}
		return err
	}, func(g *Gateway) { g.authTimeout = 40 * time.Millisecond })
	select {
	case err := <-completed:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("slow replay never completed")
	}
	cancel()
}

func TestGatewayDrainingOverflowNeverReportsConnected(t *testing.T) {
	firstEntered := make(chan struct{})
	secondEntered := make(chan struct{})
	var calls atomic.Int32
	g, cancel, _ := gatewayFake(t, func(c *websocket.Conn, n int) {
		gatewayHello(t, c, 100)
		if n == 1 {
			gatewaySend(t, c, `{"op":0,"t":"READY","s":1,"d":{"session_id":"session"}}`)
			gatewayRead(c)
			gatewaySend(t, c, `{"op":7}`)
			return
		}
		gatewaySend(t, c, `{"op":0,"t":"GROUP_AT_MESSAGE_CREATE","s":2,"d":{}}`)
		<-firstEntered
		gatewaySend(t, c, `{"op":0,"t":"RESUMED","s":3,"d":{}}`)
		gatewaySend(t, c, `{"op":0,"t":"GROUP_AT_MESSAGE_CREATE","s":4,"d":{}}`)
		gatewaySend(t, c, `{"op":0,"t":"GROUP_AT_MESSAGE_CREATE","s":5,"d":{}}`)
		for gatewayRead(c) != nil {
		}
	}, func(ctx context.Context, _ []byte) error {
		if calls.Add(1) == 1 {
			close(firstEntered)
			timer := time.NewTimer(30 * time.Millisecond)
			defer timer.Stop()
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-timer.C:
				return nil
			}
		}
		close(secondEntered)
		<-ctx.Done()
		return ctx.Err()
	}, func(g *Gateway) { g.queueSize = 2 })
	select {
	case <-secondEntered:
	case <-time.After(time.Second):
		t.Fatal("accepted queue was not drained")
	}
	if state := g.Status().State; state != "reconnecting" {
		t.Fatalf("closed connection reported %s during drain", state)
	}
	cancel()
}
