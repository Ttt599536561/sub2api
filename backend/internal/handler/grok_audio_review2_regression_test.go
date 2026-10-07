//go:build unit

package handler

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	middleware2 "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	coderws "github.com/coder/websocket"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type review2AudioLogs struct {
	service.UsageLogRepository
	mu   sync.Mutex
	rows []*service.UsageLog
}

func (r *review2AudioLogs) Create(_ context.Context, row *service.UsageLog) (bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	saved := *row
	r.rows = append(r.rows, &saved)
	return true, nil
}

type review2AudioBilling struct {
	service.UsageBillingRepository
	mu       sync.Mutex
	commands []*service.UsageBillingCommand
}

func (r *review2AudioBilling) Apply(_ context.Context, cmd *service.UsageBillingCommand) (*service.UsageBillingApplyResult, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	saved := *cmd
	r.commands = append(r.commands, &saved)
	balance := 100 - cmd.BalanceCost
	return &service.UsageBillingApplyResult{Applied: true, NewBalance: &balance}, nil
}

type review2AudioUser struct{ service.UserRepository }

func (*review2AudioUser) GetByID(context.Context, int64) (*service.User, error) {
	return &service.User{ID: 10, Balance: 100, Status: service.StatusActive}, nil
}

type review2AudioSlots struct {
	*grokMediaSlotsCache
	denyUsers bool
}

func (s *review2AudioSlots) AcquireUserSlot(ctx context.Context, id int64, max int, request string) (bool, error) {
	if s.denyUsers {
		return false, nil
	}
	return s.grokMediaSlotsCache.AcquireUserSlot(ctx, id, max, request)
}
func (s *review2AudioSlots) IncrementWaitCount(context.Context, int64, int) (bool, error) {
	return false, nil
}

func review2GrokAudioHandler(t *testing.T, baseURL string, slots *review2AudioSlots, upstream service.HTTPUpstream) (*OpenAIGatewayHandler, *service.APIKey, *review2AudioBilling) {
	t.Helper()
	cfg := &config.Config{}
	cfg.Default.RateMultiplier = 1
	cfg.Security.URLAllowlist.AllowInsecureHTTP = true
	account := service.Account{ID: 1, Platform: service.PlatformGrok, Type: service.AccountTypeAPIKey,
		Status: service.StatusActive, Schedulable: true, Concurrency: 50, GroupIDs: []int64{24},
		Credentials: map[string]any{"api_key": "test-key", "base_url": baseURL}}
	repo := openAIImagesFailoverAccountRepo{accounts: []service.Account{account}}
	concurrency := service.NewConcurrencyService(slots)
	cache := service.NewBillingCacheService(nil, &review2AudioUser{}, nil, nil, nil, nil, cfg, nil)
	t.Cleanup(cache.Stop)
	billing := &review2AudioBilling{}
	gateway := service.NewOpenAIGatewayService(repo, &review2AudioLogs{}, billing, &review2AudioUser{}, nil, nil, nil, cfg, nil, concurrency,
		service.NewBillingService(cfg, nil), nil, cache, upstream, &service.DeferredService{}, nil, nil, nil, nil, nil, nil, nil)
	h := NewOpenAIGatewayHandler(gateway, concurrency, cache, &service.APIKeyService{}, nil, nil, nil, nil, cfg)
	groupID, price := int64(24), 6000.0
	key := &service.APIKey{ID: 20, UserID: 10, GroupID: &groupID, User: &service.User{ID: 10, Balance: 100, Status: service.StatusActive},
		Group: &service.Group{ID: groupID, Platform: service.PlatformGrok, Status: service.StatusActive, RateMultiplier: 1,
			AudioRealtimePricePerMin: &price, AudioTTSPricePerMillionChars: &price}}
	return h, key, billing
}
func review2AudioSlotsFixture() *review2AudioSlots {
	return &review2AudioSlots{grokMediaSlotsCache: &grokMediaSlotsCache{accounts: map[string]int64{}, users: map[string]int64{}}}
}
func review2SetAudioIdentity(c *gin.Context, key *service.APIKey) {
	c.Set(string(middleware2.ContextKeyAPIKey), key)
	c.Set(string(middleware2.ContextKeyUser), middleware2.AuthSubject{UserID: key.UserID, Concurrency: 1})
}

func TestReview2GrokRealtimeBillsObservedAudioOnMalformedClientEnd(t *testing.T) {
	for _, malformed := range []bool{false, true} {
		name := "normal_close"
		if malformed {
			name = "malformed_after_audio"
		}
		t.Run(name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			upstream := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				conn, err := coderws.Accept(w, r, nil)
				if err != nil {
					return
				}
				defer conn.CloseNow()
				if _, _, err := conn.Read(ctx); err != nil {
					return
				}
				if err := conn.Write(ctx, coderws.MessageText, []byte(`{"type":"response.audio.delta","delta":"AQID"}`)); err != nil {
					return
				}
				for {
					if _, _, err := conn.Read(ctx); err != nil {
						return
					}
				}
			}))
			defer upstream.Close()
			originalClient := http.DefaultClient
			http.DefaultClient = upstream.Client()
			defer func() { http.DefaultClient = originalClient }()
			slots := review2AudioSlotsFixture()
			h, key, billing := review2GrokAudioHandler(t, upstream.URL, slots, nil)
			finished := make(chan struct{})
			router := gin.New()
			router.GET("/realtime", func(c *gin.Context) { review2SetAudioIdentity(c, key); h.GrokRealtime(c); close(finished) })
			downstream := httptest.NewServer(router)
			defer downstream.Close()
			conn, resp, err := coderws.Dial(ctx, "ws"+strings.TrimPrefix(downstream.URL, "http")+"/realtime", nil)
			if err != nil && resp != nil {
				t.Fatalf("handshake failed: %v status=%d", err, resp.StatusCode)
			}
			require.NoError(t, err)
			defer conn.CloseNow()
			require.NoError(t, conn.Write(ctx, coderws.MessageText, []byte(`{"type":"input_audio_buffer.append","audio":"AQID"}`)))
			_, audio, err := conn.Read(ctx)
			require.NoError(t, err)
			require.Contains(t, string(audio), "response.audio.delta", "the session delivered real upstream audio before termination")
			// Windows can report zero elapsed time for a sub-millisecond local session.
			// Keep this real audio session open across a measurable billing interval.
			select {
			case <-time.After(20 * time.Millisecond):
			case <-ctx.Done():
				t.Fatal("audio session timed out")
			}
			if malformed {
				require.NoError(t, conn.Write(ctx, coderws.MessageText, []byte(`{invalid-json`)))
				_, _, _ = conn.Read(ctx)
			} else {
				_ = conn.Close(coderws.StatusNormalClosure, "done")
			}
			select {
			case <-finished:
			case <-ctx.Done():
				t.Fatal("handler failed to complete")
			}
			billing.mu.Lock()
			defer billing.mu.Unlock()
			require.Len(t, billing.commands, 1, "already delivered realtime audio must remain billable when the client later sends malformed JSON")
			require.Positive(t, billing.commands[0].BalanceCost)
			slots.assertReleased(t)
		})
	}
}

func TestReview2GrokVoiceCannotBypassFullUserConcurrency(t *testing.T) {
	slots := review2AudioSlotsFixture()
	slots.denyUsers = true
	var reached atomic.Int32
	upstream := &grokMediaSlotUpstream{call: func(*http.Request, int64) (*http.Response, error) {
		reached.Add(1)
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"audio/mpeg"}}, Body: io.NopCloser(strings.NewReader("audio"))}, nil
	}}
	h, key, _ := review2GrokAudioHandler(t, "https://api.x.ai/v1", slots, upstream)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/tts", strings.NewReader(`{"input":"billable text"}`))
	c.Request.Header.Set("Content-Type", "application/json")
	review2SetAudioIdentity(c, key)
	h.GrokVoice(c, "tts")
	require.Zero(t, reached.Load(), "a user with a full concurrency slot must not reach the voice upstream")
}

func TestReview2GrokRealtimeCannotBypassFullUserConcurrency(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	var reached atomic.Int32
	upstream := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reached.Add(1)
		conn, err := coderws.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer conn.CloseNow()
		_, _, _ = conn.Read(ctx)
	}))
	defer upstream.Close()
	originalClient := http.DefaultClient
	http.DefaultClient = upstream.Client()
	defer func() { http.DefaultClient = originalClient }()
	slots := review2AudioSlotsFixture()
	slots.denyUsers = true
	h, key, _ := review2GrokAudioHandler(t, upstream.URL, slots, nil)
	finished := make(chan struct{})
	router := gin.New()
	router.GET("/realtime", func(c *gin.Context) { review2SetAudioIdentity(c, key); h.GrokRealtime(c); close(finished) })
	downstream := httptest.NewServer(router)
	defer downstream.Close()
	conn, _, _ := coderws.Dial(ctx, "ws"+strings.TrimPrefix(downstream.URL, "http")+"/realtime", nil)
	if conn != nil {
		_ = conn.Close(coderws.StatusNormalClosure, "done")
	}
	select {
	case <-finished:
	case <-ctx.Done():
		t.Fatal("handler failed to finish")
	}
	require.Zero(t, reached.Load(), "a full user slot must stop realtime before any upstream handshake")
}

func TestReview2GrokRealtimeMalformedEndWithoutAudioDoesNotBill(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	upstream := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := coderws.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer conn.CloseNow()
		if err := conn.Write(ctx, coderws.MessageText, []byte(`{"type":"session.created"}`)); err != nil {
			return
		}
		for {
			if _, _, err := conn.Read(ctx); err != nil {
				return
			}
		}
	}))
	defer upstream.Close()
	originalClient := http.DefaultClient
	http.DefaultClient = upstream.Client()
	defer func() { http.DefaultClient = originalClient }()
	slots := review2AudioSlotsFixture()
	h, key, billing := review2GrokAudioHandler(t, upstream.URL, slots, nil)
	finished := make(chan struct{})
	router := gin.New()
	router.GET("/realtime", func(c *gin.Context) { review2SetAudioIdentity(c, key); h.GrokRealtime(c); close(finished) })
	downstream := httptest.NewServer(router)
	defer downstream.Close()
	conn, _, err := coderws.Dial(ctx, "ws"+strings.TrimPrefix(downstream.URL, "http")+"/realtime", nil)
	require.NoError(t, err)
	defer conn.CloseNow()
	_, frame, err := conn.Read(ctx)
	require.NoError(t, err)
	require.Contains(t, string(frame), "session.created")
	require.NoError(t, conn.Write(ctx, coderws.MessageText, []byte(`{invalid-json`)))
	_, _, _ = conn.Read(ctx)
	select {
	case <-finished:
	case <-ctx.Done():
		t.Fatal("handler failed to complete")
	}
	billing.mu.Lock()
	defer billing.mu.Unlock()
	require.Empty(t, billing.commands, "opening a realtime session without audio must not incur a charge")
	slots.assertReleased(t)
	require.Equal(t, 1, slots.userAcquired)
}
