package handler

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	middleware "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func inputRoutingReasoningTestGroup(platform string) *service.Group {
	return &service.Group{
		ID: 7, Platform: platform, AllowMessagesDispatch: true,
		MaxReasoningEffort: "low", MaxReasoningEffortOverLimit: service.ReasoningEffortOverLimitDeny,
		InputRouting: service.GroupInputRoutingConfig{Enabled: true, Rules: []service.GroupInputRoutingRule{{
			ID: "small", Enabled: true, RequestType: "all", MaxInputTokens: 1000, AccountIDs: []int64{99},
		}}},
	}
}

func inputRoutingReasoningTestContext(t *testing.T, group *service.Group, protocol, body string) (*gin.Context, *httptest.ResponseRecorder, *service.APIKey) {
	t.Helper()
	path, ingress := "/v1/"+protocol, protocol
	if protocol == service.CompositeRouteEndpointChatCompletions {
		path, ingress = "/v1/chat/completions", "chat"
	}
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")
	key := &service.APIKey{ID: 11, UserID: 13, GroupID: &group.ID, Group: group, User: &service.User{ID: 13}}
	c.Set(string(middleware.ContextKeyAPIKey), key)
	c.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: 13, Concurrency: 1})
	bindGroupInputRouting(c, key, []byte(body), ingress, gjson.Get(body, "model").String(), false)
	_, matched := service.GroupInputRoutingDecisionFromContext(c.Request.Context())
	require.True(t, matched)
	return c, rec, key
}

func TestGroupInputRoutingReasoningDeniedBeforeScheduling(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, platform := range []string{service.PlatformOpenAI, service.PlatformComposite} {
		for _, protocol := range []string{service.CompositeRouteEndpointMessages, service.CompositeRouteEndpointChatCompletions, service.CompositeRouteEndpointResponses} {
			t.Run(platform+"/"+protocol, func(t *testing.T) {
				body := `{"model":"gpt-6.1-sol","max_tokens":32,"messages":[{"role":"user","content":"hello"}],"output_config":{"effort":"high"}}`
				if protocol == service.CompositeRouteEndpointChatCompletions {
					body = `{"model":"gpt-6.1-sol","messages":[{"role":"user","content":"hello"}],"reasoning_effort":"high"}`
				} else if protocol == service.CompositeRouteEndpointResponses {
					body = `{"model":"gpt-6.1-sol","input":"hello","reasoning":{"effort":"high"}}`
				}
				c, rec, key := inputRoutingReasoningTestContext(t, inputRoutingReasoningTestGroup(platform), protocol, body)
				if platform == service.PlatformComposite {
					c.Request = c.Request.WithContext(service.WithResolvedTargetPlatform(c.Request.Context(), service.PlatformOpenAI))
				}
				h := &GatewayHandler{gatewayService: &service.GatewayService{}}
				// Scheduling and billing are deliberately absent: a source policy
				// denial must be returned before either dependency is needed.
				require.NotPanics(t, func() {
					switch protocol {
					case service.CompositeRouteEndpointMessages:
						h.Messages(c)
					case service.CompositeRouteEndpointChatCompletions:
						h.ChatCompletions(c)
					default:
						h.Responses(c)
					}
				})
				require.Equal(t, http.StatusForbidden, rec.Code)
				require.Contains(t, rec.Body.String(), "reasoning effort")
				stored, _ := middleware.GetAPIKeyFromContext(c)
				require.Same(t, key, stored)
			})
		}
	}
}

func TestGroupInputRoutingReasoningUnknownCompositeDeniedBeforeForward(t *testing.T) {
	gin.SetMode(gin.TestMode)
	body := []byte(`{"model":"target-only-alias","input":"hello","reasoning":{"effort":"high"}}`)
	c, _, _ := inputRoutingReasoningTestContext(t, inputRoutingReasoningTestGroup(service.PlatformComposite), service.CompositeRouteEndpointResponses, string(body))
	stub := &inputRoutingForwardStub{openAIResult: &service.OpenAIForwardResult{}}
	result, err := newInputRoutingForwardServices(stub).forward(c.Request.Context(), c, &service.Account{Platform: service.PlatformOpenAI}, service.CompositeRouteEndpointResponses, body, nil, "", false)
	require.True(t, service.IsReasoningEffortPolicyDenied(err), "the selected platform must enforce the source ceiling for an unresolved alias")
	require.Nil(t, result)
	require.Empty(t, stub.calls, "denied requests must not reach an upstream adapter")
	_, resolved := service.ResolvedTargetPlatformFromContext(c.Request.Context())
	require.False(t, resolved, "policy fallback must not assign the routing or billing platform")
}

func TestGroupInputRoutingReasoningRetryAppliesMappingOnce(t *testing.T) {
	gin.SetMode(gin.TestMode)
	group := inputRoutingReasoningTestGroup(service.PlatformComposite)
	group.MaxReasoningEffort = ""
	group.ReasoningEffortMappings = []service.ReasoningEffortMapping{{From: "high", To: "medium"}, {From: "medium", To: "low"}}
	body := []byte(`{"model":"target-only-alias","messages":[{"role":"user","content":"hello"}],"max_tokens":32,"output_config":{"effort":"high"}}`)
	c, _, _ := inputRoutingReasoningTestContext(t, group, service.CompositeRouteEndpointMessages, string(body))
	parsed, err := service.ParseGatewayRequest(service.NewRequestBodyRef(body), service.PlatformAnthropic)
	require.NoError(t, err)
	accepted := 0
	parsed.OnUpstreamAccepted = func() { accepted++ }
	stub := &inputRoutingForwardStub{result: &service.ForwardResult{}}
	for _, accountID := range []int64{99, 100} {
		result, err := newInputRoutingForwardServices(stub).forward(c.Request.Context(), c, &service.Account{ID: accountID, Platform: service.PlatformAnthropic}, service.CompositeRouteEndpointMessages, body, parsed, "", false)
		require.NoError(t, err)
		require.NotNil(t, result.ReasoningEffort)
		require.Equal(t, "medium", *result.ReasoningEffort, "usage must describe the effort actually forwarded")
		require.NotNil(t, result.RequestedReasoningEffort)
		require.Equal(t, "high", *result.RequestedReasoningEffort)
	}
	require.Len(t, stub.calls, 2)
	for _, call := range stub.calls {
		require.Equal(t, "medium", gjson.GetBytes(call.body, "output_config.effort").String())
		require.Equal(t, "medium", call.parsed.OutputEffort)
	}
	require.Equal(t, 2, accepted, "the policy must retain the acceptance callback")
	require.Equal(t, "high", parsed.OutputEffort, "attempt policies must not mutate the retry template")
	require.Equal(t, "high", gjson.GetBytes(body, "output_config.effort").String())
}

func TestGroupInputRoutingReasoningSourceRoutePolicyKeepsForwardModel(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, test := range []struct{ name, source, want string }{
		{"openai", service.PlatformOpenAI, "minimal"},
		{"anthropic", service.PlatformAnthropic, "low"},
		{"grok", service.PlatformGrok, "high"},
	} {
		t.Run(test.name, func(t *testing.T) {
			group := inputRoutingReasoningTestGroup(service.PlatformComposite)
			group.MaxReasoningEffort = ""
			group.ReasoningEffortMappings = []service.ReasoningEffortMapping{{From: "high", To: "minimal", Model: "source-only-model", MatchType: "exact"}}
			body := []byte(`{"model":"public-alias","input":"hello","reasoning":{"effort":"high"}}`)
			c, _, key := inputRoutingReasoningTestContext(t, group, service.CompositeRouteEndpointResponses, string(body))
			BindGroupInputRoutingReasoningRoute(c, service.CompositeRouteDecision{Matched: true, TargetPlatform: test.source, UpstreamModel: "source-only-model"})
			updated, _, err := applyGroupInputRoutingReasoningEffortPolicy(c, key, body, "")
			require.NoError(t, err)
			require.Equal(t, test.want, gjson.GetBytes(updated, "reasoning.effort").String())
			require.Equal(t, "public-alias", gjson.GetBytes(updated, "model").String())
			require.Empty(t, service.QuotaPlatform(c.Request.Context(), key))
			_, resolved := service.ResolvedTargetPlatformFromContext(c.Request.Context())
			require.False(t, resolved)
		})
	}
}

func TestGroupInputRoutingReasoningKnownMessagesPolicyDoesNotMapAgain(t *testing.T) {
	gin.SetMode(gin.TestMode)
	group := inputRoutingReasoningTestGroup(service.PlatformOpenAI)
	group.MaxReasoningEffort = ""
	group.ReasoningEffortMappings = []service.ReasoningEffortMapping{{From: "high", To: "medium"}, {From: "medium", To: "low"}}
	body := []byte(`{"model":"gpt-6.1-sol","messages":[{"role":"user","content":"hello"}],"max_tokens":32,"output_config":{"effort":"high"}}`)
	c, _, key := inputRoutingReasoningTestContext(t, group, service.CompositeRouteEndpointMessages, string(body))
	updated, changed, err := applyGroupInputRoutingReasoningEffortPolicy(c, key, body, "")
	require.NoError(t, err)
	require.True(t, changed)
	stub := &inputRoutingForwardStub{openAIResult: &service.OpenAIForwardResult{}}
	for _, accountID := range []int64{99, 100} {
		_, err := newInputRoutingForwardServices(stub).forward(c.Request.Context(), c, &service.Account{ID: accountID, Platform: service.PlatformOpenAI}, service.CompositeRouteEndpointMessages, updated, nil, "", false)
		require.NoError(t, err)
	}
	for _, call := range stub.calls {
		require.Equal(t, "medium", gjson.GetBytes(call.body, "output_config.effort").String())
		bridged := []byte(`{"model":"gpt-6.1-sol","reasoning":{"effort":"medium"}}`)
		got, changed, err := service.ApplyOpenAIReasoningEffortPolicyFromContext(call.ctx, bridged)
		require.NoError(t, err)
		require.False(t, changed, "the OpenAI Messages bridge must not map a second time")
		require.Equal(t, bridged, got)
	}
	require.Equal(t, "high", *service.RequestedReasoningEffortFromContext(c.Request.Context()))
}

func TestGroupInputRoutingReasoningOmittedEffortPreservesDefaults(t *testing.T) {
	group := inputRoutingReasoningTestGroup(service.PlatformOpenAI)
	body := []byte(`{"model":"gpt-6.1-sol","max_tokens":32,"messages":[{"role":"user","content":"hello"}]}`)
	c, _, key := inputRoutingReasoningTestContext(t, group, service.CompositeRouteEndpointMessages, string(body))
	updated, changed, err := applyGroupInputRoutingReasoningEffortPolicy(c, key, body, "")
	require.NoError(t, err)
	require.False(t, changed)
	require.Equal(t, body, updated)
	require.Nil(t, service.RequestedReasoningEffortFromContext(c.Request.Context()))
}

func TestGroupInputRoutingReasoningUnknownCompositeRetryUsesSelectedPlatform(t *testing.T) {
	group := inputRoutingReasoningTestGroup(service.PlatformComposite)
	group.MaxReasoningEffort = "minimal"
	group.MaxReasoningEffortOverLimit = service.ReasoningEffortOverLimitDowngrade
	body := []byte(`{"model":"target-only-alias","input":"hello","reasoning":{"effort":"high"}}`)
	c, _, key := inputRoutingReasoningTestContext(t, group, service.CompositeRouteEndpointResponses, string(body))
	initial, changed, err := applyGroupInputRoutingReasoningEffortPolicy(c, key, body, "")
	require.NoError(t, err)
	require.False(t, changed, "an unresolved composite source waits for account selection")
	for _, attempt := range []struct{ platform, want string }{{service.PlatformOpenAI, "minimal"}, {service.PlatformAnthropic, "low"}} {
		stub := &inputRoutingForwardStub{result: &service.ForwardResult{}, openAIResult: &service.OpenAIForwardResult{}}
		_, err := newInputRoutingForwardServices(stub).forward(c.Request.Context(), c, &service.Account{Platform: attempt.platform}, service.CompositeRouteEndpointResponses, initial, nil, "", false)
		require.NoError(t, err)
		require.Len(t, stub.calls, 1)
		require.Equal(t, attempt.want, gjson.GetBytes(stub.calls[0].body, "reasoning.effort").String())
	}
	require.Equal(t, body, initial)
	require.Empty(t, service.QuotaPlatform(c.Request.Context(), key))
}

func TestGroupInputRoutingReasoningDeferredDenialWritesProtocolError(t *testing.T) {
	for _, protocol := range []string{service.CompositeRouteEndpointMessages, service.CompositeRouteEndpointChatCompletions, service.CompositeRouteEndpointResponses} {
		t.Run(protocol, func(t *testing.T) {
			body := []byte(`{"model":"target-only-alias","messages":[{"role":"user","content":"hello"}],"input":"hello","reasoning_effort":"high"}`)
			c, rec, _ := inputRoutingReasoningTestContext(t, inputRoutingReasoningTestGroup(service.PlatformComposite), protocol, string(body))
			result, err := (&GatewayHandler{}).forwardGroupInputRouting(c.Request.Context(), c, &service.Account{Platform: service.PlatformOpenAI}, protocol, body, nil, "", false)
			require.True(t, service.IsReasoningEffortPolicyDenied(err))
			require.Nil(t, result)
			require.Equal(t, http.StatusForbidden, rec.Code)
			errorField := "error.type"
			if protocol == service.CompositeRouteEndpointResponses {
				errorField = "error.code"
			}
			require.Equal(t, "permission_error", gjson.Get(rec.Body.String(), errorField).String())
		})
	}
}
