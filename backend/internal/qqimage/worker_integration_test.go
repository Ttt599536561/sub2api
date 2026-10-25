package qqimage

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func workerDBOrder(t *testing.T) (*Store, int64, *Order) {
	t.Helper()
	s := testStore(t)
	user := fixtureUser(t, s, "worker@example.com", "2")
	identity := fixtureIdentity("worker", "group")
	fixtureGrant(t, s, user, identity)
	order, err := s.CreateOrderFromEvent(context.Background(), identity, "event-worker", "msg-worker", time.Now(), "a tree")
	if err != nil {
		t.Fatal(err)
	}
	return s, user, order
}
func TestWorkerPostgresCaptureAndFallbackPersistAcrossRestart(t *testing.T) {
	s, user, order := workerDBOrder(t)
	var posts atomic.Int32
	data := fixturePNG(t)
	provider := providerServer(t, func(w http.ResponseWriter, r *http.Request) {
		posts.Add(1)
		_ = json.NewEncoder(w).Encode(map[string]any{"data": []any{map[string]string{"b64_json": base64.StdEncoding.EncodeToString(data)}}})
	})
	sender := &workerSender{status: "site_only"}
	if err := NewWorker(s, provider, sender).RunOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := NewWorker(s, provider, sender).RunOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	got, err := s.GetOrderForIdentity(context.Background(), order.Identity, order.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != "succeeded" || got.FundsStatus != "captured" || got.WebsiteStatus != "available" || got.DeliveryStatus != "site_only" || posts.Load() != 1 || sender.calls != 1 {
		t.Fatalf("unexpected persisted outcome: %+v posts=%d sends=%d", got, posts.Load(), sender.calls)
	}
	checkWallet(t, s, user, "1.00000000", "0.00000000")
	artifact, err := s.GetArtifact(context.Background(), order.ID, user)
	if err != nil || len(artifact.Data) == 0 {
		t.Fatalf("captured order not privately retrievable: %v", err)
	}
}
func TestWorkerPostgresUnknownPOSTOnlyOnceUntilRelease(t *testing.T) {
	s, user, order := workerDBOrder(t)
	var posts atomic.Int32
	provider := providerServer(t, func(w http.ResponseWriter, r *http.Request) { posts.Add(1); w.WriteHeader(503) })
	for n := 0; n < 3; n++ {
		if err := NewWorker(s, provider, nil).RunOnce(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	got, err := s.GetOrderForIdentity(context.Background(), order.Identity, order.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != "reconciling" || got.FundsStatus != "held" || posts.Load() != 1 {
		t.Fatalf("unknown outcome repeated: %+v posts=%d", got, posts.Load())
	}
	checkWallet(t, s, user, "1.00000000", "1.00000000")
	if _, err = s.db.Exec(`UPDATE qq_image_orders SET deadline=clock_timestamp()-interval '1 second' WHERE id=$1`, order.ID); err != nil {
		t.Fatal(err)
	}
	if err = NewWorker(s, provider, nil).RunOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	checkWallet(t, s, user, "2.00000000", "0.00000000")
	if posts.Load() != 1 {
		t.Fatal("deadline recovery generated again")
	}
}
func TestWorkerPostgresCrashedDispatchIsNeverReposted(t *testing.T) {
	s, user, order := workerDBOrder(t)
	if _, err := s.ClaimOrder(context.Background(), "dead-worker", time.Minute); err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.Exec(`UPDATE qq_image_orders SET lease_until=clock_timestamp()-interval '1 second' WHERE id=$1`, order.ID); err != nil {
		t.Fatal(err)
	}
	generator := &workerGenerator{}
	if err := NewWorker(s, generator, nil).RunOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	got, err := s.GetOrderForIdentity(context.Background(), order.Identity, order.ID)
	if err != nil || got.Status != "reconciling" || generator.calls != 0 {
		t.Fatalf("crashed dispatch reposted: %+v err=%v", got, err)
	}
	if _, err = s.db.Exec(`UPDATE qq_image_orders SET deadline=clock_timestamp()-interval '1 second' WHERE id=$1`, order.ID); err != nil {
		t.Fatal(err)
	}
	if err = NewWorker(s, generator, nil).RunOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	checkWallet(t, s, user, "2.00000000", "0.00000000")
}
func TestWorkerPostgresKeepsOriginalCredentialsAndModel(t *testing.T) {
	s, _, order := workerDBOrder(t)
	config, err := s.GetConfig(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	config.APIKey = "rotated-key"
	config.Model = "rotated-model"
	if _, err = s.SaveConfig(context.Background(), *config, 1); err != nil {
		t.Fatal(err)
	}
	data := fixturePNG(t)
	provider := providerServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer upstream-secret" {
			t.Error("queued order used rotated credentials")
		}
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		if body["model"] != "image-model" {
			t.Error("queued order used rotated model")
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"data": []any{map[string]string{"b64_json": base64.StdEncoding.EncodeToString(data)}}})
	})
	if err = NewWorker(s, provider, nil).RunOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	got, err := s.GetOrderForIdentity(context.Background(), order.Identity, order.ID)
	if err != nil || got.FundsStatus != "captured" {
		t.Fatalf("snapshot order failed: %+v %v", got, err)
	}
}

type concurrentQQSender struct{ calls atomic.Int32 }

func (s *concurrentQQSender) SendDelivery(context.Context, *RuntimeConfig, *Delivery, string) QQSendResult {
	s.calls.Add(1)
	return QQSendResult{Status: "unknown"}
}
func TestWorkerPostgresTwoWorkersGenerateAndDeliverOnce(t *testing.T) {
	s, user, order := workerDBOrder(t)
	var posts atomic.Int32
	data := fixturePNG(t)
	provider := providerServer(t, func(w http.ResponseWriter, r *http.Request) {
		posts.Add(1)
		_ = json.NewEncoder(w).Encode(map[string]any{"data": []any{map[string]string{"b64_json": base64.StdEncoding.EncodeToString(data)}}})
	})
	sender := &concurrentQQSender{}
	var group sync.WaitGroup
	errs := make(chan error, 2)
	for n := 0; n < 2; n++ {
		group.Add(1)
		go func() { defer group.Done(); errs <- NewWorker(s, provider, sender).RunOnce(context.Background()) }()
	}
	group.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	if posts.Load() != 1 || sender.calls.Load() != 1 {
		t.Fatalf("duplicate outbound generation=%d send=%d", posts.Load(), sender.calls.Load())
	}
	checkWallet(t, s, user, "1.00000000", "0.00000000")
	got, err := s.GetOrderForIdentity(context.Background(), order.Identity, order.ID)
	if err != nil || got.DeliveryStatus != "unknown" {
		t.Fatalf("unknown delivery not durable %+v %v", got, err)
	}
	if err = NewWorker(s, provider, sender).RunOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	if posts.Load() != 1 || sender.calls.Load() != 1 {
		t.Fatal("restart blindly retried unknown delivery")
	}
}
