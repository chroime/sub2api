package routes

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/handler"
	"github.com/Wei-Shaw/sub2api/internal/pkg/requestmodel"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type inputRoutingPolicyRouteErrorRepo struct{ compositeRouteRepoStub }

func (inputRoutingPolicyRouteErrorRepo) ListByGroup(context.Context, int64, bool) ([]service.CompositeModelRoute, error) {
	return nil, errors.New("policy route unavailable")
}

func TestGroupInputRoutingReasoningCompositeSourceAliasDeniedWithoutChangingScope(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, endpoint := range []struct{ path, protocol, body string }{
		{"/v1/messages", service.CompositeRouteEndpointMessages, `{"model":"public-alias","max_tokens":32,"messages":[{"role":"user","content":"hello"}],"output_config":{"effort":"high"}}`},
		{"/v1/chat/completions", service.CompositeRouteEndpointChatCompletions, `{"model":"public-alias","messages":[{"role":"user","content":"hello"}],"reasoning_effort":"high"}`},
		{"/v1/responses", service.CompositeRouteEndpointResponses, `{"model":"public-alias","input":"hello","reasoning":{"effort":"high"}}`},
	} {
		t.Run(endpoint.protocol, func(t *testing.T) {
			group := &service.Group{ID: 7, Platform: service.PlatformComposite, AllowMessagesDispatch: true,
				ReasoningEffortMappings: []service.ReasoningEffortMapping{{From: "high", To: service.ReasoningEffortMappingDeny, Model: "source-policy-model", MatchType: "exact"}},
				InputRouting:            service.GroupInputRoutingConfig{Enabled: true, Rules: []service.GroupInputRoutingRule{{ID: "small", Enabled: true, RequestType: "all", MaxInputTokens: 1000, AccountIDs: []int64{99}}}},
			}
			key := &service.APIKey{ID: 11, UserID: 13, Group: group, GroupID: &group.ID, User: &service.User{ID: 13}}
			resolver := service.NewCompositeRouteResolver(compositeRouteRepoStub{routes: []service.CompositeModelRoute{
				{ID: 1, GroupID: 7, PublicModel: "public-alias", MatchType: service.CompositeRouteMatchExact, TargetPlatform: service.PlatformGrok, UpstreamModel: "wrong-endpoint-model", Endpoint: service.CompositeRouteEndpointAny, Enabled: true},
				{ID: 2, GroupID: 7, PublicModel: "public-alias", MatchType: service.CompositeRouteMatchExact, TargetPlatform: service.PlatformOpenAI, UpstreamModel: "source-policy-model", Endpoint: endpoint.protocol, Enabled: true},
			}})
			r := gin.New()
			r.Use(func(c *gin.Context) {
				c.Set(string(middleware.ContextKeyAPIKey), key)
				c.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: 13, Concurrency: 1})
				c.Next()
			})
			r.Use((&handler.GatewayHandler{}).GroupInputRoutingMiddleware(), compositeTargetPlatformMiddleware(resolver))
			r.POST(endpoint.path, func(c *gin.Context) {
				body, err := io.ReadAll(c.Request.Body)
				require.NoError(t, err)
				require.Equal(t, endpoint.body, string(body))
				requestmodel.ResetRequestBody(c.Request, body)
				_, resolved := service.ResolvedTargetPlatformFromContext(c.Request.Context())
				require.False(t, resolved)
				require.Empty(t, service.QuotaPlatform(c.Request.Context(), key))
				h := &handler.GatewayHandler{}
				require.NotPanics(t, func() {
					switch endpoint.protocol {
					case service.CompositeRouteEndpointMessages:
						h.Messages(c)
					case service.CompositeRouteEndpointChatCompletions:
						h.ChatCompletions(c)
					default:
						h.Responses(c)
					}
				})
			})
			rec := httptest.NewRecorder()
			r.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, endpoint.path, strings.NewReader(endpoint.body)))
			require.Equal(t, http.StatusForbidden, rec.Code)
			require.Contains(t, rec.Body.String(), "mapping policy")
		})
	}
}

func TestGroupInputRoutingReasoningCompositeLookupFailureDoesNotBypassPolicy(t *testing.T) {
	for _, lookupFails := range []bool{false, true} {
		t.Run(map[bool]string{false: "unknown-model-keeps-input-route", true: "repository-error-fails-closed"}[lookupFails], func(t *testing.T) {
			group := &service.Group{ID: 7, Platform: service.PlatformComposite, MaxReasoningEffort: "low",
				InputRouting: service.GroupInputRoutingConfig{Enabled: true, Rules: []service.GroupInputRoutingRule{{ID: "small", Enabled: true, RequestType: "all", MaxInputTokens: 1000, AccountIDs: []int64{99}}}},
			}
			var repo service.CompositeModelRouteRepository = compositeRouteRepoStub{}
			if lookupFails {
				repo = inputRoutingPolicyRouteErrorRepo{}
			}
			r := gin.New()
			r.Use(func(c *gin.Context) {
				c.Set(string(middleware.ContextKeyAPIKey), &service.APIKey{Group: group, GroupID: &group.ID})
				c.Next()
			})
			r.Use((&handler.GatewayHandler{}).GroupInputRoutingMiddleware(), compositeTargetPlatformMiddleware(service.NewCompositeRouteResolver(repo)))
			reached := false
			r.POST("/v1/responses", func(c *gin.Context) {
				reached = true
				require.True(t, handler.UseGroupInputRoutingGateway(c))
				_, resolved := service.ResolvedTargetPlatformFromContext(c.Request.Context())
				require.False(t, resolved)
				c.Status(http.StatusNoContent)
			})
			rec := httptest.NewRecorder()
			r.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(`{"model":"target-only-alias","input":"hello"}`)))
			if lookupFails {
				require.False(t, reached)
				require.Equal(t, http.StatusInternalServerError, rec.Code)
			} else {
				require.True(t, reached)
				require.Equal(t, http.StatusNoContent, rec.Code)
			}
		})
	}
}
