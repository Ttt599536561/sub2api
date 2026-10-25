package qqimage

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/tls"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
	"time"

	_ "golang.org/x/image/webp"
)

const maxImageBytes int64 = 20 << 20
const maxImagePixels int64 = 20_000_000
const maxProviderResponse int64 = maxImageBytes*4/3 + (64 << 10)

type GenerationRequest struct {
	APIURL, APIKey, Model, Size, Prompt string
	ImageHosts                          []string
}
type ImageGenerator interface {
	Generate(context.Context, GenerationRequest) (Artifact, error)
}

type generationError struct {
	reason    string
	uncertain bool
}

func (e *generationError) Error() string { return e.reason }
func IsUncertainGeneration(err error) bool {
	var e *generationError
	return errors.As(err, &e) && e.uncertain
}
func generationFailure(reason string, uncertain bool) error {
	return &generationError{reason: reason, uncertain: uncertain}
}

// ImageProvider uses a single synchronous Images POST. It deliberately has no
// retry or query fallback: a lost response cannot prove that generation failed.
type ImageProvider struct{ client *http.Client }

func NewImageProvider() *ImageProvider {
	return &ImageProvider{client: secureHTTPClient(120 * time.Second)}
}

func (p *ImageProvider) Generate(ctx context.Context, in GenerationRequest) (Artifact, error) {
	var empty Artifact
	base, err := safeHTTPSURL(in.APIURL, "")
	if err != nil || base.RawQuery != "" || strings.HasSuffix(strings.TrimRight(base.Path, "/"), "/images/generations") || strings.TrimSpace(in.APIKey) == "" || strings.TrimSpace(in.Model) == "" || strings.TrimSpace(in.Size) == "" || strings.TrimSpace(in.Prompt) == "" {
		return empty, generationFailure("invalid_generation_configuration", false)
	}
	target := *base
	target.Path = strings.TrimRight(target.Path, "/") + "/images/generations"
	target.RawPath = ""
	body, _ := json.Marshal(struct {
		Model  string `json:"model"`
		Size   string `json:"size"`
		N      int    `json:"n"`
		Prompt string `json:"prompt"`
	}{in.Model, in.Size, 1, in.Prompt})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, target.String(), bytes.NewReader(body))
	if err != nil {
		return empty, generationFailure("invalid_generation_request", false)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+in.APIKey)
	resp, err := p.do(req)
	if err != nil {
		return empty, generationFailure("generation_result_unknown", true)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return empty, generationFailure(fmt.Sprintf("generation_http_%d", resp.StatusCode), resp.StatusCode >= 500 || resp.StatusCode == http.StatusRequestTimeout || resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode < 400)
	}
	raw, err := boundedRead(resp.Body, maxProviderResponse)
	if err != nil {
		return empty, generationFailure("invalid_generation_response", true)
	}
	var result struct {
		Data []struct {
			Base64 string `json:"b64_json"`
			URL    string `json:"url"`
		} `json:"data"`
	}
	if json.Unmarshal(raw, &result) != nil || len(result.Data) != 1 {
		return empty, generationFailure("missing_single_image", true)
	}
	var data []byte
	switch {
	case result.Data[0].Base64 != "" && result.Data[0].URL == "":
		if int64(base64.StdEncoding.DecodedLen(len(result.Data[0].Base64))) > maxImageBytes+2 {
			return empty, generationFailure("image_too_large", true)
		}
		data, err = base64.StdEncoding.DecodeString(result.Data[0].Base64)
	case result.Data[0].URL != "" && result.Data[0].Base64 == "":
		imageURL, urlErr := allowedImageURL(result.Data[0].URL, base.Hostname(), in.ImageHosts)
		if urlErr != nil {
			return empty, generationFailure("unsafe_image_url", true)
		}
		get, getErr := http.NewRequestWithContext(ctx, http.MethodGet, imageURL.String(), nil)
		if getErr != nil {
			return empty, generationFailure("unsafe_image_url", true)
		}
		// No upstream bearer credential is attached to the image request.
		imageResponse, fetchErr := p.do(get)
		if fetchErr != nil {
			return empty, generationFailure("image_fetch_failed", true)
		}
		defer func() { _ = imageResponse.Body.Close() }()
		if imageResponse.StatusCode != http.StatusOK {
			return empty, generationFailure("image_fetch_failed", true)
		}
		data, err = boundedRead(imageResponse.Body, maxImageBytes)
	default:
		return empty, generationFailure("missing_single_image", true)
	}
	if err != nil {
		return empty, generationFailure("invalid_image_data", true)
	}
	artifact, err := validateArtifact(data)
	if err != nil {
		return empty, generationFailure("invalid_image_data", true)
	}
	return artifact, nil
}
func (p *ImageProvider) do(req *http.Request) (*http.Response, error) {
	c := p.client
	if c == nil {
		c = secureHTTPClient(120 * time.Second)
	}
	// Preserve redirect policy even when using an injected transport in a test.
	copyClient := *c
	copyClient.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	return copyClient.Do(req)
}
func boundedRead(r io.Reader, limit int64) ([]byte, error) {
	data, err := io.ReadAll(io.LimitReader(r, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > limit {
		return nil, errors.New("response_too_large")
	}
	return data, nil
}
func validateArtifact(data []byte) (Artifact, error) {
	if len(data) == 0 || int64(len(data)) > maxImageBytes {
		return Artifact{}, errors.New("invalid_image_length")
	}
	config, format, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil || config.Width < 1 || config.Height < 1 || int64(config.Width) > maxImagePixels/int64(config.Height) {
		return Artifact{}, errors.New("invalid_image_dimensions")
	}
	mime := map[string]string{"png": "image/png", "jpeg": "image/jpeg", "webp": "image/webp"}[format]
	if mime == "" {
		return Artifact{}, errors.New("invalid_image_type")
	}
	decoded, actual, err := image.Decode(bytes.NewReader(data))
	if err != nil || actual != format || decoded.Bounds().Dx() != config.Width || decoded.Bounds().Dy() != config.Height {
		return Artifact{}, errors.New("invalid_image_content")
	}
	sum := sha256.Sum256(data)
	return Artifact{Data: data, MIME: mime, Width: config.Width, Height: config.Height, SHA256: hex.EncodeToString(sum[:])}, nil
}

func safeHTTPSURL(raw, allowedHost string) (*url.URL, error) {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.Hostname() == "" || u.User != nil || u.Fragment != "" || (u.Port() != "" && u.Port() != "443") || strings.HasSuffix(u.Hostname(), ".") {
		return nil, errors.New("unsafe_https_target")
	}
	host := strings.ToLower(u.Hostname())
	if allowedHost != "" && !strings.EqualFold(host, allowedHost) {
		return nil, errors.New("image_host_not_allowed")
	}
	if ip, parseErr := netip.ParseAddr(host); parseErr == nil {
		if !publicAddress(ip.String()) {
			return nil, errors.New("unsafe_https_address")
		}
	} else if !strings.Contains(host, ".") || strings.ContainsAny(host, "%\\ \t\r\n") {
		return nil, errors.New("unsafe_https_hostname")
	}
	return u, nil
}

var blockedNetworks = []netip.Prefix{
	netip.MustParsePrefix("0.0.0.0/8"), netip.MustParsePrefix("10.0.0.0/8"), netip.MustParsePrefix("100.64.0.0/10"), netip.MustParsePrefix("127.0.0.0/8"), netip.MustParsePrefix("169.254.0.0/16"), netip.MustParsePrefix("172.16.0.0/12"), netip.MustParsePrefix("192.0.0.0/24"), netip.MustParsePrefix("192.0.2.0/24"), netip.MustParsePrefix("192.168.0.0/16"), netip.MustParsePrefix("198.18.0.0/15"), netip.MustParsePrefix("198.51.100.0/24"), netip.MustParsePrefix("203.0.113.0/24"), netip.MustParsePrefix("224.0.0.0/4"), netip.MustParsePrefix("240.0.0.0/4"), netip.MustParsePrefix("::/96"), netip.MustParsePrefix("64:ff9b::/96"), netip.MustParsePrefix("64:ff9b:1::/48"), netip.MustParsePrefix("100::/64"), netip.MustParsePrefix("2001::/23"), netip.MustParsePrefix("2001:db8::/32"), netip.MustParsePrefix("2002::/16"), netip.MustParsePrefix("fc00::/7"), netip.MustParsePrefix("fe80::/10"),
}

func publicAddress(raw string) bool {
	ip, err := netip.ParseAddr(raw)
	if err != nil {
		return false
	}
	ip = ip.Unmap()
	if !ip.IsGlobalUnicast() {
		return false
	}
	for _, prefix := range blockedNetworks {
		if prefix.Contains(ip) {
			return false
		}
	}
	return true
}

// DNS is checked at the actual dial, then the selected address is pinned for
// that connection. ProxyFromEnvironment is intentionally not used.
func secureHTTPClient(timeout time.Duration) *http.Client {
	d := &net.Dialer{Timeout: 10 * time.Second, KeepAlive: 30 * time.Second}
	transport := &http.Transport{TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS12}, TLSHandshakeTimeout: 10 * time.Second, ResponseHeaderTimeout: timeout, IdleConnTimeout: 30 * time.Second, MaxIdleConns: 20, ForceAttemptHTTP2: true}
	transport.DialContext = restrictedDialContext(net.DefaultResolver, d)
	return &http.Client{Transport: transport, Timeout: timeout, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
}

type networkResolver interface {
	LookupNetIP(context.Context, string, string) ([]netip.Addr, error)
}
type networkDialer interface {
	DialContext(context.Context, string, string) (net.Conn, error)
}

func restrictedDialContext(resolver networkResolver, d networkDialer) func(context.Context, string, string) (net.Conn, error) {
	return func(ctx context.Context, network, address string) (net.Conn, error) {
		host, port, err := net.SplitHostPort(address)
		if err != nil || port != "443" {
			return nil, errors.New("unsafe_network_target")
		}
		addresses, err := resolver.LookupNetIP(ctx, "ip", host)
		if err != nil || len(addresses) == 0 {
			return nil, errors.New("target_resolution_failed")
		}
		for _, ip := range addresses {
			if !publicAddress(ip.String()) {
				return nil, errors.New("unsafe_resolved_address")
			}
		}
		var last error
		for _, ip := range addresses {
			conn, dialErr := d.DialContext(ctx, network, net.JoinHostPort(ip.String(), port))
			if dialErr == nil {
				return conn, nil
			}
			last = dialErr
		}
		return nil, last
	}

}

func allowedImageURL(raw, apiHost string, additionalHosts []string) (*url.URL, error) {
	target, err := safeHTTPSURL(raw, "")
	if err != nil {
		return nil, err
	}
	if strings.EqualFold(target.Hostname(), apiHost) {
		return target, nil
	}
	for _, host := range additionalHosts {
		if strings.EqualFold(target.Hostname(), host) {
			return target, nil
		}
	}
	return nil, errors.New("image_host_not_allowed")
}
