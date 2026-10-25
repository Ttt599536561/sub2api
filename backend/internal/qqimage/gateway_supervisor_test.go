package qqimage

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type fakeGatewayLease struct {
	lost     atomic.Bool
	released atomic.Bool
}

func (l *fakeGatewayLease) Check(context.Context) error {
	if l.lost.Load() {
		return errors.New("lost")
	}
	return nil
}
func (l *fakeGatewayLease) Guard(ctx context.Context) error { return l.Check(ctx) }
func (l *fakeGatewayLease) Release()                        { l.released.Store(true) }

type supervisorStore struct {
	mu     sync.Mutex
	config *RuntimeConfig
	err    error
	busy   bool
	lease  *fakeGatewayLease
}

func (s *supervisorStore) BotRuntimeConfig(context.Context) (*RuntimeConfig, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.config == nil {
		return nil, ErrNotFound
	}
	v := *s.config
	return &v, s.err
}
func (s *supervisorStore) AcquireGatewayLease(context.Context) (gatewayLease, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.busy {
		return nil, ErrConflict
	}
	s.lease = &fakeGatewayLease{}
	return s.lease, nil
}

type fakeGatewayRunner struct{ started, stopped chan struct{} }

func (r *fakeGatewayRunner) Run(ctx context.Context) {
	close(r.started)
	<-ctx.Done()
	close(r.stopped)
}
func (r *fakeGatewayRunner) Status() GatewayStatus { return GatewayStatus{State: "connected"} }
func awaitGateway(t *testing.T, f func() bool) {
	t.Helper()
	deadline := time.NewTimer(3 * time.Second)
	defer deadline.Stop()
	tick := time.NewTicker(time.Millisecond)
	defer tick.Stop()
	for !f() {
		select {
		case <-deadline.C:
			t.Fatal("gateway state timeout")
		case <-tick.C:
		}
	}
}
func TestGatewaySupervisorConfigChangeAndShutdown(t *testing.T) {
	s := &supervisorStore{config: qqConfig()}
	s.config.ReceiveMode = ReceiveModeWebSocket
	g := NewGatewaySupervisor(s, NewQQClient(), nil)
	g.pollInterval = 5 * time.Millisecond
	runners := make(chan *fakeGatewayRunner, 4)
	g.factory = func(*RuntimeConfig, func(context.Context, []byte) error) gatewayRunner {
		r := &fakeGatewayRunner{make(chan struct{}), make(chan struct{})}
		runners <- r
		return r
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { g.Run(ctx); close(done) }()
	defer cancel()
	var first *fakeGatewayRunner
	select {
	case first = <-runners:
	case <-time.After(3 * time.Second):
		t.Fatal("not started")
	}
	<-first.started
	s.mu.Lock()
	old := s.lease
	s.config.ID++
	s.mu.Unlock()
	awaitGateway(t, func() bool { return old.released.Load() })
	select {
	case <-first.stopped:
	case <-time.After(time.Second):
		t.Fatal("old receiver remains")
	}
	var second *fakeGatewayRunner
	select {
	case second = <-runners:
	case <-time.After(3 * time.Second):
		t.Fatal("new config not connected")
	}
	<-second.started
	s.mu.Lock()
	s.config.ReceiveMode = ReceiveModeWebhook
	s.mu.Unlock()
	awaitGateway(t, func() bool { return g.Status().State == "webhook" })
	select {
	case <-second.stopped:
	default:
		t.Fatal("mode change did not stop")
	}
	cancel()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("stop blocked")
	}
}
func TestGatewaySupervisorWaitsAndLosesLease(t *testing.T) {
	s := &supervisorStore{}
	g := NewGatewaySupervisor(s, NewQQClient(), nil)
	g.pollInterval = 5 * time.Millisecond
	runner := &fakeGatewayRunner{make(chan struct{}), make(chan struct{})}
	g.factory = func(*RuntimeConfig, func(context.Context, []byte) error) gatewayRunner { return runner }
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan struct{})
	go func() { g.Run(ctx); close(done) }()
	awaitGateway(t, func() bool { return g.Status().State == "waiting_config" })
	s.mu.Lock()
	s.config = qqConfig()
	s.config.ReceiveMode = ReceiveModeWebSocket
	s.busy = true
	s.mu.Unlock()
	awaitGateway(t, func() bool { return g.Status().State == "standby" })
	s.mu.Lock()
	s.busy = false
	s.mu.Unlock()
	select {
	case <-runner.started:
	case <-time.After(3 * time.Second):
		t.Fatal("not started")
	}
	s.mu.Lock()
	lease := s.lease
	lease.lost.Store(true)
	s.busy = true
	s.mu.Unlock()
	awaitGateway(t, func() bool { return lease.released.Load() })
	select {
	case <-runner.stopped:
	default:
		t.Fatal("lost lease kept receiver")
	}
	cancel()
	<-done
}
func TestGatewayLeaseExclusiveAndRelease(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	a, e := s.AcquireGatewayLease(ctx)
	if e != nil {
		t.Fatal(e)
	}
	defer a.Release()
	if _, e = s.AcquireGatewayLease(ctx); !errors.Is(e, ErrConflict) {
		t.Fatalf("second receiver admitted: %v", e)
	}
	if e = a.Check(ctx); e != nil {
		t.Fatal(e)
	}
	if e = a.Guard(ctx); e != nil {
		t.Fatal(e)
	}
	a.Release()
	if e = a.Guard(ctx); e == nil {
		t.Fatal("released owner admitted")
	}
	b, e := s.AcquireGatewayLease(ctx)
	if e != nil {
		t.Fatal(e)
	}
	b.Release()
}
func TestGatewayModeConfigDatabaseRoundTrip(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	c, e := s.GetConfig(ctx)
	if e != nil {
		t.Fatal(e)
	}
	if c.ReceiveMode != ReceiveModeWebhook {
		t.Fatal("missing legacy mode")
	}
	c.ReceiveMode = ReceiveModeWebSocket
	saved, e := s.SaveConfig(ctx, *c, 1)
	if e != nil {
		t.Fatal(e)
	}
	r, e := s.BotRuntimeConfig(ctx)
	if e != nil || r.ReceiveMode != ReceiveModeWebSocket || r.ID != saved.ID {
		t.Fatalf("mode lost: %v", e)
	}
}
