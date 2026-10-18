package service

import (
	"errors"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
)

// opsCyberPolicyKey 在 gin context 中携带 cyber_policy 命中标记。
// 由 gateway 服务层在检测到上游 error.code=="cyber_policy" 时设置，
// handler 在 Forward 返回后读取以触发风控记录、邮件与真实用量结算。
const opsCyberPolicyKey = "ops_cyber_policy"
const openAICyberBillingRequestKey = "openai_cyber_billing_request"
const openAICyberBillingModelKey = "openai_cyber_billing_model"

// setOpenAICyberBillingModel carries the same explicit billing model used by the
// normal result. It is separate from the sent model, which may change on retry.
func setOpenAICyberBillingModel(c *gin.Context, model string) {
	if c != nil {
		c.Set(openAICyberBillingModelKey, strings.TrimSpace(model))
	}
}

// captureOpenAICyberBillingRequest replaces attempt/turn metadata with the final
// outbound body. Missing fields intentionally clear values from a previous send.
func captureOpenAICyberBillingRequest(c *gin.Context, body []byte, turnBillingModel ...string) {
	if c == nil {
		return
	}
	model := firstValidTrimmedGJSONString(body, "model")
	billingModel := c.GetString(openAICyberBillingModelKey)
	if len(turnBillingModel) > 0 {
		billingModel = strings.TrimSpace(turnBillingModel[0])
	}
	c.Set(openAICyberBillingRequestKey, OpenAICyberBillingMetadata{
		BillingModel:    billingModel,
		UpstreamModel:   model,
		ServiceTier:     optionalStringValue(extractOpenAIServiceTierFromBody(body)),
		ReasoningEffort: optionalStringValue(extractOpenAIReasoningEffortFromBody(body, model)),
	})
}

// errOpenAICyberPolicyForwarded 表示 cyber_policy 已按当前端点格式透传给客户端
// （error 已写出/下发）。compat 路径 ForwardAsChatCompletions / ForwardAsAnthropic 出口
// 据此丢弃 result 并返回该哨兵，由 handler 按标记里的真实用量结算，
// 不 failover、不重复写响应。
var errOpenAICyberPolicyForwarded = errors.New("openai cyber_policy forwarded to client")

// OpenAICyberBillingMetadata preserves the effective outbound and observed
// upstream billing facts after an errored forwarding path discards its result.
// Every field is a value so clearing the next attempt cannot mutate a recorded mark.
type OpenAICyberBillingMetadata struct {
	BillingModel                  string
	UpstreamModel                 string
	ServiceTier                   string
	ReasoningEffort               string
	UpstreamResponseModel         string
	UpstreamResponseModelConflict bool
	UpstreamResponseServiceTier   string
	Usage                         OpenAIUsage
}

// CyberPolicyMark 记录一次 cyber_policy 硬阻断的上游证据。
type CyberPolicyMark struct {
	// Audit evidence remains first-wins; a later response terminal may finish billing facts.
	billingTerminal bool
	Billing         OpenAICyberBillingMetadata
	Code            string // 固定 "cyber_policy"
	Message         string // 上游 error.message
	Body            string // 上游 response.failed / 400 原始 body（已截断；未脱敏，ops_error 落库由 sanitizeErrorBodyForStorage、风控日志由 redactContentModerationSecrets 统一脱敏）
	UpstreamStatus  int    // 上游 HTTP 状态（流式=200，非流式=400）
	UpstreamInTok   int    // 上游已报 input tokens（如有）
	UpstreamOutTok  int    // 上游已报 output tokens（如有）
}

// MarkOpsCyberPolicy 记录 cyber 标记；首个写入生效，后续忽略（同一 turn 只记一次）。
// WS 多轮场景由 handler 在每个 turn 结束后调用 ClearOpsCyberPolicy 重置。
func MarkOpsCyberPolicy(c *gin.Context, mark CyberPolicyMark) {
	if c == nil {
		return
	}
	if GetOpsCyberPolicy(c) != nil {
		return
	}
	if value, ok := c.Get(openAICyberBillingRequestKey); ok {
		if request, ok := value.(OpenAICyberBillingMetadata); ok {
			mark.Billing.BillingModel = request.BillingModel
			mark.Billing.UpstreamModel = request.UpstreamModel
			mark.Billing.ServiceTier = request.ServiceTier
			mark.Billing.ReasoningEffort = request.ReasoningEffort
		}
	}
	mark.Code = "cyber_policy"
	mark.Message = strings.TrimSpace(mark.Message)
	mark.Body = strings.TrimSpace(mark.Body)
	c.Set(opsCyberPolicyKey, &mark)
}

// GetOpsCyberPolicy 返回 cyber 标记，未命中（或已被 Clear）返回 nil。
func GetOpsCyberPolicy(c *gin.Context) *CyberPolicyMark {
	if c == nil {
		return nil
	}
	if v, ok := c.Get(opsCyberPolicyKey); ok {
		if m, ok := v.(*CyberPolicyMark); ok && m != nil {
			return m
		}
	}
	return nil
}

// ClearOpsCyberPolicy 清除 cyber 标记（typed-nil 覆盖；gin context 无并发安全的
// 删除原语，Set 走内部锁，与异步 GetOpsCyberPolicy 不构成 data race）。
// 仅 WS 多轮路径在 turn 收尾调用；HTTP 单请求路径不调用（context 随请求销毁，
// 且中间件 shouldSkipOpsErrorLogForCyber 依赖标记防双写）。
// WS 路径 clear 发生在中间件收尾之前，连接响应状态为 101，不触发中间件 status>=400
// 落库分支，故无双写/漏写。
func ClearOpsCyberPolicy(c *gin.Context) {
	if c == nil {
		return
	}
	c.Set(opsCyberPolicyKey, (*CyberPolicyMark)(nil))
	c.Set(openAICyberBillingRequestKey, nil)
	setOpenAICyberBillingModel(c, "")
}

// detectOpenAICyberPolicy 精确识别 cyber_policy（对齐 codex api_bridge.rs:145 /
// sse/responses.rs:529）。命中返回 (true, "cyber_policy", message)。
func detectOpenAICyberPolicy(payload []byte) (bool, string, string) {
	code := gjson.GetBytes(payload, "error.code").String()
	if code == "" {
		code = gjson.GetBytes(payload, "response.error.code").String()
	}
	if !strings.EqualFold(strings.TrimSpace(code), "cyber_policy") {
		return false, "", ""
	}
	msg := gjson.GetBytes(payload, "error.message").String()
	if msg == "" {
		msg = gjson.GetBytes(payload, "response.error.message").String()
	}
	return true, "cyber_policy", strings.TrimSpace(msg)
}

func markOpenAICyberPolicyEvent(c *gin.Context, payload []byte, upstreamStatus int, usage *OpenAIUsage, observers ...*upstreamResponseModelObserver) bool {
	return markOpenAICyberPolicyEventWithType(c, payload, "", upstreamStatus, usage, observers...)
}

// SSE may carry its type only in the event line. Preserve that type when
// deciding whether a later payload completes the first cyber billing mark.
func markOpenAICyberPolicyEventWithType(c *gin.Context, payload []byte, eventType string, upstreamStatus int, usage *OpenAIUsage, observers ...*upstreamResponseModelObserver) bool {
	eventType = effectiveOpenAISSEEventType(payload, eventType)
	previous := GetOpsCyberPolicy(c)
	hit, code, message := detectOpenAICyberPolicy(payload)
	// Only genuine cyber evidence can create a mark. Once blocked, a real
	// response terminal may complete billing even if it omits/replaces the code.
	if !hit && (previous == nil || !isUpstreamResponseModelTerminalEvent(eventType)) {
		return false
	}
	mark := CyberPolicyMark{
		Code:           code,
		Message:        message,
		Body:           truncateString(string(payload), 4096),
		UpstreamStatus: upstreamStatus,
	}
	// Read the full upstream event before truncating/restoring model names. Never
	// consult the gin observer: WS observers are local and an old attempt may
	// still have its observer stored on the same context.
	observer := &upstreamResponseModelObserver{}
	mark.billingTerminal = isUpstreamResponseModelTerminalEvent(eventType)
	observer.ObserveOpenAI(payload, eventType)
	mark.Billing.UpstreamResponseModel = observer.Model()
	mark.Billing.UpstreamResponseServiceTier = observer.ServiceTier()
	if len(observers) > 0 && observers[0] != nil {
		mark.Billing.UpstreamResponseModelConflict = observers[0].Conflict()
	}
	if usage != nil {
		mark.Billing.Usage = *usage
	} else {
		parseOpenAIWSResponseUsageFromCompletedEvent(payload, &mark.Billing.Usage)
	}
	mark.UpstreamInTok = mark.Billing.Usage.InputTokens
	mark.UpstreamOutTok = mark.Billing.Usage.OutputTokens
	if previous != nil {
		if mark.billingTerminal && !previous.billingTerminal {
			// A bare error may precede response.failed with the actual usage. Keep
			// the first audit record and its outbound request identity, but replace
			// the value snapshot once the authoritative terminal arrives.
			completed := *previous
			completed.billingTerminal = true
			if mark.Billing.UpstreamResponseModel != "" {
				completed.Billing.UpstreamResponseModel = mark.Billing.UpstreamResponseModel
			}
			if mark.Billing.UpstreamResponseServiceTier != "" {
				completed.Billing.UpstreamResponseServiceTier = mark.Billing.UpstreamResponseServiceTier
			}
			completed.Billing.UpstreamResponseModelConflict = completed.Billing.UpstreamResponseModelConflict || mark.Billing.UpstreamResponseModelConflict
			if usage != nil || gjson.GetBytes(payload, "response.usage").IsObject() || gjson.GetBytes(payload, "usage").IsObject() {
				completed.Billing.Usage = mark.Billing.Usage
				completed.UpstreamInTok = mark.UpstreamInTok
				completed.UpstreamOutTok = mark.UpstreamOutTok
			}
			c.Set(opsCyberPolicyKey, &completed)
		}
		return true
	}
	MarkOpsCyberPolicy(c, mark)
	return true
}
