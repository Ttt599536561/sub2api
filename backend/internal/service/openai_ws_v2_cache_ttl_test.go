package service

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	coderws "github.com/coder/websocket"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

// Each response waits for its response.create, so turn boundaries do not depend
// on sleeps or on the relative scheduling of the two relay readers.
type openAIWSTTLTestConn struct {
	openAIWSCaptureConn
	turns              [][]string
	frames             chan []byte
	next               int
	closeAfterLastTurn bool
}

func (c *openAIWSTTLTestConn) WriteJSON(ctx context.Context, value any) error {
	payload, err := json.Marshal(value)
	if err != nil {
		return err
	}
	if gjson.GetBytes(payload, "type").String() != "response.create" {
		return nil
	}
	if c.next >= len(c.turns) {
		return fmt.Errorf("unexpected response.create")
	}
	for _, frame := range c.turns[c.next] {
		select {
		case c.frames <- []byte(frame):
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	c.next++
	if c.closeAfterLastTurn && c.next == len(c.turns) {
		close(c.frames)
	}
	return nil
}

func (c *openAIWSTTLTestConn) WriteFrame(ctx context.Context, _ coderws.MessageType, payload []byte) error {
	return c.WriteJSON(ctx, json.RawMessage(payload))
}

func (c *openAIWSTTLTestConn) ReadMessage(ctx context.Context) ([]byte, error) {
	select {
	case frame, ok := <-c.frames:
		if !ok {
			return nil, io.EOF
		}
		return frame, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func (c *openAIWSTTLTestConn) ReadFrame(ctx context.Context) (coderws.MessageType, []byte, error) {
	frame, err := c.ReadMessage(ctx)
	return coderws.MessageText, frame, err
}

func TestOpenAIWSPassthroughCacheTTLRecordUsage(t *testing.T) {
	t.Run("per turn", func(t *testing.T) { runOpenAIWSPassthroughCacheTTLRecordUsage(t, false) })
	t.Run("aggregate fallback", func(t *testing.T) { runOpenAIWSPassthroughCacheTTLRecordUsage(t, true) })
}

func runOpenAIWSPassthroughCacheTTLRecordUsage(t *testing.T, aggregateFallback bool) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	cases := []struct {
		event, details string
		short, long    int
		cost           float64
	}{
		{"response.incomplete", `{"ttl_30m_tokens":0,"ttl_12h_tokens":1000000}`, 0, 1000000, .25},
		{"response.completed", `{"ttl_30m_tokens":1000000,"ttl_12h_tokens":0}`, 1000000, 0, .125},
		{"response.failed", `{"ttl_30m_tokens":400000,"ttl_12h_tokens":600000}`, 400000, 600000, .2},
		{"response.completed", `null`, 0, 0, .125},
		{"response.completed", `{"ttl_30m_tokens":-1,"ttl_12h_tokens":1000001}`, 0, 0, .125},
	}
	if aggregateFallback {
		cases = cases[:1]
	}
	upstream := &openAIWSTTLTestConn{frames: make(chan []byte, 3), closeAfterLastTurn: aggregateFallback}
	responseID := func(i int) string {
		if aggregateFallback {
			return ""
		}
		return fmt.Sprintf("resp_ttl_%d", i)
	}
	for i, tc := range cases {
		frames := []string{fmt.Sprintf(`{"type":"response.created","response":{"id":%q,"model":"gpt-6-luna"}}`, responseID(i))}
		if tc.event == "response.failed" {
			frames = append(frames, `{"type":"error","error":{"message":"failed"}}`)
		}
		frames = append(frames, fmt.Sprintf(`{"type":%q,"response":{"id":%q,"model":"gpt-6-luna","usage":{"input_tokens":1000000,"output_tokens":0,"input_tokens_details":{"cache_write_tokens":1000000,"cache_write_tokens_details":%s}}}}`, tc.event, responseID(i), tc.details))
		upstream.turns = append(upstream.turns, frames)
	}
	cfg := &config.Config{}
	cfg.Security.URLAllowlist.Enabled = false
	cfg.Gateway.OpenAIWS.Enabled = true
	cfg.Gateway.OpenAIWS.APIKeyEnabled = true
	cfg.Gateway.OpenAIWS.ResponsesWebsocketsV2 = true
	cfg.Gateway.OpenAIWS.ModeRouterV2Enabled = true
	cfg.Gateway.OpenAIWS.IngressModeDefault = OpenAIWSIngressModeCtxPool
	cfg.Gateway.OpenAIWS.ReadTimeoutSeconds = 3
	cfg.Gateway.OpenAIWS.WriteTimeoutSeconds = 3
	svc := &OpenAIGatewayService{
		cfg: cfg, cache: &stubGatewayCache{}, httpUpstream: &httpUpstreamRecorder{},
		openaiWSResolver: NewOpenAIWSProtocolResolver(cfg), toolCorrector: NewCodexToolCorrector(),
		openaiWSPassthroughDialer: &openAIWSClientConnCancelDialer{conn: upstream},
	}
	account := &Account{ID: 454, Platform: PlatformOpenAI, Type: AccountTypeAPIKey,
		Status: StatusActive, Schedulable: true, Concurrency: 1,
		Credentials: map[string]any{"api_key": "sk-test"},
		Extra:       map[string]any{"openai_apikey_responses_websockets_v2_mode": OpenAIWSIngressModePassthrough},
	}
	results := make(chan *OpenAIForwardResult, len(cases)+1)
	hooks := &OpenAIWSIngressHooks{AfterTurn: func(_ int, result *OpenAIForwardResult, turnErr error) {
		if turnErr == nil && result != nil {
			results <- result
		}
	}}
	serverDone := make(chan error, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := coderws.Accept(w, r, nil)
		if err != nil {
			serverDone <- err
			return
		}
		defer func() { _ = conn.CloseNow() }()
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		c.Request = r
		_, first, err := conn.Read(r.Context())
		if err == nil {
			err = svc.ProxyResponsesWebSocketFromClient(r.Context(), c, conn, account, "sk-test", first, hooks)
		}
		serverDone <- err
	}))
	defer server.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	client, _, err := coderws.Dial(ctx, "ws"+strings.TrimPrefix(server.URL, "http"), nil)
	require.NoError(t, err)
	defer func() { _ = client.CloseNow() }()
	for i, tc := range cases {
		require.NoError(t, client.Write(ctx, coderws.MessageText, []byte(`{"type":"response.create","model":"gpt-6-luna","input":"hi"}`)))
		for {
			_, frame, err := client.Read(ctx)
			require.NoError(t, err)
			if gjson.GetBytes(frame, "type").String() == tc.event {
				break
			}
		}
		var result *OpenAIForwardResult
		select {
		case result = <-results:
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		}
		require.Equal(t, responseID(i), result.RequestID)
		usageRepo := &openAIRecordUsageLogRepoStub{inserted: true}
		userRepo := &openAIRecordUsageUserRepoStub{}
		billing := newOpenAIRecordUsageServiceForTest(usageRepo, userRepo, &openAIRecordUsageSubRepoStub{}, nil)
		require.NoError(t, billing.RecordUsage(ctx, &OpenAIRecordUsageInput{
			Result: result, APIKey: &APIKey{ID: 1, GroupID: i64p(1), Group: &Group{ID: 1, RateMultiplier: 1}}, User: &User{ID: 1}, Account: account,
		}))
		require.NotNil(t, usageRepo.lastLog)
		require.InDelta(t, tc.cost, usageRepo.lastLog.CacheCreationCost, 1e-12, "turn %d cache cost", i+1)
		require.InDelta(t, tc.cost, userRepo.lastAmount, 1e-12, "turn %d debit", i+1)
		require.Equal(t, 1000000, usageRepo.lastLog.CacheCreationTokens)
		require.Equal(t, tc.short, usageRepo.lastLog.CacheCreation30mTokens)
		require.Equal(t, tc.long, usageRepo.lastLog.CacheCreation12hTokens)
	}
	if !aggregateFallback {
		require.NoError(t, client.Close(coderws.StatusNormalClosure, "done"))
	}
	select {
	case err := <-serverDone:
		require.NoError(t, err)
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	require.Empty(t, results, "each turn must be billed once")
}
