package service

import (
	"context"
	"strings"
)

type openAIRequestModelResolverKey struct{}
type openAIRequestTransportKey struct{}
type openAIMessagesModelRoutingKey struct{}
type openAIMessagesModelRouting struct {
	model         string
	dispatchModel string
}
type openAIRequestModelResolver func(*Account, string, bool) string

// WithOpenAINativeModelMapping selects the mapping used by Messages and Chat
// conversion. These paths apply account mappings even when Responses HTTP
// passthrough is enabled on the same account.
func WithOpenAINativeModelMapping(ctx context.Context) context.Context {
	return context.WithValue(ctx, openAIRequestModelResolverKey{}, openAIRequestModelResolver(resolveOpenAINativeRequestModel))
}

// WithOpenAIMessagesModelRouting preserves the channel input separately from
// the group dispatch fallback. Account mappings have first refusal on that
// input; a selected fallback is already a wire model and is not mapped again.
func WithOpenAIMessagesModelRouting(ctx context.Context, model, dispatchModel string) context.Context {
	dispatchModel = strings.TrimSpace(dispatchModel)
	if dispatchModel == "" {
		return WithOpenAINativeModelMapping(ctx)
	}
	routing := openAIMessagesModelRouting{model: NormalizeOpenAICompatRequestedModel(model), dispatchModel: dispatchModel}
	ctx = context.WithValue(ctx, openAIMessagesModelRoutingKey{}, routing)
	resolver := openAIRequestModelResolver(func(account *Account, _ string, _ bool) string {
		return normalizeOpenAIModelForUpstream(account, resolveOpenAIForwardModel(account, routing.model, routing.dispatchModel))
	})
	return context.WithValue(ctx, openAIRequestModelResolverKey{}, resolver)
}

func resolveOpenAINativeRequestModel(account *Account, model string, _ bool) string {
	return normalizeOpenAIModelForUpstream(account, resolveOpenAIForwardModel(account, model, ""))
}

// WithOpenAIWSModelRouting mirrors ProxyResponsesWebSocket's mode choice.
// Native WS and the HTTP bridge share account mapping; passthrough frames do
// not. The independent Responses HTTP passthrough switch has no effect here.
func (s *OpenAIGatewayService) WithOpenAIWSModelRouting(ctx context.Context, firstMessage []byte) context.Context {
	bridgeFirstMessage := s.shouldBridgeOpenAIWSPassthroughFirstMessage(nil, firstMessage)
	resolver := openAIRequestModelResolver(func(account *Account, model string, compact bool) string {
		forceBridge := account != nil && (account.IsGrok() ||
			(s.pluginManager != nil && s.pluginManager.ShouldRouteOpenAIOAuth(account)))
		if !forceBridge && s.cfg != nil && s.cfg.Gateway.OpenAIWS.ModeRouterV2Enabled && account != nil &&
			account.ResolveOpenAIResponsesWebSocketV2Mode(s.cfg.Gateway.OpenAIWS.IngressModeDefault) == OpenAIWSIngressModePassthrough && !bridgeFirstMessage {
			return strings.TrimSpace(model)
		}
		return resolveOpenAINativeRequestModel(account, model, compact)
	})
	ctx = context.WithValue(ctx, openAIRequestTransportKey{}, OpenAIUpstreamTransportResponsesWebsocketV2Ingress)
	return context.WithValue(ctx, openAIRequestModelResolverKey{}, resolver)
}

func (s *OpenAIGatewayService) withOpenAISelectionModelRouting(ctx context.Context, transport OpenAIUpstreamTransport) context.Context {
	if s == nil || (transport != OpenAIUpstreamTransportResponsesWebsocketV2Ingress && transport != OpenAIUpstreamTransportResponsesWebsocketV2) {
		return ctx
	}
	if _, ok := ctx.Value(openAIRequestModelResolverKey{}).(openAIRequestModelResolver); !ok {
		ctx = s.WithOpenAIWSModelRouting(ctx, nil)
	}
	if current, _ := ctx.Value(openAIRequestTransportKey{}).(OpenAIUpstreamTransport); current == transport {
		return ctx
	}
	return context.WithValue(ctx, openAIRequestTransportKey{}, transport)
}

func resolveOpenAIRequestModelWithContext(ctx context.Context, account *Account, model string, compact bool) string {
	if ctx != nil {
		if resolver, ok := ctx.Value(openAIRequestModelResolverKey{}).(openAIRequestModelResolver); ok {
			return resolver(account, model, compact)
		}
	}
	return resolveOpenAIAccountUpstreamModelForRequest(account, model, compact)
}
