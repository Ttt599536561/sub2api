package qqimage

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

type workerMemoryStore struct {
	mu                                                            sync.Mutex
	order                                                         *Order
	config                                                        *RuntimeConfig
	delivery                                                      *Delivery
	claims, completed, failed, reconciled, recovered, sent, media int
	lastStatus                                                    string
}

func (s *workerMemoryStore) ClaimOrder(ctx context.Context, owner string, lease time.Duration) (*Order, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.claims++
	if s.order == nil || s.order.Status != "queued" {
		return nil, nil
	}
	s.order.Status = "dispatching"
	s.order.LeaseToken = "lease"
	copy := *s.order
	return &copy, nil
}
func (s *workerMemoryStore) RuntimeConfig(ctx context.Context, id int64) (*RuntimeConfig, error) {
	if s.config == nil {
		return nil, errors.New("key unavailable")
	}
	return s.config, nil
}
func (s *workerMemoryStore) DeliveryConfig(ctx context.Context, id int64) (*RuntimeConfig, error) {
	return s.RuntimeConfig(ctx, id)
}
func (s *workerMemoryStore) CompleteGeneration(ctx context.Context, id, token string, a Artifact) (*Order, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.completed++
	s.order.Status = "succeeded"
	s.order.FundsStatus = "captured"
	s.order.Artifact = &a
	return s.order, nil
}
func (s *workerMemoryStore) FailGeneration(ctx context.Context, id, token, reason string) (*Order, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.failed++
	s.order.Status = "failed"
	s.order.FundsStatus = "released"
	return s.order, nil
}
func (s *workerMemoryStore) MarkReconciling(ctx context.Context, id, token, reason string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.reconciled++
	s.order.Status = "reconciling"
	return nil
}
func (s *workerMemoryStore) RecoverExpired(context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.recovered++
	if s.order != nil && time.Now().After(s.order.Deadline) {
		s.order.Status = "failed"
		s.order.FundsStatus = "released"
	}
	return nil
}
func (s *workerMemoryStore) ClaimDelivery(ctx context.Context, owner string, lease time.Duration) (*Delivery, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	d := s.delivery
	s.delivery = nil
	return d, nil
}
func (s *workerMemoryStore) CompleteDelivery(ctx context.Context, id, token, status, reason string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sent++
	s.lastStatus = status
	return nil
}
func (s *workerMemoryStore) CreateMediaGrant(ctx context.Context, id, token string) (string, error) {
	s.media++
	return "opaque", nil
}
func (s *workerMemoryStore) ReadMedia(context.Context, string) (*Artifact, error) {
	return &Artifact{Data: []byte{1}, MIME: "image/png"}, nil
}

type workerGenerator struct {
	calls    int
	artifact Artifact
	err      error
	wait     bool
}

func (g *workerGenerator) Generate(ctx context.Context, _ GenerationRequest) (Artifact, error) {
	g.calls++
	if g.wait {
		<-ctx.Done()
		return Artifact{}, generationFailure("generation_result_unknown", true)
	}
	return g.artifact, g.err
}

type workerSender struct {
	calls  int
	url    string
	status string
}

func (s *workerSender) SendDelivery(ctx context.Context, c *RuntimeConfig, d *Delivery, url string) QQSendResult {
	s.calls++
	s.url = url
	return QQSendResult{Status: s.status}
}
func workerFixture() *workerMemoryStore {
	return &workerMemoryStore{order: &Order{ID: "o1", ConfigID: 42, Status: "queued", FundsStatus: "held", Deadline: time.Now().Add(time.Hour), Model: "fixed", Size: "1024x1024", Prompt: "tree"}, config: qqConfig()}
}
func TestWorkerCaptureOnlyValidatedArtifact(t *testing.T) {
	store := workerFixture()
	data := fixturePNG(t)
	artifact, _ := validateArtifact(data)
	generator := &workerGenerator{artifact: artifact}
	worker := NewWorker(store, generator, &workerSender{})
	if err := worker.RunOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	if generator.calls != 1 || store.completed != 1 || store.failed != 0 || store.order.FundsStatus != "captured" {
		t.Fatalf("invalid successful transition %+v", store)
	}
	if err := worker.RunOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	if generator.calls != 1 {
		t.Fatal("successful order resubmitted")
	}
}
func TestWorkerUnknownNeverPostsAgainAndDeadlineReleases(t *testing.T) {
	store := workerFixture()
	generator := &workerGenerator{err: generationFailure("generation_result_unknown", true)}
	worker := NewWorker(store, generator, &workerSender{})
	for i := 0; i < 3; i++ {
		if err := worker.RunOnce(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	if generator.calls != 1 || store.reconciled != 1 || store.failed != 0 || store.order.FundsStatus != "held" {
		t.Fatalf("uncertain request duplicated/released %+v", store)
	}
	store.order.Deadline = time.Now().Add(-time.Second)
	_ = worker.RunOnce(context.Background())
	if generator.calls != 1 || store.order.FundsStatus != "released" {
		t.Fatal("deadline did not release without resubmission")
	}
}
func TestWorkerExplicitFailureReleases(t *testing.T) {
	store := workerFixture()
	generator := &workerGenerator{err: generationFailure("generation_http_400", false)}
	_ = NewWorker(store, generator, &workerSender{}).RunOnce(context.Background())
	if store.failed != 1 || store.completed != 0 {
		t.Fatal("explicit failure not released")
	}
}
func TestWorkerCorruptArtifactCannotCapture(t *testing.T) {
	store := workerFixture()
	generator := &workerGenerator{artifact: Artifact{Data: []byte("invalid"), MIME: "image/png"}}
	_ = NewWorker(store, generator, &workerSender{}).RunOnce(context.Background())
	if store.completed != 0 || store.reconciled != 1 {
		t.Fatal("invalid artifact captured")
	}
}
func TestWorkerMissingOldCredentialNeverUsesNewConfig(t *testing.T) {
	store := workerFixture()
	store.config = nil
	generator := &workerGenerator{}
	_ = NewWorker(store, generator, &workerSender{}).RunOnce(context.Background())
	if generator.calls != 0 || store.reconciled != 1 {
		t.Fatal("missing immutable config was retried/substituted")
	}
}
func TestWorkerDeliveryUsesExistingArtifactAndPrivateGrant(t *testing.T) {
	store := workerFixture()
	store.order.Status = "succeeded"
	store.delivery = qqDelivery()
	store.delivery.Order.ConfigID = 42
	store.delivery.ArtifactBytes = 1
	store.delivery.ArtifactMIME = "image/png"
	generator := &workerGenerator{}
	sender := &workerSender{status: "unknown"}
	worker := NewWorker(store, generator, sender)
	_ = worker.RunOnce(context.Background())
	_ = worker.RunOnce(context.Background())
	if generator.calls != 0 || sender.calls != 1 || store.media != 1 || store.lastStatus != "unknown" || sender.url != "https://site.example.com/api/v1/qq-image/media?token=opaque" {
		t.Fatalf("delivery incorrectly retried: %+v %+v", store, sender)
	}
}
func TestWorkerStopIsBounded(t *testing.T) {
	store := workerFixture()
	generator := &workerGenerator{wait: true}
	w := NewWorker(store, generator, &workerSender{})
	w.Start()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := w.Stop(ctx); err != nil {
		t.Fatal(err)
	}
	if err := w.Stop(ctx); err != nil {
		t.Fatal(err)
	}
}
