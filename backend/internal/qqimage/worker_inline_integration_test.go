package qqimage

import (
	"bytes"
	"context"
	"errors"
	"net/url"
	"testing"
	"time"
)

type inlineAuthorizationStore struct {
	*Store
	beforeGrant func(context.Context, string) error
	beforeRead  func(context.Context) error
	reads       int
}

func (s *inlineAuthorizationStore) CreateMediaGrant(ctx context.Context, id, lease string) (string, error) {
	if s.beforeGrant != nil {
		if err := s.beforeGrant(ctx, id); err != nil {
			return "", err
		}
	}
	return s.Store.CreateMediaGrant(ctx, id, lease)
}
func (s *inlineAuthorizationStore) ReadMedia(ctx context.Context, token string) (*Artifact, error) {
	s.reads++
	if s.beforeRead != nil {
		if err := s.beforeRead(ctx); err != nil {
			return nil, err
		}
	}
	return s.Store.ReadMedia(ctx, token)
}

type inlineArtifactSender struct {
	data  []byte
	calls int
	valid bool
}

func (s *inlineArtifactSender) SendDelivery(_ context.Context, _ *RuntimeConfig, d *Delivery, _ string) QQSendResult {
	s.calls++
	s.valid = d.Artifact != nil && bytes.Equal(d.Artifact.Data, s.data) && d.Artifact.MIME == "image/png"
	return QQSendResult{Status: "sent"}
}

func TestWorkerInlinePostgresRechecksAuthorizationAndPreservesFunds(t *testing.T) {
	for _, kind := range []string{"success", "grant_revoked", "media_expired", "artifact_expired", "owner_disabled", "lease_expired"} {
		t.Run(kind, func(t *testing.T) {
			store, user, order := workerDBOrder(t)
			ctx := context.Background()
			claimed, err := store.ClaimOrder(ctx, "inline-fixture", time.Minute)
			if err != nil || claimed == nil {
				t.Fatalf("claim failed: %v", err)
			}
			artifact, err := validateArtifact(fixturePNG(t))
			if err != nil {
				t.Fatal(err)
			}
			if _, err = store.CompleteGeneration(ctx, order.ID, claimed.LeaseToken, artifact); err != nil {
				t.Fatal(err)
			}
			s := &inlineAuthorizationStore{Store: store}
			switch kind {
			case "grant_revoked":
				s.beforeRead = func(ctx context.Context) error { return store.RevokeGrant(ctx, order.GrantID, user, "inline-test") }
			case "media_expired":
				s.beforeRead = func(ctx context.Context) error {
					_, err := store.db.ExecContext(ctx, `UPDATE qq_image_media_grants SET expires_at=clock_timestamp()-interval '1 second' WHERE order_id=$1`, order.ID)
					return err
				}
			case "artifact_expired":
				s.beforeRead = func(ctx context.Context) error {
					_, err := store.db.ExecContext(ctx, `UPDATE qq_image_orders SET artifact_expires_at=clock_timestamp()-interval '1 second' WHERE id=$1`, order.ID)
					return err
				}
			case "owner_disabled":
				s.beforeRead = func(ctx context.Context) error {
					_, err := store.db.ExecContext(ctx, `UPDATE users SET status='disabled' WHERE id=$1`, user)
					return err
				}
			case "lease_expired":
				s.beforeGrant = func(ctx context.Context, id string) error {
					_, err := store.db.ExecContext(ctx, `UPDATE qq_image_deliveries SET lease_until=clock_timestamp()-interval '1 second' WHERE id=$1`, id)
					return err
				}
			}
			generator := &workerGenerator{}
			sender := &inlineArtifactSender{data: artifact.Data}
			if err = NewWorker(s, generator, sender).RunOnce(ctx); err != nil {
				t.Fatal(err)
			}
			got, err := store.GetOrder(ctx, order.ID, user)
			if err != nil {
				t.Fatal(err)
			}
			wantSends, wantFetches, wantStatus := 0, 0, "site_only"
			if kind == "success" {
				wantSends, wantFetches, wantStatus = 1, 1, "sent"
			}
			var fetches, ledgerEntries int
			var saved []byte
			if err = store.db.QueryRowContext(ctx, `SELECT COALESCE(sum(fetches),0) FROM qq_image_media_grants WHERE order_id=$1`, order.ID).Scan(&fetches); err != nil {
				t.Fatal(err)
			}
			if err = store.db.QueryRowContext(ctx, `SELECT count(*) FROM qq_image_ledger WHERE order_id=$1`, order.ID).Scan(&ledgerEntries); err != nil {
				t.Fatal(err)
			}
			if err = store.db.QueryRowContext(ctx, `SELECT artifact FROM qq_image_orders WHERE id=$1`, order.ID).Scan(&saved); err != nil {
				t.Fatal(err)
			}
			if sender.calls != wantSends || fetches != wantFetches || got.DeliveryStatus != wantStatus || got.FundsStatus != "captured" || got.Status != "succeeded" || generator.calls != 0 || ledgerEntries != 2 || !bytes.Equal(saved, artifact.Data) || (wantSends == 1 && !sender.valid) {
				t.Fatalf("authorization/financial invariant changed: sends=%d fetches=%d status=%s funds=%s ledger=%d", sender.calls, fetches, got.DeliveryStatus, got.FundsStatus, ledgerEntries)
			}
			if kind == "lease_expired" && s.reads != 0 {
				t.Fatal("expired delivery lease allowed a private read")
			}
			if next, err := store.ClaimDelivery(ctx, "another-worker", time.Minute); err != nil || next != nil {
				t.Fatalf("completed/failed attempt queued again: %v", err)
			}
			checkWallet(t, store, user, "1.00000000", "0.00000000")
		})
	}
}

func TestWorkerInlinePostgresWebPPreservesURLFetchBudget(t *testing.T) {
	store, user, order := workerDBOrder(t)
	ctx := context.Background()
	claimed, err := store.ClaimOrder(ctx, "webp-fixture", time.Minute)
	if err != nil || claimed == nil {
		t.Fatalf("claim failed: %v", err)
	}
	// This is an opaque storage fixture; the generation validator is tested separately.
	artifact := Artifact{Data: []byte("stored-webp-original"), MIME: "image/webp", Width: 1, Height: 1}
	if _, err = store.CompleteGeneration(ctx, order.ID, claimed.LeaseToken, artifact); err != nil {
		t.Fatal(err)
	}
	s := &inlineAuthorizationStore{Store: store}
	sender := &workerSender{status: "sent"}
	generator := &workerGenerator{}
	if err = NewWorker(s, generator, sender).RunOnce(ctx); err != nil {
		t.Fatal(err)
	}
	var grants, fetches int
	if err = store.db.QueryRowContext(ctx, `SELECT count(*),COALESCE(sum(fetches),0) FROM qq_image_media_grants WHERE order_id=$1`, order.ID).Scan(&grants, &fetches); err != nil {
		t.Fatal(err)
	}
	if s.reads != 0 || grants != 1 || fetches != 0 || sender.calls != 1 || generator.calls != 0 {
		t.Fatalf("URL delivery consumed or replaced its fetch budget: reads=%d grants=%d fetches=%d sends=%d", s.reads, grants, fetches, sender.calls)
	}
	media, err := url.Parse(sender.url)
	if err != nil {
		t.Fatal(err)
	}
	for range 3 {
		got, err := store.ReadMedia(ctx, media.Query().Get("token"))
		if err != nil || !bytes.Equal(got.Data, artifact.Data) {
			t.Fatalf("original three public reads not preserved: %v", err)
		}
	}
	if _, err = store.ReadMedia(ctx, media.Query().Get("token")); !errors.Is(err, ErrExpired) {
		t.Fatal("public fetch bound was expanded")
	}
	checkWallet(t, store, user, "1.00000000", "0.00000000")
}
