package handler

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/domain"
	middleware2 "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type inputRoutingForwardCall struct {
	family, protocol       string
	ctx                    context.Context
	c                      *gin.Context
	account                *service.Account
	body                   []byte
	parsed                 *service.ParsedRequest
	cacheKey, defaultModel string
	sticky                 bool
}

type inputRoutingForwardStub struct {
	calls           []inputRoutingForwardCall
	result          *service.ForwardResult
	openAIResult    *service.OpenAIForwardResult
	runtimeEndpoint string
	err             error
}

func (s *inputRoutingForwardStub) record(family, protocol string, ctx context.Context, c *gin.Context, account *service.Account, body []byte, parsed *service.ParsedRequest, cacheKey, defaultModel string, sticky bool) {
	s.calls = append(s.calls, inputRoutingForwardCall{family, protocol, ctx, c, account, body, parsed, cacheKey, defaultModel, sticky})
	if s.runtimeEndpoint != "" {
		service.SetActualOpenAIUpstreamEndpoint(c, s.runtimeEndpoint)
	}
}

type inputRoutingAnthropicStub struct{ *inputRoutingForwardStub }

func (s inputRoutingAnthropicStub) Forward(ctx context.Context, c *gin.Context, a *service.Account, p *service.ParsedRequest) (*service.ForwardResult, error) {
	s.record("anthropic", "messages", ctx, c, a, p.Body.Bytes(), p, "", "", false)
	if p.OnUpstreamAccepted != nil {
		p.OnUpstreamAccepted()
	}
	return s.result, s.err
}
func (s inputRoutingAnthropicStub) ForwardAsChatCompletions(ctx context.Context, c *gin.Context, a *service.Account, b []byte, p *service.ParsedRequest) (*service.ForwardResult, error) {
	s.record("anthropic", "chat_completions", ctx, c, a, b, p, "", "", false)
	return s.result, s.err
}
func (s inputRoutingAnthropicStub) ForwardAsResponses(ctx context.Context, c *gin.Context, a *service.Account, b []byte, p *service.ParsedRequest) (*service.ForwardResult, error) {
	s.record("anthropic", "responses", ctx, c, a, b, p, "", "", false)
	return s.result, s.err
}

type inputRoutingOpenAIStub struct{ *inputRoutingForwardStub }

func (s inputRoutingOpenAIStub) Forward(ctx context.Context, c *gin.Context, a *service.Account, b []byte) (*service.OpenAIForwardResult, error) {
	s.record("openai", "responses", ctx, c, a, b, nil, "", "", false)
	return s.openAIResult, s.err
}
func (s inputRoutingOpenAIStub) ForwardAsAnthropic(ctx context.Context, c *gin.Context, a *service.Account, b []byte, cache, model string) (*service.OpenAIForwardResult, error) {
	s.record("openai", "messages", ctx, c, a, b, nil, cache, model, false)
	return s.openAIResult, s.err
}
func (s inputRoutingOpenAIStub) ForwardAsChatCompletions(ctx context.Context, c *gin.Context, a *service.Account, b []byte, cache, model string) (*service.OpenAIForwardResult, error) {
	s.record("openai", "chat_completions", ctx, c, a, b, nil, cache, model, false)
	return s.openAIResult, s.err
}

type inputRoutingGeminiStub struct{ *inputRoutingForwardStub }

func (s inputRoutingGeminiStub) Forward(ctx context.Context, c *gin.Context, a *service.Account, b []byte) (*service.ForwardResult, error) {
	s.record("gemini", "messages", ctx, c, a, b, nil, "", "", false)
	return s.result, s.err
}
func (s inputRoutingGeminiStub) ForwardAsChatCompletions(ctx context.Context, c *gin.Context, a *service.Account, b []byte) (*service.ForwardResult, error) {
	s.record("gemini", "chat_completions", ctx, c, a, b, nil, "", "", false)
	return s.result, s.err
}

type inputRoutingAntigravityStub struct{ *inputRoutingForwardStub }

func (s inputRoutingAntigravityStub) Forward(ctx context.Context, c *gin.Context, a *service.Account, b []byte, sticky bool) (*service.ForwardResult, error) {
	s.record("antigravity", "messages", ctx, c, a, b, nil, "", "", sticky)
	return s.result, s.err
}
func (s inputRoutingAntigravityStub) ForwardAsChatCompletions(ctx context.Context, c *gin.Context, a *service.Account, b []byte, p *service.ParsedRequest) (*service.ForwardResult, error) {
	s.record("antigravity", "chat_completions", ctx, c, a, b, p, "", "", false)
	return s.result, s.err
}
func (s inputRoutingAntigravityStub) ForwardAsResponses(ctx context.Context, c *gin.Context, a *service.Account, b []byte, p *service.ParsedRequest) (*service.ForwardResult, error) {
	s.record("antigravity", "responses", ctx, c, a, b, p, "", "", false)
	return s.result, s.err
}

func newInputRoutingForwardServices(stub *inputRoutingForwardStub) groupInputRoutingForwardServices {
	return groupInputRoutingForwardServices{
		anthropic: inputRoutingAnthropicStub{stub}, openAI: inputRoutingOpenAIStub{stub},
		gemini: inputRoutingGeminiStub{stub}, antigravity: inputRoutingAntigravityStub{stub},
	}
}

func TestGroupInputRoutingForwardUsesSelectedAccountAndPreservesSource(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, protocol := range []string{"messages", "chat_completions", "responses"} {
		for _, spec := range domain.Platforms() {
			if spec.ID == service.PlatformTypeSafe || (spec.ID == service.PlatformGemini && protocol == "responses") {
				continue
			}
			for _, accountType := range []string{service.AccountTypeAPIKey, service.AccountTypeOAuth} {
				t.Run(protocol+"/"+spec.ID+"/"+accountType, func(t *testing.T) {
					body := []byte(`{"model":"public-alias","stream":true}`)
					parsed := &service.ParsedRequest{Model: "public-alias", Stream: true, Body: service.NewRequestBodyRef(body)}
					accepted := 0
					parsed.OnUpstreamAccepted = func() { accepted++ }
					account := &service.Account{ID: 99, Platform: spec.ID, Type: accountType}
					group := &service.Group{ID: 7, Platform: service.PlatformGemini, RateMultiplier: 0.4}
					key := &service.APIKey{ID: 11, GroupID: &group.ID, Group: group}
					c, _ := gin.CreateTestContext(httptest.NewRecorder())
					c.Request = httptest.NewRequest(http.MethodPost, "/v1/"+protocol, nil)
					c.Set(string(middleware2.ContextKeyAPIKey), key)
					ctx := c.Request.Context()
					stub := &inputRoutingForwardStub{result: &service.ForwardResult{RequestID: "native", Model: "public-alias"}, openAIResult: &service.OpenAIForwardResult{RequestID: "openai", Model: "public-alias"}}
					result, err := newInputRoutingForwardServices(stub).forward(ctx, c, account, protocol, body, parsed, "session-key", true)
					require.NoError(t, err)
					require.NotNil(t, result)
					require.Len(t, stub.calls, 1)
					call := stub.calls[0]
					expectedFamily := string(spec.Gateway)
					if spec.ID == service.PlatformAntigravity && accountType == service.AccountTypeAPIKey {
						expectedFamily = "anthropic"
					}
					require.Equal(t, expectedFamily, call.family)
					require.Equal(t, protocol, call.protocol)
					require.Same(t, account, call.account)
					require.Equal(t, ctx, call.ctx)
					require.Equal(t, body, call.body)
					gotKey, ok := middleware2.GetAPIKeyFromContext(call.c)
					require.True(t, ok)
					require.Same(t, key, gotKey)
					require.Same(t, group, gotKey.Group)
					require.Equal(t, int64(7), *gotKey.GroupID)
					if expectedFamily == "openai" {
						require.Equal(t, "openai", result.RequestID)
						if protocol != "responses" {
							require.Equal(t, "session-key", call.cacheKey)
						}
						require.Empty(t, call.defaultModel)
					} else {
						require.Same(t, stub.result, result)
					}
					if expectedFamily == "anthropic" && protocol == "messages" {
						require.Same(t, parsed, call.parsed)
						require.Equal(t, 1, accepted)
					} else {
						require.Zero(t, accepted)
					}
					if expectedFamily == "antigravity" {
						require.Equal(t, EndpointAntigravityGenerateContent, GetUpstreamEndpoint(c, spec.ID))
					}
				})
			}
		}
	}
}

func TestGroupInputRoutingForwardPreservesPartialUsageAndFailoverErrors(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, platform := range []string{service.PlatformOpenAI, service.PlatformAnthropic, service.PlatformGemini, service.PlatformAntigravity} {
		t.Run(platform, func(t *testing.T) {
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			parsed := &service.ParsedRequest{Body: service.NewRequestBodyRef([]byte(`{}`))}
			partial := errors.New("stream interrupted")
			stub := &inputRoutingForwardStub{result: &service.ForwardResult{RequestID: "partial"}, openAIResult: &service.OpenAIForwardResult{RequestID: "partial"}, err: partial}
			services := newInputRoutingForwardServices(stub)
			account := &service.Account{Platform: platform, Type: service.AccountTypeOAuth}
			result, err := services.forward(context.Background(), c, account, "messages", parsed.Body.Bytes(), parsed, "", false)
			require.ErrorIs(t, err, partial)
			require.Equal(t, "partial", result.RequestID)
			failover := &service.UpstreamFailoverError{StatusCode: 429}
			stub.result, stub.openAIResult, stub.err = nil, nil, failover
			result, err = services.forward(context.Background(), c, account, "messages", parsed.Body.Bytes(), parsed, "", false)
			require.ErrorIs(t, err, failover)
			require.Nil(t, result)
		})
	}
}

func TestGroupInputRoutingForwardRejectsMissingServicesAndUnsupportedPairs(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, tc := range []struct{ platform, protocol string }{
		{service.PlatformOpenAI, "messages"}, {service.PlatformAnthropic, "messages"}, {service.PlatformGemini, "chat_completions"},
		{service.PlatformAntigravity, "responses"}, {service.PlatformGemini, "responses"}, {service.PlatformTypeSafe, "messages"},
		{"unknown", "responses"}, {service.PlatformOpenAI, "count_tokens"},
	} {
		t.Run(tc.platform+"/"+tc.protocol, func(t *testing.T) {
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			result, err := (groupInputRoutingForwardServices{}).forward(context.Background(), c, &service.Account{Platform: tc.platform, Type: service.AccountTypeOAuth}, tc.protocol, nil, &service.ParsedRequest{}, "", false)
			require.Nil(t, result)
			require.Error(t, err)
			var unavailable *groupInputRoutingForwardError
			require.ErrorAs(t, err, &unavailable)
		})
	}
	for _, protocol := range []string{"messages", "chat_completions", "responses"} {
		t.Run("client-error/"+protocol, func(t *testing.T) {
			rec := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(rec)
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/"+protocol, nil)
			h := &GatewayHandler{}
			result, err := h.forwardGroupInputRouting(context.Background(), c, &service.Account{Platform: service.PlatformOpenAI}, protocol, nil, nil, "", false)
			require.Nil(t, result)
			require.Error(t, err)
			require.Equal(t, http.StatusBadGateway, rec.Code)
			require.Contains(t, rec.Body.String(), "OpenAI compatibility service is not configured")
		})
	}
}

func TestGroupInputRoutingForwardRejectsNilSuccess(t *testing.T) {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	stub := &inputRoutingForwardStub{}
	result, err := newInputRoutingForwardServices(stub).forward(context.Background(), c, &service.Account{Platform: service.PlatformOpenAI}, "responses", nil, nil, "", false)
	require.Nil(t, result)
	require.ErrorContains(t, err, "returned no result")
}

func TestGroupInputRoutingForwardRecordsActualEndpointAndClearsPreviousAttempt(t *testing.T) {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", nil)
	setActualUpstreamEndpoint(c, EndpointAntigravityGenerateContent)
	service.SetActualOpenAIUpstreamEndpoint(c, "/old/attempt")
	stub := &inputRoutingForwardStub{
		openAIResult: &service.OpenAIForwardResult{UpstreamEndpoint: EndpointMessages},
	}
	services := newInputRoutingForwardServices(stub)
	account := &service.Account{Platform: service.PlatformDeepseek, Type: service.AccountTypeAPIKey}
	_, err := services.forward(c.Request.Context(), c, account, "messages", nil, nil, "", false)
	require.NoError(t, err)
	require.Equal(t, EndpointMessages, GetUpstreamEndpoint(c, account.Platform))
	stub.openAIResult = nil
	stub.runtimeEndpoint = EndpointChatCompletions
	stub.err = &service.UpstreamFailoverError{StatusCode: 429}
	_, err = services.forward(c.Request.Context(), c, account, "messages", nil, nil, "", false)
	require.Error(t, err)
	require.Equal(t, EndpointChatCompletions, GetUpstreamEndpoint(c, account.Platform))
	stub.runtimeEndpoint = ""
	stub.err = nil
	stub.result = &service.ForwardResult{}
	account.Platform, account.Type = service.PlatformAntigravity, service.AccountTypeOAuth
	_, err = services.forward(c.Request.Context(), c, account, "messages", nil, nil, "", false)
	require.NoError(t, err)
	require.Empty(t, service.GetActualOpenAIUpstreamEndpoint(c))
	require.Equal(t, EndpointAntigravityGenerateContent, GetUpstreamEndpoint(c, account.Platform))
}

func TestGroupInputRoutingForwardPreservesSourceOpenAIMessagesPermission(t *testing.T) {
	gin.SetMode(gin.TestMode)
	group := &service.Group{ID: 7, Platform: service.PlatformOpenAI, AllowMessagesDispatch: false,
		InputRouting: service.GroupInputRoutingConfig{Enabled: true, Rules: []service.GroupInputRoutingRule{{
			ID: "small", Enabled: true, RequestType: "all", MaxInputTokens: 1000, AccountIDs: []int64{99},
		}}},
	}
	key := &service.APIKey{ID: 11, GroupID: &group.ID, Group: group, User: &service.User{ID: 13}}
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(`{"model":"gpt-6.1-sol","max_tokens":32,"messages":[{"role":"user","content":"Hello"}]}`))
	c.Request.Header.Set("Content-Type", "application/json")
	c.Set(string(middleware2.ContextKeyAPIKey), key)
	c.Set(string(middleware2.ContextKeyUser), middleware2.AuthSubject{UserID: 13, Concurrency: 1})
	h := &GatewayHandler{gatewayService: &service.GatewayService{}}
	h.Messages(c)
	_, matched := service.GroupInputRoutingDecisionFromContext(c.Request.Context())
	require.True(t, matched)
	require.Equal(t, http.StatusForbidden, rec.Code)
	require.Contains(t, rec.Body.String(), "This group does not allow /v1/messages dispatch")
	gotKey, _ := middleware2.GetAPIKeyFromContext(c)
	require.Same(t, key, gotKey)
}
