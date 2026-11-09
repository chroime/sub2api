package service

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/claude"
	"github.com/Wei-Shaw/sub2api/internal/pkg/ctxkey"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func groupUltrafastTestContext() context.Context {
	return context.WithValue(context.Background(), ctxkey.Group, &Group{
		ID: 7, Platform: PlatformOpenAI, Status: StatusActive, Hydrated: true,
		ForceOpenAIFast: true, ForceOpenAIUltrafast: true,
	})
}

func TestGroupUltrafastPolicy_OverridesClientTierAndFast(t *testing.T) {
	svc := newOpenAIGatewayServiceWithSettings(t, DefaultOpenAIFastPolicySettings())
	account := &Account{Platform: PlatformOpenAI, Type: AccountTypeOAuth}
	for _, body := range []string{
		`{"model":"gpt-5.6-sol","input":"hi"}`,
		`{"model":"gpt-5.6-sol","service_tier":"default"}`,
		`{"model":"gpt-5.6-sol","service_tier":"flex"}`,
		`{"model":"gpt-5.6-sol","service_tier":"priority"}`,
		`{"model":"gpt-5.6-sol","service_tier":"client-unknown"}`,
	} {
		t.Run(body, func(t *testing.T) {
			updated, err := svc.applyOpenAIFastPolicyToBody(groupUltrafastTestContext(), account, "gpt-5.6-sol", []byte(body))
			require.NoError(t, err)
			require.Equal(t, "ultrafast", gjson.GetBytes(updated, "service_tier").String())
			require.Equal(t, "gpt-5.6-sol", gjson.GetBytes(updated, "model").String())
		})
	}
}

func TestGroupUltrafastPolicy_EnabledWithoutFast(t *testing.T) {
	svc := newOpenAIGatewayServiceWithSettings(t, DefaultOpenAIFastPolicySettings())
	ctx := groupUltrafastTestContext()
	ctx.Value(ctxkey.Group).(*Group).ForceOpenAIFast = false
	account := &Account{Platform: PlatformOpenAI, Type: AccountTypeAPIKey}
	updated, err := svc.applyOpenAIFastPolicyToBody(ctx, account, "gpt-5.6-sol", []byte(`{"model":"gpt-5.6-sol"}`))
	require.NoError(t, err)
	require.Equal(t, "ultrafast", gjson.GetBytes(updated, "service_tier").String())
}

func TestGroupUltrafastPolicy_GlobalPolicyRemainsAuthoritative(t *testing.T) {
	for _, action := range []string{BetaPolicyActionFilter, BetaPolicyActionBlock, OpenAIFastPolicyActionForcePriority} {
		t.Run(action, func(t *testing.T) {
			svc := newOpenAIGatewayServiceWithSettings(t, &OpenAIFastPolicySettings{Rules: []OpenAIFastPolicyRule{{
				ServiceTier: OpenAIFastTierUltrafast, Action: action, Scope: BetaPolicyScopeAll,
			}}})
			account := &Account{Platform: PlatformOpenAI, Type: AccountTypeAPIKey}
			body := []byte(`{"model":"gpt-5.6-sol","service_tier":"flex"}`)
			updated, err := svc.applyOpenAIFastPolicyToBody(groupUltrafastTestContext(), account, "gpt-5.6-sol", body)
			if action == BetaPolicyActionBlock {
				var blocked *OpenAIFastBlockedError
				require.ErrorAs(t, err, &blocked)
				return
			}
			require.NoError(t, err)
			if action == BetaPolicyActionFilter {
				require.False(t, gjson.GetBytes(updated, "service_tier").Exists())
			} else {
				require.Equal(t, "priority", gjson.GetBytes(updated, "service_tier").String())
			}
		})
	}
}

func TestGroupUltrafastPolicy_RequiresTrustedOpenAIContext(t *testing.T) {
	for _, tt := range []struct {
		name     string
		group    *Group
		platform string
	}{
		{name: "no group", platform: PlatformOpenAI},
		{name: "unhydrated group", platform: PlatformOpenAI, group: &Group{ID: 7, Platform: PlatformOpenAI, Status: StatusActive, ForceOpenAIUltrafast: true}},
		{name: "other group platform", platform: PlatformOpenAI, group: &Group{ID: 7, Platform: PlatformAnthropic, Status: StatusActive, Hydrated: true, ForceOpenAIUltrafast: true}},
		{name: "composite group", platform: PlatformOpenAI, group: &Group{ID: 7, Platform: PlatformComposite, Status: StatusActive, Hydrated: true, ForceOpenAIUltrafast: true}},
		{name: "other account platform", platform: PlatformGrok, group: &Group{ID: 7, Platform: PlatformOpenAI, Status: StatusActive, Hydrated: true, ForceOpenAIUltrafast: true}},
		{name: "disabled", platform: PlatformOpenAI, group: &Group{ID: 7, Platform: PlatformOpenAI, Status: StatusActive, Hydrated: true}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			svc := newOpenAIGatewayServiceWithSettings(t, DefaultOpenAIFastPolicySettings())
			ctx := context.WithValue(context.Background(), ctxkey.Group, tt.group)
			body := []byte(`{"model":"gpt-5.6-sol","service_tier":"flex"}`)
			updated, err := svc.applyOpenAIFastPolicyToBody(ctx, &Account{Platform: tt.platform}, "gpt-5.6-sol", body)
			require.NoError(t, err)
			require.Equal(t, string(body), string(updated))
		})
	}
}

func TestGroupUltrafastPolicy_WSFramesAndGlobalPolicy(t *testing.T) {
	account := &Account{Platform: PlatformOpenAI, Type: AccountTypeAPIKey}
	for _, action := range []string{BetaPolicyActionPass, BetaPolicyActionFilter, BetaPolicyActionBlock} {
		t.Run(action, func(t *testing.T) {
			svc := newOpenAIGatewayServiceWithSettings(t, &OpenAIFastPolicySettings{Rules: []OpenAIFastPolicyRule{{
				ServiceTier: OpenAIFastTierUltrafast, Action: action, Scope: BetaPolicyScopeAll,
			}}})
			for _, frame := range []string{
				`{"type":"response.create","model":"gpt-5.6-sol","input":[]}`,
				`{"type":"response.create","previous_response_id":"resp_first","service_tier":"flex"}`,
			} {
				updated, blocked, err := svc.applyOpenAIFastPolicyToWSResponseCreate(groupUltrafastTestContext(), account, "gpt-5.6-sol", []byte(frame))
				require.NoError(t, err)
				if action == BetaPolicyActionBlock {
					require.NotNil(t, blocked)
					continue
				}
				require.Nil(t, blocked)
				if action == BetaPolicyActionFilter {
					require.False(t, gjson.GetBytes(updated, "service_tier").Exists())
				} else {
					require.Equal(t, "ultrafast", gjson.GetBytes(updated, "service_tier").String())
				}
			}
			cancel := []byte(`{"type":"response.cancel","service_tier":"flex"}`)
			updated, blocked, err := svc.applyOpenAIFastPolicyToWSResponseCreate(groupUltrafastTestContext(), account, "gpt-5.6-sol", cancel)
			require.NoError(t, err)
			require.Nil(t, blocked)
			require.Equal(t, string(cancel), string(updated))
		})
	}
}

func TestGroupUltrafastPolicy_HTTPForwarding(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, route := range []string{"responses", "passthrough", "chat", "raw-chat", "messages", "messages-raw-chat"} {
		for _, action := range []string{BetaPolicyActionPass, BetaPolicyActionFilter, BetaPolicyActionBlock} {
			t.Run(route+"/"+action, func(t *testing.T) {
				body := []byte(`{"model":"gpt-5.6-sol","stream":false,"input":"hi","service_tier":"flex"}`)
				path := "/v1/responses"
				if route == "chat" || route == "raw-chat" || strings.HasPrefix(route, "messages") {
					body = []byte(`{"model":"gpt-5.6-sol","stream":false,"max_tokens":16,"messages":[{"role":"user","content":"hi"}],"service_tier":"flex"}`)
					path = "/v1/chat/completions"
				}
				if strings.HasPrefix(route, "messages") {
					path = "/v1/messages"
				}
				c, _ := gin.CreateTestContext(httptest.NewRecorder())
				c.Request = httptest.NewRequest(http.MethodPost, path, bytes.NewReader(body))
				c.Request.Header.Set("User-Agent", "codex_cli_rs/0.1.0")
				c.Request.Header.Set("Content-Type", "application/json")
				c.Request.Header.Set("anthropic-beta", claude.BetaFastMode)
				responseBody := "data: {\"type\":\"response.completed\",\"response\":{\"id\":\"resp_ultrafast\",\"model\":\"gpt-5.6-sol\",\"output\":[],\"usage\":{\"input_tokens\":1,\"output_tokens\":1}}}\n\ndata: [DONE]\n\n"
				contentType := "text/event-stream"
				account := &Account{
					ID: 123, Name: "ultrafast-test", Platform: PlatformOpenAI, Type: AccountTypeOAuth, Concurrency: 1,
					Credentials: map[string]any{"access_token": "fixture-token", "chatgpt_account_id": "fixture-account"},
					Extra:       map[string]any{"openai_passthrough": route == "passthrough", "openai_oauth_responses_websockets_v2_mode": OpenAIWSIngressModeOff},
					Status:      StatusActive, Schedulable: true,
				}
				if route == "raw-chat" || route == "messages-raw-chat" {
					account.Type = AccountTypeAPIKey
					account.Credentials = map[string]any{"api_key": "fixture-key", "api_protocol": APIProtocolChatCompletions}
					account.Extra["openai_responses_supported"] = false
					responseBody = `{"id":"chatcmpl_ultrafast","object":"chat.completion","model":"gpt-5.6-sol","choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}],"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}}`
					contentType = "application/json"
				}
				upstream := &httpUpstreamRecorder{resp: &http.Response{
					StatusCode: http.StatusOK, Header: http.Header{"Content-Type": []string{contentType}},
					Body: io.NopCloser(strings.NewReader(responseBody)),
				}}
				svc := newOpenAIGatewayServiceWithSettings(t, &OpenAIFastPolicySettings{Rules: []OpenAIFastPolicyRule{{
					ServiceTier: OpenAIFastTierUltrafast, Action: action, Scope: BetaPolicyScopeAll,
				}}})
				svc.cfg = &config.Config{}
				svc.httpUpstream = upstream
				var result *OpenAIForwardResult
				var err error
				switch route {
				case "chat", "raw-chat":
					result, err = svc.ForwardAsChatCompletions(groupUltrafastTestContext(), c, account, body, "", "")
				case "messages", "messages-raw-chat":
					result, err = svc.ForwardAsAnthropic(groupUltrafastTestContext(), c, account, body, "", "")
				default:
					result, err = svc.Forward(groupUltrafastTestContext(), c, account, body)
				}
				if action == BetaPolicyActionBlock {
					var blocked *OpenAIFastBlockedError
					require.ErrorAs(t, err, &blocked)
					require.Empty(t, upstream.requests, "blocked requests must not reach an upstream")
					return
				}
				require.NoError(t, err)
				require.NotNil(t, result)
				require.Len(t, upstream.requests, 1)
				if action == BetaPolicyActionFilter {
					require.False(t, gjson.GetBytes(upstream.lastBody, "service_tier").Exists())
					require.Nil(t, result.ServiceTier)
				} else {
					require.Equal(t, "ultrafast", gjson.GetBytes(upstream.lastBody, "service_tier").String())
					require.NotNil(t, result.ServiceTier)
					require.Equal(t, "ultrafast", *result.ServiceTier)
				}
			})
		}
	}
}

func TestGroupUltrafastPolicy_FreeFastDoesNotDiscountUltrafast(t *testing.T) {
	key := &APIKey{Group: &Group{Platform: PlatformOpenAI, FreeOpenAIFast: true, ForceOpenAIUltrafast: true}}
	account := &Account{Platform: PlatformOpenAI, Type: AccountTypeOAuth}
	require.False(t, groupBillsOpenAIFastAtStandard(key, account, "ultrafast"))
	require.True(t, groupBillsOpenAIFastAtStandard(key, account, "priority"))
	requested := "ultrafast"
	result := &OpenAIForwardResult{ServiceTier: &requested, UpstreamResponseServiceTier: "ultrafast"}
	require.Equal(t, "ultrafast", ApplyOpenAIServiceTierBillingResolution(account, result).Billing)
}
