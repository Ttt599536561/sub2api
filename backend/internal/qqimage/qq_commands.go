package qqimage

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

type QQCommandStore interface {
	BotRuntimeConfig(context.Context) (*RuntimeConfig, error)
	RecordDiscovery(context.Context, int64, *QQEvent, string) error
	GetConfig(context.Context) (*Config, error)
	RuntimeConfig(context.Context, int64) (*RuntimeConfig, error)
	ProcessEvent(context.Context, string, string, func(context.Context) (string, string, error)) (string, string, bool, error)
	CreateBindingRequest(context.Context, Identity) (*BindingRequest, error)
	ConfirmBinding(context.Context, Identity, string) (*Grant, error)
	CreateOrderFromEvent(context.Context, Identity, string, string, time.Time, string) (*Order, error)
	GetOrderForIdentity(context.Context, Identity, string) (*Order, error)
	RequestRedelivery(context.Context, Identity, string, string) (*Order, error)
}
type QQCommandOptions struct {
	ExternalGeneralHelp bool
}

type QQCommandHandler struct {
	externalGeneralHelp bool
	store               QQCommandStore
	sender              QQTextSender
}

func NewQQCommandHandler(store QQCommandStore, sender QQTextSender) *QQCommandHandler {
	return NewQQCommandHandlerWithOptions(store, sender, QQCommandOptions{})
}

// NewQQCommandHandlerWithOptions keeps standalone help by default. A deployment
// sharing its robot with another command service can assign general help there.
func NewQQCommandHandlerWithOptions(store QQCommandStore, sender QQTextSender, options QQCommandOptions) *QQCommandHandler {
	return &QQCommandHandler{store: store, sender: sender, externalGeneralHelp: options.ExternalGeneralHelp}
}

// HandleWebhook verifies the original bytes before obtaining a trusted identity.
// Business changes and event results commit together before any reply; retries cannot
// repeat a command or an unknown QQ send. No user ID is read from message text.
func (h *QQCommandHandler) HandleWebhook(ctx context.Context, headers http.Header, raw []byte) (*QQChallenge, error) {
	runtime, err := h.store.BotRuntimeConfig(ctx)
	if err != nil {
		return nil, err
	}
	if receiveMode(runtime.ReceiveMode) != ReceiveModeWebhook {
		return nil, ErrDisabled
	}
	event, challenge, err := NewQQVerifier(runtime.AppID, runtime.BotSecret).Verify(headers, raw)
	if err != nil {
		return nil, err
	}
	if challenge != nil {
		return challenge, nil
	}
	id, appID := runtime.ID, runtime.AppID
	return h.handleEvent(ctx, runtime, event, func(ctx context.Context) error { return h.currentConnectionConfig(ctx, id, appID, ReceiveModeWebhook) })
}

// HandleGatewayEvent accepts frames only from our authenticated official WSS
// receiver. It is never exposed as an HTTP endpoint or signature bypass.
func (h *QQCommandHandler) HandleGatewayEvent(ctx context.Context, configID int64, appID string, raw []byte) error {
	if err := checkGatewayGuard(ctx); err != nil {
		return err
	}
	runtime, err := h.store.BotRuntimeConfig(ctx)
	if err != nil {
		return err
	}
	if receiveMode(runtime.ReceiveMode) != ReceiveModeWebSocket {
		return ErrDisabled
	}
	if runtime.ID != configID || runtime.AppID != appID {
		return ErrConflict
	}
	event, err := parseQQGroupEvent(appID, raw, true)
	if err != nil {
		return err
	}
	_, err = h.handleEvent(ctx, runtime, event, func(ctx context.Context) error {
		if err := h.currentConnectionConfig(ctx, configID, appID, ReceiveModeWebSocket); err != nil {
			return err
		}
		return checkGatewayGuard(ctx)
	})
	return err
}
func (h *QQCommandHandler) currentConnectionConfig(ctx context.Context, id int64, appID, mode string) error {
	current, err := h.store.BotRuntimeConfig(ctx)
	if err != nil {
		return err
	}
	if current.ID != id || current.AppID != appID || receiveMode(current.ReceiveMode) != mode {
		return ErrConflict
	}
	return nil
}

type gatewayGuardKey struct{}

func withGatewayGuard(ctx context.Context, guard func(context.Context) error) context.Context {
	return context.WithValue(ctx, gatewayGuardKey{}, guard)
}
func checkGatewayGuard(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if guard, ok := ctx.Value(gatewayGuardKey{}).(func(context.Context) error); ok {
		return guard(ctx)
	}
	return nil
}
func (h *QQCommandHandler) handleEvent(ctx context.Context, runtime *RuntimeConfig, event *QQEvent, admission func(context.Context) error) (*QQChallenge, error) {
	age := time.Since(event.CreatedAt)
	if age < -time.Minute || age >= 5*time.Minute {
		return nil, ErrExpired
	}
	command, arg := splitQQCommand(event.Content)
	if command == "/识别群" {
		return nil, h.store.RecordDiscovery(ctx, runtime.ID, event, arg)
	}
	allowed := false
	for _, group := range runtime.AllowedGroups {
		if group == event.Identity.GroupID {
			allowed = true
			break
		}
	}
	if !allowed {
		return nil, ErrForbidden
	}
	if h.externalGeneralHelp && command == "/帮助" {
		return nil, nil
	}
	switch command {
	case "/绑定", "/确认绑定", "/帮助", "/生图帮助", "/生图", "/任务", "/重发":
	default:
		return nil, nil
	}
	// Help and redelivery of stored images need only bot credentials.
	// Provider unavailability must not prevent sending an existing result.
	if command != "/帮助" && command != "/生图帮助" && command != "/重发" {
		var err error
		runtime, err = h.store.RuntimeConfig(ctx, runtime.ID)
		if err != nil {
			return nil, err
		}
	}
	canonical, _ := json.Marshal(struct {
		Identity           Identity
		MessageID, Content string
	}{event.Identity, event.MessageID, event.Content})
	payloadHash := sha256.Sum256(canonical)
	// Capture one immutable revision before entering the command transaction.
	// An unavailable template table falls back without aborting business SQL.
	overrides := map[string]string{}
	if reader, ok := h.store.(interface {
		GetReplyTemplates(context.Context) (*ReplyTemplateSnapshot, error)
	}); ok {
		if snapshot, err := reader.GetReplyTemplates(ctx); err == nil && snapshot != nil {
			for key, value := range snapshot.Overrides {
				overrides[key] = value
			}
		}
	}
	content, manual, fresh, err := h.store.ProcessEvent(ctx, "command:"+event.Identity.Key()+":"+event.MessageID, hex.EncodeToString(payloadHash[:]), func(txctx context.Context) (string, string, error) {
		if err := admission(txctx); err != nil {
			return "", "", err
		}
		return h.execute(txctx, runtime, event, command, arg, overrides)
	})
	if err != nil {
		return nil, err
	}
	// Business changes and the processed result commit together. A repeated
	// callback cannot repeat a command or a QQ send whose receipt is unknown.
	if !fresh {
		return nil, nil
	}
	if h.sender != nil && content != "" {
		replyContext, cancel := context.WithTimeout(ctx, 20*time.Second)
		defer cancel()
		result := h.sender.SendText(replyContext, runtime, event, content, 1)
		// A known rejection of a link can safely use the manual short-code flow.
		// Unknown receipts are never automatically retried with a new sequence.
		if manual != "" && result.Status == "failed" {
			h.sender.SendText(replyContext, runtime, event, manual, 2)
		}
	}
	return nil, nil
}
func splitQQCommand(content string) (string, string) {
	content = strings.TrimSpace(content)
	// Reserve the help prefix so a misspelled help command cannot create a paid
	// image. Only image commands accept compact prompts and Unicode separators.
	if suffix, ok := strings.CutPrefix(content, "/生图帮助"); ok {
		first, _ := utf8.DecodeRuneInString(suffix)
		if suffix == "" || unicode.IsSpace(first) {
			return "/生图帮助", strings.TrimSpace(suffix)
		}
	} else if suffix, ok := strings.CutPrefix(content, "/生图"); ok {
		first, _ := utf8.DecodeRuneInString(suffix)
		// An invisible/control character adjoining the command remains unknown;
		// characters inside a properly separated prompt are never rewritten.
		if suffix == "" || unicode.IsSpace(first) || !unicode.In(first, unicode.Cc, unicode.Cf) {
			return "/生图", strings.TrimSpace(suffix)
		}
	}
	pos := strings.IndexAny(content, " \t\r\n")
	if pos < 0 {
		return content, ""
	}
	return content[:pos], strings.TrimSpace(content[pos:])
}
func (h *QQCommandHandler) execute(ctx context.Context, config *RuntimeConfig, event *QQEvent, command, arg string, overrides map[string]string) (string, string, error) {
	variables := replyCommandVariables(config)
	render := func(key string) string { return RenderReplyTemplate(overrides, key, variables) }
	commandError := func(err error) (string, string, error) { return replyTemplateCommandError(overrides, err) }
	orderURL := func(id string) string {
		return strings.TrimRight(config.PublicBaseURL, "/") + "/qq/orders/" + url.PathEscape(id)
	}
	switch command {
	case "/帮助", "/生图帮助":
		variables["settlement_rules"] = "图片在原站可领取时结算；"
		variables["redelivery_rules"] = "重发已有图片不再扣费。"
		return render("image.help"), "", nil
	case "/绑定":
		if arg != "" {
			variables["binding_rules"] = "请勿替他人授权。"
			return render("binding.usage"), "", nil
		}
		request, err := h.store.CreateBindingRequest(ctx, event.Identity)
		if err != nil {
			return commandError(err)
		}
		link := strings.TrimRight(config.PublicBaseURL, "/") + "/qq/bind?request=" + url.QueryEscape(request.ID)
		variables["request_id"], variables["binding_url"] = request.ID, link
		return render("binding.request"), render("binding.manual"), nil
	case "/确认绑定":
		if len(arg) < 4 || len(arg) > 32 || strings.ContainsAny(arg, " \t\r\n") {
			return render("binding.confirm_usage"), "", nil
		}
		_, err := h.store.ConfirmBinding(ctx, event.Identity, arg)
		if err != nil {
			return commandError(err)
		}
		return render("binding.success"), "", nil
	case "/生图":
		if arg == "" {
			return render("image.generate_usage"), "", nil
		}
		order, err := h.store.CreateOrderFromEvent(ctx, event.Identity, event.EventID, event.MessageID, event.CreatedAt, arg)
		if err != nil {
			return commandError(err)
		}
		variables["order_id"], variables["price"], variables["order_url"] = order.ID, order.Price, orderURL(order.ID)
		return render("order.accepted"), "", nil
	case "/任务":
		if !validQQOrderArgument(arg) {
			return render("order.status_usage"), "", nil
		}
		order, err := h.store.GetOrderForIdentity(ctx, event.Identity, arg)
		if err != nil {
			return commandError(err)
		}
		variables["order_id"], variables["order_status"], variables["funds_status"] = order.ID, order.Status, order.FundsStatus
		variables["delivery_status"], variables["order_url"] = order.DeliveryStatus, orderURL(order.ID)
		return render("order.status"), "", nil
	case "/重发":
		if !validQQOrderArgument(arg) {
			return render("order.redelivery_usage"), "", nil
		}
		order, err := h.store.RequestRedelivery(ctx, event.Identity, arg, event.MessageID)
		if err != nil {
			return commandError(err)
		}
		variables["order_id"], variables["order_url"] = order.ID, orderURL(order.ID)
		return render("order.redelivery"), "", nil
	}
	return "", "", nil
}
func validQQOrderArgument(value string) bool {
	return value != "" && len(value) <= 128 && !strings.ContainsAny(value, " \t\r\n")
}
func qqCommandError(err error) string {
	switch {
	case errors.Is(err, ErrInsufficientBalance):
		return "可用余额不足，本次未生成；请登录原网站检查余额。"
	case errors.Is(err, ErrLimit):
		return "请求过于频繁、已有在途订单或已达到额度；请稍后在原网站查看。"
	case errors.Is(err, ErrExpired):
		return "申请或图片已过期，请在原网站查看；绑定需重新发送 /绑定。"
	case errors.Is(err, ErrDisabled):
		return "当前未开放新申请或生图；已有订单可在原网站查看。"
	case errors.Is(err, ErrConflict):
		return "当前状态不允许此操作，请回原网站检查本人申请或订单。"
	case errors.Is(err, ErrForbidden), errors.Is(err, ErrNotFound):
		return "无法完成此操作，请登录原网站检查本人绑定、邮箱资格和订单。"
	case errors.Is(err, ErrInvalid):
		return "指令内容不符合要求，请使用 /帮助 查看说明。"
	default:
		return "暂时无法处理，请稍后到原网站检查；请勿重复提交生图。"
	}
}

func qqCommandExecutionError(err error) (string, string, error) {
	for _, expected := range []error{ErrInsufficientBalance, ErrLimit, ErrExpired, ErrDisabled, ErrConflict, ErrForbidden, ErrNotFound, ErrInvalid, ErrEncryptionUnavailable} {
		if errors.Is(err, expected) {
			return qqCommandError(err), "", nil
		}
	}
	// Transient database failures roll back both business changes and the event
	// result, allowing a signed platform retry to resume the original command.
	return "", "", err
}
