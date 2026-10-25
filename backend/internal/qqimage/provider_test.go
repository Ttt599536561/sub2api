package qqimage

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func fixturePNG(t *testing.T) []byte {
	t.Helper()
	im := image.NewRGBA(image.Rect(0, 0, 2, 2))
	im.Set(1, 1, color.RGBA{R: 255, A: 255})
	var b bytes.Buffer
	if err := png.Encode(&b, im); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

// The mock transport rewrites a validated public request only inside tests.
// Production has no private-network switch and never receives this transport.
func providerServer(t *testing.T, handler http.HandlerFunc) *ImageProvider {
	t.Helper()
	server := httptest.NewTLSServer(handler)
	t.Cleanup(server.Close)
	c := server.Client()
	original := c.Transport
	c.Transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
		copied := r.Clone(r.Context())
		target := *r.URL
		target.Scheme = "https"
		target.Host = strings.TrimPrefix(server.URL, "https://")
		copied.URL = &target
		return original.RoundTrip(copied)
	})
	return &ImageProvider{client: c}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func providerRequest() GenerationRequest {
	return GenerationRequest{APIURL: "https://images.example.com/v1", APIKey: "private-key", Model: "fixed-model", Size: "1024x1024", Prompt: "a tree"}
}

func TestProviderSingleImageFixedContract(t *testing.T) {
	data := fixturePNG(t)
	calls := 0
	p := providerServer(t, func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.URL.Path != "/v1/images/generations" || r.Header.Get("Authorization") != "Bearer private-key" {
			t.Errorf("unexpected authenticated request: %s", r.URL.Path)
		}
		var req map[string]any
		_ = json.NewDecoder(r.Body).Decode(&req)
		if req["model"] != "fixed-model" || req["n"] != float64(1) || req["size"] != "1024x1024" || req["prompt"] != "a tree" {
			t.Errorf("wrong fixed request: %v", req)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"data": []any{map[string]string{"b64_json": base64.StdEncoding.EncodeToString(data)}}})
	})
	got, err := p.Generate(context.Background(), providerRequest())
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got.Data, data) || got.MIME != "image/png" || got.Width != 2 || got.Height != 2 || len(got.SHA256) != 64 || calls != 1 {
		t.Fatalf("bad artifact: %+v, calls=%d", got, calls)
	}
}
func TestProviderUncertainResponsesAreNotRetried(t *testing.T) {
	for _, tc := range []struct {
		name   string
		status int
		body   string
	}{{"empty", 200, `{"data":[]}`}, {"corrupt", 200, `{"data":[{"b64_json":"dW5zYWZl"}]}`}, {"multi", 200, `{"data":[{},{}]}`}, {"server", 503, `{"error":"internal key secret"}`}} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			p := providerServer(t, func(w http.ResponseWriter, r *http.Request) {
				calls++
				w.WriteHeader(tc.status)
				_, _ = io.WriteString(w, tc.body)
			})
			_, err := p.Generate(context.Background(), providerRequest())
			if !IsUncertainGeneration(err) || calls != 1 {
				t.Fatalf("wanted single uncertain request, calls=%d err=%v", calls, err)
			}
			if strings.Contains(err.Error(), "internal key") {
				t.Fatal("upstream body leaked")
			}
		})
	}
}
func TestProviderExplicitRejection(t *testing.T) {
	p := providerServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(400)
		_, _ = io.WriteString(w, `{"error":"content rejected"}`)
	})
	_, err := p.Generate(context.Background(), providerRequest())
	if err == nil || IsUncertainGeneration(err) {
		t.Fatalf("explicit rejection should release: %v", err)
	}
}
func TestProviderDangerousURLNeverFetched(t *testing.T) {
	for _, target := range []string{"http://images.example.com/a.png", "https://127.0.0.1/a.png", "https://169.254.169.254/a.png", "https://elsewhere.example.com/a.png", "https://images.example.com:8443/a.png", "https://user:pass@images.example.com/a.png"} {
		t.Run(target, func(t *testing.T) {
			calls := 0
			p := providerServer(t, func(w http.ResponseWriter, r *http.Request) {
				calls++
				_ = json.NewEncoder(w).Encode(map[string]any{"data": []any{map[string]string{"url": target}}})
			})
			_, err := p.Generate(context.Background(), providerRequest())
			if err == nil || calls != 1 {
				t.Fatalf("unsafe fetch calls=%d err=%v", calls, err)
			}
		})
	}
}
func TestProviderSafeURLAndNoCredentialForwarding(t *testing.T) {
	data := fixturePNG(t)
	p := providerServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			_, _ = io.WriteString(w, `{"data":[{"url":"https://images.example.com/result.png"}]}`)
			return
		}
		if r.Header.Get("Authorization") != "" {
			t.Error("image GET must not contain key")
		}
		_, _ = w.Write(data)
	})
	got, err := p.Generate(context.Background(), providerRequest())
	if err != nil || !bytes.Equal(got.Data, data) {
		t.Fatalf("artifact=%v err=%v", got, err)
	}
}
func TestProviderRejectsRedirect(t *testing.T) {
	var calls atomic.Int32
	p := providerServer(t, func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.Method == http.MethodPost {
			_, _ = io.WriteString(w, `{"data":[{"url":"https://images.example.com/result.png"}]}`)
			return
		}
		http.Redirect(w, r, "https://127.0.0.1/metadata", http.StatusFound)
	})
	_, err := p.Generate(context.Background(), providerRequest())
	if err == nil || calls.Load() != 2 {
		t.Fatalf("redirect followed calls=%d err=%v", calls.Load(), err)
	}
}
func TestProviderNetworkPolicy(t *testing.T) {
	for _, ip := range []string{"127.0.0.1", "10.1.1.1", "169.254.169.254", "100.64.0.1", "192.0.0.1", "192.0.2.1", "198.51.100.1", "203.0.113.1", "224.0.0.1", "0.0.0.0", "::1", "fe80::1", "fc00::1", "::ffff:127.0.0.1"} {
		if publicAddress(ip) {
			t.Errorf("unsafe address accepted %s", ip)
		}
	}
	if !publicAddress("8.8.8.8") || !publicAddress("2606:4700:4700::1111") {
		t.Fatal("public address blocked")
	}
}
func TestProviderRejectsTooLargeActualImage(t *testing.T) {
	p := providerServer(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = fmt.Fprintf(w, `{"data":[{"b64_json":"%s"}]}`, base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{1}, int(maxImageBytes)+1)))
	})
	_, err := p.Generate(context.Background(), providerRequest())
	if err == nil {
		t.Fatal("oversized image accepted")
	}
}

func TestProviderTimeoutAndDisconnectNeverRetryPOST(t *testing.T) {
	for _, kind := range []string{"timeout", "disconnect"} {
		t.Run(kind, func(t *testing.T) {
			var calls atomic.Int32
			p := providerServer(t, func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				if kind == "timeout" {
					select {
					case <-r.Context().Done():
					case <-time.After(200 * time.Millisecond):
					}
					return
				}
				hijacker, ok := w.(http.Hijacker)
				if !ok {
					t.Error("test response does not support hijacking")
					return
				}
				connection, _, err := hijacker.Hijack()
				if err != nil {
					t.Error(err)
					return
				}
				_ = connection.Close()
			})
			ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
			defer cancel()
			_, err := p.Generate(ctx, providerRequest())
			if !IsUncertainGeneration(err) || calls.Load() != 1 {
				t.Fatalf("ambiguous POST retried/released: calls=%d err=%v", calls.Load(), err)
			}
		})
	}
}
func TestProviderTruncatedImageIsNotAValidArtifact(t *testing.T) {
	data := fixturePNG(t)
	p := providerServer(t, func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"data": []any{map[string]string{"b64_json": base64.StdEncoding.EncodeToString(data[:len(data)-8])}}})
	})
	_, err := p.Generate(context.Background(), providerRequest())
	if !IsUncertainGeneration(err) {
		t.Fatalf("truncated image should reconcile: %v", err)
	}
}

// Resolve and dial are separated so DNS changes can be exercised without
// granting private-network exceptions to the real HTTP client.
type fakePublicResolver struct{ addresses []netip.Addr }

func (r *fakePublicResolver) LookupNetIP(context.Context, string, string) ([]netip.Addr, error) {
	return r.addresses, nil
}

type observingPublicDialer struct{ addresses []string }

func (d *observingPublicDialer) DialContext(_ context.Context, _, address string) (net.Conn, error) {
	d.addresses = append(d.addresses, address)
	return nil, errors.New("test dial stopped")
}
func TestProviderDNSRebindingNeverDialsPrivateAddress(t *testing.T) {
	resolver := &fakePublicResolver{addresses: []netip.Addr{netip.MustParseAddr("8.8.8.8")}}
	dialer := &observingPublicDialer{}
	dial := restrictedDialContext(resolver, dialer)
	_, _ = dial(context.Background(), "tcp", "images.example.com:443")
	if len(dialer.addresses) != 1 || dialer.addresses[0] != "8.8.8.8:443" {
		t.Fatalf("DNS answer not pinned: %v", dialer.addresses)
	}
	resolver.addresses = []netip.Addr{netip.MustParseAddr("8.8.8.8"), netip.MustParseAddr("127.0.0.1")}
	_, err := dial(context.Background(), "tcp", "images.example.com:443")
	if err == nil || len(dialer.addresses) != 1 {
		t.Fatalf("rebound mixed DNS result dialed: %v %v", dialer.addresses, err)
	}
	_, err = dial(context.Background(), "tcp", "images.example.com:8443")
	if err == nil || len(dialer.addresses) != 1 {
		t.Fatal("unapproved port dialed")
	}
}

func TestProviderRejectsFullGenerationEndpointBeforePOST(t *testing.T) {
	calls := 0
	p := providerServer(t, func(w http.ResponseWriter, r *http.Request) { calls++ })
	req := providerRequest()
	req.APIURL = "https://images.example.com/v1/images/generations"
	_, err := p.Generate(context.Background(), req)
	if err == nil || IsUncertainGeneration(err) || calls != 0 {
		t.Fatalf("full endpoint dispatched: calls=%d err=%v", calls, err)
	}
}
