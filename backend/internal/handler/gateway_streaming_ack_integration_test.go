//go:build unit

package handler

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/ctxkey"
	"github.com/Wei-Shaw/sub2api/internal/pkg/tlsfingerprint"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type gatewayACKHTTPUpstream struct {
	client *http.Client
	target *url.URL
}

func (u *gatewayACKHTTPUpstream) Do(req *http.Request, _ string, _ int64, _ int) (*http.Response, error) {
	cloned := req.Clone(req.Context())
	cloned.URL.Scheme = u.target.Scheme
	cloned.URL.Host = u.target.Host
	return u.client.Do(cloned)
}

func (u *gatewayACKHTTPUpstream) DoWithTLS(req *http.Request, proxy string, id int64, limit int, _ *tlsfingerprint.Profile) (*http.Response, error) {
	return u.Do(req, proxy, id, limit)
}

type gatewayACKUsageRepo struct {
	service.UsageLogRepository
	logs chan *service.UsageLog
}

type gatewayACKDefaultSettings struct{ service.SettingRepository }

func (*gatewayACKDefaultSettings) GetValue(context.Context, string) (string, error) {
	return "", service.ErrSettingNotFound
}

func (*gatewayACKDefaultSettings) GetMultiple(context.Context, []string) (map[string]string, error) {
	return map[string]string{}, nil
}

func (r *gatewayACKUsageRepo) CreateBestEffort(_ context.Context, row *service.UsageLog) error {
	r.logs <- row
	return nil
}

func TestGatewayStreamingACKRealForwardingRoutes(t *testing.T) {
	const anthropicEvents = "event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"id\":\"msg_ack_test\",\"type\":\"message\",\"role\":\"assistant\",\"model\":\"claude-sonnet-4-5\",\"content\":[],\"usage\":{\"input_tokens\":5,\"output_tokens\":0}}}\n\n" +
		"event: content_block_start\ndata: {\"type\":\"content_block_start\",\"index\":0,\"content_block\":{\"type\":\"text\",\"text\":\"\"}}\n\n" +
		"event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\"hello\"}}\n\n" +
		"event: content_block_stop\ndata: {\"type\":\"content_block_stop\",\"index\":0}\n\n" +
		"event: message_delta\ndata: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"end_turn\"},\"usage\":{\"output_tokens\":1}}\n\n" +
		"event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n"
	const geminiEvents = "data: {\"candidates\":[{\"content\":{\"role\":\"model\",\"parts\":[{\"text\":\"hello\"}]},\"finishReason\":\"STOP\"}],\"usageMetadata\":{\"promptTokenCount\":5,\"candidatesTokenCount\":1,\"totalTokenCount\":6}}\n\n"
	for _, tc := range []struct {
		name, platform, path, body, payload, terminal string
	}{
		{"anthropic messages", service.PlatformAnthropic, "/v1/messages", `{"model":"claude-sonnet-4-5","max_tokens":32,"messages":[{"role":"user","content":"hello"}],"stream":true}`, anthropicEvents, "message_stop"},
		{"anthropic responses", service.PlatformAnthropic, "/v1/responses", `{"model":"claude-sonnet-4-5","input":"hello","stream":true}`, anthropicEvents, "response.completed"},
		{"anthropic chat", service.PlatformAnthropic, "/v1/chat/completions", `{"model":"claude-sonnet-4-5","messages":[{"role":"user","content":"hello"}],"stream":true}`, anthropicEvents, "[DONE]"},
		{"gemini messages", service.PlatformGemini, "/v1/messages", `{"model":"gemini-2.5-flash","max_tokens":32,"messages":[{"role":"user","content":"hello"}],"stream":true}`, geminiEvents, "message_stop"},
		{"gemini chat", service.PlatformGemini, "/v1/chat/completions", `{"model":"gemini-2.5-flash","messages":[{"role":"user","content":"hello"}],"stream":true}`, geminiEvents, "[DONE]"},
		{"gemini native", service.PlatformGemini, "/v1beta/models/gemini-2.5-flash:streamGenerateContent?alt=sse", `{"contents":[{"role":"user","parts":[{"text":"hello"}]}]}`, geminiEvents, "finishReason"},
	} {
		for _, rejected := range []bool{false, true} {
			name := tc.name
			if rejected {
				name += " upstream error after ACK"
			}
			t.Run(name, func(t *testing.T) {
				upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					timer := time.NewTimer(150 * time.Millisecond)
					defer timer.Stop()
					select {
					case <-timer.C:
					case <-r.Context().Done():
						return
					}
					if rejected {
						w.Header().Set("Content-Type", "application/json")
						w.WriteHeader(http.StatusBadRequest)
						_, _ = io.WriteString(w, `{"type":"error","error":{"code":400,"type":"invalid_request_error","message":"fixture invalid input"}}`)
						return
					}
					w.Header().Set("Content-Type", "text/event-stream")
					_, _ = io.WriteString(w, tc.payload)
				}))
				t.Cleanup(upstream.Close)
				upstreamURL, err := url.Parse(upstream.URL)
				require.NoError(t, err)
				transport := &gatewayACKHTTPUpstream{client: upstream.Client(), target: upstreamURL}
				cfg := gatewayACKTestConfig(20)
				cfg.RunMode = config.RunModeSimple
				groupID := int64(7)
				ackEnabled := true
				group := &service.Group{ID: groupID, Hydrated: true, Platform: tc.platform, Status: service.StatusActive, RateMultiplier: 1, StreamingACKEnabled: &ackEnabled}
				account := gatewayACKTestAccount(tc.platform, false, groupID)
				account.ID, account.Concurrency, account.Priority = 10, 1, 1
				account.Status, account.Schedulable = service.StatusActive, true
				account.Credentials = map[string]any{"api_key": "fixture-key"}
				account.AccountGroups = []service.AccountGroup{{AccountID: account.ID, GroupID: groupID}}
				scheduler := service.NewSchedulerSnapshotService(&fakeSchedulerCache{accounts: []*service.Account{account}}, nil, nil, nil, nil)
				logs := &gatewayACKUsageRepo{logs: make(chan *service.UsageLog, 1)}
				deferred := service.NewDeferredService(nil, nil, time.Hour)
				settings := service.NewSettingService(&gatewayACKDefaultSettings{}, cfg)
				gateway := service.NewGatewayService(nil, &fakeGroupRepo{group: group}, logs, nil, nil, nil, nil, nil, cfg,
					scheduler, nil, service.NewBillingService(cfg, nil), nil, nil, nil, transport, deferred,
					nil, nil, nil, nil, settings, nil, nil, nil, nil, nil, nil)
				billing := service.NewBillingCacheService(nil, nil, nil, nil, nil, nil, cfg, nil)
				t.Cleanup(billing.Stop)
				h := &GatewayHandler{gatewayService: gateway, cfg: cfg, billingCacheService: billing,
					concurrencyHelper:  NewConcurrencyHelper(service.NewConcurrencyService(&fakeConcurrencyCache{}), SSEPingFormatClaude, 0),
					maxAccountSwitches: 1, maxAccountSwitchesGemini: 1,
					geminiCompatService: service.NewGeminiMessagesCompatService(nil, nil, nil, nil, nil, nil, transport, nil, cfg)}
				key := &service.APIKey{ID: 1, UserID: 2, GroupID: &groupID, Group: group, User: &service.User{ID: 2, Balance: 100, Concurrency: 10}}
				router := gin.New()
				router.Use(func(c *gin.Context) {
					c.Request = c.Request.WithContext(context.WithValue(c.Request.Context(), ctxkey.Group, group))
					c.Set(string(middleware.ContextKeyAPIKey), key)
					c.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: 2, Concurrency: 10})
					c.Next()
				})
				router.POST("/v1/messages", h.Messages)
				router.POST("/v1/responses", h.Responses)
				router.POST("/v1/chat/completions", h.ChatCompletions)
				router.POST("/v1beta/models/*modelAction", h.GeminiV1BetaModels)
				server := httptest.NewServer(router)
				t.Cleanup(server.Close)
				client := server.Client()
				client.Timeout = 3 * time.Second
				resp, err := client.Post(server.URL+tc.path, "application/json", strings.NewReader(tc.body))
				require.NoError(t, err)
				defer resp.Body.Close()
				reader := bufio.NewReader(resp.Body)
				line, err := reader.ReadString('\n')
				require.NoError(t, err)
				require.Equal(t, ":\n", line, "real gateway route must send the ACK before model events")
				tail, err := io.ReadAll(reader)
				require.NoError(t, err)
				if rejected {
					require.Contains(t, string(tail), "data: ")
					require.Contains(t, string(tail), `"error":`)
					if tc.path == "/v1/responses" {
						require.Contains(t, string(tail), "response.failed")
					}
					for _, line := range strings.Split(string(tail), "\n") {
						if strings.HasPrefix(line, "data: ") && line != "data: [DONE]" {
							require.True(t, json.Valid([]byte(strings.TrimPrefix(line, "data: "))), line)
						}
					}
					return
				}
				require.Contains(t, string(tail), "hello")
				require.Contains(t, string(tail), tc.terminal)
				select {
				case row := <-logs.logs:
					require.NotNil(t, row.StreamingAckMs)
					require.NotNil(t, row.FirstTokenMs)
					require.Less(t, *row.StreamingAckMs, *row.FirstTokenMs)
				case <-time.After(time.Second):
					t.Fatal("successful request did not record usage with ACK latency")
				}
			})
		}
	}
}
