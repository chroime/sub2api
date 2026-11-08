package handler

import (
	"context"
	"errors"
	"net/http"

	"github.com/Wei-Shaw/sub2api/internal/domain"
	middleware "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

type groupInputRoutingAnthropicForwarder interface {
	Forward(context.Context, *gin.Context, *service.Account, *service.ParsedRequest) (*service.ForwardResult, error)
	ForwardAsChatCompletions(context.Context, *gin.Context, *service.Account, []byte, *service.ParsedRequest) (*service.ForwardResult, error)
	ForwardAsResponses(context.Context, *gin.Context, *service.Account, []byte, *service.ParsedRequest) (*service.ForwardResult, error)
}

type groupInputRoutingOpenAIForwarder interface {
	Forward(context.Context, *gin.Context, *service.Account, []byte) (*service.OpenAIForwardResult, error)
	ForwardAsAnthropic(context.Context, *gin.Context, *service.Account, []byte, string, string) (*service.OpenAIForwardResult, error)
	ForwardAsChatCompletions(context.Context, *gin.Context, *service.Account, []byte, string, string) (*service.OpenAIForwardResult, error)
}

type groupInputRoutingGeminiForwarder interface {
	Forward(context.Context, *gin.Context, *service.Account, []byte) (*service.ForwardResult, error)
	ForwardAsChatCompletions(context.Context, *gin.Context, *service.Account, []byte) (*service.ForwardResult, error)
}

type groupInputRoutingAntigravityForwarder interface {
	Forward(context.Context, *gin.Context, *service.Account, []byte, bool) (*service.ForwardResult, error)
	ForwardAsChatCompletions(context.Context, *gin.Context, *service.Account, []byte, *service.ParsedRequest) (*service.ForwardResult, error)
	ForwardAsResponses(context.Context, *gin.Context, *service.Account, []byte, *service.ParsedRequest) (*service.ForwardResult, error)
}

type groupInputRoutingForwardServices struct {
	anthropic   groupInputRoutingAnthropicForwarder
	openAI      groupInputRoutingOpenAIForwarder
	gemini      groupInputRoutingGeminiForwarder
	antigravity groupInputRoutingAntigravityForwarder
}

type groupInputRoutingForwardError struct{ message string }

func (e *groupInputRoutingForwardError) Error() string { return e.message }

// Only matched input-routing requests use this dispatcher. The selected account
// determines the upstream adapter; the source API key and billing context stay
// attached to the request throughout every attempt and usage recording.
func (h *GatewayHandler) forwardGroupInputRouting(ctx context.Context, c *gin.Context, account *service.Account, protocol string, body []byte, parsed *service.ParsedRequest, sessionKey string, sticky bool) (*service.ForwardResult, error) {
	services := groupInputRoutingForwardServices{}
	// Avoid typed nil interfaces so a missing adapter fails before invoking it.
	if h.gatewayService != nil {
		services.anthropic = h.gatewayService
	}
	if h.openAIGatewayService != nil {
		services.openAI = h.openAIGatewayService
	}
	if h.geminiCompatService != nil {
		services.gemini = h.geminiCompatService
	}
	if h.antigravityGatewayService != nil {
		services.antigravity = h.antigravityGatewayService
	}
	result, err := services.forward(ctx, c, account, protocol, body, parsed, sessionKey, sticky)
	if service.IsReasoningEffortPolicyDenied(err) {
		switch protocol {
		case service.CompositeRouteEndpointResponses:
			respondOpenAIReasoningEffortPolicyError(c, err, h.responsesErrorResponse)
		case service.CompositeRouteEndpointChatCompletions:
			respondOpenAIReasoningEffortPolicyError(c, err, h.chatCompletionsErrorResponse)
		default:
			respondOpenAIReasoningEffortPolicyError(c, err, func(c *gin.Context, status int, code, message string) {
				h.handleStreamingAwareError(c, status, code, message, gatewaySSEAlreadyStarted(c))
			})
		}
		return result, err
	}
	var unavailable *groupInputRoutingForwardError
	if errors.As(err, &unavailable) {
		switch protocol {
		case service.CompositeRouteEndpointResponses:
			h.responsesErrorResponse(c, http.StatusBadGateway, "upstream_error", unavailable.Error())
		case service.CompositeRouteEndpointChatCompletions:
			h.chatCompletionsErrorResponse(c, http.StatusBadGateway, "upstream_error", unavailable.Error())
		default:
			h.handleStreamingAwareError(c, http.StatusBadGateway, "upstream_error", unavailable.Error(), gatewaySSEAlreadyStarted(c))
		}
	}
	return result, err
}

func (s groupInputRoutingForwardServices) forward(ctx context.Context, c *gin.Context, account *service.Account, protocol string, body []byte, parsed *service.ParsedRequest, sessionKey string, sticky bool) (result *service.ForwardResult, err error) {
	policyChanged := false
	defer func() {
		if err == nil && result == nil {
			err = &groupInputRoutingForwardError{message: "Selected gateway returned no result"}
		}
		if result != nil && c != nil && c.Request != nil {
			stampForwardRequestedReasoningEffort(result, service.RequestedReasoningEffortFromContext(c.Request.Context()))
			// The generic Messages forwarder relies on its handler to fill this
			// field. A deferred policy used an attempt copy of parsed, so retain
			// that forwarded effort before the outer handler sees its template.
			if policyChanged && protocol == service.CompositeRouteEndpointMessages && parsed != nil && result.ReasoningEffort == nil {
				result.ReasoningEffort = service.NormalizeClaudeOutputEffort(parsed.OutputEffort)
			}
		}
	}()
	if account == nil {
		return nil, &groupInputRoutingForwardError{message: "No account was selected for input routing"}
	}
	switch protocol {
	case service.CompositeRouteEndpointMessages, service.CompositeRouteEndpointChatCompletions, service.CompositeRouteEndpointResponses:
	default:
		return nil, &groupInputRoutingForwardError{message: "Input routing does not support this request protocol"}
	}
	if apiKey, ok := middleware.GetAPIKeyFromContext(c); ok {
		policyBody, changed, policyErr := applyGroupInputRoutingReasoningEffortPolicy(c, apiKey, body, account.Platform)
		if policyErr != nil {
			return nil, policyErr
		}
		if changed {
			policyChanged = true
			body = policyBody
			if parsed != nil {
				attempt, cloneErr := parsed.CloneForBody(body)
				if cloneErr != nil {
					return nil, &groupInputRoutingForwardError{message: "Failed to apply reasoning effort policy"}
				}
				attempt.OnUpstreamAccepted = parsed.OnUpstreamAccepted
				parsed = attempt
			}
		}
	}
	setActualUpstreamEndpoint(c, "")
	service.ClearActualOpenAIUpstreamEndpoint(c)
	switch {
	case domain.UsesOpenAIGateway(account.Platform):
		if s.openAI == nil {
			return nil, &groupInputRoutingForwardError{message: "OpenAI compatibility service is not configured"}
		}
		var native *service.OpenAIForwardResult
		switch protocol {
		case service.CompositeRouteEndpointMessages:
			native, err = s.openAI.ForwardAsAnthropic(ctx, c, account, body, sessionKey, "")
		case service.CompositeRouteEndpointChatCompletions:
			native, err = s.openAI.ForwardAsChatCompletions(ctx, c, account, body, sessionKey, "")
		case service.CompositeRouteEndpointResponses:
			native, err = s.openAI.Forward(ctx, c, account, body)
		}
		setActualUpstreamEndpoint(c, resolveOpenAIUpstreamEndpoint(c, account, native))
		return service.WrapGroupInputRoutingOpenAIResult(native), err
	case account.Platform == service.PlatformGemini:
		if protocol == service.CompositeRouteEndpointResponses {
			return nil, &groupInputRoutingForwardError{message: "Gemini accounts do not support Responses input routing"}
		}
		if s.gemini == nil {
			return nil, &groupInputRoutingForwardError{message: "Gemini compatibility service is not configured"}
		}
		if protocol == service.CompositeRouteEndpointMessages {
			return s.gemini.Forward(ctx, c, account, body)
		}
		return s.gemini.ForwardAsChatCompletions(ctx, c, account, body)
	case shouldUseAntigravityCompat(account) || (protocol == service.CompositeRouteEndpointMessages && account.Platform == service.PlatformAntigravity && account.Type != service.AccountTypeAPIKey):
		if s.antigravity == nil {
			return nil, &groupInputRoutingForwardError{message: "Antigravity compatibility service is not configured"}
		}
		setActualUpstreamEndpoint(c, EndpointAntigravityGenerateContent)
		switch protocol {
		case service.CompositeRouteEndpointMessages:
			return s.antigravity.Forward(ctx, c, account, body, sticky)
		case service.CompositeRouteEndpointChatCompletions:
			return s.antigravity.ForwardAsChatCompletions(ctx, c, account, body, parsed)
		case service.CompositeRouteEndpointResponses:
			return s.antigravity.ForwardAsResponses(ctx, c, account, body, parsed)
		}
	case account.Platform == service.PlatformAnthropic || (account.Platform == service.PlatformAntigravity && account.Type == service.AccountTypeAPIKey):
		if s.anthropic == nil {
			return nil, &groupInputRoutingForwardError{message: "Anthropic compatibility service is not configured"}
		}
		switch protocol {
		case service.CompositeRouteEndpointMessages:
			if parsed == nil {
				return nil, &groupInputRoutingForwardError{message: "Parsed Messages request is missing"}
			}
			// Preserve the attempt's acceptance callback so the actual Anthropic
			// account releases its message queue before the full stream completes.
			return s.anthropic.Forward(ctx, c, account, parsed)
		case service.CompositeRouteEndpointChatCompletions:
			return s.anthropic.ForwardAsChatCompletions(ctx, c, account, body, parsed)
		case service.CompositeRouteEndpointResponses:
			return s.anthropic.ForwardAsResponses(ctx, c, account, body, parsed)
		}
	}
	return nil, &groupInputRoutingForwardError{message: "Selected account does not support this request protocol"}
}
