package qqimage

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

func TestGatewayOfficialURLsOnly(t *testing.T) {
	for _, target := range []string{"wss://api.sgroup.qq.com/websocket", "wss://api.bot.qq.com:443/websocket?encoding=json&v=2"} {
		if !validGatewayURL(target) {
			t.Errorf("valid official URL rejected")
		}
	}
	for _, target := range []string{"wss://api.sgroup.qq.com./websocket", "wss://API.sgroup.qq.com/websocket", "wss://api.sgroup.qq.com:0443/websocket", "wss://api.sgroup.qq.com/%77ebsocket", "wss://api.sgroup.qq.com/websocket?encoding=json&encoding=json", "wss://api.sgroup.qq.com/websocket?compress=zlib-stream", "wss://api.sgroup.qq.com/websocket?encoding=etf", "wss://api.sgroup.qq.com/websocket?v=1%zz", "wss://api.sgroup.qq.com/websocket#", "wss://api.sgroup.qq.com/websocket?secret=password"} {
		if validGatewayURL(target) {
			t.Errorf("unsafe URL accepted: %q", target)
		}
	}
}
func TestGatewayBootstrapHTTPStatusHasControlledPolicy(t *testing.T) {
	for _, status := range []int{301, 401, 403, 408, 429, 500, 503} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			client := qqClientServer(t, func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(status)
				_, _ = io.WriteString(w, "token=private-access-token session=secret")
			})
			g := NewGateway(client, qqConfig(), nil)
			_, result := g.endpoint(context.Background(), "token")
			if result.code == "" || strings.Contains(result.code, "secret") {
				t.Fatal("unsafe status")
			}
			if status == 401 && !result.refreshToken {
				t.Fatal("401 must invalidate cached token")
			}
			if status == 429 && result.delay < time.Minute {
				t.Fatal("429 must back off")
			}
			if (status == 301 || status == 403) && !result.terminal {
				t.Fatal("nonrecoverable response retried")
			}
			if (status == 408 || status >= 500) && result.terminal {
				t.Fatal("transient failure cannot be permanent")
			}
		})
	}
}
func TestGatewayOversizeFrameResumesWithoutCheckpointLoss(t *testing.T) {
	next := make(chan int64, 1)
	hold := make(chan struct{})
	_, cancel, _ := gatewayFake(t, func(c *websocket.Conn, n int) {
		auth := gatewayHello(t, c, 15)
		if n == 1 {
			gatewaySend(t, c, `{"op":0,"t":"READY","s":1,"d":{"session_id":"session"}}`)
			gatewayRead(c)
			gatewaySend(t, c, `{"op":11}`)
			gatewaySend(t, c, `{"op":0,"t":"GROUP_AT_MESSAGE_CREATE","s":2,"d":{"payload":"`+strings.Repeat("x", gatewayMaxFrame)+`"}}`)
			for gatewayRead(c) != nil {
			}
			return
		}
		var d struct{ Seq int64 }
		_ = json.Unmarshal(auth["d"], &d)
		select {
		case next <- d.Seq:
		default:
		}
		<-hold
	}, func(context.Context, []byte) error { t.Error("oversized frame dispatched"); return nil })
	defer close(hold)
	select {
	case seq := <-next:
		if seq != 1 {
			t.Fatalf("checkpoint lost: %d", seq)
		}
	case <-time.After(time.Second):
		t.Fatal("oversize frame did not reconnect")
	}
	cancel()
}
func TestGatewayMalformedHelloEndsConnection(t *testing.T) {
	for i, hello := range []string{`null`, `{}`, `{"op":10,"d":{"heartbeat_interval":0}}`, `{"op":10,"d":{"heartbeat_interval":300001}}`, `{"op":10,"d":{"heartbeat_interval":"15"}}`, `{"op":0,"d":{}}`} {
		t.Run(fmt.Sprint(i), func(t *testing.T) {
			g, _, done := gatewayFake(t, func(c *websocket.Conn, n int) {
				gatewaySend(t, c, hello)
				if m := gatewayRead(c); m != nil {
					t.Error("identified after malformed hello")
				}
			}, func(context.Context, []byte) error { return nil }, func(g *Gateway) { g.wait = func(context.Context, time.Duration) bool { return false } })
			select {
			case <-done:
			case <-time.After(time.Second):
				t.Fatal("malformed hello hung")
			}
			if g.Status().ErrorCode == "" {
				t.Fatal("missing controlled error")
			}
		})
	}
}
func TestGatewayReadyDisconnectChurnKeepsIncreasingBackoff(t *testing.T) {
	var delays []time.Duration
	_, _, done := gatewayFake(t, func(c *websocket.Conn, n int) {
		gatewayHello(t, c, 10)
		kind := "RESUMED"
		if n == 1 {
			kind = "READY"
		}
		gatewaySend(t, c, fmt.Sprintf(`{"op":0,"t":%q,"s":1,"d":{"session_id":"session"}}`, kind))
		gatewayRead(c)
		if n < 8 {
			gatewaySend(t, c, `{"op":7}`)
		} else {
			_ = c.WriteControl(websocket.CloseMessage, websocket.FormatCloseMessage(4915, "stop"), time.Now().Add(time.Second))
		}
	}, func(context.Context, []byte) error { return nil }, func(g *Gateway) {
		g.wait = func(ctx context.Context, d time.Duration) bool { delays = append(delays, d); return ctx.Err() == nil }
	})
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("churn test did not finish")
	}
	if len(delays) != 7 {
		t.Fatalf("unexpected reconnect waits %d", len(delays))
	}
	for i, d := range delays {
		if d < gatewayBackoff(i)/2 || d > time.Minute {
			t.Fatalf("ready reset retry backoff at %d: %s", i, d)
		}
	}
}
func TestGatewayLocalBudgetPreventsRepeatedIdentify(t *testing.T) {
	var dials atomic.Int32
	var waits []time.Duration
	client := gatewayTestClient(t, strings.Replace(gatewayTestBootstrap, `"remaining":999`, `"remaining":1`, 1))
	// A real fake WebSocket still exercises successful IDENTIFY before op9 false.
	_, cancel, done := gatewayFake(t, func(c *websocket.Conn, n int) {
		dials.Add(1)
		gatewayHello(t, c, 15)
		gatewaySend(t, c, `{"op":9,"d":false}`)
		for gatewayRead(c) != nil {
		}
	}, func(context.Context, []byte) error { return nil }, func(fake *Gateway) {
		fake.client = client
		fake.wait = func(ctx context.Context, d time.Duration) bool {
			waits = append(waits, d)
			return d < time.Hour && ctx.Err() == nil
		}
	})
	defer cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("budget limit not reached")
	}
	if dials.Load() != 1 || len(waits) != 2 || waits[0] < 3*time.Second || waits[1] < 23*time.Hour {
		t.Fatalf("identify budget not preserved: dials %d waits %v", dials.Load(), waits)
	}
}
func TestGatewayDialErrorsNeverExposeSecrets(t *testing.T) {
	g := NewGateway(gatewayTestClient(t, gatewayTestBootstrap), qqConfig(), func(context.Context, []byte) error { return nil })
	g.dial = func(context.Context, string) (*websocket.Conn, *http.Response, error) {
		return nil, nil, errors.New("private-access-token private-session private-host")
	}
	g.wait = func(context.Context, time.Duration) bool { return false }
	g.Run(context.Background())
	raw, _ := json.Marshal(g.Status())
	if strings.Contains(string(raw), "private-") || g.Status().ErrorCode != "qq_gateway_connect_failed" {
		t.Fatal("raw dial failure exposed")
	}
}
