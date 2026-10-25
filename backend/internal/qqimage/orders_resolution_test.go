package qqimage

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestOrderResolveFailureReleaseReplayAndLateResult(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	u := fixtureUser(t, s, "resolution@example.com", "2")
	i := fixtureIdentity("one", "group")
	fixtureGrant(t, s, u, i)
	o, err := s.CreateOrder(ctx, i, "event", "cat")
	if err != nil {
		t.Fatal(err)
	}
	claim, err := s.ClaimOrder(ctx, "worker", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.MarkReconciling(ctx, o.ID, claim.LeaseToken, "provider_result_unknown"); err != nil {
		t.Fatal(err)
	}
	const reason = "operator confirmed failure from provider evidence"
	for range 2 {
		resolved, err := s.ResolveOrderFailure(ctx, o.ID, 99, reason)
		if err != nil {
			t.Fatal(err)
		}
		if resolved.Status != "failed" || resolved.FundsStatus != "released" || resolved.Error != reason {
			t.Fatalf("unexpected resolution: %+v", resolved)
		}
	}
	checkWallet(t, s, u, "2.00000000", "0.00000000")
	var ledger, audits int
	if err = s.db.QueryRow(`SELECT count(*) FROM qq_image_ledger WHERE order_id=$1 AND operation='release'`, o.ID).Scan(&ledger); err != nil {
		t.Fatal(err)
	}
	if err = s.db.QueryRow(`SELECT count(*) FROM qq_image_audit WHERE object_id=$1 AND action='order.resolve_failure' AND actor_id=99 AND reason=$2`, o.ID, reason).Scan(&audits); err != nil {
		t.Fatal(err)
	}
	if ledger != 1 || audits != 1 {
		t.Fatalf("duplicate or missing durable effects: ledger=%d audit=%d", ledger, audits)
	}
	if _, err = s.ResolveOrderFailure(ctx, o.ID, 99, "different evidence"); !errors.Is(err, ErrConflict) {
		t.Fatalf("different reason rewrote resolution: %v", err)
	}
	late, err := s.CompleteGeneration(ctx, o.ID, claim.LeaseToken, Artifact{Data: []byte("image"), MIME: "image/png", Width: 1, Height: 1})
	if err != nil {
		t.Fatal(err)
	}
	if late.FundsStatus != "released" || late.WebsiteStatus != "available" {
		t.Fatalf("late result charged or unavailable: %+v", late)
	}
	checkWallet(t, s, u, "2.00000000", "0.00000000")
}

func TestOrderResolveFailureRejectsUnsafeStatesAndReasons(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	u := fixtureUser(t, s, "resolution@example.com", "2")
	i := fixtureIdentity("one", "group")
	fixtureGrant(t, s, u, i)
	o, err := s.CreateOrder(ctx, i, "event", "cat")
	if err != nil {
		t.Fatal(err)
	}
	for _, reason := range []string{"", " \t\n", strings.Repeat("x", 501)} {
		if _, err = s.ResolveOrderFailure(ctx, o.ID, 99, reason); !errors.Is(err, ErrInvalid) {
			t.Fatalf("invalid reason accepted: %v", err)
		}
	}
	if _, err = s.ResolveOrderFailure(ctx, o.ID, 0, "evidence"); !errors.Is(err, ErrInvalid) {
		t.Fatal("missing administrator actor accepted")
	}
	if _, err = s.ResolveOrderFailure(ctx, o.ID, 99, "evidence"); !errors.Is(err, ErrConflict) {
		t.Fatal("queued order must use cancel")
	}
	claim, err := s.ClaimOrder(ctx, "worker", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.ResolveOrderFailure(ctx, o.ID, 99, "evidence"); !errors.Is(err, ErrConflict) {
		t.Fatal("dispatching order resolved without reconciliation")
	}
	if _, err = s.CompleteGeneration(ctx, o.ID, claim.LeaseToken, Artifact{Data: []byte("image"), MIME: "image/png", Width: 1, Height: 1}); err != nil {
		t.Fatal(err)
	}
	if _, err = s.ResolveOrderFailure(ctx, o.ID, 99, "evidence"); !errors.Is(err, ErrConflict) {
		t.Fatal("captured order was released")
	}
	if _, err = s.RefundOrder(ctx, o.ID, 99, "compensation"); err != nil {
		t.Fatal(err)
	}
	if _, err = s.ResolveOrderFailure(ctx, o.ID, 99, "evidence"); !errors.Is(err, ErrConflict) {
		t.Fatal("refunded order was released")
	}
	checkWallet(t, s, u, "2.00000000", "0.00000000")
}

func TestOrderResolveFailureCaptureRaceHasSingleTerminal(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	u := fixtureUser(t, s, "resolution@example.com", "2")
	i := fixtureIdentity("one", "group")
	fixtureGrant(t, s, u, i)
	o, err := s.CreateOrder(ctx, i, "event", "cat")
	if err != nil {
		t.Fatal(err)
	}
	claim, err := s.ClaimOrder(ctx, "worker", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.MarkReconciling(ctx, o.ID, claim.LeaseToken, "unknown"); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	var resolutionErr, captureErr error
	wg.Add(2)
	go func() { defer wg.Done(); _, resolutionErr = s.ResolveOrderFailure(ctx, o.ID, 99, "confirmed failure") }()
	go func() {
		defer wg.Done()
		_, captureErr = s.CompleteGeneration(ctx, o.ID, claim.LeaseToken, Artifact{Data: []byte("image"), MIME: "image/png", Width: 1, Height: 1})
	}()
	wg.Wait()
	if captureErr != nil {
		t.Fatal(captureErr)
	}
	current, err := s.GetOrder(ctx, o.ID, u)
	if err != nil {
		t.Fatal(err)
	}
	switch current.FundsStatus {
	case "released":
		if resolutionErr != nil {
			t.Fatal(resolutionErr)
		}
		checkWallet(t, s, u, "2.00000000", "0.00000000")
	case "captured":
		if !errors.Is(resolutionErr, ErrConflict) {
			t.Fatalf("capture and manual release both succeeded: %v", resolutionErr)
		}
		checkWallet(t, s, u, "1.00000000", "0.00000000")
	default:
		t.Fatalf("invalid terminal funds status: %s", current.FundsStatus)
	}
	var terminal int
	if err = s.db.QueryRow(`SELECT count(*) FROM qq_image_ledger WHERE order_id=$1 AND operation IN ('capture','release')`, o.ID).Scan(&terminal); err != nil {
		t.Fatal(err)
	}
	if terminal != 1 {
		t.Fatalf("terminal ledger count: %d", terminal)
	}
}
