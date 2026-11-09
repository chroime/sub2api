package service

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/ctxkey"
	coderws "github.com/coder/websocket"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

// The pool writes JSON while passthrough writes frames. Capture both at the
// upstream boundary and release each response only after its request arrives.
type groupUltrafastWSUpstream struct{ *stagedPassthroughConn }

func (c *groupUltrafastWSUpstream) WriteJSON(ctx context.Context, value any) error {
	payload, err := json.Marshal(value)
	if err != nil {
		return err
	}
	return c.WriteFrame(ctx, coderws.MessageText, payload)
}

func TestGroupUltrafastPolicy_WSIngressAndPassthroughEveryTurn(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, mode := range []string{OpenAIWSIngressModeCtxPool, OpenAIWSIngressModePassthrough} {
		for _, action := range []string{BetaPolicyActionPass, BetaPolicyActionFilter} {
			t.Run(mode+"/"+action, func(t *testing.T) {
				cfg := passthroughLifecycleConfig()
				cfg.Gateway.OpenAIFirstOutputTimeoutSeconds = 5
				cfg.Gateway.OpenAIWS.IngressModeDefault = mode
				cfg.Gateway.OpenAIWS.MaxConnsPerAccount = 1
				cfg.Gateway.OpenAIWS.MinIdlePerAccount = 0
				cfg.Gateway.OpenAIWS.MaxIdlePerAccount = 1
				cfg.Gateway.OpenAIWS.QueueLimitPerConn = 8
				upstream := &groupUltrafastWSUpstream{newStagedPassthroughConn()}
				dialer := &stagedPassthroughDialer{conn: upstream}
				pool := newOpenAIWSConnPool(cfg)
				pool.setClientDialerForTest(dialer)
				defer pool.Close()
				svc := newOpenAIGatewayServiceWithSettings(t, &OpenAIFastPolicySettings{Rules: []OpenAIFastPolicyRule{{
					ServiceTier: OpenAIFastTierUltrafast, Action: action, Scope: BetaPolicyScopeAll,
				}}})
				svc.cfg = cfg
				svc.httpUpstream = &httpUpstreamRecorder{}
				svc.cache = &stubGatewayCache{}
				svc.openaiWSResolver = NewOpenAIWSProtocolResolver(cfg)
				svc.toolCorrector = NewCodexToolCorrector()
				svc.openaiWSPool = pool
				svc.openaiWSPassthroughDialer = dialer
				account := &Account{
					ID: 901, Platform: PlatformOpenAI, Type: AccountTypeAPIKey,
					Status: StatusActive, Schedulable: true, Concurrency: 1,
					Credentials: map[string]any{"api_key": "fixture-key"},
					Extra:       map[string]any{"openai_apikey_responses_websockets_v2_mode": mode},
				}
				results := make(chan *OpenAIForwardResult, 2)
				hooks := &OpenAIWSIngressHooks{AfterTurn: func(_ int, result *OpenAIForwardResult, err error) {
					if err == nil && result != nil {
						results <- result
					}
				}}
				serverDone := make(chan error, 1)
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					conn, err := coderws.Accept(w, r, nil)
					if err != nil {
						serverDone <- err
						return
					}
					defer conn.CloseNow()
					ctx := context.WithValue(r.Context(), ctxkey.Group, groupUltrafastTestContext().Value(ctxkey.Group))
					c, _ := gin.CreateTestContext(httptest.NewRecorder())
					c.Request = r.WithContext(ctx)
					_, first, err := conn.Read(ctx)
					if err != nil {
						serverDone <- err
						return
					}
					serverDone <- svc.ProxyResponsesWebSocketFromClient(ctx, c, conn, account, "fixture-key", first, hooks)
				}))
				defer server.Close()
				ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
				defer cancel()
				client, _, err := coderws.Dial(ctx, "ws"+strings.TrimPrefix(server.URL, "http"), nil)
				require.NoError(t, err)
				defer client.CloseNow()
				for i, frame := range []string{
					`{"type":"response.create","model":"gpt-5.6-sol","input":[]}`,
					`{"type":"response.create","model":"gpt-5.6-sol","service_tier":"flex","input":[]}`,
				} {
					require.NoError(t, client.Write(ctx, coderws.MessageText, []byte(frame)))
					select {
					case request := <-upstream.writes:
						if action == BetaPolicyActionFilter {
							require.False(t, gjson.GetBytes(request, "service_tier").Exists(), "turn %d", i+1)
						} else {
							require.Equal(t, "ultrafast", gjson.GetBytes(request, "service_tier").String(), "turn %d", i+1)
						}
					case <-ctx.Done():
						t.Fatal("upstream did not receive response.create")
					}
					upstream.Send(fmt.Sprintf(`{"type":"response.completed","response":{"id":"resp_ultrafast_%d","model":"gpt-5.6-sol","usage":{"input_tokens":1,"output_tokens":1}}}`, i+1))
					_, event, err := client.Read(ctx)
					require.NoError(t, err)
					require.Equal(t, "response.completed", gjson.GetBytes(event, "type").String())
					select {
					case result := <-results:
						if action == BetaPolicyActionFilter {
							require.Nil(t, result.ServiceTier)
						} else {
							require.NotNil(t, result.ServiceTier)
							require.Equal(t, "ultrafast", *result.ServiceTier)
						}
					case <-ctx.Done():
						t.Fatal("turn result was not recorded")
					}
				}
				_ = client.Close(coderws.StatusNormalClosure, "done")
				select {
				case err := <-serverDone:
					if err != nil {
						require.Contains(t, err.Error(), "StatusNormalClosure")
					}
				case <-ctx.Done():
					t.Fatal("websocket proxy did not stop")
				}
			})
		}
	}
}
