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
	anonymousConnections := map[string]bool{}
	t.Run("bare error rejects overlap", func(t *testing.T) { runOpenAIWSPassthroughCacheTTLRecordUsage(t, "overlap", false) })
	for _, ids := range []string{"known", "missing", "mixed"} {
		for _, reverse := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/reverse=%v", ids, reverse), func(t *testing.T) {
				namespace := runOpenAIWSPassthroughCacheTTLRecordUsage(t, ids, reverse)
				if namespace != "" {
					require.NotContains(t, anonymousConnections, namespace, "reconnect must not reuse a previous connection billing namespace")
					anonymousConnections[namespace] = true
				}
			})
		}
	}
}

func runOpenAIWSPassthroughCacheTTLRecordUsage(t *testing.T, ids string, reverse bool) string {
	t.Helper()
	gin.SetMode(gin.TestMode)
	cases := []struct {
		event, details string
		short, long    int
		cost           float64
	}{
		{"response.incomplete", `{"ttl_30m_tokens":0,"ttl_12h_tokens":1000000}`, 0, 1000000, .25},
		{"response.completed", `null`, 0, 0, .125},
		{"response.failed", `{"ttl_30m_tokens":400000,"ttl_12h_tokens":600000}`, 400000, 600000, .2},
		{"response.completed", `{"ttl_30m_tokens":1000000,"ttl_12h_tokens":0}`, 1000000, 0, .125},
		{"response.completed", `{"ttl_30m_tokens":-1,"ttl_12h_tokens":1000001}`, 0, 0, .125},
	}
	if reverse {
		for i, j := 0, len(cases)-1; i < j; i, j = i+1, j-1 {
			cases[i], cases[j] = cases[j], cases[i]
		}
	}
	cases = append(cases, struct {
		event, details string
		short, long    int
		cost           float64
	}{"error", `null`, 0, 0, .125})
	upstream := &openAIWSTTLTestConn{frames: make(chan []byte, 12), closeAfterLastTurn: ids != "overlap"}
	responseID := func(i int) string {
		if ids == "overlap" || ids == "missing" || (ids == "mixed" && i%2 == 0) {
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
		if tc.event != "error" {
			terminal := frames[len(frames)-1]
			frames = append(frames, terminal)
			if responseID(i) == "" {
				frames = append(frames, strings.Replace(terminal, `"id":""`, fmt.Sprintf(`"id":"late_%d"`, i), 1))
			}
		}
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
	seenIDs := map[string]bool{}
	total := 0.0
	for i := range cases {
		request := fmt.Sprintf(`{"type":"response.create","model":"gpt-6-luna","input":"hi","reasoning":{"effort":%q}}`, []string{"low", "high"}[i%2])
		require.NoError(t, client.Write(ctx, coderws.MessageText, []byte(request)))
		for _, expectedFrame := range upstream.turns[i] {
			_, frame, err := client.Read(ctx)
			require.NoError(t, err)
			require.JSONEq(t, expectedFrame, string(frame), "internal billing identity must not change wire payload")
		}
	}
	if ids == "overlap" {
		require.NoError(t, client.Write(ctx, coderws.MessageText, []byte(`{"type":"response.create","model":"gpt-5.4","reasoning":{"effort":"high"}}`)))
		_, _, err := client.Read(ctx)
		require.Equal(t, coderws.StatusPolicyViolation, coderws.CloseStatus(err))
	} else {
		require.NoError(t, client.Close(coderws.StatusNormalClosure, "done"))
	}
	for i, tc := range cases {
		var result *OpenAIForwardResult
		select {
		case result = <-results:
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		}
		if responseID(i) != "" {
			require.Equal(t, responseID(i), result.RequestID)
		} else {
			require.NotEmpty(t, result.RequestID, "anonymous turns need distinct billing keys")
		}
		require.NotNil(t, result.ReasoningEffort)
		require.Equal(t, []string{"low", "high"}[i%2], *result.ReasoningEffort, "accepted request effort must stay with its turn")
		require.NotContains(t, seenIDs, result.RequestID, "each turn needs its own billing identity")
		seenIDs[result.RequestID] = true
		usageRepo := &openAIRecordUsageLogRepoStub{inserted: true}
		userRepo := &openAIRecordUsageUserRepoStub{}
		billing := newOpenAIRecordUsageServiceForTest(usageRepo, userRepo, &openAIRecordUsageSubRepoStub{}, nil)
		require.NoError(t, billing.RecordUsage(ctx, &OpenAIRecordUsageInput{
			Result: result, APIKey: &APIKey{ID: 1, GroupID: i64p(1), Group: &Group{ID: 1, RateMultiplier: 1}}, User: &User{ID: 1}, Account: account,
		}))
		require.NotNil(t, usageRepo.lastLog)
		total += userRepo.lastAmount
		if i == 1 && !reverse {
			require.InDelta(t, .375, total, 1e-12, "known 12h plus unknown must preserve the 12h price")
		}
		t.Logf("turn=%d id=%s ttl30m=%d ttl12h=%d cache_cost=%.3f debit=%.3f expected=%.3f", i+1, result.RequestID, usageRepo.lastLog.CacheCreation30mTokens, usageRepo.lastLog.CacheCreation12hTokens, usageRepo.lastLog.CacheCreationCost, userRepo.lastAmount, tc.cost)
		require.InDelta(t, tc.cost, usageRepo.lastLog.CacheCreationCost, 1e-12, "turn %d cache cost", i+1)
		require.InDelta(t, tc.cost, userRepo.lastAmount, 1e-12, "turn %d debit", i+1)
		require.Equal(t, 1000000, usageRepo.lastLog.CacheCreationTokens)
		require.Equal(t, tc.short, usageRepo.lastLog.CacheCreation30mTokens)
		require.Equal(t, tc.long, usageRepo.lastLog.CacheCreation12hTokens)
	}
	require.InDelta(t, .95, total, 1e-12)
	select {
	case err := <-serverDone:
		if ids == "overlap" {
			require.ErrorContains(t, err, "overlapping response.create")
		} else {
			require.NoError(t, err)
		}
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	require.Empty(t, results, "each turn must be billed once")
	for id := range seenIDs {
		if strings.HasPrefix(id, "ws_turn_") {
			return id[:strings.LastIndex(id, "_")]
		}
	}
	return ""
}

func TestOpenAIWSPassthroughDuplicateTerminalDoesNotUnlockActiveTurn(t *testing.T) {
	for _, overlap := range []bool{true, false} {
		t.Run(fmt.Sprintf("overlap=%v", overlap), func(t *testing.T) { runOpenAIWSPassthroughDuplicateTerminal(t, overlap) })
	}
}

func runOpenAIWSPassthroughDuplicateTerminal(t *testing.T, overlap bool) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	upstream := &openAIWSTTLTestConn{frames: make(chan []byte, 12)}
	first := `{"type":"response.completed","response":{"id":"resp_a","model":"gpt-6-luna","usage":{"input_tokens":1000000,"output_tokens":0,"input_tokens_details":{"cache_write_tokens":1000000,"cache_write_tokens_details":{"ttl_30m_tokens":0,"ttl_12h_tokens":1000000}}}}}`
	upstream.turns = [][]string{
		{first},
		{`{"type":"response.created","response":{"id":"resp_b","model":"gpt-6-luna"}}`, first},
		{`{"type":"response.completed","response":{"id":"resp_c","usage":{"input_tokens":1,"output_tokens":1}}}`},
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
	results := make(chan *OpenAIForwardResult, 4)
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

	require.NoError(t, client.Write(ctx, coderws.MessageText, []byte(`{"type":"response.create","model":"gpt-6-luna","reasoning":{"effort":"low"}}`)))
	_, _, err = client.Read(ctx)
	require.NoError(t, err)
	require.NoError(t, client.Write(ctx, coderws.MessageText, []byte(`{"type":"response.create","model":"gpt-6-luna","reasoning":{"effort":"high"}}`)))
	for range 2 {
		_, _, err = client.Read(ctx)
		require.NoError(t, err)
	}
	if overlap {
		require.NoError(t, client.Write(ctx, coderws.MessageText, []byte(`{"type":"response.create","model":"gpt-5.4","reasoning":{"effort":"medium"}}`)))
		_, _, err = client.Read(ctx)
		require.Equal(t, coderws.StatusPolicyViolation, coderws.CloseStatus(err), "late duplicate A must leave B in flight and reject C")
	} else {
		upstream.frames <- []byte(`{"type":"response.completed","response":{"id":"resp_b","model":"gpt-6-luna","usage":{"input_tokens":1000000,"output_tokens":0,"input_tokens_details":{"cache_write_tokens":1000000}}}}`)
		close(upstream.frames)
		_, _, err = client.Read(ctx)
		require.NoError(t, err)
		require.NoError(t, client.Close(coderws.StatusNormalClosure, "done"))
	}
	select {
	case err := <-serverDone:
		if overlap {
			require.ErrorContains(t, err, "overlapping response.create")
		} else {
			require.NoError(t, err)
		}
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	wantTurns := 2
	if overlap {
		wantTurns = 1
	}
	require.Len(t, results, wantTurns, "duplicates cannot settle B or accept C")
	for i := 0; i < wantTurns; i++ {
		result := <-results
		require.Equal(t, []string{"resp_a", "resp_b"}[i], result.RequestID)
		require.Equal(t, "gpt-6-luna", result.Model)
		require.Equal(t, []string{"low", "high"}[i], *result.ReasoningEffort)
		usageRepo := &openAIRecordUsageLogRepoStub{inserted: true}
		userRepo := &openAIRecordUsageUserRepoStub{}
		billing := newOpenAIRecordUsageServiceForTest(usageRepo, userRepo, &openAIRecordUsageSubRepoStub{}, nil)
		require.NoError(t, billing.RecordUsage(ctx, &OpenAIRecordUsageInput{Result: result, APIKey: &APIKey{ID: 1, GroupID: i64p(1), Group: &Group{ID: 1, RateMultiplier: 1}}, User: &User{ID: 1}, Account: account}))
		require.InDelta(t, []float64{.25, .125}[i], userRepo.lastAmount, 1e-12)
	}
}
