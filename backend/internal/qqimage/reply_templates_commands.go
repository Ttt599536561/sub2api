package qqimage

import (
	"errors"
	"strings"
)

func replyTemplateCommandError(overrides map[string]string, err error) (string, string, error) {
	text, _, failure := qqCommandExecutionError(err)
	if failure != nil {
		return "", "", failure
	}
	key := "unavailable"
	for _, entry := range []struct {
		err error
		key string
	}{
		{ErrInsufficientBalance, "insufficient_balance"}, {ErrLimit, "limit"}, {ErrExpired, "expired"}, {ErrDisabled, "disabled"}, {ErrConflict, "conflict"}, {ErrForbidden, "forbidden"}, {ErrNotFound, "forbidden"}, {ErrInvalid, "invalid"},
	} {
		if errors.Is(err, entry.err) {
			key = entry.key
			break
		}
	}
	return RenderReplyTemplate(overrides, "error."+key, map[string]string{"error_rules": text}), "", nil
}

func replyCommandVariables(config *RuntimeConfig) map[string]string {
	v := map[string]string{
		"image_commands": "生图指令：/绑定、/确认绑定 核对码、/生图 提示词、/任务 订单号、/重发 订单号。",
		"availability":   "", "model_details": "", "price_details": "",
		"authorization_rules": "需本群获授权并完成网站与 QQ 双端绑定。",
		"settlement_rules":    "图片在原站可领取时结算。",
		"visibility_rules":    "群图片会被群成员看到，",
		"redelivery_rules":    "不会重新生成或扣费。",
		"binding_rules":       "请手动打开原网站的 /qq/bind 页面，登录后输入此编号并确认本人授权，再回原群发送 /确认绑定 核对码。请勿替他人授权。",
		"confirmation_rules":  "请使用 /确认绑定 核对码；核对码仅从本人已登录的原网站获取。",
		"success_rules":       "绑定成功。此群身份现可使用原网站账号余额生图；可在原网站管理或撤销授权。",
		"private_rules":       "仅本人登录后领取：",
	}
	if !config.Enabled {
		v["availability"] = "生图暂未开放。\n"
	}
	details := []string{}
	if config.Model != "" {
		details = append(details, "固定模型 "+config.Model)
	}
	if config.Size != "" {
		details = append(details, "规格 "+config.Size)
	}
	if len(details) > 0 {
		v["model_details"] = strings.Join(details, "，")
		if config.Price != "" {
			v["model_details"] += "，"
		} else {
			v["model_details"] += "。\n"
		}
	}
	if config.Price != "" {
		v["price_details"] = "每张 US$" + config.Price + "。\n"
	}
	return v
}
