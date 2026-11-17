package handler

import (
	"context"
	"math"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
)

// inflightReservationEstimator 估算单请求在途预留金额（USD）；false 表示无法定价。
type inflightReservationEstimator interface {
	EstimateInflightReservation(ctx context.Context, apiKey *service.APIKey, req service.InflightEstimateRequest) (float64, bool)
}

// requestMaxOutputTokens 从请求体中提取输出 token 上限（兼容 Anthropic / OpenAI Chat / Responses / Gemini）。
func requestMaxOutputTokens(body []byte) int {
	for _, path := range []string{"max_tokens", "max_completion_tokens", "max_output_tokens", "generationConfig.maxOutputTokens", "generation_config.max_output_tokens"} {
		if v := gjson.GetBytes(body, path); v.Exists() && v.Type == gjson.Number && v.Int() > 0 {
			return int(v.Int())
		}
	}
	return 0
}

// tokenInflightEstimate 文本类请求的估算输入。
func tokenInflightEstimate(model string, body []byte) service.InflightEstimateRequest {
	return service.InflightEstimateRequest{
		Model:                    model,
		BodyBytes:                len(body),
		MaxTokens:                requestMaxOutputTokens(body),
		Kind:                     service.InflightEstimateToken,
		OpenAICompactionFallback: service.HasCompactionTriggerInInput(body),
	}
}

// Only HTTP Responses uses the image-only model bridge inspected by the estimator.
func responsesInflightEstimate(model string, body []byte) service.InflightEstimateRequest {
	req := tokenInflightEstimate(model, body)
	req.ImageOnlyRequest, req.ImageToolModel = service.OpenAIResponsesImageOnlyEstimate(model, body)
	return req
}

func inflightNoop() {}

// reserveInflightBalance 在 CheckBillingEligibility 之后为现金或补充额度请求登记在途预留。
//
// 成功时把预留句柄挂到 c.Request 的 context 上：之后通过 submit*UsageRecordTask 提交的
// 计费任务会接管一个引用，直到余额缓存实际扣减后才释放。返回的 done 必须 defer 调用
// （停止续期并归还 handler 引用；没有提交计费任务时即刻释放）。
// 现金模式沿用原开关和无法定价策略；补充额度强制检查计费依赖和价格，
// 仅明确免费时不预占，无补额订阅保持原有行为。
func reserveInflightBalance(
	c *gin.Context,
	billing *service.BillingCacheService,
	estimator inflightReservationEstimator,
	apiKey *service.APIKey,
	subscription *service.UserSubscription,
	req service.InflightEstimateRequest,
) (func(), error) {
	if c == nil || c.Request == nil {
		if subscription != nil && subscription.HasSubscriptionSupplement() {
			return inflightNoop, service.ErrBillingServiceUnavailable
		}
		return inflightNoop, nil
	}
	req.OpenAICompactPath = service.IsOpenAIResponsesCompactPath(c)
	ctx, done, err := reserveInflightBalanceCtx(c.Request.Context(), billing, estimator, apiKey, subscription, req)
	if err != nil {
		return inflightNoop, err
	}
	c.Request = c.Request.WithContext(ctx)
	return done, nil
}

// reserveInflightBalanceCtx 同 reserveInflightBalance，但返回携带预留句柄的新 context
// （供 WebSocket 等自管 context 的路径）。
func reserveInflightBalanceCtx(
	ctx context.Context,
	billing *service.BillingCacheService,
	estimator inflightReservationEstimator,
	apiKey *service.APIKey,
	subscription *service.UserSubscription,
	req service.InflightEstimateRequest,
) (context.Context, func(), error) {
	supplement := subscription != nil && subscription.HasSubscriptionSupplement()
	if billing == nil || estimator == nil || apiKey == nil || apiKey.User == nil {
		if supplement {
			return ctx, inflightNoop, service.ErrBillingServiceUnavailable
		}
		return ctx, inflightNoop, nil
	}
	if supplement && apiKey.Group == nil {
		return ctx, inflightNoop, service.ErrBillingServiceUnavailable
	}
	if !billing.InflightReservationEnabled() && !supplement {
		return ctx, inflightNoop, nil
	}
	if apiKey.Group != nil && apiKey.Group.IsSubscriptionType() && subscription != nil && !supplement {
		return ctx, inflightNoop, nil
	}
	estimate, priced := estimator.EstimateInflightReservation(ctx, apiKey, req)
	if supplement && (!priced || estimate < 0 || math.IsNaN(estimate) || math.IsInf(estimate, 0)) {
		return ctx, inflightNoop, service.ErrBillingServiceUnavailable
	}
	if !priced && billing.InflightReservationFailClosedOnUnpriced() {
		return ctx, inflightNoop, service.ErrInsufficientBalance
	}
	if estimate <= 0 {
		return ctx, inflightNoop, nil
	}
	res, err := billing.ReserveInflight(ctx, apiKey.User, apiKey.Group, subscription, estimate)
	if err != nil {
		return ctx, inflightNoop, err
	}
	if res == nil {
		return ctx, inflightNoop, nil
	}
	return service.WithInflightReservation(ctx, res), res.HandlerDone, nil
}

// grokMediaInflightEstimate 媒体生成请求的估算输入；状态/内容查询返回空模型（不预留：
// 查询会为已生成的媒体计费，不能因余额预留而拦截用户取回已付费结果）。
func grokMediaInflightEstimate(endpoint service.GrokMediaEndpoint, model string, info service.GrokMediaRequestInfo, body []byte) service.InflightEstimateRequest {
	if !endpoint.IsGenerationRequest() {
		return service.InflightEstimateRequest{}
	}
	switch endpoint {
	case service.GrokMediaEndpointImagesGenerations, service.GrokMediaEndpointImagesEdits:
		return service.InflightEstimateRequest{Model: model, BodyBytes: len(body), Kind: service.InflightEstimateImage, Units: info.N}
	default:
		return service.InflightEstimateRequest{
			Model:                model,
			BodyBytes:            len(body),
			Kind:                 service.InflightEstimateVideo,
			Units:                1,
			VideoResolution:      info.Resolution,
			VideoDurationSeconds: info.DurationSeconds,
		}
	}
}

// grokVoiceSTTBytesPerSecond STT 时长粗估（~128kbps 压缩音频）。
const grokVoiceSTTBytesPerSecond = 16000

// grokVoiceInflightEstimate 语音 HTTP 接口估算：TTS 按输入字符数（百万字符），STT 按音频字节粗估时长（小时）。
// 其他接口（custom-voices）无音频计量，返回的估算为 0（不预留）。
func grokVoiceInflightEstimate(endpoint string, body []byte) service.InflightEstimateRequest {
	req := service.InflightEstimateRequest{Model: endpoint, Kind: service.InflightEstimateAudio, AudioMode: endpoint}
	switch endpoint {
	case "tts":
		req.AudioUnits = float64(len([]rune(extractGrokTTSInputText(body)))) / 1e6
	case "stt":
		req.AudioUnits = float64(len(body)) / grokVoiceSTTBytesPerSecond / 3600
	}
	return req
}
