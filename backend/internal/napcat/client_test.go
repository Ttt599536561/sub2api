package napcat

import (
	"context"
	"crypto/hmac"
	"crypto/sha1"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func testClient(t *testing.T, h http.HandlerFunc) *Client {
	t.Helper()
	s := httptest.NewServer(h)
	t.Cleanup(s.Close)
	c, err := NewClient(Config{OneBotURL: s.URL, OneBotToken: "onebot-secret", WebUIURL: s.URL, WebUIToken: "web-secret"})
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func reply(w http.ResponseWriter, body string) {
	w.Header().Set("Content-Type", "application/json")
	_, _ = io.WriteString(w, body)
}

func TestFixedAddresses(t *testing.T) {
	for _, target := range []string{"http://example.com", "http://169.254.169.254", "file:///etc/passwd", "http://napcat@evil.example", "http://napcat/path", "http://napcat?token=secret", "http://napcat#fragment", "http://napcat.", "http://napcat:bad", "http://127.0.0.2"} {
		_, err := NewClient(Config{OneBotURL: target, WebUIURL: "http://napcat:6099", OneBotToken: "x", WebUIToken: "x"})
		if !errors.Is(err, ErrInvalid) {
			t.Errorf("unsafe address accepted: %s (%v)", target, err)
		}
	}
	for _, target := range []string{"http://napcat:3000", "http://localhost:3000", "https://127.0.0.1:3000"} {
		if _, err := NewClient(Config{OneBotURL: target, WebUIURL: "http://napcat:6099", OneBotToken: "x", WebUIToken: "x"}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := NewClient(Config{OneBotURL: "https://bot.internal:3000", WebUIURL: "http://napcat:6099", OneBotToken: "x", WebUIToken: "x", AllowedHosts: []string{"bot.internal"}}); err != nil {
		t.Fatal(err)
	}
	if _, err := NewClient(Config{}); !errors.Is(err, ErrNotConfigured) {
		t.Fatal(err)
	}
}

func TestWebUILoginStatusCachesCredential(t *testing.T) {
	var logins atomic.Int32
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("method %s", r.Method)
		}
		switch r.URL.Path {
		case "/api/auth/login":
			logins.Add(1)
			var v struct {
				Hash string `json:"hash"`
			}
			_ = json.NewDecoder(r.Body).Decode(&v)
			want := sha256.Sum256([]byte("web-secret.napcat"))
			if v.Hash != hex.EncodeToString(want[:]) {
				t.Error("incorrect password hash")
			}
			reply(w, `{"code":0,"data":{"Credential":"signed-credential"}}`)
		case "/api/QQLogin/CheckLoginStatus":
			if r.Header.Get("Authorization") != "Bearer signed-credential" {
				t.Error("wrong credential")
			}
			reply(w, `{"code":0,"data":{"isLogin":true,"isOffline":false,"coreReady":true,"loginPhase":"ready","qrcodeurl":"secret-qr","loginError":"sensitive error"}}`)
		case "/api/QQLogin/GetQQLoginInfo":
			reply(w, `{"code":0,"data":{"uin":"123456","nick":"robot","uid":"private-uid","online":true}}`)
		default:
			t.Errorf("unexpected path %s", r.URL.Path)
			w.WriteHeader(404)
		}
	})
	var wg sync.WaitGroup
	for range 8 {
		wg.Go(func() {
			s, err := c.Status(context.Background())
			if err != nil || !s.Online || s.QQ != "123456" || s.Nickname != "robot" {
				t.Errorf("status: %+v %v", s, err)
			}
			body, _ := json.Marshal(s)
			if strings.Contains(string(body), "secret") || strings.Contains(string(body), "sensitive") || strings.Contains(string(body), "private") {
				t.Error("sensitive status fields")
			}
		})
	}
	wg.Wait()
	if logins.Load() != 1 {
		t.Fatalf("login calls=%d", logins.Load())
	}
}

func TestExpiredWebUICredentialRefreshesOnce(t *testing.T) {
	var logins atomic.Int32
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/auth/login" {
			n := logins.Add(1)
			reply(w, fmt.Sprintf(`{"code":0,"data":{"Credential":"credential-%d"}}`, n))
			return
		}
		if r.Header.Get("Authorization") == "Bearer credential-1" {
			reply(w, `{"code":-1,"message":"Unauthorized"}`)
			return
		}
		reply(w, `{"code":0,"data":{"qrcode":"https://txz.qq.com/p?k=example"}}`)
	})
	q, err := c.QRCode(context.Background())
	if err != nil || q.URL != "https://txz.qq.com/p?k=example" || logins.Load() != 2 {
		t.Fatalf("%+v %v calls=%d", q, err, logins.Load())
	}
}

func TestWebUIFailuresAndQR(t *testing.T) {
	for _, tc := range []struct {
		name, body string
		want       error
	}{
		{"2fa", `{"code":0,"data":{"require2FA":true}}`, ErrTwoFactor},
		{"token", `{"code":-1,"message":"token is invalid"}`, ErrUnauthorized},
		{"rate", `{"code":-1,"message":"login rate limit"}`, ErrRateLimited},
		{"missing", `{"data":{"Credential":"bad"}}`, ErrProtocol},
		{"leak", `{"code":-1,"message":"web-secret onebot-secret sensitive"}`, ErrUnavailable},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := testClient(t, func(w http.ResponseWriter, r *http.Request) { reply(w, tc.body) })
			_, err := c.QRCode(context.Background())
			if !errors.Is(err, tc.want) {
				t.Fatalf("%v", err)
			}
			if strings.Contains(err.Error(), "secret") {
				t.Fatal("secret exposed")
			}
		})
	}
	for _, qr := range []string{"javascript:alert(1)", "https://evil.example/qr", "data:image/png;base64,AAAA", "https://txz.qq.com.evil.example/p", "https://user:pass@txz.qq.com/p"} {
		t.Run(qr, func(t *testing.T) {
			c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/api/auth/login" {
					reply(w, `{"code":0,"data":{"Credential":"x"}}`)
					return
				}
				body, _ := json.Marshal(map[string]any{"code": 0, "data": map[string]any{"qrcode": qr}})
				reply(w, string(body))
			})
			if _, err := c.QRCode(context.Background()); !errors.Is(err, ErrProtocol) {
				t.Fatal(err)
			}
		})
	}
}

func TestRefreshRestartAndQuickLoginAreExplicit(t *testing.T) {
	var restarts atomic.Int32
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/auth/login":
			reply(w, `{"code":0,"data":{"Credential":"x"}}`)
		case "/api/QQLogin/RefreshQRcode":
			reply(w, `{"code":0,"data":{"qrcodeurl":"","restarting":true}}`)
		case "/api/QQLogin/RestartNapCat":
			restarts.Add(1)
			reply(w, `{"code":0,"data":{"message":"Restart initiated"}}`)
		case "/api/QQLogin/SetQuickLogin":
			var v map[string]string
			_ = json.NewDecoder(r.Body).Decode(&v)
			if v["uin"] != "123456" {
				t.Error(v)
			}
			reply(w, `{"code":0,"data":null}`)
		default:
			t.Errorf("unexpected %s", r.URL.Path)
		}
	})
	q, err := c.RefreshQRCode(context.Background())
	if err != nil || !q.Restarting || q.URL != "" || restarts.Load() != 0 {
		t.Fatalf("%+v %v", q, err)
	}
	if err = c.QuickLogin(context.Background(), "123456"); err != nil {
		t.Fatal(err)
	}
	if err = c.Restart(context.Background()); err != nil || restarts.Load() != 1 {
		t.Fatal(err)
	}
}

func TestOneBotLevelMembersAndMessages(t *testing.T) {
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer onebot-secret" {
			t.Error("wrong OneBot token")
		}
		var v struct {
			UserID  string `json:"user_id"`
			GroupID string `json:"group_id"`
			NoCache bool   `json:"no_cache"`
			Message []struct {
				Type string            `json:"type"`
				Data map[string]string `json:"data"`
			} `json:"message"`
		}
		_ = json.NewDecoder(r.Body).Decode(&v)
		switch r.URL.Path {
		case "/get_stranger_info":
			if v.UserID != "123456" || !v.NoCache {
				t.Error(v)
			}
			reply(w, `{"status":"ok","retcode":0,"data":{"qqLevel":18,"nickname":"ignored"}}`)
		case "/get_group_member_list":
			if v.GroupID != "777777" {
				t.Error(v)
			}
			reply(w, `{"status":"ok","retcode":0,"data":[{"user_id":123456},{"user_id":"987654"}]}`)
		case "/send_group_msg":
			if v.GroupID != "777777" {
				t.Error(v)
			}
			msg := v.Message
			if len(msg) != 2 {
				t.Error(v)
			} else {
				if msg[0].Type != "at" || msg[0].Data["qq"] != "123456" || msg[1].Type != "text" || msg[1].Data["text"] != "literal [CQ:at,qq=all]" {
					t.Error(v)
				}
			}
			reply(w, `{"status":"ok","retcode":0,"data":{"message_id":-123}}`)
		case "/send_private_msg":
			if v.UserID != "123456" {
				t.Error(v)
			}
			reply(w, `{"status":"ok","retcode":0,"data":{"message_id":"456"}}`)
		default:
			t.Errorf("unexpected %s", r.URL.Path)
		}
	})
	ctx := context.Background()
	if level, err := c.GetLevel(ctx, "123456"); err != nil || level != 18 {
		t.Fatalf("%d %v", level, err)
	}
	if members, err := c.GroupMembers(ctx, "777777"); err != nil || strings.Join(members, ",") != "123456,987654" {
		t.Fatalf("%v %v", members, err)
	}
	if id, err := c.SendGroup(ctx, "777777", "literal [CQ:at,qq=all]", "123456"); err != nil || id != "-123" {
		t.Fatalf("%s %v", id, err)
	}
	if id, err := c.SendPrivate(ctx, "123456", "hello"); err != nil || id != "456" {
		t.Fatalf("%s %v", id, err)
	}
}

func TestLevelUnknownNeverBecomesZero(t *testing.T) {
	for _, data := range []string{`{}`, `{"qqLevel":null}`, `{"qqLevel":0}`, `{"qqLevel":-1}`, `{"qqLevel":1.5}`, `{"qqLevel":"20"}`, `{"level":20}`} {
		t.Run(data, func(t *testing.T) {
			c := testClient(t, func(w http.ResponseWriter, r *http.Request) { reply(w, `{"status":"ok","retcode":0,"data":`+data+`}`) })
			if _, err := c.GetLevel(context.Background(), "123456"); !errors.Is(err, ErrUnknownLevel) {
				t.Fatal(err)
			}
		})
	}
}

func TestLimitsRedirectsAndSafeErrors(t *testing.T) {
	var redirected atomic.Int32
	other := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { redirected.Add(1) }))
	defer other.Close()
	for _, tc := range []struct {
		name string
		h    http.HandlerFunc
		want error
	}{
		{"redirect", func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, other.URL, http.StatusFound) }, ErrUnavailable},
		{"size", func(w http.ResponseWriter, r *http.Request) { reply(w, strings.Repeat("x", (1<<20)+1)) }, ErrProtocol},
		{"malformed", func(w http.ResponseWriter, r *http.Request) { reply(w, `{"data":{"qqLevel":18}}`) }, ErrProtocol},
		{"status", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(503); reply(w, "secret") }, ErrUnavailable},
		{"auth", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(403); reply(w, "secret") }, ErrUnauthorized},
		{"failed", func(w http.ResponseWriter, r *http.Request) {
			reply(w, `{"status":"failed","retcode":1200,"message":"secret"}`)
		}, ErrUnavailable},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := testClient(t, tc.h)
			_, err := c.GetLevel(context.Background(), "123456")
			if !errors.Is(err, tc.want) {
				t.Fatal(err)
			}
			if strings.Contains(err.Error(), "secret") {
				t.Fatal("leak")
			}
		})
	}
	if redirected.Load() != 0 {
		t.Fatal("followed redirect")
	}
}

func TestSendAmbiguityNeverRetries(t *testing.T) {
	for _, body := range []string{`garbage`, `{"status":"ok","retcode":0,"data":{}}`, `{"status":"ok","retcode":0,"data":{"message_id":0}}`} {
		t.Run(body, func(t *testing.T) {
			var calls atomic.Int32
			c := testClient(t, func(w http.ResponseWriter, r *http.Request) { calls.Add(1); reply(w, body) })
			_, err := c.SendPrivate(context.Background(), "123456", "reward")
			if !errors.Is(err, ErrDeliveryUnknown) || calls.Load() != 1 {
				t.Fatalf("%v calls=%d", err, calls.Load())
			}
		})
	}
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(50 * time.Millisecond)
		reply(w, `{"status":"ok","retcode":0,"data":{"message_id":1}}`)
	})
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	if _, err := c.SendPrivate(ctx, "123456", "reward"); !errors.Is(err, ErrDeliveryUnknown) {
		t.Fatal(err)
	}
}

func TestInvalidIDsDoNotMakeRequests(t *testing.T) {
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) { t.Error("invalid request sent") })
	for _, id := range []string{"", "0", "-1", "123?token=secret", "1.2", "abc", "00123"} {
		if _, err := c.GetLevel(context.Background(), id); !errors.Is(err, ErrInvalid) {
			t.Errorf("%s %v", id, err)
		}
		if id != "" {
			if _, err := c.SendGroup(context.Background(), "123456", "text", id); !errors.Is(err, ErrInvalid) {
				t.Errorf("%s %v", id, err)
			}
		}
	}
}

func TestVerifySignatureUsesOriginalBytes(t *testing.T) {
	body := []byte(`{"post_type":"notice", "user_id":123456}`)
	h := hmac.New(sha1.New, []byte("event-secret"))
	_, _ = h.Write(body)
	sig := "sha1=" + hex.EncodeToString(h.Sum(nil))
	if !VerifySignature("event-secret", sig, body) {
		t.Fatal("valid rejected")
	}
	for _, s := range []string{"", "sha1=bad", strings.Replace(sig, "sha1=", "sha256=", 1), sig + "0"} {
		if VerifySignature("event-secret", s, body) {
			t.Fatal("invalid accepted")
		}
	}
	if VerifySignature("", sig, body) || VerifySignature("wrong", sig, body) || VerifySignature("event-secret", sig, append(body, ' ')) {
		t.Fatal("tampered accepted")
	}
}

func TestCloseReleasesIdleConnections(t *testing.T) {
	closed := make(chan struct{})
	var once sync.Once
	s := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reply(w, `{"status":"ok","retcode":0,"data":{"qqLevel":18}}`)
	}))
	s.Config.ConnState = func(_ net.Conn, state http.ConnState) {
		if state == http.StateClosed {
			once.Do(func() { close(closed) })
		}
	}
	s.Start()
	defer s.Close()
	c, err := NewClient(Config{OneBotURL: s.URL, WebUIURL: s.URL, OneBotToken: "onebot", WebUIToken: "web"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = c.GetLevel(context.Background(), "123456"); err != nil {
		t.Fatal(err)
	}
	c.Close()
	select {
	case <-closed:
	case <-time.After(time.Second):
		t.Fatal("idle connection retained")
	}
	c.Close()
}
