package qqimage

import (
	"context"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestGatewayDiscoveryRejectsLostLeaseWhileWaitingForTransaction(t *testing.T) {
	s, _, _ := discoveryFixture(t)
	ctx := context.Background()
	c, e := s.GetConfig(ctx)
	if e != nil {
		t.Fatal(e)
	}
	c.ReceiveMode = ReceiveModeWebSocket
	c, e = s.SaveConfig(ctx, *c, 1)
	if e != nil {
		t.Fatal(e)
	}
	d, e := s.StartDiscovery(ctx, 1)
	if e != nil {
		t.Fatal(e)
	}
	code := strings.TrimPrefix(d.Command, "@机器人 /识别群 ")
	lease, e := s.AcquireGatewayLease(ctx)
	if e != nil {
		t.Fatal(e)
	}
	defer lease.Release()
	blocker, e := s.db.BeginTx(ctx, nil)
	if e != nil {
		t.Fatal(e)
	}
	defer blocker.Rollback()
	if _, e = blocker.Exec(`SELECT pg_advisory_xact_lock(716709231248)`); e != nil {
		t.Fatal(e)
	}
	checked := make(chan struct{})
	var checks atomic.Int32
	guarded := withGatewayGuard(ctx, func(ctx context.Context) error {
		err := lease.Guard(ctx)
		if checks.Add(1) == 1 {
			close(checked)
		}
		return err
	})
	done := make(chan error, 1)
	go func() {
		done <- NewQQCommandHandler(s, &commandSender{}).HandleGatewayEvent(guarded, c.ID, c.AppID, commandPayload("/识别群 "+code))
	}()
	select {
	case <-checked:
	case <-time.After(3 * time.Second):
		t.Fatal("initial ownership check missing")
	}
	lease.Release()
	if e = blocker.Commit(); e != nil {
		t.Fatal(e)
	}
	select {
	case e = <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("discovery did not finish")
	}
	if !errors.Is(e, ErrConflict) {
		t.Fatalf("stale receiver mutated discovery: %v", e)
	}
	current, e := s.GetDiscovery(ctx)
	if e != nil || current.Status != "waiting" || current.Candidate != nil {
		t.Fatalf("lost lease wrote candidate: %v %+v", e, current)
	}
}
