package qqimage

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"testing"
	"time"
)

type inlineWorkerStore struct {
	*workerMemoryStore
	artifact              *Artifact
	readErr, grantErr     error
	reads                 int
	reason                string
	contexts              []context.Context
	grantedID, grantLease string
	readToken             string
	cancelRead            context.CancelFunc
}

func (s *inlineWorkerStore) DeliveryConfig(ctx context.Context, id int64) (*RuntimeConfig, error) {
	s.contexts = append(s.contexts, ctx)
	return s.workerMemoryStore.DeliveryConfig(ctx, id)
}
func (s *inlineWorkerStore) CreateMediaGrant(ctx context.Context, id, lease string) (string, error) {
	s.contexts = append(s.contexts, ctx)
	s.grantedID, s.grantLease = id, lease
	if s.grantErr != nil {
		return "", s.grantErr
	}
	return s.workerMemoryStore.CreateMediaGrant(ctx, id, lease)
}
func (s *inlineWorkerStore) ReadMedia(ctx context.Context, token string) (*Artifact, error) {
	s.contexts = append(s.contexts, ctx)
	s.reads++
	s.readToken = token
	if s.cancelRead != nil {
		s.cancelRead()
	}
	return s.artifact, s.readErr
}
func (s *inlineWorkerStore) CompleteDelivery(ctx context.Context, id, lease, status, reason string) error {
	s.reason = reason
	return s.workerMemoryStore.CompleteDelivery(ctx, id, lease, status, reason)
}

func inlineWorkerFixture(t *testing.T) *inlineWorkerStore {
	t.Helper()
	a, err := validateArtifact(fixturePNG(t))
	if err != nil {
		t.Fatal(err)
	}
	s := &inlineWorkerStore{workerMemoryStore: workerFixture(), artifact: &a}
	s.order.Status = "succeeded"
	s.order.FundsStatus = "captured"
	s.delivery = qqDelivery()
	s.delivery.Order.ConfigID = 42
	s.delivery.LeaseToken = "delivery-lease"
	s.delivery.ArtifactBytes = int64(len(a.Data))
	s.delivery.ArtifactMIME = a.MIME
	return s
}

func TestWorkerInlineUploadUsesAuthorizedOriginalOnce(t *testing.T) {
	s := inlineWorkerFixture(t)
	original := append([]byte(nil), s.artifact.Data...)
	calls, uploads, messages := 0, 0, 0
	c := qqClientServer(t, func(w http.ResponseWriter, r *http.Request) {
		calls++
		switch r.URL.Path {
		case "/app/getAppAccessToken":
			_, _ = io.WriteString(w, `{"access_token":"token","expires_in":7200}`)
		case "/v2/groups/group-1/files":
			uploads++
			var body map[string]any
			_ = json.NewDecoder(r.Body).Decode(&body)
			encoded, _ := body["file_data"].(string)
			data, err := base64.StdEncoding.DecodeString(encoded)
			_, hasURL := body["url"]
			if err != nil || !bytes.Equal(data, original) || body["file_name"] != "image.png" || body["file_type"] != float64(1) || body["srv_send_msg"] != false || hasURL {
				t.Error("upload did not contain only the authorized original bytes and a fixed filename")
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			_, _ = io.WriteString(w, `{"file_info":"media","ttl":60}`)
		case "/v2/groups/group-1/messages":
			messages++
			_, _ = io.WriteString(w, `{"id":"receipt"}`)
		}
	})
	generator := &workerGenerator{}
	worker := NewWorker(s, generator, c)
	for i := 0; i < 2; i++ {
		if err := worker.RunOnce(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	if s.reads != 1 || s.media != 1 || s.readToken != "opaque" || s.grantedID != "delivery1" || s.grantLease != "delivery-lease" || calls != 3 || uploads != 1 || messages != 1 || s.lastStatus != "sent" || generator.calls != 0 || s.completed != 0 || s.order.FundsStatus != "captured" || !bytes.Equal(s.artifact.Data, original) {
		t.Fatalf("inline delivery changed authorization/business or repeated: reads=%d grants=%d calls=%d uploads=%d messages=%d status=%s", s.reads, s.media, calls, uploads, messages, s.lastStatus)
	}
	for _, ctx := range s.contexts {
		deadline, ok := ctx.Deadline()
		if !ok || time.Until(deadline) > 35*time.Second {
			t.Fatal("configuration/grant/read escaped the shared delivery deadline")
		}
	}
}

func TestWorkerInlineReadFailureCannotSendOrFallBack(t *testing.T) {
	for _, kind := range []string{"read_error", "nil_artifact", "empty_artifact", "size_mismatch", "mime_mismatch", "unknown_claim_mime", "cancelled", "grant_denied"} {
		t.Run(kind, func(t *testing.T) {
			s := inlineWorkerFixture(t)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			switch kind {
			case "read_error":
				s.readErr = errors.New("private database failure")
			case "nil_artifact":
				s.artifact = nil
			case "empty_artifact":
				s.artifact = &Artifact{MIME: "image/png"}
			case "size_mismatch":
				s.delivery.ArtifactBytes++
			case "mime_mismatch":
				s.artifact.MIME = "image/jpeg"
			case "unknown_claim_mime":
				s.delivery.ArtifactMIME = "image/gif"
			case "cancelled":
				s.cancelRead = cancel
			case "grant_denied":
				s.grantErr = ErrForbidden
			}
			sender := &workerSender{status: "sent"}
			generator := &workerGenerator{}
			if err := NewWorker(s, generator, sender).RunOnce(ctx); err != nil {
				t.Fatal(err)
			}
			if sender.calls != 0 || generator.calls != 0 || s.lastStatus != "site_only" || s.order.FundsStatus != "captured" || s.completed != 0 {
				t.Fatalf("failed read/authorization caused side effect: sends=%d generates=%d status=%s", sender.calls, generator.calls, s.lastStatus)
			}
			if kind == "grant_denied" && s.reads != 0 {
				t.Fatal("read occurred without a media grant")
			}
		})
	}
}

func TestWorkerInlineURLPathsDoNotReadMedia(t *testing.T) {
	for _, tt := range []struct {
		name, mime string
		bytes      int64
	}{
		{"small_webp", "image/webp", 100},
		{"large_png", "image/png", 3_500_001},
		{"large_jpeg", "image/jpeg", 5 << 20},
	} {
		t.Run(tt.name, func(t *testing.T) {
			s := inlineWorkerFixture(t)
			s.delivery.ArtifactMIME, s.delivery.ArtifactBytes = tt.mime, tt.bytes
			s.readErr = errors.New("URL path must not consume a private read")
			sender := &workerSender{status: "sent"}
			if err := NewWorker(s, &workerGenerator{}, sender).RunOnce(context.Background()); err != nil {
				t.Fatal(err)
			}
			if s.reads != 0 || s.media != 1 || sender.calls != 1 || s.lastStatus != "sent" || sender.url != "https://site.example.com/api/v1/qq-image/media?token=opaque" {
				t.Fatalf("URL path consumed private read or altered original grant: reads=%d grants=%d sends=%d status=%s", s.reads, s.media, sender.calls, s.lastStatus)
			}
		})
	}
}
