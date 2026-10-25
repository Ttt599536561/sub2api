package qqimage

import (
	"context"
	"encoding/json"
	"errors"
	"math/rand/v2"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gorilla/websocket"
)

// GatewayStatus deliberately exposes no URL, token, session identifier or raw error.
type GatewayStatus struct {
	State           string     `json:"state"`
	LastConnectedAt *time.Time `json:"last_connected_at,omitempty"`
	LastHeartbeatAt *time.Time `json:"last_heartbeat_at,omitempty"`
	LastEventAt     *time.Time `json:"last_event_at,omitempty"`
	ErrorCode       string     `json:"error_code,omitempty"`
}

type Gateway struct {
	client       *QQClient
	config       *RuntimeConfig
	dispatch     func(context.Context, []byte) error
	dial         func(context.Context, string) (*websocket.Conn, *http.Response, error)
	wait         func(context.Context, time.Duration) bool
	queueSize    int
	drainTimeout time.Duration
	authTimeout  time.Duration
	runMu        sync.Mutex
	mu           sync.RWMutex
	status       GatewayStatus
	session      string
	checkpoint   *int64
}

type gatewayResult struct {
	code         string
	terminal     bool
	clearSession bool
	refreshToken bool
	delay        time.Duration
	stable       bool
}

type gatewayFrame struct {
	Op       *int            `json:"op"`
	Data     json.RawMessage `json:"d"`
	Sequence *int64          `json:"s"`
	Type     string          `json:"t"`
	raw      []byte
}

func NewGateway(client *QQClient, config *RuntimeConfig, dispatch func(context.Context, []byte) error) *Gateway {
	if client == nil {
		client = NewQQClient()
	}
	var frozen *RuntimeConfig
	if config != nil {
		copyConfig := *config
		copyConfig.AllowedGroups = append([]string(nil), config.AllowedGroups...)
		copyConfig.ImageHosts = append([]string(nil), config.ImageHosts...)
		frozen = &copyConfig
	}
	return &Gateway{client: client, config: frozen, dispatch: dispatch, dial: secureGatewayDial, wait: gatewayWait, queueSize: 64, drainTimeout: 30 * time.Second, authTimeout: 15 * time.Second, status: GatewayStatus{State: "stopped"}}
}
func (g *Gateway) Status() GatewayStatus {
	g.mu.RLock()
	defer g.mu.RUnlock()
	s := g.status
	clone := func(p *time.Time) *time.Time {
		if p == nil {
			return nil
		}
		v := *p
		return &v
	}
	s.LastConnectedAt = clone(s.LastConnectedAt)
	s.LastHeartbeatAt = clone(s.LastHeartbeatAt)
	s.LastEventAt = clone(s.LastEventAt)
	return s
}
func (g *Gateway) setState(state, code string) {
	g.mu.Lock()
	g.status.State = state
	g.status.ErrorCode = code
	g.mu.Unlock()
}
func (g *Gateway) sessionSnapshot() (string, *int64) {
	g.mu.RLock()
	defer g.mu.RUnlock()
	if g.checkpoint == nil {
		return g.session, nil
	}
	v := *g.checkpoint
	return g.session, &v
}
func (g *Gateway) clearSession() { g.mu.Lock(); g.session = ""; g.checkpoint = nil; g.mu.Unlock() }

// Run owns one connection at a time. The supervisor owns configuration changes
// and the database leadership lease; cancellation joins all connection workers.
func (g *Gateway) Run(ctx context.Context) {
	if !g.runMu.TryLock() {
		return
	}
	defer g.runMu.Unlock()
	if g.config == nil || g.config.AppID == "" || g.config.BotSecret == "" || g.dispatch == nil {
		g.setState("error", "qq_gateway_configuration_unavailable")
		return
	}
	var budgetUntil, nextIdentify time.Time
	var remaining int64
	attempts := 0
	g.setState("connecting", "")
	for ctx.Err() == nil {
		token, reason := g.client.token(ctx, g.config)
		var endpoint gatewayEndpoint
		result := gatewayResult{}
		if reason != "" {
			result.code = "qq_gateway_token_unavailable"
		} else {
			endpoint, result = g.endpoint(ctx, token)
		}
		if result.code == "" {
			now := time.Now()
			if budgetUntil.IsZero() || !now.Before(budgetUntil) {
				remaining = endpoint.remaining
				budgetUntil = now.Add(endpoint.resetAfter)
			} else if endpoint.remaining < remaining {
				remaining = endpoint.remaining
			}
			session, seq := g.sessionSnapshot()
			if session == "" || seq == nil {
				if remaining == 0 {
					g.setState("reconnecting", "qq_gateway_session_limit")
					if !g.wait(ctx, time.Until(budgetUntil)) {
						break
					}
					continue
				}
				if delay := time.Until(nextIdentify); delay > 0 {
					g.setState("reconnecting", "qq_gateway_identify_limit")
					if !g.wait(ctx, delay) {
						break
					}
				}
			}
			result = g.connection(ctx, endpoint.url, token, func() { remaining--; nextIdentify = time.Now().Add(5 * time.Second) })
		}
		if ctx.Err() != nil {
			break
		}
		if result.clearSession {
			g.clearSession()
		}
		if result.refreshToken {
			g.invalidateToken()
		}
		if result.terminal {
			g.setState("error", result.code)
			return
		}
		if result.stable {
			attempts = 0
		}
		delay := gatewayBackoff(attempts)
		// Equal jitter retains a nonzero lower bound and caps the full delay at 60s.
		delay = delay/2 + time.Duration(rand.Int64N(int64(delay/2)+1))
		if result.delay > delay {
			delay = result.delay
		}
		if attempts < 30 {
			attempts++
		}
		g.setState("reconnecting", result.code)
		if !g.wait(ctx, delay) {
			break
		}
	}
	g.mu.Lock()
	g.status.State = "stopped"
	g.mu.Unlock()
}
func gatewayWait(ctx context.Context, d time.Duration) bool {
	if d < 0 {
		d = 0
	}
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return ctx.Err() == nil
	}
}
func gatewayBackoff(attempt int) time.Duration {
	if attempt < 0 {
		attempt = 0
	}
	if attempt >= 6 {
		return time.Minute
	}
	d := time.Second << attempt
	if d > time.Minute {
		return time.Minute
	}
	return d
}
func gatewayCloseResult(code int) gatewayResult {
	result := gatewayResult{code: "qq_gateway_disconnected"}
	switch {
	case code == 4001 || code == 4002 || (code >= 4010 && code <= 4014) || code == 4914 || code == 4915:
		result.terminal = true
		result.code = "qq_gateway_fatal_close"
	case code == 4004:
		result.refreshToken = true
		result.code = "qq_gateway_authentication_failed"
	case code == 4006 || code == 4007 || (code >= 4900 && code <= 4913):
		result.clearSession = true
		result.code = "qq_gateway_session_invalid"
	case code == 4008:
		result.delay = time.Minute
		result.code = "qq_gateway_rate_limited"
	}
	return result
}
func gatewayReadFrame(conn *websocket.Conn) (gatewayFrame, gatewayResult) {
	kind, raw, err := conn.ReadMessage()
	if err != nil {
		var closed *websocket.CloseError
		if errors.As(err, &closed) {
			return gatewayFrame{}, gatewayCloseResult(closed.Code)
		}
		return gatewayFrame{}, gatewayResult{code: "qq_gateway_disconnected"}
	}
	var frame gatewayFrame
	if kind != websocket.TextMessage || json.Unmarshal(raw, &frame) != nil || frame.Op == nil {
		return frame, gatewayResult{code: "qq_gateway_invalid_frame"}
	}
	frame.raw = raw
	return frame, gatewayResult{}
}
func gatewayWrite(conn *websocket.Conn, op int, data any) error {
	if err := conn.SetWriteDeadline(time.Now().Add(5 * time.Second)); err != nil {
		return err
	}
	return conn.WriteJSON(struct {
		Op   int `json:"op"`
		Data any `json:"d"`
	}{op, data})
}

func (g *Gateway) connection(ctx context.Context, target, token string, identify func()) (out gatewayResult) {
	conn, response, err := g.dial(ctx, target)
	if response != nil && response.Body != nil {
		_ = response.Body.Close()
	}
	if err != nil || conn == nil {
		return gatewayResult{code: "qq_gateway_connect_failed"}
	}
	conn.SetReadLimit(gatewayMaxFrame)
	connCtx, cancelConn := context.WithCancel(ctx)
	businessCtx, cancelBusiness := context.WithCancel(ctx)
	var workers sync.WaitGroup
	workers.Add(1)
	go func() { defer workers.Done(); <-connCtx.Done(); _ = conn.Close() }()
	defer func() { cancelBusiness(); cancelConn(); _ = conn.Close(); workers.Wait() }()
	_ = conn.SetReadDeadline(time.Now().Add(15 * time.Second))
	hello, result := gatewayReadFrame(conn)
	if result.code != "" {
		return result
	}
	var data struct {
		Interval int64 `json:"heartbeat_interval"`
	}
	if *hello.Op != 10 || json.Unmarshal(hello.Data, &data) != nil || data.Interval < 1 || data.Interval > 300000 {
		return gatewayResult{code: "qq_gateway_invalid_hello"}
	}
	_ = conn.SetReadDeadline(time.Time{})
	session, checkpoint := g.sessionSnapshot()
	resuming := session != "" && checkpoint != nil
	if resuming {
		err = gatewayWrite(conn, 6, map[string]any{"token": "QQBot " + token, "session_id": session, "seq": *checkpoint})
	} else {
		identify()
		err = gatewayWrite(conn, 2, map[string]any{"token": "QQBot " + token, "intents": 1 << 25, "shard": [2]int{0, 1}})
	}
	if err != nil {
		return gatewayResult{code: "qq_gateway_write_failed"}
	}
	queueSize := g.queueSize
	if queueSize < 1 {
		queueSize = 1
	}
	queue := make(chan gatewayFrame, queueSize)
	failures := make(chan gatewayResult, 1)
	fail := func(r gatewayResult) {
		select {
		case failures <- r:
		default:
		}
	}
	businessFailures := make(chan gatewayResult, 1)
	businessDone := make(chan struct{})
	ready := make(chan struct{})
	readySignal := ready
	authenticatedOnWire := make(chan struct{})
	wireSignal := authenticatedOnWire
	requestedHeartbeat := make(chan struct{}, 1)
	var awaitingACK atomic.Bool
	// Heartbeats acknowledge receipt; RESUME uses only the business checkpoint.
	var receivedSequence atomic.Pointer[int64]
	receivedSequence.Store(checkpoint)
	workers.Add(3)
	go func() {
		defer workers.Done()
		defer close(businessDone)
		g.processFrames(businessCtx, queue, resuming, readySignal, func(r gatewayResult) { businessFailures <- r; fail(r) })
	}()
	go func() {
		defer workers.Done()
		defer close(queue)
		resumeAccepted := false
		for connCtx.Err() == nil {
			frame, result := gatewayReadFrame(conn)
			if result.code != "" {
				fail(result)
				return
			}
			switch *frame.Op {
			case 0:
				if frame.Sequence == nil || *frame.Sequence < 0 || frame.Type == "" {
					fail(gatewayResult{code: "qq_gateway_invalid_frame"})
					return
				}
				receivedSequence.Store(frame.Sequence)
				// Replayed dispatches can precede RESUMED. Their arrival proves the
				// server accepted RESUME, without advancing the business checkpoint.
				if resuming && !resumeAccepted && frame.Type != "READY" {
					resumeAccepted = true
					close(wireSignal)
				}
				select {
				case queue <- frame:
				default:
					fail(gatewayResult{code: "qq_gateway_queue_full"})
					return
				}
				// Persist READY before a reconnect or burst can cancel its worker.
				if frame.Type == "READY" && !resuming {
					select {
					case <-readySignal:
					case <-connCtx.Done():
						return
					}
				}
			case 1:
				select {
				case requestedHeartbeat <- struct{}{}:
				default:
				}
			case 11:
				awaitingACK.Store(false)
				g.mu.Lock()
				now := time.Now()
				g.status.LastHeartbeatAt = &now
				g.mu.Unlock()
			case 7:
				fail(gatewayResult{code: "qq_gateway_reconnect_requested"})
				return
			case 9:
				var resumable bool
				if json.Unmarshal(frame.Data, &resumable) != nil {
					fail(gatewayResult{code: "qq_gateway_invalid_frame"})
					return
				}
				fail(gatewayResult{code: "qq_gateway_session_invalid", clearSession: !resumable, delay: 3 * time.Second})
				return
			default:
				fail(gatewayResult{code: "qq_gateway_invalid_frame"})
				return
			}
		}
	}()
	go func() {
		defer workers.Done()
		ticker := time.NewTicker(time.Duration(data.Interval) * time.Millisecond)
		defer ticker.Stop()
		for {
			scheduled := false
			select {
			case <-connCtx.Done():
				return
			case <-ticker.C:
				scheduled = true
			case <-requestedHeartbeat:
			}
			if scheduled && awaitingACK.Load() {
				fail(gatewayResult{code: "qq_gateway_heartbeat_timeout"})
				return
			}
			awaitingACK.Store(true)
			if gatewayWrite(conn, 1, receivedSequence.Load()) != nil {
				fail(gatewayResult{code: "qq_gateway_write_failed"})
				return
			}
		}
	}()
	authTimeout := g.authTimeout
	if authTimeout <= 0 {
		authTimeout = 15 * time.Second
	}
	authTimer := time.NewTimer(authTimeout)
	defer authTimer.Stop()
	var connectedAt time.Time
	for {
		select {
		case <-ctx.Done():
			return gatewayResult{}
		case result := <-failures:
			result.stable = !connectedAt.IsZero() && time.Since(connectedAt) >= 30*time.Second
			if result.code == "qq_gateway_queue_full" {
				// Stop network intake while the bounded accepted prefix finishes. Canceling
				// it immediately can replay the same full burst forever without progress.
				// Business still inherits the supervisor context, so losing leadership or
				// changing configuration cancels it immediately, including queued events.
				cancelConn()
				_ = conn.Close()
				g.setState("reconnecting", result.code)
				timeout := g.drainTimeout
				if timeout <= 0 {
					timeout = 30 * time.Second
				}
				drainTimer := time.NewTimer(timeout)
				select {
				case <-ctx.Done():
					drainTimer.Stop()
					return gatewayResult{}
				case <-businessDone:
					drainTimer.Stop()
				case <-drainTimer.C:
					cancelBusiness()
					<-businessDone
				}
				select {
				case failed := <-businessFailures:
					return failed
				default:
				}
			}
			return result
		case <-authenticatedOnWire:
			authenticatedOnWire = nil
			authTimer.Stop()
		case <-ready:
			connectedAt = time.Now()
			g.mu.Lock()
			now := connectedAt
			g.status.State = "connected"
			g.status.ErrorCode = ""
			g.status.LastConnectedAt = &now
			g.mu.Unlock()
			ready = nil
			authTimer.Stop()
		case <-authTimer.C:
			return gatewayResult{code: "qq_gateway_ready_timeout"}
		}
	}
}
func (g *Gateway) processFrames(ctx context.Context, queue <-chan gatewayFrame, resuming bool, ready chan<- struct{}, fail func(gatewayResult)) {
	authenticated := false
	for ctx.Err() == nil {
		var frame gatewayFrame
		select {
		case <-ctx.Done():
			return
		case next, ok := <-queue:
			if !ok {
				return
			}
			frame = next
		}
		if ctx.Err() != nil {
			return
		}
		switch frame.Type {
		case "READY":
			var data struct {
				Session string `json:"session_id"`
			}
			if authenticated || resuming || json.Unmarshal(frame.Data, &data) != nil || data.Session == "" || len(data.Session) > 1024 {
				fail(gatewayResult{code: "qq_gateway_invalid_ready", clearSession: true})
				return
			}
			g.mu.Lock()
			g.session = data.Session
			g.mu.Unlock()
			authenticated = true
		case "RESUMED":
			if authenticated || !resuming {
				fail(gatewayResult{code: "qq_gateway_invalid_ready", clearSession: true})
				return
			}
			authenticated = true
		default:
			if !authenticated && !resuming {
				fail(gatewayResult{code: "qq_gateway_event_before_ready"})
				return
			}
			_, checkpoint := g.sessionSnapshot()
			if checkpoint != nil && *frame.Sequence <= *checkpoint {
				continue
			}
			if frame.Type == "GROUP_AT_MESSAGE_CREATE" {
				if err := g.dispatch(ctx, frame.raw); err != nil && !gatewayDomainRejection(err) {
					fail(gatewayResult{code: "qq_gateway_dispatch_failed"})
					return
				}
				g.mu.Lock()
				now := time.Now()
				g.status.LastEventAt = &now
				g.mu.Unlock()
			}
		}
		g.mu.Lock()
		seq := *frame.Sequence
		g.checkpoint = &seq

		g.mu.Unlock()
		if frame.Type == "READY" || frame.Type == "RESUMED" {
			close(ready)
		}
	}
}
func gatewayDomainRejection(err error) bool {
	for _, domain := range []error{ErrInvalid, ErrForbidden, ErrExpired, ErrDisabled, ErrConflict, ErrNotFound, ErrQQAuthentication, ErrQQUnsupportedEvent, ErrInsufficientBalance, ErrLimit} {
		if errors.Is(err, domain) {
			return true
		}
	}
	return false
}
