package qqimage

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"testing"
)

func TestProviderRateLimitRemainsUnknownWithoutRetry(t *testing.T) {
	calls := 0
	p := providerServer(t, func(w http.ResponseWriter, r *http.Request) { calls++; w.WriteHeader(429) })
	_, err := p.Generate(context.Background(), providerRequest())
	if !IsUncertainGeneration(err) || calls != 1 {
		t.Fatalf("429 does not prove no task was created: calls=%d err=%v", calls, err)
	}
}
func TestProviderGPTImageRequestDoesNotForceLegacyModelParameters(t *testing.T) {
	data := fixturePNG(t)
	p := providerServer(t, func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		for _, name := range []string{"response_format", "quality", "style", "output_format"} {
			if _, ok := body[name]; ok {
				t.Errorf("unconfigured model-specific parameter %s must be absent", name)
			}
		}
		if body["model"] != "gpt-image-1.5" || body["n"] != float64(1) {
			t.Errorf("fixed request lost: %v", body)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"data": []any{map[string]string{"b64_json": base64.StdEncoding.EncodeToString(data)}}})
	})
	request := providerRequest()
	request.Model = "gpt-image-1.5"
	if _, err := p.Generate(context.Background(), request); err != nil {
		t.Fatal(err)
	}
}
func TestProviderExplicitImageHostWhitelist(t *testing.T) {
	data := fixturePNG(t)
	for _, allowed := range []bool{false, true} {
		calls := 0
		p := providerServer(t, func(w http.ResponseWriter, r *http.Request) {
			calls++
			if r.Method == "POST" {
				_ = json.NewEncoder(w).Encode(map[string]any{"data": []any{map[string]string{"url": "https://cdn.example.com/result.png"}}})
				return
			}
			if r.Header.Get("Authorization") != "" {
				t.Error("image CDN received upstream credential")
			}
			_, _ = w.Write(data)
		})
		request := providerRequest()
		if allowed {
			request.ImageHosts = []string{"cdn.example.com"}
		}
		_, err := p.Generate(context.Background(), request)
		if allowed && (err != nil || calls != 2) {
			t.Fatalf("explicit CDN not fetched once: calls=%d err=%v", calls, err)
		}
		if !allowed && (err == nil || calls != 1) {
			t.Fatal("unapproved host was fetched")
		}
	}
}
