package service

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

type codexResponseHookDialer struct {
	conn    openAIWSClientConn
	status  int
	headers chan http.Header
}

func (d *codexResponseHookDialer) Dial(_ context.Context, _ string, headers http.Header, _ string) (openAIWSClientConn, int, http.Header, error) {
	d.headers <- headers.Clone()
	if d.status != http.StatusSwitchingProtocols {
		return nil, d.status, http.Header{"Retry-After": {"620"}}, errors.New("fixture handshake rejection")
	}
	return d.conn, d.status, http.Header{}, nil
}

func seedCodexResponseHookTicket(t *testing.T, svc *OpenAIGatewayService, account *Account, mode string) string {
	t.Helper()
	length, err := strconv.Atoi(mode)
	require.NoError(t, err)
	now := time.Now().Truncate(time.Second)
	state := codexTicketStateAt(length, now.Add(-10*time.Second))
	svc.storeOpenAICodexTicket(context.Background(), account, &openAICodexTicket{
		AccountID: account.ID, Model: "gpt-6-astra", State: state, Length: length,
		IssuedAt: now.Add(-10 * time.Second), CapturedAt: now, ExpiresAt: now.Add(50 * time.Minute),
		CredentialHash: OpenAICodexTicketCredentialHash(account),
	})
	require.NotNil(t, svc.lookupOpenAICodexTicket(account, "gpt-6-astra"))
	return state
}

func TestCodexTicketHTTPResponseHooks(t *testing.T) {
	for _, mode := range []string{"292", "332"} {
		for _, path := range []string{"responses", "passthrough", "messages", "compact", "chat-completions", "ws-http-bridge"} {
			for _, status := range []int{401, 403, 429} {
				t.Run(mode+"/"+path+"/"+strconv.Itoa(status), func(t *testing.T) {
					upstream := &httpUpstreamRecorder{resp: &http.Response{
						StatusCode: status,
						Header:     http.Header{"Content-Type": {"application/json"}, "Retry-After": {"620"}},
						Body:       io.NopCloser(strings.NewReader(`{"error":{"message":"fixture rejection"}}`)),
					}}
					svc := &OpenAIGatewayService{cfg: &config.Config{Gateway: config.GatewayConfig{
						OpenAICodexTicket:    config.OpenAICodexTicketConfig{Enabled: true, FailClosed: true},
						OpenAICodexTicket332: config.OpenAICodexTicketConfig{Enabled: true, FailClosed: true},
					}}, httpUpstream: upstream, toolCorrector: NewCodexToolCorrector()}
					account := ticketTestAccount(41)
					account.Extra = map[string]any{"codex_ticket_mode": mode, "openai_passthrough": path == "passthrough"}
					state := seedCodexResponseHookTicket(t, svc, account, mode)
					c, _ := newTurnStateTestContext(t, 7, "response-hook")
					body := []byte(`{"model":"gpt-6-astra","instructions":"fixture","input":"hi","stream":true}`)
					c.Request.URL.Path = "/v1/responses"
					switch path {
					case "messages":
						c.Request.URL.Path = "/v1/messages"
						body = []byte(`{"model":"gpt-6-astra","max_tokens":16,"messages":[{"role":"user","content":"hi"}],"stream":true}`)
						_, _ = svc.ForwardAsAnthropic(context.Background(), c, account, body, "", "")
					case "chat-completions":
						c.Request.URL.Path = "/v1/chat/completions"
						body = []byte(`{"model":"gpt-6-astra","messages":[{"role":"user","content":"hi"}],"stream":true}`)
						_, _ = svc.ForwardAsChatCompletions(context.Background(), c, account, body, "", "")
					case "ws-http-bridge":
						body = []byte(`{"type":"response.create","model":"gpt-6-astra","instructions":"fixture","input":"hi"}`)
						_, _ = svc.proxyOpenAIWSHTTPBridgeTurn(context.Background(), c, account, "tok", body, len(body),
							"gpt-6-astra", "", "", "", "", 1, func([]byte) error { return nil })
					default:
						if path == "compact" {
							c.Request.URL.Path = "/v1/responses/compact"
						}
						_, _ = svc.Forward(context.Background(), c, account, body)
					}
					require.NotEmpty(t, upstream.requests, "must exercise a real forwarding boundary using the local mock")
					require.Equal(t, state, upstream.requests[0].Header.Get(openAICodexTurnStateHeader))
					if status == 429 {
						require.NotNil(t, svc.lookupOpenAICodexTicket(account, "gpt-6-astra"), "rate limiting must retain the ticket")
						runtime := svc.loadOpenAICodexTicketRuntime(account, mode, "gpt-6-astra")
						require.Greater(t, time.Until(runtime.NextAttemptAt), 615*time.Second, "respect the upstream Retry-After")
					} else {
						require.Nil(t, svc.lookupOpenAICodexTicket(account, "gpt-6-astra"), "the actual used ticket must be revoked")
					}
				})
			}
		}
	}
}

func TestCodexTicketBuildersRejectChangedOutboundCredential(t *testing.T) {
	for _, transport := range []string{"http", "passthrough", "websocket"} {
		t.Run(transport, func(t *testing.T) {
			svc := ticketTestService(t, config.OpenAICodexTicketConfig{Enabled: true, FailClosed: true}, nil)
			account := ticketTestAccount(41)
			seedCodexResponseHookTicket(t, svc, account, "292")
			c, _ := newTurnStateTestContext(t, 7, "identity-test")
			body := []byte(`{"model":"gpt-6-astra","stream":true}`)
			var err error
			switch transport {
			case "http":
				_, err = svc.buildUpstreamRequest(context.Background(), c, account, body, "changed-token", true, "", true)
			case "passthrough":
				_, err = svc.buildUpstreamRequestOpenAIPassthrough(context.Background(), c, account, body, "changed-token")
			case "websocket":
				_, _, err = svc.buildOpenAIWSHeaders(context.Background(), c, account, "changed-token",
					OpenAIWSProtocolDecision{Transport: OpenAIUpstreamTransportResponsesWebsocketV2}, true, "", "", "", "gpt-6-astra", "")
			}
			require.ErrorIs(t, err, ErrOpenAICodexTicketUnavailable, "a ticket may only accompany its bound outbound bearer")
		})
	}
}

func TestCodexTicketWebSocketResponseHooks(t *testing.T) {
	for _, transport := range []string{"http-to-ws", "http-to-ws-prewarm", OpenAIWSIngressModeCtxPool, OpenAIWSIngressModePassthrough} {
		for _, tc := range []struct {
			name        string
			status      int
			event       string
			rateLimited bool
		}{
			{name: "handshake_auth", status: 401},
			{name: "handshake_rate_limit", status: 429, rateLimited: true},
			{name: "frame_auth", status: 101, event: `{"type":"error","error":{"type":"authentication_error","code":"invalid_api_key","message":"fixture"}}`},
			{name: "generic_wrapper_auth", status: 101, event: `{"type":"error","error":{"type":"invalid_request_error","code":"invalid_api_key","message":"fixture"}}`},
			{name: "failed_response_auth", status: 101, event: `{"type":"response.failed","response":{"id":"fixture","status":"failed","error":{"type":"permission_error","code":"forbidden","message":"fixture"}}}`},
			{name: "frame_rate_limit", status: 101, event: `{"type":"error","error":{"type":"rate_limit_error","code":"rate_limit_exceeded","resets_in_seconds":620}}`, rateLimited: true},
			{name: "generic_wrapper_rate_limit", status: 101, event: `{"type":"error","error":{"type":"invalid_request_error","code":"usage_limit_reached","resets_in_seconds":620}}`, rateLimited: true},
		} {
			t.Run(transport+"/"+tc.name, func(t *testing.T) {
				ctx, cancel := context.WithTimeout(context.Background(), 6*time.Second)
				defer cancel()
				cfg := passthroughLifecycleConfig()
				cfg.Gateway.OpenAIWS.OAuthEnabled = true
				cfg.Gateway.OpenAIWS.PrewarmGenerateEnabled = transport == "http-to-ws-prewarm"
				cfg.Gateway.OpenAIWS.MaxConnsPerAccount = 1
				cfg.Gateway.OpenAIWS.MaxIdlePerAccount = 1
				cfg.Gateway.OpenAICodexTicket = config.OpenAICodexTicketConfig{Enabled: true, FailClosed: true}
				upstream := &codexWSTicketStagedConn{newStagedPassthroughConn()}
				dialer := &codexResponseHookDialer{conn: upstream, status: tc.status, headers: make(chan http.Header, 4)}
				svc := newPassthroughLifecycleService(cfg, upstream.stagedPassthroughConn)
				svc.openaiWSPassthroughDialer = dialer
				pool := newOpenAIWSConnPool(cfg)
				pool.setClientDialerForTest(dialer)
				svc.openaiWSPool = pool
				t.Cleanup(pool.Close)
				account := ticketTestAccount(41)
				account.Credentials["access_token"] = "sk-test" // ingress fixture supplies this bearer
				account.Status, account.Concurrency = StatusActive, 1
				account.Extra = map[string]any{"codex_ticket_mode": "292", "openai_oauth_responses_websockets_v2_mode": transport}
				state := seedCodexResponseHookTicket(t, svc, account, "292")
				if strings.HasPrefix(transport, "http-to-ws") {
					if tc.event != "" {
						upstream.Send(tc.event)
					}
					c, _ := newTurnStateTestContext(t, 7, "ws-response-hook")
					_, _ = svc.forwardOpenAIWSV2(ctx, c, account, map[string]any{"model": "gpt-6-astra", "input": "hi"}, "", "", "sk-test",
						OpenAIWSProtocolDecision{Transport: OpenAIUpstreamTransportResponsesWebsocketV2}, true, false,
						"gpt-6-astra", "gpt-6-astra", time.Now(), 0, "", nil)
				} else {
					server, done := startPassthroughLifecycleServer(t, ctx, svc, account)
					defer server.Close()
					client := dialPassthroughLifecycleClientWithPayload(t, server, `{"type":"response.create","model":"gpt-6-astra"}`)
					defer func() { _ = client.CloseNow() }()
					if tc.event != "" {
						_ = requirePassthroughUpstreamWrite(t, upstream.stagedPassthroughConn, 2*time.Second)
						upstream.Send(tc.event)
					}
					_, _ = readPassthroughLifecycleFrame(t, client, 2*time.Second)
					_ = client.CloseNow()
					select {
					case <-done:
					case <-ctx.Done():
						t.Fatal("websocket forwarding did not exit")
					}
				}
				select {
				case headers := <-dialer.headers:
					require.Equal(t, state, headers.Get(openAICodexTurnStateHeader))
				default:
					t.Fatal("expected the mock upstream handshake to be reached")
				}
				if tc.rateLimited {
					require.NotNil(t, svc.lookupOpenAICodexTicket(account, "gpt-6-astra"))
					runtime := svc.loadOpenAICodexTicketRuntime(account, "292", "gpt-6-astra")
					require.Greater(t, time.Until(runtime.NextAttemptAt), 610*time.Second)
				} else {
					require.Nil(t, svc.lookupOpenAICodexTicket(account, "gpt-6-astra"), "auth failure must revoke the handshake ticket")
				}
			})
		}
	}
}
