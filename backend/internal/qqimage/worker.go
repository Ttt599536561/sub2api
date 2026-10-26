package qqimage

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"log/slog"
	"net/url"
	"sync"
	"time"
)

// WorkerStore claims and state changes are transactional. In particular,
// ClaimOrder returns only queued orders and persists dispatching before return;
// an expired dispatching lease becomes reconciling, never queued again.
type WorkerStore interface {
	ClaimOrder(context.Context, string, time.Duration) (*Order, error)
	RuntimeConfig(context.Context, int64) (*RuntimeConfig, error)
	DeliveryConfig(context.Context, int64) (*RuntimeConfig, error)
	CompleteGeneration(context.Context, string, string, Artifact) (*Order, error)
	FailGeneration(context.Context, string, string, string) (*Order, error)
	MarkReconciling(context.Context, string, string, string) error
	RecoverExpired(context.Context) error
	ClaimDelivery(context.Context, string, time.Duration) (*Delivery, error)
	CompleteDelivery(context.Context, string, string, string, string) error
	CreateMediaGrant(context.Context, string, string) (string, error)
	ReadMedia(context.Context, string) (*Artifact, error)
}
type Worker struct {
	store     WorkerStore
	generator ImageGenerator
	sender    QQSender
	owner     string
	mu        sync.Mutex
	started   bool
	cancel    context.CancelFunc
	done      chan struct{}
}

func NewWorker(store WorkerStore, generator ImageGenerator, sender QQSender) *Worker {
	entropy := make([]byte, 16)
	_, _ = rand.Read(entropy)
	return &Worker{store: store, generator: generator, sender: sender, owner: hex.EncodeToString(entropy)}
}
func (w *Worker) Start() {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.started {
		return
	}
	w.started = true
	ctx, cancel := context.WithCancel(context.Background())
	w.cancel = cancel
	w.done = make(chan struct{})
	go func() {
		defer close(w.done)
		var running sync.WaitGroup
		// Database admission controls the configured group/global limits across
		// instances. This local bound also caps concurrent HTTP and image decoding.
		for i := 0; i < 5; i++ {
			running.Add(1)
			go func() {
				defer running.Done()
				ticker := time.NewTicker(time.Second)
				defer ticker.Stop()
				for {
					if ctx.Err() != nil {
						return
					}
					if err := w.RunOnce(ctx); err != nil && ctx.Err() == nil {
						slog.Warn("QQ image worker iteration failed; persisted state will be recovered")
					}
					select {
					case <-ctx.Done():
						return
					case <-ticker.C:
					}
				}
			}()
		}
		running.Wait()
	}()
}
func (w *Worker) Stop(ctx context.Context) error {
	w.mu.Lock()
	if !w.started {
		w.mu.Unlock()
		return nil
	}
	cancel, done := w.cancel, w.done
	w.mu.Unlock()
	cancel()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// RunOnce is also useful for bounded recovery operations. It never attempts a
// second generation POST for dispatching/reconciling orders.
func (w *Worker) RunOnce(ctx context.Context) error {
	if w.store == nil || w.generator == nil {
		return errors.New("qq_worker_dependencies_unavailable")
	}
	if err := w.store.RecoverExpired(ctx); err != nil {
		return err
	}
	order, err := w.store.ClaimOrder(ctx, w.owner, 150*time.Second)
	if err != nil {
		return err
	}
	if order != nil {
		if err = w.generate(ctx, order); err != nil {
			return err
		}
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	delivery, err := w.store.ClaimDelivery(ctx, w.owner, 60*time.Second)
	if err != nil {
		return err
	}
	if delivery == nil {
		return nil
	}
	return w.deliver(ctx, delivery)
}
func recoveryContext(ctx context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
}
func (w *Worker) generate(ctx context.Context, order *Order) error {
	config, err := w.store.RuntimeConfig(ctx, order.ConfigID)
	if err != nil || config == nil {
		finish, cancel := recoveryContext(ctx)
		defer cancel()
		return w.store.MarkReconciling(finish, order.ID, order.LeaseToken, "generation_configuration_unavailable")
	}
	deadline := time.Now().Add(120 * time.Second)
	if order.Deadline.Before(deadline) {
		deadline = order.Deadline
	}
	request, cancel := context.WithDeadline(ctx, deadline)
	artifact, generateErr := w.generator.Generate(request, GenerationRequest{APIURL: config.APIURL, APIKey: config.APIKey, Model: order.Model, Size: order.Size, Prompt: order.Prompt, ImageHosts: config.ImageHosts})
	cancel()
	finish, finishCancel := recoveryContext(ctx)
	defer finishCancel()
	if generateErr != nil {
		var classified *generationError
		if errors.As(generateErr, &classified) && !classified.uncertain {
			_, err = w.store.FailGeneration(finish, order.ID, order.LeaseToken, classified.reason)
			return err
		}
		reason := "generation_result_unknown"
		if classified != nil {
			reason = classified.reason
		}
		return w.store.MarkReconciling(finish, order.ID, order.LeaseToken, reason)
	}
	// The storage/capture boundary revalidates bytes even if an implementation of
	// ImageGenerator supplied a misleading MIME, digest or dimension field.
	validated, err := validateArtifact(artifact.Data)
	if err != nil {
		return w.store.MarkReconciling(finish, order.ID, order.LeaseToken, "invalid_image_data")
	}
	_, err = w.store.CompleteGeneration(finish, order.ID, order.LeaseToken, validated)
	return err
}
func (w *Worker) deliver(ctx context.Context, delivery *Delivery) error {
	// Configuration, authorization, private reading and sending share one budget,
	// leaving time to persist a result before the 60-second delivery lease expires.
	request, requestCancel := context.WithTimeout(ctx, 35*time.Second)
	result := w.sendDelivery(request, delivery)
	requestCancel()
	if result.Status != "sent" && result.Status != "unknown" && result.Status != "site_only" {
		result = QQSendResult{Status: "site_only", Reason: "qq_delivery_failed"}
	}
	finish, cancel := recoveryContext(ctx)
	defer cancel()
	return w.store.CompleteDelivery(finish, delivery.ID, delivery.LeaseToken, result.Status, result.Reason)
}

func (w *Worker) sendDelivery(ctx context.Context, delivery *Delivery) QQSendResult {
	fallback := func(reason string) QQSendResult { return QQSendResult{Status: "site_only", Reason: reason} }
	if delivery.Order == nil || w.sender == nil {
		return fallback("qq_delivery_unavailable")
	}
	config, err := w.store.DeliveryConfig(ctx, delivery.Order.ConfigID)
	if err != nil || config == nil {
		return fallback("qq_credentials_unavailable")
	}
	base, err := safeHTTPSURL(config.PublicBaseURL, "")
	if err != nil {
		return fallback("qq_media_origin_invalid")
	}
	token, err := w.store.CreateMediaGrant(ctx, delivery.ID, delivery.LeaseToken)
	if err != nil {
		return fallback("qq_media_authorization_unavailable")
	}
	if delivery.ArtifactBytes <= 0 {
		return fallback("qq_media_read_unavailable")
	}
	inline := false
	switch delivery.ArtifactMIME {
	case "image/png", "image/jpeg":
		inline = delivery.ArtifactBytes <= maxQQInlineArtifactBytes
	case "image/webp": // Leave all three public fetches available to QQ.
	default:
		return fallback("qq_media_type_unsupported")
	}
	attempt := *delivery
	attempt.Artifact = nil
	if inline {
		// Reuse the same grant and read checks used by the public media endpoint.
		// A failed private read never falls through to a different upload path.
		artifact, readErr := w.store.ReadMedia(ctx, token)
		if readErr != nil || artifact == nil || len(artifact.Data) == 0 || int64(len(artifact.Data)) != delivery.ArtifactBytes || artifact.MIME != delivery.ArtifactMIME {
			return fallback("qq_media_read_unavailable")
		}
		attempt.Artifact = artifact
	}
	if ctx.Err() != nil {
		return fallback("qq_delivery_cancelled")
	}
	base.Path = "/api/v1/qq-image/media"
	base.RawPath = ""
	base.RawQuery = url.Values{"token": []string{token}}.Encode()
	return w.sender.SendDelivery(ctx, config, &attempt, base.String())
}
