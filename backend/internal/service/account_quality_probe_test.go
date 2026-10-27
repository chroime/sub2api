//go:build unit

package service

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/accountquality"
	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/tlsfingerprint"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

type qualityProbeAccountRepo struct {
	AccountRepository
	account *Account
	lookups []int64
}

func (r *qualityProbeAccountRepo) GetByID(_ context.Context, id int64) (*Account, error) {
	r.lookups = append(r.lookups, id)
	if r.account == nil || r.account.ID != id {
		return nil, errors.New("account not found")
	}
	return r.account, nil
}

type qualityProbeUpstream struct {
	call       func(*http.Request) (*http.Response, error)
	bodies     [][]byte
	requests   []*http.Request
	accountIDs []int64
	proxies    []string
}

func (u *qualityProbeUpstream) Do(req *http.Request, proxy string, id int64, limit int) (*http.Response, error) {
	return u.DoWithTLS(req, proxy, id, limit, nil)
}

func (u *qualityProbeUpstream) DoWithTLS(req *http.Request, proxy string, id int64, _ int, _ *tlsfingerprint.Profile) (*http.Response, error) {
	body, err := io.ReadAll(req.Body)
	if err != nil {
		return nil, err
	}
	u.bodies = append(u.bodies, body)
	u.requests = append(u.requests, req)
	u.accountIDs = append(u.accountIDs, id)
	u.proxies = append(u.proxies, proxy)
	return u.call(req)
}

type qualityProbeSlotCache struct {
	ConcurrencyCache
	available bool
	acquired  []int64
	released  []int64
}

func (c *qualityProbeSlotCache) AcquireAccountSlot(_ context.Context, id int64, _ int, _ string) (bool, error) {
	c.acquired = append(c.acquired, id)
	return c.available, nil
}

func (c *qualityProbeSlotCache) ReleaseAccountSlot(_ context.Context, id int64, _ string) error {
	c.released = append(c.released, id)
	return nil
}

func qualityProbeResponse(body string) *http.Response {
	return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": {"text/event-stream"}, "X-Request-Id": {"probe-request-1"}}, Body: io.NopCloser(strings.NewReader(body))}
}

func qualityProbeFixture(account *Account, stream string) (*AccountTestService, *qualityProbeUpstream, *qualityProbeSlotCache) {
	if account.Status == "" {
		account.Status, account.Schedulable = StatusActive, true
	}
	u := &qualityProbeUpstream{call: func(*http.Request) (*http.Response, error) { return qualityProbeResponse(stream), nil }}
	slots := &qualityProbeSlotCache{available: true}
	svc := &AccountTestService{accountRepo: &qualityProbeAccountRepo{account: account}, httpUpstream: u, cfg: &config.Config{}}
	svc.SetQualityProbeConcurrencyService(NewConcurrencyService(slots))
	return svc, u, slots
}

func qualityProbeAPIAccount() *Account {
	return &Account{ID: 71, Platform: PlatformOpenAI, Type: AccountTypeAPIKey, Concurrency: 3,
		Credentials: map[string]any{"api_key": "secret-api-key", "base_url": "https://upstream.example", "model_mapping": map[string]any{"public-model": "gpt-5.2"}}}
}

func qualityProbeRequest() accountquality.ProbeRequest {
	return accountquality.ProbeRequest{AccountID: 71, BindingID: 12, Model: "public-model", APIMode: "responses", Prompt: "Solve the challenge and return 21.", Effort: "high", MaxOutputTokens: 128, TimeoutSeconds: 5}
}

// A gateway fallback, fixed hi payload, dropped parameter, or missing slot release breaks this contract.
func TestAccountQualityProbeResponsesUsesFixedAccountAndCustomParameters(t *testing.T) {
	a := qualityProbeAPIAccount()
	proxyID := int64(9)
	a.ProxyID, a.Proxy = &proxyID, &Proxy{ID: 9, Protocol: "http", Host: "127.0.0.1", Port: 19090}
	svc, u, slots := qualityProbeFixture(a, "data: {\"type\":\"response.output_text.delta\",\"delta\":\"21\"}\n\ndata: {\"type\":\"response.completed\",\"response\":{\"status\":\"completed\",\"model\":\"gpt-5.2-2026-01-01\",\"usage\":{\"input_tokens\":11,\"output_tokens\":9,\"output_tokens_details\":{\"reasoning_tokens\":7}}}}\n\n")
	r, err := svc.ProbeAccountText(context.Background(), qualityProbeRequest())
	require.NoError(t, err)
	require.Equal(t, "completed", r.ExecutionStatus)
	require.Equal(t, "21", r.Text)
	require.Equal(t, int64(71), r.AccountID)
	require.Equal(t, int64(71), r.CredentialAccountID)
	require.NotEmpty(t, r.IdentityVersion)
	require.Equal(t, "public-model", r.RequestedModel)
	require.Equal(t, "gpt-5.2", r.MappedModel)
	require.Equal(t, "gpt-5.2-2026-01-01", r.ReturnedModel)
	require.Equal(t, int64(11), r.InputTokens)
	require.Equal(t, int64(9), r.OutputTokens)
	require.Equal(t, int64(7), r.ReasoningTokens)
	require.Equal(t, "probe-request-1", r.UpstreamRequestID)
	require.True(t, r.AutoControlEligible)
	require.Equal(t, "upstream", r.OutputLimitMode)
	require.Equal(t, []int64{71}, u.accountIDs)
	require.Equal(t, []string{"http://127.0.0.1:19090"}, u.proxies)
	require.Equal(t, []int64{71}, slots.acquired)
	require.Equal(t, slots.acquired, slots.released)
	require.Equal(t, "https://upstream.example/v1/responses", u.requests[0].URL.String())
	require.Equal(t, "Bearer secret-api-key", u.requests[0].Header.Get("Authorization"))
	require.Equal(t, "Solve the challenge and return 21.", gjson.GetBytes(u.bodies[0], "input.0.content.0.text").String())
	require.Equal(t, "high", gjson.GetBytes(u.bodies[0], "reasoning.effort").String())
	require.Equal(t, int64(128), gjson.GetBytes(u.bodies[0], "max_output_tokens").Int())
	_, deadline := u.requests[0].Context().Deadline()
	require.True(t, deadline)
}

func TestAccountQualityProbeOAuthUsesActualPromptAndFreshLocallyBoundedSession(t *testing.T) {
	a := &Account{ID: 71, Platform: PlatformOpenAI, Type: AccountTypeOAuth, Concurrency: 2, Credentials: map[string]any{"access_token": "oauth-secret", "chatgpt_account_id": "stable-account", "chatgpt_user_id": "stable-user"}}
	svc, u, _ := qualityProbeFixture(a, "data: {\"type\":\"response.completed\",\"response\":{\"status\":\"completed\",\"model\":\"gpt-5.6-sol\",\"output\":[{\"type\":\"message\",\"content\":[{\"type\":\"output_text\",\"text\":\"21\"}]}]}}\n\n")
	request := qualityProbeRequest()
	request.Model = "gpt-5.6"
	first, err := svc.ProbeAccountText(context.Background(), request)
	require.NoError(t, err)
	require.Equal(t, "completed", first.ExecutionStatus)
	require.Equal(t, "21", first.Text)
	require.True(t, first.AutoControlEligible)
	require.Equal(t, "local", first.OutputLimitMode)
	require.Equal(t, "gpt-5.6-sol", first.MappedModel)
	require.Equal(t, "gpt-5.6-sol", gjson.GetBytes(u.bodies[0], "model").String())
	require.Equal(t, request.Prompt, gjson.GetBytes(u.bodies[0], "input.0.content.0.text").String())
	require.Equal(t, "high", gjson.GetBytes(u.bodies[0], "reasoning.effort").String())
	require.False(t, gjson.GetBytes(u.bodies[0], "max_output_tokens").Exists())
	require.False(t, gjson.GetBytes(u.bodies[0], "store").Bool())
	require.Equal(t, "Bearer oauth-secret", u.requests[0].Header.Get("Authorization"))
	require.Equal(t, "chatgpt.com", u.requests[0].URL.Host)
	a.Credentials["access_token"] = "rotated-oauth-secret"
	a.Credentials["expires_at"] = time.Now().Add(time.Hour).Unix()
	second, err := svc.ProbeAccountText(context.Background(), request)
	require.NoError(t, err)
	require.Equal(t, first.IdentityVersion, second.IdentityVersion)
	require.NotEmpty(t, u.requests[0].Header.Get("session_id"))
	require.NotEqual(t, u.requests[0].Header.Get("session_id"), u.requests[1].Header.Get("session_id"))
}

func TestAccountQualityProbeDoesNotConvertExecutionFailuresToQualityEvidence(t *testing.T) {
	for _, tc := range []struct{ name, mode, body, status, class string }{
		{"responses_output_limit", "responses", "data: {\"type\":\"response.incomplete\",\"response\":{\"status\":\"incomplete\",\"incomplete_details\":{\"reason\":\"max_output_tokens\"}}}\n\n", "truncated", "output_limit"},
		{"responses_eof", "responses", "data: {\"type\":\"response.output_text.delta\",\"delta\":\"wrong\"}\n\n", "error", "incomplete_stream"},
		{"chat_length", "chat_completions", "data: {\"model\":\"gpt-5.2\",\"choices\":[{\"delta\":{\"content\":\"partial\"},\"finish_reason\":\"length\"}]}\n\ndata: [DONE]\n\n", "truncated", "output_limit"},
		{"chat_missing_finish", "chat_completions", "data: {\"choices\":[{\"delta\":{\"content\":\"wrong\"}}]}\n\ndata: [DONE]\n\n", "error", "incomplete_stream"},
		{"responses_malformed", "responses", "data: nope\n\n", "error", "invalid_response"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			svc, _, slots := qualityProbeFixture(qualityProbeAPIAccount(), tc.body)
			req := qualityProbeRequest()
			req.APIMode = tc.mode
			r, err := svc.ProbeAccountText(context.Background(), req)
			require.NoError(t, err)
			require.Equal(t, tc.status, r.ExecutionStatus)
			require.Equal(t, tc.class, r.ErrorClass)
			require.Equal(t, []int64{71}, slots.released)
		})
	}
}

func TestAccountQualityProbeChatCollectsTerminalUsage(t *testing.T) {
	svc, u, _ := qualityProbeFixture(qualityProbeAPIAccount(), "data: {\"model\":\"gpt-5.2\",\"choices\":[{\"delta\":{\"content\":\"21\"},\"finish_reason\":null}]}\n\ndata: {\"choices\":[{\"delta\":{},\"finish_reason\":\"stop\"}]}\n\ndata: {\"choices\":[],\"usage\":{\"prompt_tokens\":12,\"completion_tokens\":8,\"completion_tokens_details\":{\"reasoning_tokens\":6}}}\n\ndata: [DONE]\n\n")
	req := qualityProbeRequest()
	req.APIMode = "chat_completions"
	r, err := svc.ProbeAccountText(context.Background(), req)
	require.NoError(t, err)
	require.Equal(t, "completed", r.ExecutionStatus)
	require.Equal(t, "21", r.Text)
	require.Equal(t, int64(12), r.InputTokens)
	require.Equal(t, int64(8), r.OutputTokens)
	require.Equal(t, int64(6), r.ReasoningTokens)
	require.Equal(t, "high", gjson.GetBytes(u.bodies[0], "reasoning_effort").String())
	require.Equal(t, int64(128), gjson.GetBytes(u.bodies[0], "max_completion_tokens").Int())
	require.Equal(t, req.Prompt, gjson.GetBytes(u.bodies[0], "messages.0.content").String())
}

func TestAccountQualityProbeAnthropicAndGeminiForwardParametersAndUsage(t *testing.T) {
	for _, tc := range []struct{ platform, mode, model, effort, stream, promptPath, effortPath, capPath string }{
		{PlatformAnthropic, "anthropic", "claude-sonnet-4-6", "high", "data: {\"type\":\"message_start\",\"message\":{\"model\":\"claude-sonnet-4-6\",\"usage\":{\"input_tokens\":13}}}\n\ndata: {\"type\":\"content_block_delta\",\"delta\":{\"type\":\"text_delta\",\"text\":\"21\"}}\n\ndata: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"end_turn\"},\"usage\":{\"output_tokens\":10}}\n\ndata: {\"type\":\"message_stop\"}\n\n", "messages.0.content.0.text", "output_config.effort", "max_tokens"},
		{PlatformGemini, "gemini", "gemini-3.1-pro", "high", "data: {\"modelVersion\":\"gemini-3.1-pro\",\"candidates\":[{\"content\":{\"parts\":[{\"thought\":true,\"text\":\"private reasoning\"},{\"text\":\"21\"}]},\"finishReason\":\"STOP\"}],\"usageMetadata\":{\"promptTokenCount\":13,\"candidatesTokenCount\":10,\"thoughtsTokenCount\":7}}\n\n", "contents.0.parts.0.text", "generationConfig.thinkingConfig.thinkingLevel", "generationConfig.maxOutputTokens"},
	} {
		t.Run(tc.platform, func(t *testing.T) {
			a := qualityProbeAPIAccount()
			a.Platform = tc.platform
			delete(a.Credentials, "model_mapping")
			svc, u, _ := qualityProbeFixture(a, tc.stream)
			req := qualityProbeRequest()
			req.APIMode, req.Model, req.Effort = tc.mode, tc.model, tc.effort
			r, err := svc.ProbeAccountText(context.Background(), req)
			require.NoError(t, err)
			require.Equal(t, "completed", r.ExecutionStatus)
			require.Equal(t, "21", r.Text)
			require.Equal(t, int64(13), r.InputTokens)
			if tc.platform == PlatformGemini {
				require.Equal(t, int64(17), r.OutputTokens)
			} else {
				require.Equal(t, int64(10), r.OutputTokens)
			}
			require.Equal(t, req.Prompt, gjson.GetBytes(u.bodies[0], tc.promptPath).String())
			require.Equal(t, "high", gjson.GetBytes(u.bodies[0], tc.effortPath).String())
			require.Equal(t, int64(128), gjson.GetBytes(u.bodies[0], tc.capPath).Int())
			require.True(t, r.AutoControlEligible)
		})
	}
}

func TestAccountQualityProbeRejectsUnvalidatedRoutesAndUnavailableSlotsBeforeTraffic(t *testing.T) {
	for _, tc := range []string{"shadow", "antigravity", "missing_proxy", "wrong_proxy", "concurrency", "unsupported_effort", "missing_concurrency"} {
		t.Run(tc, func(t *testing.T) {
			a := qualityProbeAPIAccount()
			svc, u, slots := qualityProbeFixture(a, "")
			req := qualityProbeRequest()
			switch tc {
			case "shadow":
				id := int64(99)
				a.ParentAccountID = &id
			case "antigravity":
				a.Platform, a.Type = PlatformAntigravity, AccountTypeOAuth
			case "missing_proxy":
				id := int64(9)
				a.ProxyID = &id
			case "wrong_proxy":
				id := int64(9)
				a.ProxyID, a.Proxy = &id, &Proxy{ID: 10}
			case "concurrency":
				slots.available = false
			case "unsupported_effort":
				req.Effort = "superhuman"
			case "missing_concurrency":
				svc.SetQualityProbeConcurrencyService(nil)
			}
			r, err := svc.ProbeAccountText(context.Background(), req)
			require.NoError(t, err)
			require.NotEqual(t, "completed", r.ExecutionStatus)
			require.False(t, r.AutoControlEligible)
			require.Empty(t, u.requests, "unvalidated execution must not issue upstream traffic")
		})
	}
}

func TestAccountQualityProbeClassifiesTimeoutCancellationAndHTTPWithoutLeakingCredentials(t *testing.T) {
	for _, tc := range []struct{ name, wantStatus, wantClass string }{
		{"transport_timeout", "error", "network_timeout"}, {"cancelled", "cancelled", "cancelled"}, {"rate_limit", "error", "rate_limit"}, {"auth", "error", "authentication"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			svc, u, slots := qualityProbeFixture(qualityProbeAPIAccount(), "")
			u.call = func(req *http.Request) (*http.Response, error) {
				switch tc.name {
				case "transport_timeout":
					return nil, fmt.Errorf("request failed: %w", context.DeadlineExceeded)
				case "cancelled":
					return nil, context.Canceled
				default:
					r := qualityProbeResponse(`{"error":{"message":"secret-api-key https://user:pass@proxy.example"}}`)
					r.StatusCode = http.StatusTooManyRequests
					if tc.name == "auth" {
						r.StatusCode = http.StatusUnauthorized
					}
					return r, nil
				}
			}
			r, err := svc.ProbeAccountText(context.Background(), qualityProbeRequest())
			require.NoError(t, err)
			require.Equal(t, tc.wantStatus, r.ExecutionStatus)
			require.Equal(t, tc.wantClass, r.ErrorClass)
			require.NotContains(t, r.ErrorMessage, "secret-api-key")
			require.NotContains(t, r.ErrorMessage, "user:pass")
			require.Equal(t, []int64{71}, slots.released)
		})
	}
}

func TestAccountQualityProbeTransportTimeoutBeforePolicyDeadlineIsNetworkFailure(t *testing.T) {
	for _, tc := range []struct {
		name string
		err  error
	}{
		{"dial_deadline", context.DeadlineExceeded},
		{"io_deadline", os.ErrDeadlineExceeded},
	} {
		t.Run(tc.name, func(t *testing.T) {
			svc, u, slots := qualityProbeFixture(qualityProbeAPIAccount(), "")
			u.call = func(req *http.Request) (*http.Response, error) {
				require.NoError(t, req.Context().Err(), "the diagnostic policy deadline is still active")
				return nil, &url.Error{
					Op:  "Post",
					URL: "https://user:proxy-secret@upstream.example/private?api_key=secret-api-key",
					Err: &net.OpError{Op: "dial", Net: "tcp", Err: tc.err},
				}
			}
			req := qualityProbeRequest()
			req.TimeoutSeconds = 120
			r, err := svc.ProbeAccountText(context.Background(), req)
			require.NoError(t, err)
			require.Equal(t, "error", r.ExecutionStatus)
			require.Equal(t, "network_timeout", r.ErrorClass)
			require.Zero(t, r.HTTPStatus)
			require.False(t, r.AutoControlEligible)
			require.NotContains(t, r.ErrorMessage, "exceeded its timeout")
			require.NotContains(t, r.ErrorMessage, "proxy-secret")
			require.NotContains(t, r.ErrorMessage, "secret-api-key")
			require.NotContains(t, r.ErrorMessage, "upstream.example")
			require.Equal(t, []int64{71}, slots.released)
		})
	}
}

func TestAccountQualityProbeGrokUsesGrokEndpointsAndRetainsParameters(t *testing.T) {
	for _, mode := range []string{"responses", "chat_completions"} {
		t.Run(mode, func(t *testing.T) {
			a := qualityProbeAPIAccount()
			a.Platform = PlatformGrok
			a.Credentials["model_mapping"] = map[string]any{"public-model": "grok-4.7"}
			body := `{"status":"completed","error":null,"model":"grok-4.7","output":[{"content":[{"type":"output_text","text":"21"}]}],"usage":{"input_tokens":4,"output_tokens":3}}`
			if mode == "chat_completions" {
				body = `{"model":"grok-4.7","choices":[{"message":{"content":"21"},"finish_reason":"stop"}],"usage":{"prompt_tokens":4,"completion_tokens":3}}`
			}
			svc, u, _ := qualityProbeFixture(a, body)
			req := qualityProbeRequest()
			req.APIMode = mode
			r, err := svc.ProbeAccountText(context.Background(), req)
			require.NoError(t, err)
			require.Equal(t, "completed", r.ExecutionStatus)
			require.Equal(t, "21", r.Text)
			require.Equal(t, "Bearer secret-api-key", u.requests[0].Header.Get("Authorization"))
			require.Equal(t, "upstream.example", u.requests[0].URL.Host)
			if mode == "responses" {
				require.Equal(t, "/v1/responses", u.requests[0].URL.Path)
				require.Equal(t, "high", gjson.GetBytes(u.bodies[0], "reasoning.effort").String())
			} else {
				require.Equal(t, "/v1/chat/completions", u.requests[0].URL.Path)
				require.Equal(t, "high", gjson.GetBytes(u.bodies[0], "reasoning_effort").String())
			}
			require.True(t, r.AutoControlEligible)
		})
	}
}

func TestAccountQualityProbeUnaryResponsesAcceptsNullError(t *testing.T) {
	svc, _, _ := qualityProbeFixture(qualityProbeAPIAccount(), `{"status":"completed","error":null,"model":"gpt-5.2","output":[{"content":[{"type":"output_text","text":"21"}]}],"usage":{"input_tokens":4,"output_tokens":3}}`)
	r, err := svc.ProbeAccountText(context.Background(), qualityProbeRequest())
	require.NoError(t, err)
	require.Equal(t, "completed", r.ExecutionStatus)
	require.Equal(t, "21", r.Text)
	require.Equal(t, int64(4), r.InputTokens)
}

func TestAccountQualityProbeLocalOutputBoundKeepsValidUTF8AndCannotComplete(t *testing.T) {
	a := &Account{ID: 71, Platform: PlatformOpenAI, Type: AccountTypeOAuth, Concurrency: 1, Credentials: map[string]any{"access_token": "local-token"}}
	svc, _, slots := qualityProbeFixture(a, "data: {\"type\":\"response.output_text.delta\",\"delta\":\""+strings.Repeat("糖", 20)+"\"}\n\ndata: {\"type\":\"response.completed\",\"response\":{\"status\":\"completed\"}}\n\n")
	req := qualityProbeRequest()
	req.Model, req.MaxOutputTokens = "gpt-5.6", 1
	r, err := svc.ProbeAccountText(context.Background(), req)
	require.NoError(t, err)
	require.Equal(t, "truncated", r.ExecutionStatus)
	require.Equal(t, "output_limit", r.ErrorClass)
	require.Equal(t, strings.Repeat("糖", 5), r.Text)
	require.False(t, r.AutoControlEligible)
	require.Equal(t, []int64{71}, slots.released)
}

func TestAccountQualityProbeOAuthReusesRefreshedCredentialProvider(t *testing.T) {
	a := &Account{ID: 71, Platform: PlatformOpenAI, Type: AccountTypeOAuth, Concurrency: 1, Credentials: map[string]any{"access_token": "stale-token", "chatgpt_account_id": "stable"}}
	svc, u, _ := qualityProbeFixture(a, "data: {\"type\":\"response.completed\",\"response\":{\"status\":\"completed\"}}\n\n")
	svc.SetOpenAIGatewayService(&OpenAIGatewayService{openAITokenProvider: &OpenAITokenProvider{tokenCache: &openAITokenCacheStub{tokens: map[string]string{OpenAITokenCacheKey(a): "fresh-cached-token"}}}})
	req := qualityProbeRequest()
	req.Model = "gpt-5.6"
	r, err := svc.ProbeAccountText(context.Background(), req)
	require.NoError(t, err)
	require.Equal(t, "completed", r.ExecutionStatus)
	require.Equal(t, "Bearer fresh-cached-token", u.requests[0].Header.Get("Authorization"))
}

func TestAccountQualityProbeWrappedGeminiOAuthForwardsFullOptions(t *testing.T) {
	a := &Account{ID: 71, Platform: PlatformGemini, Type: AccountTypeOAuth, Concurrency: 1, Credentials: map[string]any{"access_token": "gemini-oauth", "project_id": "probe-project"}}
	svc, u, _ := qualityProbeFixture(a, "data: {\"response\":{\"modelVersion\":\"gemini-3.1-pro\",\"candidates\":[{\"content\":{\"parts\":[{\"text\":\"21\"}]},\"finishReason\":\"STOP\"}],\"usageMetadata\":{\"promptTokenCount\":4,\"candidatesTokenCount\":3}}}\n\n")
	svc.geminiTokenProvider = &GeminiTokenProvider{}
	req := qualityProbeRequest()
	req.Model, req.APIMode = "gemini-3.1-pro", "gemini"
	r, err := svc.ProbeAccountText(context.Background(), req)
	require.NoError(t, err)
	require.Equal(t, "completed", r.ExecutionStatus)
	require.Equal(t, "21", r.Text)
	require.Equal(t, "Bearer gemini-oauth", u.requests[0].Header.Get("Authorization"))
	require.Equal(t, "probe-project", gjson.GetBytes(u.bodies[0], "project").String())
	require.Equal(t, req.Prompt, gjson.GetBytes(u.bodies[0], "request.contents.0.parts.0.text").String())
	require.Equal(t, int64(128), gjson.GetBytes(u.bodies[0], "request.generationConfig.maxOutputTokens").Int())
	require.Equal(t, "high", gjson.GetBytes(u.bodies[0], "request.generationConfig.thinkingConfig.thinkingLevel").String())
}

func TestAccountQualityProbeAnthropicOAuthUsesProviderAndFreshMetadata(t *testing.T) {
	a := &Account{ID: 71, Platform: PlatformAnthropic, Type: AccountTypeOAuth, Concurrency: 1, Credentials: map[string]any{"access_token": "claude-oauth"}}
	svc, u, _ := qualityProbeFixture(a, "data: {\"type\":\"message_start\",\"message\":{\"model\":\"claude-sonnet-4-6\"}}\n\ndata: {\"type\":\"content_block_delta\",\"delta\":{\"type\":\"text_delta\",\"text\":\"21\"}}\n\ndata: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"end_turn\"}}\n\ndata: {\"type\":\"message_stop\"}\n\n")
	svc.claudeTokenProvider = &ClaudeTokenProvider{}
	req := qualityProbeRequest()
	req.Model, req.APIMode = "claude-sonnet-4-6", "anthropic"
	for i := 0; i < 2; i++ {
		r, err := svc.ProbeAccountText(context.Background(), req)
		require.NoError(t, err)
		require.Equal(t, "completed", r.ExecutionStatus)
		require.Equal(t, "Bearer claude-oauth", u.requests[i].Header.Get("Authorization"))
		require.Equal(t, req.Prompt, gjson.GetBytes(u.bodies[i], "messages.0.content.0.text").String())
	}
	require.NotEmpty(t, gjson.GetBytes(u.bodies[0], "metadata.user_id").String())
	require.NotEqual(t, gjson.GetBytes(u.bodies[0], "metadata.user_id").String(), gjson.GetBytes(u.bodies[1], "metadata.user_id").String())
}

func TestAccountQualityProbeVertexAPIKeyForwardsNativeOptions(t *testing.T) {
	a := &Account{ID: 71, Platform: PlatformGemini, Type: AccountTypeServiceAccount, Concurrency: 1, Credentials: map[string]any{"auth_mode": "apikey", "api_key": "vertex-key", "project_id": "probe-project", "location": "global"}}
	svc, u, _ := qualityProbeFixture(a, `{"modelVersion":"gemini-3.1-pro","candidates":[{"content":{"parts":[{"text":"21"}]},"finishReason":"STOP"}]}`)
	req := qualityProbeRequest()
	req.Model, req.APIMode = "gemini-3.1-pro", "gemini"
	r, err := svc.ProbeAccountText(context.Background(), req)
	require.NoError(t, err)
	require.Equal(t, "completed", r.ExecutionStatus)
	require.Equal(t, "vertex-key", u.requests[0].Header.Get("x-goog-api-key"))
	require.Equal(t, "aiplatform.googleapis.com", u.requests[0].URL.Host)
	require.Contains(t, u.requests[0].URL.Path, "/projects/probe-project/locations/global/")
	require.Equal(t, int64(128), gjson.GetBytes(u.bodies[0], "generationConfig.maxOutputTokens").Int())
	require.Equal(t, "high", gjson.GetBytes(u.bodies[0], "generationConfig.thinkingConfig.thinkingLevel").String())
}

func TestAccountQualityProbeHonorsRequestDeadlineAndReleasesAccountSlot(t *testing.T) {
	svc, u, slots := qualityProbeFixture(qualityProbeAPIAccount(), "")
	u.call = func(req *http.Request) (*http.Response, error) {
		<-req.Context().Done()
		return nil, req.Context().Err()
	}
	req := qualityProbeRequest()
	req.TimeoutSeconds = 1
	r, err := svc.ProbeAccountText(context.Background(), req)
	require.NoError(t, err)
	require.Equal(t, "timeout", r.ExecutionStatus)
	require.Equal(t, "timeout", r.ErrorClass)
	require.Equal(t, []int64{71}, slots.released)
	require.GreaterOrEqual(t, r.LatencyMS, int64(900))
}

func TestAccountQualityProbeRejectsUnsupportedEffortForNonReasoningModel(t *testing.T) {
	a := qualityProbeAPIAccount()
	a.Credentials["model_mapping"] = map[string]any{"public-model": "gpt-4.1"}
	svc, u, _ := qualityProbeFixture(a, "")
	r, err := svc.ProbeAccountText(context.Background(), qualityProbeRequest())
	require.NoError(t, err)
	require.Equal(t, "unsupported", r.ExecutionStatus)
	require.Equal(t, "unsupported_effort", r.ErrorClass)
	require.Empty(t, u.requests)
}

func TestAccountQualityProbePassthroughKeepsTheExactBusinessModel(t *testing.T) {
	for _, typ := range []string{AccountTypeAPIKey, AccountTypeOAuth} {
		for _, flag := range []string{"openai_passthrough", "openai_oauth_passthrough"} {
			t.Run(typ+"/"+flag, func(t *testing.T) {
				a := qualityProbeAPIAccount()
				a.Type = typ
				a.Credentials["access_token"] = "passthrough-oauth"
				a.Credentials["model_mapping"] = map[string]any{"gpt-5.6": "gpt-5.2"}
				a.Extra = map[string]any{flag: true}
				svc, u, _ := qualityProbeFixture(a, `{"status":"completed","model":"gpt-5.6","output":[{"content":[{"type":"output_text","text":"21"}]}]}`)
				req := qualityProbeRequest()
				req.Model = "gpt-5.6"
				r, err := svc.ProbeAccountText(context.Background(), req)
				require.NoError(t, err)
				require.Equal(t, "completed", r.ExecutionStatus)
				require.Equal(t, "gpt-5.6", r.MappedModel, "a stale account mapping must not redirect a passthrough diagnostic")
				require.Equal(t, "gpt-5.6", gjson.GetBytes(u.bodies[0], "model").String(), "OAuth must not normalize the business model again after the shared resolver")
			})
		}
	}
}

func TestAccountQualityProbeAdmissionBypassesOnlyItsOwnedQualityGate(t *testing.T) {
	for _, tc := range []string{"own_only", "other_owner", "other_model", "manual_disabled", "status_error", "rate_limited", "missing_owner"} {
		t.Run(tc, func(t *testing.T) {
			a := qualityProbeAPIAccount()
			a.Status, a.Schedulable = StatusActive, true
			gates := map[string]any{"12": map[string]any{"blocked": true, "scope": "model", "model": "public-model"}}
			a.Extra = map[string]any{AccountQualityGatesExtraKey: gates, "unrelated": "retain"}
			req := qualityProbeRequest()
			switch tc {
			case "other_owner":
				gates["99"] = map[string]any{"blocked": true, "scope": "account"}
			case "other_model":
				gates["99"] = map[string]any{"blocked": true, "scope": "model", "model": "gpt-4.1"}
			case "manual_disabled":
				a.Schedulable = false
			case "status_error":
				a.Status = StatusError
			case "rate_limited":
				until := time.Now().Add(time.Hour)
				a.RateLimitResetAt = &until
			case "missing_owner":
				req.BindingID = 0
			}
			svc, u, slots := qualityProbeFixture(a, `{"status":"completed","model":"gpt-5.2","output":[{"content":[{"type":"output_text","text":"21"}]}]}`)
			r, err := svc.ProbeAccountText(context.Background(), req)
			require.NoError(t, err)
			if tc == "own_only" || tc == "other_model" {
				require.Equal(t, "completed", r.ExecutionStatus)
				require.Len(t, u.requests, 1)
			} else {
				require.Equal(t, "error", r.ExecutionStatus)
				require.Equal(t, "admission_blocked", r.ErrorClass)
				require.Empty(t, u.requests)
				require.Empty(t, slots.acquired)
			}
			require.Equal(t, true, gates["12"].(map[string]any)["blocked"], "admission must not change the original owner's gate")
			require.Contains(t, a.Extra[AccountQualityGatesExtraKey].(map[string]any), "12")
			require.Equal(t, "retain", a.Extra["unrelated"])
		})
	}
}

func TestAccountQualityProbeManualPausedAccountRetainsBusinessPause(t *testing.T) {
	for _, typ := range []string{AccountTypeAPIKey, AccountTypeOAuth} {
		for _, manual := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/manual_%t", typ, manual), func(t *testing.T) {
				a := qualityProbeAPIAccount()
				a.Type, a.Status, a.Schedulable = typ, StatusActive, false
				a.Credentials["access_token"] = "oauth-secret"
				gates := map[string]any{"12": map[string]any{"blocked": true, "scope": "model", "model": "public-model"}}
				a.Extra = map[string]any{AccountQualityGatesExtraKey: gates}
				svc, u, slots := qualityProbeFixture(a, `{"status":"completed","model":"gpt-5.2","output":[{"content":[{"type":"output_text","text":"21"}]}]}`)
				req := qualityProbeRequest()
				req.Manual = manual
				r, err := svc.ProbeAccountText(context.Background(), req)
				require.NoError(t, err)
				if manual {
					require.Equal(t, "completed", r.ExecutionStatus)
					require.Equal(t, "21", r.Text)
					require.Equal(t, []int64{71}, u.accountIDs)
					require.Equal(t, []int64{71}, slots.released)
				} else {
					require.Equal(t, "admission_blocked", r.ErrorClass)
					require.Empty(t, u.requests, "scheduled probes must respect the business pause")
					require.Empty(t, slots.acquired)
				}
				require.Equal(t, StatusActive, a.Status)
				require.False(t, a.Schedulable, "manual diagnostics must not enable business routing")
				require.True(t, gates["12"].(map[string]any)["blocked"].(bool), "manual diagnostics must not remove the stored quality gate")
				require.False(t, a.IsSchedulableForModelWithContext(context.Background(), req.Model))
			})
		}
	}
}

func TestAccountQualityProbeManualPauseBypassPreservesIndependentControls(t *testing.T) {
	for _, tc := range []string{"disabled", "error", "expired", "overloaded", "rate_limited", "cooldown", "quota", "model_rate_limited", "other_owner_account", "other_owner_model", "missing_owner", "missing_proxy", "wrong_proxy", "missing_concurrency", "concurrency", "missing_credential"} {
		t.Run(tc, func(t *testing.T) {
			a := qualityProbeAPIAccount()
			a.Status, a.Schedulable = StatusActive, false
			gates := map[string]any{"12": map[string]any{"blocked": true, "scope": "model", "model": "public-model"}}
			a.Extra = map[string]any{AccountQualityGatesExtraKey: gates}
			req := qualityProbeRequest()
			req.Manual = true
			svc, u, slots := qualityProbeFixture(a, "")
			future, past := time.Now().Add(time.Hour), time.Now().Add(-time.Hour)
			wantClass := "admission_blocked"
			switch tc {
			case "disabled":
				a.Status = StatusDisabled
			case "error":
				a.Status = StatusError
			case "expired":
				a.AutoPauseOnExpired, a.ExpiresAt = true, &past
			case "overloaded":
				a.OverloadUntil = &future
			case "rate_limited":
				a.RateLimitResetAt = &future
			case "cooldown":
				a.TempUnschedulableUntil = &future
			case "quota":
				a.Extra["quota_limit"], a.Extra["quota_used"] = float64(1), float64(1)
			case "model_rate_limited":
				a.Extra[modelRateLimitsKey] = map[string]any{"gpt-5.2": map[string]any{"rate_limit_reset_at": future.Format(time.RFC3339)}}
			case "other_owner_account":
				gates["99"] = map[string]any{"blocked": true, "scope": "account"}
			case "other_owner_model":
				gates["99"] = map[string]any{"blocked": true, "scope": "model", "model": "public-model"}
			case "missing_owner":
				req.BindingID = 0
			case "missing_proxy", "wrong_proxy":
				id := int64(9)
				a.ProxyID = &id
				if tc == "wrong_proxy" {
					a.Proxy = &Proxy{ID: 10}
				}
				wantClass = "proxy_unavailable"
			case "missing_concurrency":
				svc.SetQualityProbeConcurrencyService(nil)
				wantClass = "concurrency_unavailable"
			case "concurrency":
				slots.available = false
				wantClass = "concurrency_unavailable"
			case "missing_credential":
				delete(a.Credentials, "api_key")
				wantClass = "request_preparation"
			}
			r, err := svc.ProbeAccountText(context.Background(), req)
			require.NoError(t, err)
			require.Equal(t, "error", r.ExecutionStatus)
			require.Equal(t, wantClass, r.ErrorClass)
			require.False(t, r.AutoControlEligible)
			require.Empty(t, u.requests, "a manual diagnostic must retain independent controls")
			require.False(t, a.Schedulable)
			require.Contains(t, a.Extra[AccountQualityGatesExtraKey].(map[string]any), "12")
		})
	}
}
