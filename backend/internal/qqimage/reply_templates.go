package qqimage

import (
	"regexp"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"
)

const ReplyTemplateMaxLength = 2000

type ReplyTemplateDefinition struct {
	Key               string         `json:"key"`
	Label             string         `json:"label"`
	Category          string         `json:"category"`
	DefaultText       string         `json:"default_text"`
	AllowedVariables  []string       `json:"allowed_variables"`
	RequiredVariables []string       `json:"required_variables"`
	VariableLimits    map[string]int `json:"variable_limits"`
}

var replyVariablePattern = regexp.MustCompile(`\{\{([a-z][a-z0-9_]*)\}\}`)

func replyDefinition(key, label, category, text string, limits map[string]int) ReplyTemplateDefinition {
	names := make([]string, 0, len(limits))
	for name := range limits {
		names = append(names, name)
	}
	sort.Strings(names)
	return ReplyTemplateDefinition{key, label, category, text, names, []string{}, limits}
}

// Display fields are optional. Authorization, monetary and delivery decisions
// are enforced by the command handler and store, independently of reply text.
func ReplyTemplateCatalog() []ReplyTemplateDefinition {
	c := []ReplyTemplateDefinition{
		replyDefinition("common.menu", "统一帮助菜单", "common", "【智商查询】\n{{iq_commands}}\n\n可用分组：\n{{group_names}}\n\n【生图】\n{{image_commands}}\n\n{{usage_rules}}", map[string]int{"group_names": 680, "iq_commands": 128, "image_commands": 256, "usage_rules": 256}),
		replyDefinition("iq.ack", "智商查询受理", "iq", "收到，正在查询并生成截图，请稍候。", map[string]int{"group_title": 160}),
		replyDefinition("iq.caption", "智商截图说明", "iq", "{{group_title}}\n截图时间：{{captured_at}}（{{timezone}}）\n{{query_rules}}{{schedule_rules}}", map[string]int{"group_title": 160, "captured_at": 64, "timezone": 64, "schedule_rules": 256, "query_rules": 256}),
		replyDefinition("iq.error.capture", "截图失败", "iq", "本次截图生成失败，请稍后重新查询。{{query_rules}}", map[string]int{"query_rules": 256}),
		replyDefinition("iq.error.station", "检测站异常", "iq", "暂时无法读取分组状态，请稍后重新查询。{{query_rules}}", map[string]int{"query_rules": 256}),
		replyDefinition("iq.error.delivery_unknown", "截图送达未知", "iq", "{{delivery_rules}}如果未收到，请稍后重试查询指令。{{query_rules}}", map[string]int{"delivery_rules": 256, "query_rules": 256}),
		replyDefinition("image.help", "生图帮助", "image", "{{image_commands}}\n{{availability}}{{model_details}}{{price_details}}{{authorization_rules}}{{settlement_rules}}{{visibility_rules}}{{redelivery_rules}}", map[string]int{"image_commands": 160, "availability": 64, "model_details": 512, "price_details": 64, "authorization_rules": 128, "settlement_rules": 96, "visibility_rules": 96, "redelivery_rules": 96}),
		replyDefinition("image.generate_usage", "生图用法", "image", "请使用 /生图 提示词；可用 /生图帮助 查看固定价格。", map[string]int{}),
		replyDefinition("binding.usage", "绑定用法", "binding", "请使用 /绑定 发起本人申请。{{binding_rules}}", map[string]int{"binding_rules": 256}),
		replyDefinition("binding.request", "绑定申请", "binding", "请仅绑定您本人刚发起的申请：{{binding_url}}\n绑定申请编号：{{request_id}}。{{binding_rules}}", map[string]int{"binding_url": 768, "request_id": 128, "binding_rules": 256}),
		replyDefinition("binding.manual", "绑定手动备用", "binding", "绑定申请编号：{{request_id}}。{{binding_rules}}", map[string]int{"request_id": 128, "binding_rules": 256}),
		replyDefinition("binding.confirm_usage", "确认绑定用法", "binding", "{{confirmation_rules}}", map[string]int{"confirmation_rules": 256}),
		replyDefinition("binding.success", "绑定成功", "binding", "{{success_rules}}", map[string]int{"success_rules": 256}),
		replyDefinition("order.accepted", "订单受理", "order", "订单 {{order_id}} 已受理，已预留 US${{price}}。{{settlement_rules}}\n{{order_url}}", map[string]int{"order_id": 128, "price": 32, "settlement_rules": 96, "order_url": 768}),
		replyDefinition("order.status_usage", "任务用法", "order", "请使用 /任务 订单号。", map[string]int{}),
		replyDefinition("order.status", "任务状态", "order", "订单 {{order_id}}：任务 {{order_status}}；资金 {{funds_status}}；投递 {{delivery_status}}。\n{{private_rules}}{{order_url}}", map[string]int{"order_id": 128, "order_status": 64, "funds_status": 64, "delivery_status": 64, "private_rules": 96, "order_url": 768}),
		replyDefinition("order.redelivery_usage", "重发用法", "order", "请使用 /重发 订单号。", map[string]int{}),
		replyDefinition("order.redelivery", "重发受理", "order", "正在重发订单 {{order_id}} 的已有图片，{{redelivery_rules}}若 QQ 无法投递，请到原网站领取：{{order_url}}", map[string]int{"order_id": 128, "redelivery_rules": 96, "order_url": 768}),
	}
	for _, entry := range []struct{ key, label string }{
		{"insufficient_balance", "余额不足"}, {"limit", "请求或额度限制"}, {"expired", "申请或图片过期"}, {"disabled", "入口未开放"}, {"conflict", "状态冲突"}, {"forbidden", "未获授权"}, {"invalid", "指令无效"}, {"unavailable", "服务暂不可用"},
	} {
		c = append(c, replyDefinition("error."+entry.key, entry.label, "error", "{{error_rules}}", map[string]int{"error_rules": 256}))
	}
	return c
}

func replyDefinitionFor(key string) (ReplyTemplateDefinition, bool) {
	for _, def := range ReplyTemplateCatalog() {
		if def.Key == key {
			return def, true
		}
	}
	return ReplyTemplateDefinition{}, false
}

func replyUTF16Length(text string) int {
	n := 0
	for _, r := range text {
		n++
		if r > 0xffff {
			n++
		}
	}
	return n
}
func replyUnsafeText(text string) bool {
	if !utf8.ValidString(text) {
		return true
	}
	for _, r := range text {
		if (unicode.IsControl(r) && r != '\n') || (unicode.Is(unicode.Cf, r) && r != '\u200c' && r != '\u200d') {
			return true
		}
	}
	return false
}

func replyBlankText(text string) bool {
	return strings.TrimSpace(strings.NewReplacer("\u200c", "", "\u200d", "").Replace(text)) == ""
}

// ValidateReplyTemplate returns controlled diagnostics, never interpolated input.
func ValidateReplyTemplate(key, text string) (string, int, []string) {
	text = strings.ReplaceAll(text, "\r\n", "\n")
	errs := []string{}
	def, ok := replyDefinitionFor(key)
	if !ok {
		return text, 0, []string{"unknown_template"}
	}
	if replyBlankText(text) {
		errs = append(errs, "empty_template")
	}
	if replyUnsafeText(text) {
		errs = append(errs, "invalid_control_character")
	}
	worst := replyUTF16Length(text)
	for _, match := range replyVariablePattern.FindAllStringSubmatch(text, -1) {
		name := match[1]
		limit, allowed := def.VariableLimits[name]
		if !allowed {
			errs = append(errs, "unknown_variable")
		} else {
			worst += limit - replyUTF16Length(match[0])
		}
	}
	remainder := replyVariablePattern.ReplaceAllString(text, "")
	if strings.ContainsAny(remainder, "{}") {
		errs = append(errs, "malformed_placeholder")
	}
	if worst > ReplyTemplateMaxLength {
		errs = append(errs, "expansion_limit_exceeded")
	}
	return text, worst, errs
}

type ReplyTemplateValidationError struct{ Errors []string }

func (e *ReplyTemplateValidationError) Error() string { return "invalid_reply_templates" }
func (e *ReplyTemplateValidationError) Unwrap() error { return ErrInvalid }
func ValidateReplyTemplateOverrides(overrides map[string]string) (map[string]string, error) {
	clean := map[string]string{}
	errs := []string{}
	keys := make([]string, 0, len(overrides))
	for key := range overrides {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		text, _, issues := ValidateReplyTemplate(key, overrides[key])
		if len(issues) > 0 {
			if _, ok := replyDefinitionFor(key); !ok {
				errs = append(errs, "unknown_template")
			} else {
				for _, issue := range issues {
					errs = append(errs, key+":"+issue)
				}
			}
		} else {
			clean[key] = text
		}
	}
	if len(errs) > 0 {
		return nil, &ReplyTemplateValidationError{Errors: errs}
	}
	return clean, nil
}

type ReplyTemplatePreview struct {
	Valid       bool     `json:"valid"`
	Text        string   `json:"text"`
	Length      int      `json:"length"`
	WorstLength int      `json:"worst_length"`
	MaxLength   int      `json:"max_length"`
	Errors      []string `json:"errors"`
}

func PreviewReplyTemplate(key, text string) ReplyTemplatePreview {
	normalized, worst, errs := ValidateReplyTemplate(key, text)
	p := ReplyTemplatePreview{Valid: len(errs) == 0, Text: "", WorstLength: worst, MaxLength: ReplyTemplateMaxLength, Errors: errs}
	if !p.Valid {
		return p
	}
	def, _ := replyDefinitionFor(key)
	vars := map[string]string{}
	for _, name := range def.AllowedVariables {
		vars[name] = "示例"
	}
	for name, value := range replyExampleVariables(key) {
		vars[name] = value
	}
	rendered, valid := renderReplyText(def, normalized, vars)
	if !valid {
		p.Valid = false
		if replyBlankText(rendered) {
			p.Errors = []string{"empty_rendered_text"}
		} else {
			p.Errors = []string{"invalid_rendered_text"}
		}
		return p
	}
	p.Text = rendered
	p.Length = replyUTF16Length(p.Text)
	return p
}

func replyExampleVariables(key string) map[string]string {
	v := replyCommandVariables(&RuntimeConfig{Config: Config{Enabled: true, Model: "示例模型", Size: "1024x1024", Price: "1.00000000"}})
	for name, value := range map[string]string{"group_names": "月卡分组、按次分组", "iq_commands": "@机器人 /智商 分组名", "image_commands": "/绑定、/确认绑定 核对码、/生图 提示词、/任务 订单号、/重发 订单号。", "usage_rules": "请按菜单指令操作，授权和扣费以网站规则为准。", "group_title": "月卡分组", "captured_at": "2026-01-01 12:00:00", "timezone": "Asia/Shanghai", "schedule_rules": "本站按固定计划更新。", "query_rules": "可稍后重新查询。", "delivery_rules": "未知送达结果不自动重复发送。", "binding_url": "https://example.invalid/qq/bind?request=example-request", "request_id": "example-request", "order_id": "example-order", "price": "1.00000000", "order_status": "succeeded", "funds_status": "captured", "delivery_status": "site_only", "order_url": "https://example.invalid/qq/orders/example-order", "error_rules": "示例规则：请登录原网站检查；请勿重复提交生图。"} {
		v[name] = value
	}
	// Match each live context's program-controlled facts using only fake data.
	v["timezone"] = "北京时间"
	v["schedule_rules"] = "自动检测每30分钟一次，北京时间02:00–07:00暂停。"
	v["query_rules"] = "不会额外发起模型检测。"
	v["delivery_rules"] = "暂未能确认截图或查询结果是否送达。"
	if key == "iq.caption" {
		v["query_rules"] = "查询已有结果，"
	}
	if key == "common.menu" {
		v["group_names"] = "• 月卡分组\n• 按次分组"
		v["iq_commands"] = "/智商 分组名称\n示例：/智商 月卡分组"
		v["image_commands"] = "/绑定\n/确认绑定 核对码\n/生图 提示词\n/任务 订单号\n/重发 订单号\n/生图帮助 — 查看模型、规格和价格"
		v["usage_rules"] = "群里先真正@机器人，再发送指令；生图需管理员授权本群并绑定网站账号，按站内报价使用余额。"
	}
	if key == "image.help" {
		v["image_commands"] = "生图指令：/绑定、/确认绑定 核对码、/生图 提示词、/任务 订单号、/重发 订单号。"
		v["settlement_rules"] = "图片在原站可领取时结算；"
		v["redelivery_rules"] = "重发已有图片不再扣费。"
	}
	if key == "binding.usage" {
		v["binding_rules"] = "请勿替他人授权。"
	}
	return v
}

func renderReplyText(def ReplyTemplateDefinition, text string, variables map[string]string) (string, bool) {
	ok := true
	out := replyVariablePattern.ReplaceAllStringFunc(text, func(match string) string {
		name := match[2 : len(match)-2]
		value, exists := variables[name]
		if !exists || replyUnsafeText(value) || replyUTF16Length(value) > def.VariableLimits[name] {
			ok = false
			return ""
		}
		return value
	})
	return out, ok && !replyBlankText(out) && replyUTF16Length(out) <= ReplyTemplateMaxLength
}

// RenderReplyTemplate evaluates placeholders once. Values are never parsed as
// templates; a corrupt revision or an oversized legacy config uses a safe reply.
func RenderReplyTemplate(overrides map[string]string, key string, variables map[string]string) string {
	def, ok := replyDefinitionFor(key)
	if !ok {
		return "请登录原网站查看当前状态；请勿重复提交生图。"
	}
	text := def.DefaultText
	if custom, exists := overrides[key]; exists {
		if normalized, _, errs := ValidateReplyTemplate(key, custom); len(errs) == 0 {
			text = normalized
		}
	}
	if out, ok := renderReplyText(def, text, variables); ok {
		return out
	}
	if out, ok := renderReplyText(def, def.DefaultText, variables); ok {
		return out
	}
	return replySafeFallback(key, variables)
}

func replySafeFallback(key string, v map[string]string) string {
	safe := func(name string) string {
		value := v[name]
		if replyUTF16Length(value) <= 128 && !replyUnsafeText(value) {
			return value
		}
		return ""
	}
	switch key {
	case "binding.request", "binding.manual":
		return "绑定申请编号：" + safe("request_id") + "。请手动打开原网站的 /qq/bind 页面，登录后输入此编号并确认本人授权，再回原群发送 /确认绑定 核对码。请勿替他人授权。"
	case "order.accepted":
		return "订单 " + safe("order_id") + " 已受理，已预留 US$" + safe("price") + "。图片在原站可领取时结算。请登录原网站查看订单；请勿重复提交生图。"
	case "order.status":
		return "订单 " + safe("order_id") + "：任务 " + safe("order_status") + "；资金 " + safe("funds_status") + "；投递 " + safe("delivery_status") + "。仅本人登录原网站查看和领取。"
	case "order.redelivery":
		return "正在重发订单 " + safe("order_id") + " 的已有图片，不会重新生成或扣费。若 QQ 无法投递，请本人登录原网站领取。"
	case "image.help":
		return "生图指令：/绑定、/确认绑定 核对码、/生图 提示词、/任务 订单号、/重发 订单号。\n模型、规格和价格请在原网站查看。需本群获授权并完成网站与 QQ 双端绑定。图片在原站可领取时结算；群图片会被群成员看到，重发已有图片不再扣费。" + safe("availability")
	default:
		if strings.HasPrefix(key, "iq.") || key == "common.menu" {
			return "查询信息暂时不可用，请稍后重试；送达结果未知时不会自动重复发送。"
		}
		return "请登录原网站检查本人申请或订单；请勿重复提交生图。"
	}
}
