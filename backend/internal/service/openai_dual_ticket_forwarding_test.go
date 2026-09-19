package service

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

func TestCodexDualTicketForwardingUsesSelectedMechanism(t *testing.T) {
	for _, mode := range []string{"292", "332"} {
		for _, transport := range []string{"http", "passthrough", "websocket"} {
			t.Run(mode+"/"+transport, func(t *testing.T) {
				svc := &OpenAIGatewayService{cfg: &config.Config{Gateway: config.GatewayConfig{
					OpenAICodexTicket:    config.OpenAICodexTicketConfig{Enabled: true, FailClosed: true},
					OpenAICodexTicket332: config.OpenAICodexTicketConfig{Enabled: true, FailClosed: true},
				}}}
				account := ticketTestAccount(41)
				account.Credentials["access_token"] = "test-token"
				account.Extra = map[string]any{"codex_ticket_mode": mode}
				for _, length := range []int{292, 332} {
					account.Extra["codex_turn_ticket:"+strconv.Itoa(length)+":gpt-6-astra"] = boundCodexTicketFixture(account, "gpt-6-astra", length)
				}
				length, err := strconv.Atoi(mode)
				require.NoError(t, err)
				build := func(account *Account) (http.Header, error) {
					c, _ := newTurnStateTestContext(t, 7, "dual-ticket-session")
					c.Request.Header.Set(openAICodexTurnStateHeader, "client-state")
					body := []byte(`{"model":"gpt-6-astra","stream":true}`)
					if transport == "websocket" {
						h, _, buildErr := svc.buildOpenAIWSHeaders(context.Background(), c, account, "test-token",
							OpenAIWSProtocolDecision{Transport: OpenAIUpstreamTransportResponsesWebsocketV2},
							true, "client-state", "", "", "gpt-6-astra", "")
						return h, buildErr
					}
					var req *http.Request
					var buildErr error
					if transport == "passthrough" {
						req, buildErr = svc.buildUpstreamRequestOpenAIPassthrough(context.Background(), c, account, body, "test-token")
					} else {
						req, buildErr = svc.buildUpstreamRequest(context.Background(), c, account, body, "test-token", true, "", true)
					}
					if buildErr != nil {
						return nil, buildErr
					}
					if req.Body != nil {
						_ = req.Body.Close()
					}
					return req.Header, nil
				}
				headers, err := build(account)
				require.NoError(t, err)
				require.Equal(t, []string{fakeCodexTicketState(length)}, headers.Values(openAICodexTurnStateHeader))

				// Another account cannot borrow the first account's populated cache.
				missing := ticketTestAccount(42)
				missing.Extra = map[string]any{"codex_ticket_mode": mode}
				_, err = build(missing)
				require.ErrorIs(t, err, ErrOpenAICodexTicketUnavailable)
			})
		}
	}
}

func TestCodexDualTicketMessagesBridgeInjectsSelectedMechanism(t *testing.T) {
	for _, mode := range []string{"292", "332"} {
		t.Run(mode, func(t *testing.T) {
			upstream := &httpUpstreamRecorder{err: errors.New("test transport stopped after request capture")}
			svc := &OpenAIGatewayService{
				cfg: &config.Config{Gateway: config.GatewayConfig{
					OpenAICodexTicket:    config.OpenAICodexTicketConfig{Enabled: true, FailClosed: true},
					OpenAICodexTicket332: config.OpenAICodexTicketConfig{Enabled: true, FailClosed: true},
				}},
				httpUpstream: upstream,
			}
			length, err := strconv.Atoi(mode)
			require.NoError(t, err)
			account := ticketTestAccount(41)
			account.Extra = map[string]any{"codex_ticket_mode": mode}
			svc.storeOpenAICodexTicket(context.Background(), account, &openAICodexTicket{
				Model: "gpt-6-astra", State: fakeCodexTicketState(length), Length: length,
				CapturedAt: time.Now(), ExpiresAt: time.Now().Add(time.Hour),
			})
			body := []byte(`{"model":"gpt-6-astra","max_tokens":32,"messages":[{"role":"user","content":"hello"}],"stream":false}`)
			c, _ := newTurnStateTestContext(t, 7, "messages-ticket")
			c.Request.URL.Path = "/v1/messages"
			_, err = svc.ForwardAsAnthropic(context.Background(), c, account, body, "", "")
			require.Error(t, err, "the local mock intentionally stops at the transport boundary")
			require.Len(t, upstream.requests, 1)
			require.Equal(t, []string{fakeCodexTicketState(length)}, upstream.requests[0].Header.Values(openAICodexTurnStateHeader))
		})
	}
}
