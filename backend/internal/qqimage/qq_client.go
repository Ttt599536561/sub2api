package qqimage

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

const qqAPIBase = "https://api.bot.qq.com"

// Keep Base64 and JSON overhead below QQ's 5 MB inline request limit.
// This is deliberately smaller than the independent 5 MiB image limit.
const maxQQInlineArtifactBytes int64 = 3_500_000

type qqRequestStage string

const (
	qqStageToken       qqRequestStage = "token"
	qqStageMediaUpload qqRequestStage = "media_upload"
	qqStageMessageSend qqRequestStage = "message_send"
)

type QQSendResult struct{ Status, Reason string }
type QQSender interface {
	SendDelivery(context.Context, *RuntimeConfig, *Delivery, string) QQSendResult
}
type QQTextSender interface {
	SendText(context.Context, *RuntimeConfig, *QQEvent, string, int) QQSendResult
}
type qqToken struct {
	value   string
	expires time.Time
}
type QQClient struct {
	client *http.Client
	now    func() time.Time
	mu     sync.Mutex
	tokens map[string]qqToken
}

func NewQQClient() *QQClient {
	return &QQClient{client: secureHTTPClient(15 * time.Second), now: time.Now, tokens: map[string]qqToken{}}
}
func (c *QQClient) currentTime() time.Time {
	if c.now != nil {
		return c.now()
	}
	return time.Now()
}
func (c *QQClient) doJSON(ctx context.Context, stage qqRequestStage, path, token string, body any) (map[string]json.RawMessage, bool, string) {
	raw, err := json.Marshal(body)
	if err != nil {
		return nil, false, "qq_invalid_request"
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, qqAPIBase+path, bytes.NewReader(raw))
	if err != nil {
		return nil, false, "qq_invalid_request"
	}
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "QQBot "+token)
	}
	client := c.client
	if client == nil {
		client = secureHTTPClient(15 * time.Second)
	}
	copyClient := *client
	copyClient.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	response, err := copyClient.Do(req)
	if err != nil {
		return nil, true, "qq_result_unknown"
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		reason := fmt.Sprintf("qq_%s_http_%d", stage, response.StatusCode)
		// Persist only our fixed stage and numeric protocol fields. QQ messages
		// and other response fields can contain credentials or private URLs.
		if data, readErr := boundedRead(response.Body, 64<<10); readErr == nil {
			var failure struct {
				Code *int64 `json:"code"`
			}
			if json.Unmarshal(data, &failure) == nil && failure.Code != nil {
				reason += fmt.Sprintf("_code_%d", *failure.Code)
			}
		}
		// A diagnostic code cannot establish whether an uncertain send happened.
		return nil, response.StatusCode >= 500 || response.StatusCode == 408 || response.StatusCode < 400, reason
	}
	data, err := boundedRead(response.Body, 64<<10)
	if err != nil {
		return nil, true, "qq_invalid_response"
	}
	var result map[string]json.RawMessage
	if json.Unmarshal(data, &result) != nil {
		return nil, true, "qq_invalid_response"
	}
	if rawCode, ok := result["code"]; ok {
		var code int
		if json.Unmarshal(rawCode, &code) != nil {
			return nil, true, "qq_invalid_response"
		}
		if code != 0 {
			return nil, false, fmt.Sprintf("qq_error_%d", code)
		}
	}
	return result, false, ""
}
func (c *QQClient) token(ctx context.Context, config *RuntimeConfig) (string, string) {
	if config == nil || config.AppID == "" || config.BotSecret == "" {
		return "", "qq_credentials_unavailable"
	}
	digest := sha256.Sum256([]byte(config.AppID + "\x00" + config.BotSecret))
	cacheKey := hex.EncodeToString(digest[:])
	c.mu.Lock()
	defer c.mu.Unlock()
	now := c.currentTime()
	if token, ok := c.tokens[cacheKey]; ok && now.Add(time.Minute).Before(token.expires) {
		return token.value, ""
	}
	result, _, reason := c.doJSON(ctx, qqStageToken, "/app/getAppAccessToken", "", map[string]string{"appId": config.AppID, "clientSecret": config.BotSecret})
	if reason != "" {
		return "", reason
	}
	var value string
	_ = json.Unmarshal(result["access_token"], &value)
	expiration := string(result["expires_in"])
	expiration = strings.Trim(expiration, "\"")
	seconds, err := strconv.Atoi(expiration)
	if value == "" || len(value) > 8192 || err != nil || seconds <= 60 || seconds > 86400 {
		return "", "qq_invalid_token_response"
	}
	if c.tokens == nil {
		c.tokens = map[string]qqToken{}
	}
	for key, value := range c.tokens {
		if !now.Before(value.expires) {
			delete(c.tokens, key)
		}
	}
	c.tokens[cacheKey] = qqToken{value: value, expires: now.Add(time.Duration(seconds) * time.Second)}
	return value, ""
}
func (c *QQClient) eligible(config *RuntimeConfig, identity Identity, messageID string, createdAt time.Time, sequence int) bool {
	if config == nil || identity.Validate() != nil || identity.AppID != config.AppID || messageID == "" || len(messageID) > 256 || sequence < 1 || sequence > 5 {
		return false
	}
	age := c.currentTime().Sub(createdAt)
	if createdAt.IsZero() || age < -time.Minute || age >= 5*time.Minute {
		return false
	}
	for _, group := range config.AllowedGroups {
		if group == identity.GroupID {
			return true
		}
	}
	return false
}
func (c *QQClient) SendText(ctx context.Context, config *RuntimeConfig, event *QQEvent, content string, sequence int) QQSendResult {
	if event == nil || !c.eligible(config, event.Identity, event.MessageID, event.CreatedAt, sequence) || strings.TrimSpace(content) == "" || len([]rune(content)) > 2000 {
		return QQSendResult{Status: "failed", Reason: "qq_reply_ineligible"}
	}
	token, reason := c.token(ctx, config)
	if reason != "" {
		return QQSendResult{Status: "failed", Reason: reason}
	}
	body := map[string]any{"msg_type": 0, "content": content, "msg_id": event.MessageID, "msg_seq": sequence}
	response, unknown, reason := c.doJSON(ctx, qqStageMessageSend, "/v2/groups/"+url.PathEscape(event.Identity.GroupID)+"/messages", token, body)
	if reason != "" {
		status := "failed"
		if unknown {
			status = "unknown"
		}
		return QQSendResult{Status: status, Reason: reason}
	}
	var id string
	_ = json.Unmarshal(response["id"], &id)
	if id == "" {
		return QQSendResult{Status: "unknown", Reason: "qq_missing_receipt"}
	}
	return QQSendResult{Status: "sent"}
}
func (c *QQClient) SendDelivery(ctx context.Context, config *RuntimeConfig, delivery *Delivery, mediaURL string) QQSendResult {
	fallback := func(reason string) QQSendResult { return QQSendResult{Status: "site_only", Reason: reason} }
	if delivery == nil || delivery.Order == nil || !c.eligible(config, delivery.Order.Identity, delivery.ReplyMessageID, delivery.ReplyCreatedAt, delivery.ReplySequence) {
		return fallback("qq_reply_ineligible")
	}
	if delivery.ArtifactBytes > 5<<20 {
		return fallback("qq_media_too_large")
	}
	base, err := safeHTTPSURL(config.PublicBaseURL, "")
	if err != nil {
		return fallback("qq_media_origin_invalid")
	}
	target, err := safeHTTPSURL(mediaURL, base.Hostname())
	if err != nil || target.Path != "/api/v1/qq-image/media" || target.Query().Get("token") == "" || len(target.Query()) != 1 {
		return fallback("qq_media_url_invalid")
	}
	uploadBody := map[string]any{"file_type": 1, "srv_send_msg": false}
	if artifact := delivery.Artifact; artifact != nil {
		if len(artifact.Data) == 0 || int64(len(artifact.Data)) != delivery.ArtifactBytes {
			return fallback("qq_media_data_invalid")
		}
		filename := ""
		switch artifact.MIME {
		case "image/png":
			filename = "image.png"
		case "image/jpeg":
			filename = "image.jpg"
		case "image/webp": // Preserve the existing URL path; never transcode originals.
		default:
			return fallback("qq_media_type_unsupported")
		}
		if filename != "" && delivery.ArtifactBytes <= maxQQInlineArtifactBytes {
			uploadBody["file_data"] = base64.StdEncoding.EncodeToString(artifact.Data)
			uploadBody["file_name"] = filename
		}
	}
	if _, inline := uploadBody["file_data"]; !inline {
		uploadBody["url"] = mediaURL
	}
	token, reason := c.token(ctx, config)
	if reason != "" {
		return fallback(reason)
	}
	path := "/v2/groups/" + url.PathEscape(delivery.Order.Identity.GroupID)
	// srv_send_msg=false makes an ambiguous upload harmless: it cannot itself
	// publish a message. Only the subsequent send can have unknown delivery.
	upload, _, reason := c.doJSON(ctx, qqStageMediaUpload, path+"/files", token, uploadBody)
	if reason != "" {
		return fallback(reason)
	}
	var fileInfo string
	_ = json.Unmarshal(upload["file_info"], &fileInfo)
	if fileInfo == "" || len(fileInfo) > 32768 {
		return fallback("qq_missing_media_receipt")
	}
	// QQ requires content for group messages, including media-only messages.
	response, unknown, reason := c.doJSON(ctx, qqStageMessageSend, path+"/messages", token, map[string]any{"msg_type": 7, "content": " ", "media": map[string]string{"file_info": fileInfo}, "msg_id": delivery.ReplyMessageID, "msg_seq": delivery.ReplySequence})
	if reason != "" {
		if unknown {
			return QQSendResult{Status: "unknown", Reason: reason}
		}
		return fallback(reason)
	}
	var id string
	_ = json.Unmarshal(response["id"], &id)
	if id == "" {
		return QQSendResult{Status: "unknown", Reason: "qq_missing_receipt"}
	}
	return QQSendResult{Status: "sent"}
}
