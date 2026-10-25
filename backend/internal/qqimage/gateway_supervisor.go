package qqimage

import (
	"context"
	"errors"
	"sync"
	"time"
)

type gatewayLease interface {
	Check(context.Context) error
	Guard(context.Context) error
	Release()
}
type gatewaySupervisorStore interface {
	BotRuntimeConfig(context.Context) (*RuntimeConfig, error)
	AcquireGatewayLease(context.Context) (gatewayLease, error)
}
type gatewayRunner interface {
	Run(context.Context)
	Status() GatewayStatus
}

// GatewaySupervisor owns a single database-scoped receiver. A configuration
// revision or lost database session cancels the old transport before replacing it.
type GatewaySupervisor struct {
	store        gatewaySupervisorStore
	commands     *QQCommandHandler
	factory      func(*RuntimeConfig, func(context.Context, []byte) error) gatewayRunner
	pollInterval time.Duration
	runMu        sync.Mutex
	mu           sync.RWMutex
	runner       gatewayRunner
	state        GatewayStatus
}

func NewGatewaySupervisor(store gatewaySupervisorStore, client *QQClient, commands *QQCommandHandler) *GatewaySupervisor {
	return &GatewaySupervisor{store: store, commands: commands, pollInterval: 2 * time.Second, state: GatewayStatus{State: "waiting_config"}, factory: func(c *RuntimeConfig, f func(context.Context, []byte) error) gatewayRunner {
		return NewGateway(client, c, f)
	}}
}
func (g *GatewaySupervisor) Status() GatewayStatus {
	g.mu.RLock()
	defer g.mu.RUnlock()
	if g.runner != nil {
		return g.runner.Status()
	}
	return g.state
}
func (g *GatewaySupervisor) set(state, code string) {
	g.mu.Lock()
	g.state = GatewayStatus{State: state, ErrorCode: code}
	g.mu.Unlock()
}
func (g *GatewaySupervisor) Run(ctx context.Context) {
	if !g.runMu.TryLock() {
		return
	}
	defer g.runMu.Unlock()
	var activeID int64
	var activeApp string
	var lease gatewayLease
	var cancel context.CancelFunc
	var done chan struct{}
	stop := func() {
		if cancel != nil {
			cancel()
			<-done
			cancel = nil
		}
		g.mu.Lock()
		g.runner = nil
		g.mu.Unlock()
		if lease != nil {
			lease.Release()
			lease = nil
		}
		activeID = 0
		activeApp = ""
	}
	defer func() { stop(); g.set("stopped", "") }()
	interval := g.pollInterval
	if interval <= 0 {
		interval = 2 * time.Second
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		if ctx.Err() != nil {
			return
		}
		op, finish := context.WithTimeout(ctx, 3*time.Second)
		c, err := g.store.BotRuntimeConfig(op)
		finish()
		switch {
		case err != nil:
			stop()
			if errors.Is(err, ErrNotFound) || errors.Is(err, ErrDisabled) || errors.Is(err, ErrEncryptionUnavailable) {
				g.set("waiting_config", "")
			} else {
				g.set("error", "database_unavailable")
			}
		case c == nil || c.AppID == "" || c.BotSecret == "":
			stop()
			g.set("waiting_config", "")
		case receiveMode(c.ReceiveMode) != ReceiveModeWebSocket:
			stop()
			g.set("webhook", "")
		default:
			if cancel != nil && (activeID != c.ID || activeApp != c.AppID) {
				stop()
			}
			if lease != nil {
				op, finish = context.WithTimeout(ctx, 3*time.Second)
				err = lease.Check(op)
				finish()
				if err != nil {
					stop()
					g.set("error", "receiver_lease_lost")
				}
			}
			if lease == nil {
				op, finish = context.WithTimeout(ctx, 3*time.Second)
				lease, err = g.store.AcquireGatewayLease(op)
				finish()
				if err != nil {
					if errors.Is(err, ErrConflict) {
						g.set("standby", "")
					} else {
						g.set("error", "database_unavailable")
					}
				}
			}
			if lease != nil && cancel == nil {
				activeID, activeApp = c.ID, c.AppID
				boundID, boundApp, boundLease := activeID, activeApp, lease
				receiverCtx, receiverCancel := context.WithCancel(ctx)
				cancel = receiverCancel
				done = make(chan struct{})
				runner := g.factory(c, func(eventCtx context.Context, raw []byte) error {
					if g.commands == nil {
						return ErrDisabled
					}
					if receiverCtx.Err() != nil {
						return receiverCtx.Err()
					}
					return g.commands.HandleGatewayEvent(withGatewayGuard(eventCtx, boundLease.Guard), boundID, boundApp, raw)
				})
				g.mu.Lock()
				g.runner = runner
				g.mu.Unlock()
				receiverDone := done
				go func() { defer close(receiverDone); runner.Run(receiverCtx) }()
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}
