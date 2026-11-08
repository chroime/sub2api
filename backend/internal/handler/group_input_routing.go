package handler

import (
	"context"
	"net/http"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/pkg/ctxkey"
	pkghttputil "github.com/Wei-Shaw/sub2api/internal/pkg/httputil"
	"github.com/Wei-Shaw/sub2api/internal/pkg/requestmodel"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
)

type groupInputRoutingEvaluatedKey struct{}

// GroupInputRoutingMiddleware resolves the input rule before choosing a provider
// handler. Authentication and model allowlist admission still use the source key.
func (h *GatewayHandler) GroupInputRoutingMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		apiKey, ok := middleware.GetAPIKeyFromContext(c)
		if !ok || apiKey == nil || apiKey.Group == nil || !apiKey.Group.InputRouting.Enabled || c.Request.Method != http.MethodPost {
			c.Next()
			return
		}
		protocol, model, stream := inputRoutingIngress(c)
		if protocol == "" {
			c.Next()
			return
		}
		body, err := pkghttputil.ReadRequestBodyWithPrealloc(c.Request)
		if err != nil {
			if !middleware.RejectRequestBodyReadError(c, "middleware.input_routing", err) {
				middleware.AbortWithRequestBodyReadFailure(c, "middleware.input_routing", err)
			}
			return
		}
		requestmodel.ResetRequestBody(c.Request, body)
		if protocol != "gemini" {
			model = gjson.GetBytes(body, "model").String()
			var valid bool
			stream, valid = parseOpenAICompatibleStream(body)
			if !valid {
				c.Request = c.Request.WithContext(context.WithValue(c.Request.Context(), groupInputRoutingEvaluatedKey{}, true))
				c.Next() // The protocol handler reports malformed stream fields.
				return
			}
		}
		bindGroupInputRouting(c, apiKey, body, protocol, model, stream)
		c.Request = c.Request.WithContext(context.WithValue(c.Request.Context(), groupInputRoutingEvaluatedKey{}, true))
		c.Next()
	}
}

func inputRoutingIngress(c *gin.Context) (protocol, model string, stream bool) {
	if c == nil || c.Request == nil || c.Request.Method != http.MethodPost {
		return "", "", false
	}
	path := strings.TrimRight(c.Request.URL.Path, "/")
	switch {
	case strings.HasSuffix(path, "/messages"):
		return "messages", "", false
	case strings.HasSuffix(path, "/chat/completions"):
		return "chat", "", false
	case strings.HasSuffix(path, "/responses"):
		return "responses", "", false
	case strings.Contains(path, "/v1beta/models/"):
		model, action, err := parseGeminiModelAction(strings.TrimPrefix(c.Param("modelAction"), "/"))
		if err == nil && (action == "generateContent" || action == "streamGenerateContent") {
			return "gemini", model, action == "streamGenerateContent"
		}
	}
	return "", "", false
}

// UseGroupInputRoutingGateway routes a matched text request through the common
// handler, which dispatches the selected account to its native forwarder.
func UseGroupInputRoutingGateway(c *gin.Context) bool {
	if c == nil || c.Request == nil {
		return false
	}
	_, matched := service.GroupInputRoutingDecisionFromContext(c.Request.Context())
	protocol, _, _ := inputRoutingIngress(c)
	return matched && (protocol == "messages" || protocol == "chat" || protocol == "responses")
}

func hasNativeGeminiInputRouting(c *gin.Context) bool {
	if c == nil || c.Request == nil {
		return false
	}
	_, matched := service.GroupInputRoutingDecisionFromContext(c.Request.Context())
	protocol, _, _ := inputRoutingIngress(c)
	return matched && protocol == "gemini"
}

// bindGroupInputRouting matches the downstream HTTP request before any account
// selection or upstream protocol conversion. The API key and its billing group
// remain unchanged; only request-scoped scheduling constraints are attached.
func bindGroupInputRouting(c *gin.Context, apiKey *service.APIKey, body []byte, protocol, model string, stream bool) {
	if c == nil || c.Request == nil || apiKey == nil || apiKey.Group == nil || !apiKey.Group.InputRouting.Enabled {
		return
	}
	if evaluated, _ := c.Request.Context().Value(groupInputRoutingEvaluatedKey{}).(bool); evaluated {
		return // An unmatched or unknown original input cannot match after rewriting.
	}
	if _, matched := service.GroupInputRoutingDecisionFromContext(c.Request.Context()); matched {
		return // Middleware already captured the original client input before mapping.
	}
	estimated, known := service.EstimateGroupInputRoutingTokens(body, protocol, model)
	if !known {
		estimated = -1
	}
	ctx := service.WithGroupInputRoutingRequest(c.Request.Context(), apiKey.Group, estimated, stream)
	if _, matched := service.GroupInputRoutingDecisionFromContext(ctx); matched {
		if ingress, _, _ := inputRoutingIngress(c); ingress != "" {
			ctx = service.WithGroupInputRoutingProtocol(ctx, ingress)
		}
		if _, exists := service.RequestedPublicModelFromContext(ctx); !exists {
			ctx = context.WithValue(ctx, ctxkey.RequestedPublicModel, model)
		}
	}
	c.Request = c.Request.WithContext(ctx)
}
