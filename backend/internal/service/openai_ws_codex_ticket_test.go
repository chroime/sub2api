package service

import (
	"context"
	"encoding/json"
	"errors"
	"maps"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	coderws "github.com/coder/websocket"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func codexWSTicketHeaders(t *testing.T, svc *OpenAIGatewayService, account *Account, model, clientState string) http.Header {
	t.Helper()
	h, _, err := svc.buildOpenAIWSHeaders(context.Background(), nil, account, "fixture",
		OpenAIWSProtocolDecision{Transport: OpenAIUpstreamTransportResponsesWebsocketV2}, true,
		clientState, "", "", model, "")
	require.NoError(t, err)
	return h
}

func TestCodexWSTicketPoolSeparatesServerTickets(t *testing.T) {
	cfg := &config.Config{}
	cfg.Gateway.OpenAIWS.MaxConnsPerAccount = 1
	cfg.Gateway.OpenAIWS.MaxIdlePerAccount = 1
	cfg.Gateway.OpenAICodexTicket = config.OpenAICodexTicketConfig{Enabled: true, FailClosed: true}
	cfg.Gateway.OpenAICodexTicket332 = config.OpenAICodexTicketConfig{Enabled: true, FailClosed: true}
	svc := &OpenAIGatewayService{cfg: cfg}
	account := ticketTestAccount(41)
	account.Extra = map[string]any{"codex_ticket_mode": "292"}
	for _, length := range []int{292, 332} {
		mode := "292"
		if length == 332 {
			mode = "332"
		}
		account.Extra["codex_turn_ticket:"+mode+":gpt-6-astra"] = &openAICodexTicket{
			AccountID: account.ID, Model: "gpt-6-astra", State: fakeCodexTicketState(length), Length: length,
			ExpiresAt: time.Now().Add(time.Hour),
		}
	}
	pool := newOpenAIWSConnPool(cfg)
	t.Cleanup(pool.Close)
	dialer := &openAIWSCountingDialer{}
	pool.setClientDialerForTest(dialer)
	first, err := pool.Acquire(context.Background(), openAIWSAcquireRequest{
		Account: account, WSURL: "wss://example.invalid/responses", Headers: codexWSTicketHeaders(t, svc, account, "gpt-6-astra", ""),
	})
	require.NoError(t, err)
	firstID := first.ConnID()
	first.Release()
	account.Extra["codex_ticket_mode"] = "332"
	second, err := pool.Acquire(context.Background(), openAIWSAcquireRequest{
		Account: account, WSURL: "wss://example.invalid/responses", Headers: codexWSTicketHeaders(t, svc, account, "gpt-6-astra", ""),
	})
	require.NoError(t, err)
	defer second.Release()
	require.NotEqual(t, firstID, second.ConnID(), "a 332 request must not reuse a connection authenticated with a 292 ticket")
	require.Equal(t, 2, dialer.DialCount())
}

func TestCodexWSTicketClientTurnStateKeepsExistingPoolReuse(t *testing.T) {
	for _, enabled := range []bool{false, true} {
		t.Run(map[bool]string{false: "disabled", true: "fail-open"}[enabled], func(t *testing.T) {
			cfg := &config.Config{}
			cfg.Gateway.OpenAIWS.MaxConnsPerAccount = 1
			cfg.Gateway.OpenAIWS.MaxIdlePerAccount = 1
			cfg.Gateway.OpenAICodexTicket = config.OpenAICodexTicketConfig{Enabled: enabled, FailClosed: false}
			svc := &OpenAIGatewayService{cfg: cfg}
			account := ticketTestAccount(41)
			firstHeaders := codexWSTicketHeaders(t, svc, account, "gpt-6-astra", "client-first-state")
			nextHeaders := codexWSTicketHeaders(t, svc, account, "gpt-6-astra", "client-second-state")
			require.Empty(t, firstHeaders.Get(openAIWSCodexTicketSignatureHeader))
			require.Empty(t, nextHeaders.Get(openAIWSCodexTicketSignatureHeader))
			require.Equal(t, "client-second-state", nextHeaders.Get(openAICodexTurnStateHeader))
			pool := newOpenAIWSConnPool(cfg)
			t.Cleanup(pool.Close)
			dialer := &openAIWSCountingDialer{}
			pool.setClientDialerForTest(dialer)
			first, err := pool.Acquire(context.Background(), openAIWSAcquireRequest{Account: account, WSURL: "wss://example.invalid/responses", Headers: firstHeaders})
			require.NoError(t, err)
			id := first.ConnID()
			first.Release()
			next, err := pool.Acquire(context.Background(), openAIWSAcquireRequest{Account: account, WSURL: "wss://example.invalid/responses", Headers: nextHeaders})
			require.NoError(t, err)
			defer next.Release()
			require.Equal(t, id, next.ConnID())
			require.Equal(t, 1, dialer.DialCount())
		})
	}
}

func TestCodexWSTicketSignatureBindsIdentityWithoutRawSecret(t *testing.T) {
	svc := ticketTestService(t, config.OpenAICodexTicketConfig{Enabled: true, FailClosed: true}, nil)
	account := ticketTestAccount(41)
	for _, model := range []string{"gpt-6-astra", "gpt-5.6-sol"} {
		svc.storeOpenAICodexTicket(context.Background(), account, &openAICodexTicket{
			Model: model, State: fakeCodexTicketState(292), Length: 292, ExpiresAt: time.Now().Add(time.Hour),
		})
	}
	first := codexWSTicketHeaders(t, svc, account, "gpt-6-astra", "client-a")
	firstSignature := first.Get(openAIWSCodexTicketSignatureHeader)
	require.Regexp(t, "^[0-9a-f]{64}$", firstSignature)
	require.NotContains(t, firstSignature, fakeCodexTicketState(292))
	require.Equal(t, firstSignature, codexWSTicketHeaders(t, svc, account, "gpt-6-astra", "client-b").Get(openAIWSCodexTicketSignatureHeader))
	require.NotEqual(t, firstSignature, codexWSTicketHeaders(t, svc, account, "gpt-5.6-sol", "").Get(openAIWSCodexTicketSignatureHeader))
	other := ticketTestAccount(42)
	svc.storeOpenAICodexTicket(context.Background(), other, &openAICodexTicket{
		Model: "gpt-6-astra", State: fakeCodexTicketState(292), Length: 292, ExpiresAt: time.Now().Add(time.Hour),
	})
	require.NotEqual(t, firstSignature, codexWSTicketHeaders(t, svc, other, "gpt-6-astra", "").Get(openAIWSCodexTicketSignatureHeader))
	stripped := openAIWSHeadersForUpstream(first)
	require.Empty(t, stripped.Get(openAIWSCodexTicketSignatureHeader))
	require.Equal(t, fakeCodexTicketState(292), stripped.Get(openAICodexTurnStateHeader))
	require.Equal(t, firstSignature, first.Get(openAIWSCodexTicketSignatureHeader), "dial sanitization must not erase the pool's private identity")
}

func TestCodexWSTicketPrivateMetadataNeverComesFromOverrides(t *testing.T) {
	account := &Account{ID: 41, Platform: PlatformOpenAI, Type: AccountTypeAPIKey, Credentials: map[string]any{
		"header_override_enabled": true,
		"header_overrides":        map[string]any{"x-sub2api-internal-codex-ticket": "override-must-not-be-metadata"},
	}}
	headers := codexWSTicketHeaders(t, &OpenAIGatewayService{}, account, "gpt-6-astra", "")
	for name := range headers {
		require.False(t, strings.EqualFold(name, openAIWSCodexTicketSignatureHeader), "account override must not manufacture internal identity")
	}
	for _, name := range []string{"x-sub2api-internal-codex-ticket", "X-Sub2api-Internal-Codex-Ticket", "X-SUB2API-INTERNAL-CODEX-TICKET"} {
		headers[name] = []string{"private-value"}
	}
	for name := range openAIWSHeadersForUpstream(headers) {
		require.False(t, strings.EqualFold(name, openAIWSCodexTicketSignatureHeader), "dial boundary must strip every case variant")
	}
}

func TestCodexWSTicketPrewarmChecksBeforeSending(t *testing.T) {
	cfg := &config.Config{}
	cfg.Gateway.OpenAIWS.PrewarmGenerateEnabled = true
	cfg.Gateway.OpenAICodexTicket = config.OpenAICodexTicketConfig{Enabled: true, FailClosed: true}
	svc := &OpenAIGatewayService{cfg: cfg, toolCorrector: NewCodexToolCorrector()}
	account := ticketTestAccount(41)
	svc.storeOpenAICodexTicket(context.Background(), account, &openAICodexTicket{
		Model: "gpt-6-astra", State: fakeCodexTicketState(292), Length: 292, ExpiresAt: time.Now().Add(time.Hour),
	})
	headers := codexWSTicketHeaders(t, svc, account, "gpt-6-astra", "")
	upstream := &openAIWSCaptureConn{events: [][]byte{[]byte(`{"type":"response.completed","response":{"id":"resp_prewarm","model":"gpt-6-astra"}}`)}}
	conn := newOpenAIWSConn("prewarm_ticket_fixture", account.ID, upstream, nil)
	conn.handshakeCompatibility = normalizeOpenAIWSHandshakeCompatibility(account, headers)
	lease := &openAIWSConnLease{accountID: account.ID, conn: conn}
	latest := *account
	latest.Extra = map[string]any{"codex_ticket_mode": "off"}
	repo := &codexWSTicketAccountRepo{}
	repo.current.Store(&latest)
	svc.accountRepo = repo
	err := svc.performOpenAIWSGeneratePrewarm(context.Background(), lease,
		OpenAIWSProtocolDecision{Transport: OpenAIUpstreamTransportResponsesWebsocketV2},
		map[string]any{"type": "response.create", "model": "gpt-6-astra"}, "", nil, account, nil, 0)
	require.ErrorContains(t, err, "reconnect")
	require.Empty(t, upstream.writes, "generate=false response.create must also respect ticket policy")
}

// Native ingress writes JSON; passthrough writes frames. Both share this staged
// transport so the assertions observe the actual outbound response.create.
type codexWSTicketStagedConn struct{ *stagedPassthroughConn }

func (c *codexWSTicketStagedConn) WriteJSON(ctx context.Context, value any) error {
	body, err := json.Marshal(value)
	if err != nil {
		return err
	}
	return c.WriteFrame(ctx, coderws.MessageText, body)
}

func TestCodexWSTicketLaterTurnCannotBypassMissingTicket(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, transport := range []string{OpenAIWSIngressModeCtxPool, OpenAIWSIngressModePassthrough} {
		for _, mode := range []string{"292", "332"} {
			t.Run(transport+"/"+mode, func(t *testing.T) {
				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				cfg := passthroughLifecycleConfig()
				cfg.Gateway.OpenAIWS.OAuthEnabled = true
				cfg.Gateway.OpenAIWS.MaxConnsPerAccount = 1
				cfg.Gateway.OpenAIWS.MaxIdlePerAccount = 1
				cfg.Gateway.OpenAICodexTicket = config.OpenAICodexTicketConfig{Enabled: true, FailClosed: true}
				cfg.Gateway.OpenAICodexTicket332 = config.OpenAICodexTicketConfig{Enabled: true, FailClosed: true}
				upstream := &codexWSTicketStagedConn{newStagedPassthroughConn()}
				svc := newPassthroughLifecycleService(cfg, upstream.stagedPassthroughConn)
				svc.openaiWSPassthroughDialer = &stagedPassthroughDialer{conn: upstream}
				pool := newOpenAIWSConnPool(cfg)
				pool.setClientDialerForTest(&stagedPassthroughDialer{conn: upstream})
				svc.openaiWSPool = pool
				t.Cleanup(pool.Close)
				account := ticketTestAccount(41)
				account.Status = StatusActive
				account.Concurrency = 1
				account.Extra = map[string]any{"codex_ticket_mode": mode, "openai_oauth_responses_websockets_v2_mode": transport}
				server, serverErr := startPassthroughLifecycleServer(t, ctx, svc, account)
				defer server.Close()
				client := dialPassthroughLifecycleClientWithPayload(t, server, `{"type":"response.create","model":"gpt-5.4"}`)
				defer func() { _ = client.CloseNow() }()
				require.Equal(t, "gpt-5.4", gjson.GetBytes(requirePassthroughUpstreamWrite(t, upstream.stagedPassthroughConn, time.Second), "model").String())
				upstream.Send(`{"type":"response.completed","response":{"id":"resp_ticket_first","model":"gpt-5.4","usage":{"input_tokens":1,"output_tokens":1}}}`)
				_, err := readPassthroughLifecycleFrame(t, client, time.Second)
				require.NoError(t, err)
				require.NoError(t, client.Write(ctx, coderws.MessageText, []byte(`{"type":"response.create","model":"gpt-6-astra"}`)))
				// A close handshake needs the client's reader to run concurrently.
				go func() { _, _, _ = client.Read(ctx) }()
				select {
				case payload := <-upstream.writes:
					t.Fatalf("missing-ticket turn reached upstream: %s", payload)
				case err := <-serverErr:
					require.Error(t, err)
					require.Contains(t, strings.ToLower(err.Error()), "reconnect")
					var closeErr *OpenAIWSClientCloseError
					require.ErrorAs(t, err, &closeErr)
					require.Equal(t, coderws.StatusTryAgainLater, closeErr.StatusCode())
					select {
					case payload := <-upstream.writes:
						t.Fatalf("rejected turn was sent before the error: %s", payload)
					default:
					}
				case <-time.After(3 * time.Second):
					t.Fatal("missing-ticket turn was not rejected")
				}
			})
		}
	}
}

type codexWSTicketAccountRepo struct {
	AccountRepository
	current atomic.Pointer[Account]
}

func (r *codexWSTicketAccountRepo) GetByID(context.Context, int64) (*Account, error) {
	account := *r.current.Load()
	account.Extra = maps.Clone(account.Extra)
	account.Credentials = maps.Clone(account.Credentials)
	return &account, nil
}

type codexWSTicketCaptureDialer struct {
	conn    openAIWSClientConn
	headers chan http.Header
}

func (d *codexWSTicketCaptureDialer) Dial(_ context.Context, _ string, headers http.Header, _ string) (openAIWSClientConn, int, http.Header, error) {
	d.headers <- headers.Clone()
	return d.conn, http.StatusSwitchingProtocols, http.Header{}, nil
}

func TestCodexWSTicketLiveConnectionTracksPolicyChanges(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, transport := range []string{OpenAIWSIngressModeCtxPool, OpenAIWSIngressModePassthrough} {
		for _, mode := range []string{"292", "332"} {
			for _, change := range []string{"same", "model", "refresh", "mode", "disable", "enable", "expire"} {
				t.Run(transport+"/"+mode+"/"+change, func(t *testing.T) {
					ctx, cancel := context.WithCancel(context.Background())
					defer cancel()
					cfg := passthroughLifecycleConfig()
					cfg.Gateway.OpenAIWS.OAuthEnabled = true
					cfg.Gateway.OpenAIWS.MaxConnsPerAccount = 1
					cfg.Gateway.OpenAIWS.MaxIdlePerAccount = 1
					cfg.Gateway.OpenAICodexTicket = config.OpenAICodexTicketConfig{Enabled: true, FailClosed: true}
					cfg.Gateway.OpenAICodexTicket332 = config.OpenAICodexTicketConfig{Enabled: true, FailClosed: true}
					selectedCfg := &cfg.Gateway.OpenAICodexTicket
					length := 292
					if mode == "332" {
						selectedCfg = &cfg.Gateway.OpenAICodexTicket332
						length = 332
					}
					if change == "enable" {
						selectedCfg.Enabled = false
					}
					account := ticketTestAccount(41)
					account.Status = StatusActive
					account.Concurrency = 1
					account.Extra = map[string]any{"codex_ticket_mode": mode, "openai_oauth_responses_websockets_v2_mode": transport}
					now := time.Now()
					for _, ticketMode := range []string{"292", "332"} {
						count := 292
						if ticketMode == "332" {
							count = 332
						}
						for _, model := range []string{"gpt-6-astra", "gpt-5.6-sol"} {
							account.Extra["codex_turn_ticket:"+ticketMode+":"+model] = &openAICodexTicket{
								AccountID: account.ID, Model: model, State: fakeCodexTicketState(count), Length: count,
								CapturedAt: now, ExpiresAt: now.Add(time.Hour),
							}
						}
					}
					upstream := &codexWSTicketStagedConn{newStagedPassthroughConn()}
					dialer := &codexWSTicketCaptureDialer{conn: upstream, headers: make(chan http.Header, 4)}
					svc := newPassthroughLifecycleService(cfg, upstream.stagedPassthroughConn)
					svc.openaiWSPassthroughDialer = dialer
					pool := newOpenAIWSConnPool(cfg)
					pool.setClientDialerForTest(dialer)
					svc.openaiWSPool = pool
					t.Cleanup(pool.Close)
					repo := &codexWSTicketAccountRepo{}
					repo.current.Store(account)
					svc.accountRepo = repo
					var before, after atomic.Int32
					server, serverErr := startPassthroughLifecycleServerWithHooks(t, ctx, svc, account, func(*gin.Context) *OpenAIWSIngressHooks {
						return &OpenAIWSIngressHooks{
							BeforeTurn: func(int) error { before.Add(1); return nil },
							AfterTurn:  func(_ int, _ *OpenAIForwardResult, _ error) { after.Add(1) },
							MapRequestModel: func(turn int, requested string) (string, error) {
								if turn != 2 {
									return requested, nil
								}
								latest := *account
								latest.Extra = maps.Clone(account.Extra)
								switch change {
								case "model":
									return "gpt-5.6-sol", nil // check the final mapped model
								case "mode":
									latest.Extra["codex_ticket_mode"] = "332"
									if mode == "332" {
										latest.Extra["codex_ticket_mode"] = "292"
									}
								case "refresh", "expire":
									key := "codex_turn_ticket:" + mode + ":gpt-6-astra"
									stored, ok := latest.Extra[key].(*openAICodexTicket)
									if !ok || stored == nil {
										return "", errors.New("missing ticket fixture")
									}
									ticket := *stored
									ticket.CapturedAt = now.Add(time.Second)
									if change == "refresh" {
										ticket.State = "gAAAAA" + strings.Repeat("C", length-6)
									} else {
										ticket.ExpiresAt = now.Add(-time.Second)
										svc.openaiCodexTickets.Delete(openAICodexTicketModeKey(mode, account.ID, "gpt-6-astra"))
									}
									latest.Extra[key] = &ticket
								case "disable":
									selectedCfg.Enabled = false
								case "enable":
									selectedCfg.Enabled = true
								}
								repo.current.Store(&latest) // original long-lived snapshot is unchanged
								return requested, nil
							},
						}
					})
					defer server.Close()
					client := dialPassthroughLifecycleClientWithPayload(t, server, `{"type":"response.create","model":"gpt-6-astra"}`)
					defer func() { _ = client.CloseNow() }()
					require.Equal(t, "gpt-6-astra", gjson.GetBytes(requirePassthroughUpstreamWrite(t, upstream.stagedPassthroughConn, time.Second), "model").String())
					headers := <-dialer.headers
					require.Empty(t, headers.Get(openAIWSCodexTicketSignatureHeader), "private compatibility metadata must not go upstream")
					if change != "enable" {
						require.Equal(t, fakeCodexTicketState(length), headers.Get(openAICodexTurnStateHeader))
					}
					upstream.Send(`{"type":"response.completed","response":{"id":"resp_bound_first","model":"gpt-6-astra","usage":{"input_tokens":1,"output_tokens":1}}}`)
					_, err := readPassthroughLifecycleFrame(t, client, time.Second)
					require.NoError(t, err)
					require.NoError(t, client.Write(ctx, coderws.MessageText, []byte(`{"type":"response.create","model":"gpt-6-astra"}`)))
					if change == "same" {
						require.Equal(t, "gpt-6-astra", gjson.GetBytes(requirePassthroughUpstreamWrite(t, upstream.stagedPassthroughConn, time.Second), "model").String())
						upstream.Send(`{"type":"response.completed","response":{"id":"resp_bound_second","model":"gpt-6-astra","usage":{"input_tokens":1,"output_tokens":1}}}`)
						_, err = readPassthroughLifecycleFrame(t, client, time.Second)
						require.NoError(t, err)
						_ = client.CloseNow()
						select {
						case <-serverErr:
						case <-time.After(3 * time.Second):
							t.Fatal("same-ticket session did not exit")
						}
						require.GreaterOrEqual(t, before.Load(), int32(1), "later-turn admission hooks remain active")
						require.Equal(t, int32(2), after.Load(), "both completed turns retain settlement hooks")
						return
					}
					go func() { _, _, _ = client.Read(ctx) }()
					select {
					case payload := <-upstream.writes:
						t.Fatalf("incompatible %s turn reached upstream: %s", change, payload)
					case err := <-serverErr:
						require.ErrorContains(t, err, "reconnect")
						var closeErr *OpenAIWSClientCloseError
						require.ErrorAs(t, err, &closeErr)
						require.Equal(t, coderws.StatusTryAgainLater, closeErr.StatusCode())
					case <-time.After(3 * time.Second):
						t.Fatal("incompatible turn was not rejected")
					}
					select {
					case payload := <-upstream.writes:
						t.Fatalf("rejected turn was sent before close: %s", payload)
					default:
					}
				})
			}
		}
	}
}
