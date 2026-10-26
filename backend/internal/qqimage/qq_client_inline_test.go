package qqimage

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"image"
	"image/jpeg"
	"io"
	"net/http"
	"testing"
)

func TestQQClientInlineUploadFormatAndSizeBoundaries(t *testing.T) {
	var jpg bytes.Buffer
	if err := jpeg.Encode(&jpg, image.NewRGBA(image.Rect(0, 0, 2, 2)), nil); err != nil {
		t.Fatal(err)
	}
	png := fixturePNG(t)
	for _, tt := range []struct {
		name, mime, filename string
		data                 []byte
	}{
		{"png", "image/png", "image.png", png},
		{"jpeg", "image/jpeg", "image.jpg", jpg.Bytes()},
		{"inline_limit", "image/png", "image.png", append(append([]byte(nil), png...), make([]byte, 3_500_000-len(png))...)},
		{"above_inline_limit", "image/png", "", make([]byte, 3_500_001)},
		{"qq_image_limit", "image/jpeg", "", make([]byte, 5<<20)},
		{"webp", "image/webp", "", []byte("stored-webp-original")},
	} {
		t.Run(tt.name, func(t *testing.T) {
			original := append([]byte(nil), tt.data...)
			delivery := qqDelivery()
			delivery.ArtifactBytes = int64(len(tt.data))
			delivery.Artifact = &Artifact{Data: tt.data, MIME: tt.mime}
			calls, uploads := 0, 0
			c := qqClientServer(t, func(w http.ResponseWriter, r *http.Request) {
				calls++
				switch r.URL.Path {
				case "/app/getAppAccessToken":
					_, _ = io.WriteString(w, `{"access_token":"token","expires_in":7200}`)
				case "/v2/groups/group-1/files":
					uploads++
					raw, err := io.ReadAll(r.Body)
					if err != nil || len(raw) >= 5_000_000 {
						t.Error("inline JSON exceeded the separate request-size bound")
					}
					var body map[string]any
					_ = json.Unmarshal(raw, &body)
					encoded, inline := body["file_data"].(string)
					_, hasURL := body["url"]
					_, hasFilename := body["file_name"]
					if body["file_type"] != float64(1) || body["srv_send_msg"] != false {
						t.Error("upload acquired automatic message side effects")
					}
					if tt.filename != "" {
						data, decodeErr := base64.StdEncoding.DecodeString(encoded)
						if !inline || hasURL || body["file_name"] != tt.filename || decodeErr != nil || !bytes.Equal(data, original) {
							t.Error("inline data/filename differed from the stored original")
						}
					} else if inline || hasFilename || !hasURL || body["url"] != "https://site.example.com/api/v1/qq-image/media?token=opaque" {
						t.Error("large/WebP image did not preserve its sole URL upload")
					}
					_, _ = io.WriteString(w, `{"file_info":"media","ttl":60}`)
				case "/v2/groups/group-1/messages":
					_, _ = io.WriteString(w, `{"id":"receipt"}`)
				}
			})
			result := c.SendDelivery(context.Background(), qqConfig(), delivery, "https://site.example.com/api/v1/qq-image/media?token=opaque")
			if result.Status != "sent" || calls != 3 || uploads != 1 || !bytes.Equal(delivery.Artifact.Data, original) {
				t.Fatalf("delivery changed/retried original: result=%+v calls=%d uploads=%d", result, calls, uploads)
			}
		})
	}
}

func TestQQClientInlineInvalidArtifactNeverFallsBack(t *testing.T) {
	for _, tt := range []struct {
		name, mime, reason string
		data               []byte
		claimed            int64
	}{
		{"empty", "image/png", "qq_media_data_invalid", nil, 0},
		{"mismatch", "image/png", "qq_media_data_invalid", []byte("image"), 6},
		{"unknown_mime", "image/gif", "qq_media_type_unsupported", []byte("image"), 5},
		{"unsafe_mime", "image/png\r\nsecret", "qq_media_type_unsupported", []byte("image"), 5},
		{"above_qq_limit", "image/png", "qq_media_too_large", make([]byte, (5<<20)+1), (5 << 20) + 1},
	} {
		t.Run(tt.name, func(t *testing.T) {
			calls := 0
			c := qqClientServer(t, func(w http.ResponseWriter, r *http.Request) { calls++ })
			delivery := qqDelivery()
			delivery.ArtifactBytes = tt.claimed
			delivery.Artifact = &Artifact{Data: tt.data, MIME: tt.mime}
			result := c.SendDelivery(context.Background(), qqConfig(), delivery, "https://site.example.com/api/v1/qq-image/media?token=opaque")
			if result.Status != "site_only" || result.Reason != tt.reason || calls != 0 {
				t.Fatalf("invalid artifact sent or fell back: result=%+v calls=%d", result, calls)
			}
		})
	}
}

func TestQQClientInlineUploadFailureDoesNotTryURL(t *testing.T) {
	for _, tt := range []struct {
		name, failPath, status string
		code, calls            int
	}{
		{"upload_rejected", "/v2/groups/group-1/files", "site_only", http.StatusBadRequest, 2},
		{"upload_uncertain", "/v2/groups/group-1/files", "site_only", http.StatusServiceUnavailable, 2},
		{"message_uncertain", "/v2/groups/group-1/messages", "unknown", http.StatusServiceUnavailable, 3},
	} {
		t.Run(tt.name, func(t *testing.T) {
			calls, uploads := 0, 0
			c := qqClientServer(t, func(w http.ResponseWriter, r *http.Request) {
				calls++
				if r.URL.Path == "/app/getAppAccessToken" {
					_, _ = io.WriteString(w, `{"access_token":"token","expires_in":7200}`)
					return
				}
				if r.URL.Path == "/v2/groups/group-1/files" {
					uploads++
					var body map[string]any
					_ = json.NewDecoder(r.Body).Decode(&body)
					if _, hasURL := body["url"]; hasURL || body["file_data"] == nil {
						t.Error("inline attempt fell back to URL")
					}
				}
				if r.URL.Path == tt.failPath {
					w.WriteHeader(tt.code)
					_, _ = io.WriteString(w, `{"code":850027,"message":"private failure"}`)
					return
				}
				_, _ = io.WriteString(w, `{"file_info":"media","ttl":60}`)
			})
			data := fixturePNG(t)
			delivery := qqDelivery()
			delivery.ArtifactBytes = int64(len(data))
			delivery.Artifact = &Artifact{Data: data, MIME: "image/png"}
			result := c.SendDelivery(context.Background(), qqConfig(), delivery, "https://site.example.com/api/v1/qq-image/media?token=opaque")
			if result.Status != tt.status || calls != tt.calls || uploads != 1 {
				t.Fatalf("failed attempt was retried: result=%+v calls=%d uploads=%d", result, calls, uploads)
			}
		})
	}
}
