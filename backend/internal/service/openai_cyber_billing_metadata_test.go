package service

import (
	"context"
	"fmt"
	coderws "github.com/coder/websocket"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestCyberBillingMetadataHTTPDispatchAndReset(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, kind := range []string{"responses", "passthrough", "chat"} {
		t.Run(kind, func(t *testing.T) {
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
			account := &Account{Platform: PlatformOpenAI, Type: AccountTypeAPIKey, Credentials: map[string]any{"api_key": "test"}}
			svc := &OpenAIGatewayService{httpUpstream: &httpUpstreamRecorder{resp: &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader("{}"))}}}
			send := func(body string) {
				var err error
				switch kind {
				case "responses":
					_, err = svc.buildUpstreamRequest(context.Background(), c, account, []byte(body), "test", false, "", false)
				case "passthrough":
					_, err = svc.buildUpstreamRequestOpenAIPassthrough(context.Background(), c, account, []byte(body), "test")
				case "chat":
					_, err = svc.sendCCUpstreamRequest(context.Background(), c, account, "https://api.openai.com/v1/chat/completions", []byte(body), false, "test", "", "")
				}
				require.NoError(t, err)
			}
			send(`{"model":"gpt-6-astra","service_tier":"ultrafast","reasoning":{"effort":"high"}}`)
			markOpenAICyberPolicyEvent(c, []byte(`{"type":"response.failed","response":{"model":"gpt-6-astra","service_tier":"default","error":{"code":"cyber_policy"}}}`), 200, &OpenAIUsage{InputTokens: 100, OutputTokens: 3, CacheReadInputTokens: 60})
			saved := *GetOpsCyberPolicy(c)
			require.Equal(t, "gpt-6-astra", saved.Billing.UpstreamModel)
			require.Equal(t, "ultrafast", saved.Billing.ServiceTier)
			require.Equal(t, "high", saved.Billing.ReasoningEffort)
			require.Equal(t, "default", saved.Billing.UpstreamResponseServiceTier)
			require.Equal(t, 60, saved.Billing.Usage.CacheReadInputTokens)
			ClearOpsCyberPolicy(c)
			MarkOpsCyberPolicy(c, CyberPolicyMark{Message: "no dispatch"})
			require.Empty(t, GetOpsCyberPolicy(c).Billing.UpstreamModel)
			require.Empty(t, GetOpsCyberPolicy(c).Billing.ServiceTier)
			ClearOpsCyberPolicy(c)
			send(`{"model":"gpt-6-astra","service_tier":"ultrafast"}`)
			send(`{"model":"gpt-4o"}`)
			markOpenAICyberPolicyEvent(c, []byte(`{"error":{"code":"cyber_policy"}}`), 400, nil)
			next := GetOpsCyberPolicy(c).Billing
			require.Equal(t, "gpt-4o", next.UpstreamModel)
			require.Empty(t, next.ServiceTier, "new attempt without tier must replace the old tier")
			require.Empty(t, next.ReasoningEffort)
			require.Empty(t, next.UpstreamResponseModel)
			require.Empty(t, next.UpstreamResponseServiceTier)
			require.Zero(t, next.Usage)
			require.Equal(t, "ultrafast", saved.Billing.ServiceTier, "stored mark is a value snapshot")
		})
	}
}

func TestCyberBillingMetadataFullPayloadAndNoStaleObserver(t *testing.T) {
	for _, tier := range []string{"ultrafast", "default", "priority", "flex", "auto", "unknown", ""} {
		t.Run(tier, func(t *testing.T) {
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			observer := beginUpstreamResponseModelObservation(c)
			observer.Observe("old-attempt", true)
			observer.ObserveServiceTier("ultrafast", true)
			payload := []byte(fmt.Sprintf(`{"padding":%q,"type":"response.failed","response":{"model":"gpt-6.1-sol","service_tier":%q,"error":{"code":"cyber_policy"}}}`, strings.Repeat("x", 5000), tier))
			require.True(t, markOpenAICyberPolicyEvent(c, payload, 200, &OpenAIUsage{InputTokens: 12, CacheReadInputTokens: 5}))
			mark := GetOpsCyberPolicy(c)
			require.LessOrEqual(t, len(mark.Body), 4096)
			require.Equal(t, "gpt-6.1-sol", mark.Billing.UpstreamResponseModel)
			expectedTier := tier
			if tier == "auto" || tier == "unknown" {
				expectedTier = ""
			}
			require.Equal(t, expectedTier, mark.Billing.UpstreamResponseServiceTier)
			require.Empty(t, mark.Billing.ServiceTier, "response tier alone must not invent outbound Ultra")
			require.Equal(t, 5, mark.Billing.Usage.CacheReadInputTokens)
		})
	}
}

func TestCyberBillingMetadataObserverAcceptsUltrafast(t *testing.T) {
	o := &upstreamResponseModelObserver{}
	o.ObserveOpenAI([]byte(`{"type":"response.failed","response":{"model":"gpt-6-astra","service_tier":"ultrafast"}}`), "response.failed")
	require.Equal(t, "ultrafast", o.ServiceTier())
	require.Equal(t, "default", ResolveOpenAIServiceTierBilling(&Account{Type: AccountTypeAPIKey, Platform: PlatformOpenAI}, "ultrafast", "default").Billing)
}

func TestCyberBillingMetadataHTTPToWSUsesUpstreamModel(t *testing.T) {
	for _, stream := range []bool{false, true} {
		t.Run(fmt.Sprint(stream), func(t *testing.T) {
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
			cfg := newOpenAIWSV2TestConfig()
			cfg.Security.URLAllowlist.Enabled = false
			cfg.Security.URLAllowlist.AllowInsecureHTTP = true
			cfg.Gateway.OpenAIWS.MinIdlePerAccount = 0
			captureConn := &openAIWSCaptureConn{events: [][]byte{[]byte(`{"type":"response.failed","response":{"id":"resp_cyber_model","model":"gpt-6-astra","service_tier":"default","status":"failed","error":{"code":"cyber_policy"},"usage":{"input_tokens":20,"output_tokens":2,"input_tokens_details":{"cached_tokens":12}}}}`)}}
			pool := newOpenAIWSConnPool(cfg)
			pool.setClientDialerForTest(&openAIWSCaptureDialer{conn: captureConn})
			svc := &OpenAIGatewayService{cfg: cfg, httpUpstream: &httpUpstreamRecorder{}, cache: &stubGatewayCache{}, openaiWSResolver: NewOpenAIWSProtocolResolver(cfg), toolCorrector: NewCodexToolCorrector(), openaiWSPool: pool}
			account := &Account{ID: 5884, Platform: PlatformOpenAI, Type: AccountTypeAPIKey, Status: StatusActive, Schedulable: true, Concurrency: 1, Credentials: map[string]any{"api_key": "sk-test", "model_mapping": map[string]any{"public-alias": "gpt-6-astra"}}, Extra: map[string]any{"responses_websockets_v2_enabled": true}}
			_, err := svc.Forward(context.Background(), c, account, []byte(fmt.Sprintf(`{"model":"public-alias","stream":%t,"service_tier":"ultrafast","input":"hi"}`, stream)))
			require.NoError(t, err)
			mark := GetOpsCyberPolicy(c)
			require.NotNil(t, mark)
			require.Equal(t, "gpt-6-astra", mark.Billing.UpstreamModel)
			require.Equal(t, "gpt-6-astra", mark.Billing.BillingModel)
			require.Equal(t, "gpt-6-astra", mark.Billing.UpstreamResponseModel, "must capture before restoring the client alias")
			require.Equal(t, "ultrafast", mark.Billing.ServiceTier)
			require.Equal(t, "default", mark.Billing.UpstreamResponseServiceTier)
			require.Equal(t, 12, mark.Billing.Usage.CacheReadInputTokens)
		})
	}
}

func TestCyberBillingMetadataCopiesUsageAndConflict(t *testing.T) {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	observer := &upstreamResponseModelObserver{}
	observer.Observe("earlier-model", false)
	observer.Observe("gpt-6-astra", true)
	payload := []byte(`{"type":"response.failed","response":{"model":"gpt-6-astra","service_tier":"priority","error":{"code":"cyber_policy"},"usage":{"input_tokens":20,"output_tokens":2,"input_tokens_details":{"cached_tokens":12}}}}`)
	require.True(t, markOpenAICyberPolicyEvent(c, payload, 200, nil, observer))
	mark := GetOpsCyberPolicy(c)
	require.True(t, mark.Billing.UpstreamResponseModelConflict)
	require.Equal(t, 20, mark.Billing.Usage.InputTokens)
	require.Equal(t, 12, mark.Billing.Usage.CacheReadInputTokens)
	require.Equal(t, 20, mark.UpstreamInTok)
	require.Equal(t, 2, mark.UpstreamOutTok)
}

func TestCyberBillingMetadataPassthroughTurnClearOrdering(t *testing.T) {
	controlCtx, cancel := context.WithCancelCause(context.Background())
	upstream := newStagedPassthroughConn()
	cfg := passthroughLifecycleConfig()
	cfg.Gateway.OpenAIWS.IngressInterTurnIdleTimeoutSeconds = 5
	entered := make(chan struct{})
	release := make(chan struct{})
	marks := make(chan OpenAICyberBillingMetadata, 2)
	server, serverErr := startPassthroughLifecycleServerWithHooks(t, controlCtx, newPassthroughLifecycleService(cfg, upstream), passthroughLifecycleAccount(), func(c *gin.Context) *OpenAIWSIngressHooks {
		return &OpenAIWSIngressHooks{AfterTurn: func(turn int, _ *OpenAIForwardResult, _ error) {
			if turn == 1 {
				close(entered)
				select {
				case <-release:
				case <-controlCtx.Done():
				}
			}
			if mark := GetOpsCyberPolicy(c); mark != nil {
				marks <- mark.Billing
			}
			ClearOpsCyberPolicy(c)
		}}
	})
	client := dialPassthroughLifecycleClient(t, server)
	defer func() { cancel(context.Canceled); _ = client.CloseNow(); server.Close() }()
	requirePassthroughUpstreamWrite(t, upstream, time.Second)
	upstream.Send(`{"type":"response.completed","response":{"id":"first","model":"gpt-5.1","usage":{"input_tokens":1,"output_tokens":1}}}`)
	readDone := make(chan error, 1)
	go func() {
		ctx, stop := context.WithTimeout(context.Background(), 3*time.Second)
		defer stop()
		_, _, err := client.Read(ctx)
		readDone <- err
	}()
	select {
	case <-entered:
	case <-time.After(3 * time.Second):
		t.Fatal("first AfterTurn was not reached")
	}
	select {
	case err := <-readDone:
		t.Fatalf("terminal released before AfterTurn cleared billing metadata: %v", err)
	case <-time.After(30 * time.Millisecond):
	}
	close(release)
	select {
	case err := <-readDone:
		require.NoError(t, err)
	case <-time.After(3 * time.Second):
		t.Fatal("terminal was not released")
	}
	for _, tc := range []struct{ request, event, model, tier string }{
		{`{"type":"response.create","model":"gpt-6-astra","service_tier":"ultrafast"}`, `{"type":"response.failed","response":{"id":"second","model":"gpt-6-astra","service_tier":"default","error":{"code":"cyber_policy"},"usage":{"input_tokens":3,"output_tokens":1}}}`, "gpt-6-astra", "ultrafast"},
		{`{"type":"response.create","model":"gpt-4o"}`, `{"type":"response.failed","response":{"id":"third","model":"gpt-4o","error":{"code":"cyber_policy"},"usage":{"input_tokens":2,"output_tokens":1}}}`, "gpt-4o", ""},
	} {
		ctx, stop := context.WithTimeout(context.Background(), time.Second)
		err := client.Write(ctx, coderws.MessageText, []byte(tc.request))
		stop()
		require.NoError(t, err)
		requirePassthroughUpstreamWrite(t, upstream, time.Second)
		upstream.Send(tc.event)
		_, err = readPassthroughLifecycleFrame(t, client, 3*time.Second)
		require.NoError(t, err)
		select {
		case mark := <-marks:
			require.Equal(t, tc.model, mark.UpstreamModel)
			require.Equal(t, tc.model, mark.BillingModel)
			require.Equal(t, tc.tier, mark.ServiceTier)
			if tc.tier == "" {
				require.Empty(t, mark.UpstreamResponseServiceTier)
			}
		case <-time.After(time.Second):
			t.Fatal("cyber turn did not retain its own billing snapshot")
		}
	}
	require.NoError(t, client.Close(coderws.StatusNormalClosure, "done"))
	select {
	case <-serverErr:
	case <-time.After(3 * time.Second):
		t.Fatal("passthrough did not exit")
	}
}

func TestCyberBillingMetadataExplicitBillingModelDoesNotFollowFallback(t *testing.T) {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	setOpenAICyberBillingModel(c, "gpt-6-astra")
	captureOpenAICyberBillingRequest(c, []byte(`{"model":"gpt-6-astra","service_tier":"ultrafast"}`))
	// Compact/rejected-field retries may replace the sent model without changing
	// the normal forwarding result's explicit billing model.
	captureOpenAICyberBillingRequest(c, []byte(`{"model":"gpt-5.4"}`))
	MarkOpsCyberPolicy(c, CyberPolicyMark{})
	require.Equal(t, "gpt-6-astra", GetOpsCyberPolicy(c).Billing.BillingModel)
	require.Equal(t, "gpt-5.4", GetOpsCyberPolicy(c).Billing.UpstreamModel)
	ClearOpsCyberPolicy(c)
	captureOpenAICyberBillingRequest(c, []byte(`{"model":"gpt-4o"}`))
	MarkOpsCyberPolicy(c, CyberPolicyMark{})
	require.Empty(t, GetOpsCyberPolicy(c).Billing.BillingModel, "turn clear drops the old explicit billing model")
	ClearOpsCyberPolicy(c)
	setOpenAICyberBillingModel(c, "old-account")
	setOpenAICyberBillingModel(c, "")
	captureOpenAICyberBillingRequest(c, []byte(`{"model":"gpt-4o"}`))
	MarkOpsCyberPolicy(c, CyberPolicyMark{})
	require.Empty(t, GetOpsCyberPolicy(c).Billing.BillingModel, "attempt entry resets an old account's billing model")
	ClearOpsCyberPolicy(c)
	captureOpenAICyberBillingRequest(c, []byte(`{"model":"gpt-4o"}`), "gpt-4o")
	MarkOpsCyberPolicy(c, CyberPolicyMark{})
	require.Equal(t, "gpt-4o", GetOpsCyberPolicy(c).Billing.BillingModel, "WS turn supplies its own upstream default")
}

func TestCyberBillingMetadataTerminalCompletesFirstAuditMark(t *testing.T) {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	captureOpenAICyberBillingRequest(c, []byte(`{"model":"gpt-6-astra","service_tier":"ultrafast"}`), "priced-alias")
	first := []byte(`{"type":"error","error":{"code":"cyber_policy","message":"first evidence"}}`)
	require.True(t, markOpenAICyberPolicyEvent(c, first, 200, nil))
	original := *GetOpsCyberPolicy(c)
	terminal := []byte(`{"type":"response.failed","response":{"model":"gpt-6-astra","service_tier":"default","error":{"code":"cyber_policy","message":"terminal evidence"}}}`)
	require.True(t, markOpenAICyberPolicyEvent(c, terminal, 200, &OpenAIUsage{InputTokens: 100, OutputTokens: 20, CacheReadInputTokens: 30}))
	completed := *GetOpsCyberPolicy(c)
	require.Equal(t, original.Message, completed.Message)
	require.Equal(t, original.Body, completed.Body)
	require.Equal(t, 100, completed.UpstreamInTok)
	require.Equal(t, 20, completed.UpstreamOutTok)
	require.Equal(t, 30, completed.Billing.Usage.CacheReadInputTokens)
	require.Equal(t, "default", completed.Billing.UpstreamResponseServiceTier)
	require.Equal(t, "ultrafast", completed.Billing.ServiceTier)
	require.Equal(t, "priced-alias", completed.Billing.BillingModel)
	require.Zero(t, original.UpstreamInTok, "replacement must not mutate a prior snapshot")
	require.True(t, markOpenAICyberPolicyEvent(c, terminal, 200, &OpenAIUsage{InputTokens: 999}))
	require.Equal(t, completed, *GetOpsCyberPolicy(c), "first completed terminal remains authoritative")
}
