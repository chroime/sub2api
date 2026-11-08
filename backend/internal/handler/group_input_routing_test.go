package handler

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/pkg/ctxkey"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestGroupInputRoutingMiddlewareMatchesBeforeDispatchAndPreservesSource(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, endpoint := range []struct{ path, route, protocol, input string }{
		{"/v1/messages", "/v1/messages", "messages", `"messages":[{"role":"user","content":"Hello"}]`},
		{"/v1/chat/completions", "/v1/chat/completions", "chat", `"messages":[{"role":"user","content":"Hello"}]`},
		{"/v1/responses", "/v1/responses", "responses", `"input":"Hello"`},
		{"/responses", "/responses", "responses", `"input":"Hello"`},
		{"/backend-api/codex/responses", "/backend-api/codex/responses", "responses", `"input":"Hello"`},
		{"/v1beta/models/gemini-3.8-flash:streamGenerateContent", "/v1beta/models/*modelAction", "gemini", `"contents":[{"parts":[{"text":"Hello"}]}]`},
	} {
		for _, delta := range []int{-1, 0, 1} {
			t.Run(fmt.Sprintf("%s/upper-bound%+d", endpoint.path, delta), func(t *testing.T) {
				model := "gpt-6.1-sol"
				if endpoint.protocol == "gemini" {
					model = "gemini-3.8-flash"
				}
				body := fmt.Sprintf(`{"model":%q,"stream":true,%s}`, model, endpoint.input)
				estimate, known := service.EstimateGroupInputRoutingTokens([]byte(body), endpoint.protocol, model)
				require.True(t, known)
				group := &service.Group{ID: 9, Platform: service.PlatformAnthropic, RateMultiplier: 0.35, InputRouting: service.GroupInputRoutingConfig{
					Enabled: true, Rules: []service.GroupInputRoutingRule{{ID: "first", Enabled: true, RequestType: "stream", MinInputTokens: 0, MaxInputTokens: estimate + delta, AccountIDs: []int64{30}}},
				}}
				key := &service.APIKey{GroupID: &group.ID, Group: group}
				router := gin.New()
				router.Use(func(c *gin.Context) { c.Set(string(middleware.ContextKeyAPIKey), key); c.Next() })
				router.Use((&GatewayHandler{}).GroupInputRoutingMiddleware())
				router.POST(endpoint.route, func(c *gin.Context) {
					decision, matched := service.GroupInputRoutingDecisionFromContext(c.Request.Context())
					require.Equal(t, delta >= 0, matched)
					require.Equal(t, matched && endpoint.protocol != "gemini", UseGroupInputRoutingGateway(c))
					if matched {
						require.Equal(t, "first", decision.RuleID)
						require.Equal(t, estimate, decision.EstimatedInputTokens)
					}
					restored, err := io.ReadAll(c.Request.Body)
					require.NoError(t, err)
					require.Equal(t, body, string(restored))
					stored, ok := middleware.GetAPIKeyFromContext(c)
					require.True(t, ok)
					require.Same(t, key, stored)
					require.Same(t, group, stored.Group)
					require.Equal(t, 0.35, stored.Group.RateMultiplier)
					c.Status(http.StatusNoContent)
				})
				res := httptest.NewRecorder()
				router.ServeHTTP(res, httptest.NewRequest(http.MethodPost, endpoint.path, strings.NewReader(body)))
				require.Equal(t, http.StatusNoContent, res.Code)
			})
		}
	}
}

func TestGroupInputRoutingMiddlewareSkipsNonTextAndUnknownInput(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, test := range []struct{ path, body string }{
		{"/v1/messages/count_tokens", `{"model":"gpt-6.1-sol","messages":[{"role":"user","content":"Hello"}]}`},
		{"/v1/audio/speech", `{"model":"grok-tts","input":"Hello"}`},
		{"/v1/responses/compact", `{"model":"gpt-6.1-sol","input":"Hello"}`},
		{"/v1/responses", `{"model":"gpt-6.1-sol","input":"Hello","previous_response_id":"r1"}`},
		{"/v1/responses", `{"model":"gpt-6.1-sol","input":"Hello","stream":"true"}`},
	} {
		t.Run(test.path+test.body, func(t *testing.T) {
			group := &service.Group{ID: 9, InputRouting: service.GroupInputRoutingConfig{Enabled: true, Rules: []service.GroupInputRoutingRule{{ID: "small", Enabled: true, RequestType: "all", MaxInputTokens: 1000, AccountIDs: []int64{30}}}}}
			r := gin.New()
			r.Use(func(c *gin.Context) {
				c.Set(string(middleware.ContextKeyAPIKey), &service.APIKey{Group: group, GroupID: &group.ID})
				c.Next()
			})
			r.Use((&GatewayHandler{}).GroupInputRoutingMiddleware())
			r.POST(test.path, func(c *gin.Context) {
				_, matched := service.GroupInputRoutingDecisionFromContext(c.Request.Context())
				require.False(t, matched)
				if test.path == "/v1/responses" {
					bindGroupInputRouting(c, &service.APIKey{Group: group, GroupID: &group.ID}, []byte(`{"input":"hello"}`), "responses", "gpt-6.1-sol", false)
					_, matched = service.GroupInputRoutingDecisionFromContext(c.Request.Context())
					require.False(t, matched, "normalization must not rematch the original unknown input")
				}
				c.Status(http.StatusNoContent)
			})
			res := httptest.NewRecorder()
			r.ServeHTTP(res, httptest.NewRequest(http.MethodPost, test.path, strings.NewReader(test.body)))
			require.Equal(t, http.StatusNoContent, res.Code)
		})
	}
}

func TestBindGroupInputRoutingPreservesBillingAndDownstreamMode(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, stream := range []bool{false, true} {
		for _, protocol := range []string{"responses", "chat", "messages", "gemini"} {
			t.Run(protocol+map[bool]string{false: "-sync", true: "-stream"}[stream], func(t *testing.T) {
				group := &service.Group{ID: 9, RateMultiplier: 0.35, InputRouting: service.GroupInputRoutingConfig{
					Enabled: true, Rules: []service.GroupInputRoutingRule{
						{ID: "sync-rule", Enabled: true, RequestType: "sync", MaxInputTokens: 1000, GroupIDs: []int64{20}},
						{ID: "stream-rule", Enabled: true, RequestType: "stream", MaxInputTokens: 1000, AccountIDs: []int64{30}},
					},
				}}
				apiKey := &service.APIKey{GroupID: &group.ID, Group: group}
				c, _ := gin.CreateTestContext(httptest.NewRecorder())
				ctx := context.WithValue(context.Background(), ctxkey.Group, group)
				ctx = context.WithValue(ctx, ctxkey.RequestedPublicModel, "public-model-alias")
				c.Request = httptest.NewRequest(http.MethodPost, "/test", nil).WithContext(ctx)
				body := []byte(`{"messages":[{"role":"user","content":"Hello"}]}`)
				if protocol == "responses" {
					body = []byte(`{"input":"Hello"}`)
				} else if protocol == "gemini" {
					body = []byte(`{"contents":[{"parts":[{"text":"Hello"}]}]}`)
				}
				bindGroupInputRouting(c, apiKey, body, protocol, "gpt-6.1-sol", stream)
				decision, matched := service.GroupInputRoutingDecisionFromContext(c.Request.Context())
				require.True(t, matched)
				require.Equal(t, group.ID, decision.SourceGroupID)
				require.Greater(t, decision.EstimatedInputTokens, 0)
				require.Equal(t, map[bool]string{false: "sync-rule", true: "stream-rule"}[stream], decision.RuleID)
				require.Same(t, group, apiKey.Group)
				require.Same(t, group, c.Request.Context().Value(ctxkey.Group))
				require.Equal(t, int64(9), *apiKey.GroupID)
				require.Equal(t, 0.35, apiKey.Group.RateMultiplier)
				publicModel, found := service.RequestedPublicModelFromContext(c.Request.Context())
				require.True(t, found)
				require.Equal(t, "public-model-alias", publicModel, "upstream model mapping must not replace the client's alias")
			})
		}
	}
}

func TestBindGroupInputRoutingUnknownHistoryAndDisabledRulesKeepOriginalContext(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, enabled := range []bool{false, true} {
		group := &service.Group{ID: 9, InputRouting: service.GroupInputRoutingConfig{Enabled: enabled, Rules: []service.GroupInputRoutingRule{
			{ID: "small", Enabled: true, RequestType: "all", MaxInputTokens: 1000, AccountIDs: []int64{30}},
		}}}
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
		before := c.Request.Context()
		bindGroupInputRouting(c, &service.APIKey{GroupID: &group.ID, Group: group}, []byte(`{"input":"Hello","previous_response_id":"resp_old"}`), "responses", "gpt-6.1-sol", true)
		_, matched := service.GroupInputRoutingDecisionFromContext(c.Request.Context())
		require.False(t, matched)
		require.Equal(t, before, c.Request.Context())
	}
}

func TestBindGroupInputRoutingAllTokenModeIgnoresRangeWithoutExpandingProtocolScope(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, tc := range []struct {
		name    string
		body    string
		matched bool
	}{
		{"ordinary text", `{"input":"Hello"}`, true},
		{"background stays excluded", `{"input":"Hello","background":true}`, false},
		{"server history stays excluded", `{"input":"Hello","previous_response_id":"resp_old"}`, false},
		{"media stays excluded", `{"input":[{"role":"user","content":[{"type":"input_image","image_url":"https://fixture.example/image.png"}]}]}`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			group := &service.Group{ID: 9, InputRouting: service.GroupInputRoutingConfig{Enabled: true, Rules: []service.GroupInputRoutingRule{{
				ID: "all-lengths", Enabled: true, RequestType: "all", InputTokensMode: "all", MinInputTokens: 500, MaxInputTokens: 600, AccountIDs: []int64{30},
			}}}}
			key := &service.APIKey{GroupID: &group.ID, Group: group}
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
			bindGroupInputRouting(c, key, []byte(tc.body), "responses", "gpt-6.1-sol", false)
			decision, matched := service.GroupInputRoutingDecisionFromContext(c.Request.Context())
			require.Equal(t, tc.matched, matched)
			if matched {
				require.Equal(t, "all-lengths", decision.RuleID)
				require.Same(t, group, key.Group)
			}
		})
	}
}
