package handler

import (
	"bufio"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func gatewayACKTestConfig(delay int) *config.Config {
	return &config.Config{Gateway: config.GatewayConfig{SyntheticFirstResponse: config.GatewaySyntheticFirstResponseConfig{
		Enabled: true, MinDelayMs: delay, MaxDelayMs: delay,
		UnderOneSecondPercent: 100, UnderOneSecondMaxDelayMs: delay,
	}}}
}

func gatewayACKTestAccount(platform string, enabled bool, groups ...int64) *service.Account {
	return &service.Account{Platform: platform, Type: service.AccountTypeAPIKey, GroupIDs: groups,
		Extra: map[string]any{"streaming_ack_enabled": enabled}}
}

func TestGatewayStreamingACKUsesRuntimeSwitchAndAccountScope(t *testing.T) {
	for _, tc := range []struct {
		name, platform, runtime string
		enabled, stream, want   bool
		group                   int64
	}{
		{"anthropic", service.PlatformAnthropic, "true", true, true, true, 7},
		{"bedrock", service.PlatformAnthropic, "true", true, true, true, 7},
		{"gemini", service.PlatformGemini, "true", true, true, true, 7},
		{"antigravity", service.PlatformAntigravity, "true", true, true, true, 7},
		{"global disabled", service.PlatformAnthropic, "false", true, true, false, 7},
		{"account disabled", service.PlatformGemini, "true", false, true, false, 7},
		{"wrong group", service.PlatformAnthropic, "true", true, true, false, 8},
		{"non streaming", service.PlatformAnthropic, "true", true, false, false, 7},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := gatewayACKTestConfig(5)
			settings := service.NewSettingService(&streamingACKHandlerRepo{value: tc.runtime}, cfg)
			gateway := service.NewGatewayService(nil, nil, nil, nil, nil, nil, nil, nil, cfg,
				nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, settings, nil, nil, nil, nil, nil, nil)
			h := &GatewayHandler{cfg: cfg, gatewayService: gateway}
			c, recorder := syntheticFirstResponseHandlerContext()
			account := gatewayACKTestAccount(tc.platform, tc.enabled, 7)
			if tc.name == "bedrock" {
				account.Type = service.AccountTypeBedrock
			}
			stop := h.startStreamingACK(c, tc.stream, time.Now(), account, &tc.group)
			t.Cleanup(stop)
			if tc.want {
				require.Eventually(t, func() bool { return service.StreamingACKCommitted(c) }, time.Second, time.Millisecond)
			} else {
				time.Sleep(15 * time.Millisecond)
			}
			stop()
			if tc.want {
				require.Equal(t, ":\n\n", recorder.Body.String())
			} else {
				require.Empty(t, recorder.Body.String())
			}
		})
	}
}

func TestGatewayStreamingACKAcrossSSEProtocolsOverHTTP(t *testing.T) {
	for _, tc := range []struct {
		name, platform, path, event string
	}{
		{"anthropic messages", service.PlatformAnthropic, "/v1/messages", "event: message_start\ndata: {\"type\":\"message_start\"}\n\n"},
		{"bedrock messages", service.PlatformAnthropic, "/v1/messages", "event: message_start\ndata: {\"type\":\"message_start\"}\n\n"},
		{"antigravity responses", service.PlatformAntigravity, "/v1/responses", "event: response.created\ndata: {\"type\":\"response.created\"}\n\n"},
		{"gemini chat", service.PlatformGemini, "/v1/chat/completions", "data: {\"choices\":[{\"delta\":{\"content\":\"hello\"}}]}\n\n"},
		{"gemini native", service.PlatformGemini, "/v1beta/models/gemini-test:streamGenerateContent?alt=sse", "data: {\"candidates\":[{\"content\":{\"parts\":[{\"text\":\"hello\"}]}}]}\n\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var upstreamProduced atomic.Bool
			releaseUpstream := make(chan struct{})
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				select {
				case <-releaseUpstream:
				case <-r.Context().Done():
					return
				}
				upstreamProduced.Store(true)
				w.Header().Set("Content-Type", "text/event-stream")
				_, _ = io.WriteString(w, tc.event)
			}))
			t.Cleanup(upstream.Close)
			h := &GatewayHandler{cfg: gatewayACKTestConfig(20)}
			resultCh := make(chan *service.ForwardResult, 1)
			gin.SetMode(gin.TestMode)
			router := gin.New()
			router.POST("/*path", func(c *gin.Context) {
				stream := true
				if strings.Contains(tc.path, "/v1beta/") {
					stream = geminiStreamingACKEligible(c, stream)
				}
				account := gatewayACKTestAccount(tc.platform, true)
				if tc.name == "bedrock messages" {
					account.Type = service.AccountTypeBedrock
				}
				stop := h.startStreamingACK(c, stream, time.Now(), account, nil)
				defer stop()
				req, _ := http.NewRequestWithContext(c.Request.Context(), http.MethodPost, upstream.URL, nil)
				resp, err := upstream.Client().Do(req)
				if err != nil {
					return
				}
				defer resp.Body.Close()
				_, _ = io.Copy(c.Writer, resp.Body)
				c.Writer.Flush()
				realTTFT := 450
				result := &service.ForwardResult{FirstTokenMs: &realTTFT}
				service.ApplyStreamingACKResult(c, result)
				resultCh <- result
			})
			server := httptest.NewServer(router)
			t.Cleanup(server.Close)
			client := server.Client()
			client.Timeout = 2 * time.Second
			resp, err := client.Post(server.URL+tc.path, "application/json", strings.NewReader(`{"stream":true}`))
			require.NoError(t, err)
			defer resp.Body.Close()
			reader := bufio.NewReader(resp.Body)
			line, err := reader.ReadString('\n')
			require.NoError(t, err)
			require.Equal(t, ":\n", line)
			require.False(t, upstreamProduced.Load(), "the ACK must reach the client before upstream model output")
			close(releaseUpstream)
			tail, err := io.ReadAll(reader)
			require.NoError(t, err)
			require.Equal(t, ":\n\n"+tc.event, line+string(tail), "protocol payload must remain unchanged")
			result := <-resultCh
			require.Equal(t, 450, *result.FirstTokenMs, "real model latency must remain intact")
			require.NotNil(t, result.StreamingAckMs)
			require.Less(t, *result.StreamingAckMs, 450)
		})
	}
}

func TestGatewayStreamingACKFastOutputAndRetry(t *testing.T) {
	h := &GatewayHandler{cfg: gatewayACKTestConfig(30)}
	for _, fast := range []bool{false, true} {
		c, recorder := syntheticFirstResponseHandlerContext()
		account := gatewayACKTestAccount(service.PlatformAnthropic, true)
		stop := h.startStreamingACK(c, true, time.Now(), account, nil)
		before := service.OpenAICompactKeepaliveAdjustedWrittenSize(c)
		if fast {
			_, _ = c.Writer.WriteString("data: real\n\n")
		} else {
			resetSyntheticFirstResponseForRetry(c, &stop, before)
			require.Nil(t, stop)
			stop = h.startStreamingACK(c, true, time.Now(), gatewayACKTestAccount(service.PlatformGemini, false), nil)
		}
		time.Sleep(50 * time.Millisecond)
		stop()
		require.Nil(t, service.StreamingACKMs(c))
		require.NotContains(t, recorder.Body.String(), ":\n\n")
		if fast {
			require.Equal(t, "data: real\n\n", recorder.Body.String())
		}
	}
}

func TestGatewayStreamingACKKeepsErrorsProtocolCorrect(t *testing.T) {
	h := &GatewayHandler{cfg: gatewayACKTestConfig(5)}
	for _, tc := range []struct {
		path, marker string
		write        func(*gin.Context)
	}{
		{"/v1/messages", `"type":"error"`, func(c *gin.Context) { h.errorResponse(c, 502, "upstream_error", "failed") }},
		{"/v1/chat/completions", `"error":`, func(c *gin.Context) { h.handleCCFailoverExhausted(c, nil, true) }},
		{"/v1/responses", `"type":"response.failed"`, func(c *gin.Context) { h.handleResponsesFailoverExhausted(c, nil, true) }},
		{"/v1beta/models/gemini:streamGenerateContent?alt=sse", `"code":503`, func(c *gin.Context) { googleError(c, 503, "failed") }},
	} {
		t.Run(tc.path, func(t *testing.T) {
			c, recorder := syntheticFirstResponseHandlerContext()
			c.Request = httptest.NewRequest(http.MethodPost, tc.path, nil)
			stop := h.startStreamingACK(c, true, time.Now(), gatewayACKTestAccount(service.PlatformAnthropic, true), nil)
			t.Cleanup(stop)
			require.Eventually(t, func() bool { return service.StreamingACKCommitted(c) }, time.Second, time.Millisecond)
			require.True(t, gatewayStreamHasOnlyHeartbeats(c))
			require.False(t, gatewayForwardErrorAlreadyCommunicated(c, -1, errors.New("failed")))
			tc.write(c)
			stop()
			require.Equal(t, http.StatusOK, recorder.Code)
			require.Contains(t, recorder.Body.String(), tc.marker)
			require.Contains(t, recorder.Body.String(), "data: ")
			if tc.path == "/v1/messages" {
				require.Contains(t, recorder.Body.String(), "event: error\ndata: ")
			}
			require.False(t, gatewayStreamHasOnlyHeartbeats(c))
			for _, line := range strings.Split(recorder.Body.String(), "\n") {
				if strings.HasPrefix(line, "data: ") {
					require.True(t, json.Valid([]byte(strings.TrimPrefix(line, "data: "))), line)
				}
			}
		})
	}
}

func TestGeminiStreamingACKRequiresSSETransport(t *testing.T) {
	for _, tc := range []struct {
		query  string
		stream bool
		want   bool
	}{
		{"?alt=sse", true, true}, {"?alt=json", true, false}, {"", true, false}, {"?alt=sse", false, false},
	} {
		c, _ := syntheticFirstResponseHandlerContext()
		c.Request = httptest.NewRequest(http.MethodPost, "/v1beta/models/gemini:streamGenerateContent"+tc.query, nil)
		require.Equal(t, tc.want, geminiStreamingACKEligible(c, tc.stream))
	}
}

func TestGatewayStreamingACKEarlyErrorsRemainJSON(t *testing.T) {
	h := &GatewayHandler{cfg: gatewayACKTestConfig(30)}
	for _, write := range []func(*gin.Context){
		func(c *gin.Context) { h.errorResponse(c, 400, "invalid_request_error", "invalid") },
		func(c *gin.Context) { h.responsesErrorResponse(c, 400, "invalid_request_error", "invalid") },
		func(c *gin.Context) { h.chatCompletionsErrorResponse(c, 400, "invalid_request_error", "invalid") },
		func(c *gin.Context) { googleError(c, 400, "invalid") },
	} {
		c, recorder := syntheticFirstResponseHandlerContext()
		stop := h.startStreamingACK(c, true, time.Now(), gatewayACKTestAccount(service.PlatformAnthropic, true), nil)
		write(c)
		time.Sleep(45 * time.Millisecond)
		stop()
		require.Equal(t, http.StatusBadRequest, recorder.Code)
		require.Contains(t, recorder.Header().Get("Content-Type"), "application/json")
		require.True(t, json.Valid(recorder.Body.Bytes()))
		require.Nil(t, service.StreamingACKMs(c))
	}
}
