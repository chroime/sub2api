package service

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/require"
)

func newGatewayHeartbeatTestContext(t *testing.T, path string) (*gin.Context, *httptest.ResponseRecorder) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, path, nil)
	c.Header("Content-Type", "text/event-stream")
	n, err := c.Writer.Write([]byte(": keepalive\n\n"))
	require.NoError(t, err)
	// This is the shared marker used by admission and concurrency waiters.
	c.Set("gateway_stream_heartbeat_bytes", n)
	c.Writer.Flush()
	return c, rec
}

func TestGatewayWaitHeartbeatDoesNotCountAsClientOutput(t *testing.T) {
	c, _ := newGatewayHeartbeatTestContext(t, "/v1/responses")
	require.True(t, c.Writer.Written())
	require.False(t, openAIStreamClientOutputStarted(c, false))
	require.Equal(t, -1, OpenAICompactKeepaliveAdjustedWrittenSize(c))
	_, err := c.Writer.Write([]byte("data: real-output\n\n"))
	require.NoError(t, err)
	require.True(t, openAIStreamClientOutputStarted(c, false))
	require.Equal(t, len("data: real-output\n\n"), OpenAICompactKeepaliveAdjustedWrittenSize(c))
}

func TestChatFailoverAfterGatewayWaitHeartbeatHasNoOutputResult(t *testing.T) {
	c, rec := newGatewayHeartbeatTestContext(t, "/v1/chat/completions")
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": {"text/event-stream"}},
		Body:       io.NopCloser(strings.NewReader("data: {\"type\":\"response.failed\",\"response\":{\"error\":{\"code\":\"server_error\",\"message\":\"overloaded\"}}}\n\n")),
	}
	svc := &OpenAIGatewayService{cfg: &config.Config{}}
	result, err := svc.handleChatStreamingResponse(resp, c, &Account{ID: 1, Platform: PlatformOpenAI}, "gpt-5", "gpt-5", "gpt-5", time.Now(), 0)
	var failoverErr *UpstreamFailoverError
	require.True(t, errors.As(err, &failoverErr))
	require.Nil(t, result, "heartbeat-only output must not become a billable partial response")
	require.Equal(t, ": keepalive\n\n", rec.Body.String())
}

func TestOpenAIWSRetryAfterGatewayWaitHeartbeat(t *testing.T) {
	var attempts atomic.Int32
	upgrader := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			t.Error(err)
			return
		}
		defer func() { _ = conn.Close() }()
		var request map[string]any
		if err := conn.ReadJSON(&request); err != nil {
			t.Error(err)
			return
		}
		if attempts.Add(1) == 1 {
			_ = conn.WriteJSON(map[string]any{"type": "error", "error": map[string]any{"code": "previous_response_not_found", "type": "invalid_request_error", "message": "previous response not found"}})
			return
		}
		_ = conn.WriteJSON(map[string]any{"type": "response.output_text.delta", "delta": "OK"})
		_ = conn.WriteJSON(map[string]any{"type": "response.completed", "response": map[string]any{"id": "resp_wait_retry_ok", "model": "gpt-5.3-codex", "usage": map[string]any{"input_tokens": 1, "output_tokens": 1}}})
	}))
	defer upstream.Close()

	c, rec := newGatewayHeartbeatTestContext(t, "/openai/v1/responses")
	cfg := &config.Config{}
	cfg.Security.URLAllowlist.AllowInsecureHTTP = true
	cfg.Gateway.OpenAIWS.Enabled = true
	cfg.Gateway.OpenAIWS.APIKeyEnabled = true
	cfg.Gateway.OpenAIWS.ResponsesWebsocketsV2 = true
	svc := &OpenAIGatewayService{cfg: cfg, openaiWSResolver: NewOpenAIWSProtocolResolver(cfg), toolCorrector: NewCodexToolCorrector()}
	account := &Account{
		ID: 91, Platform: PlatformOpenAI, Type: AccountTypeAPIKey, Concurrency: 1,
		Credentials: map[string]any{"api_key": "sk-test", "base_url": upstream.URL},
		Extra:       map[string]any{"responses_websockets_v2_enabled": true},
	}
	body := []byte(`{"model":"gpt-5.3-codex","stream":true,"previous_response_id":"resp_missing","input":[{"type":"input_text","text":"hello"}]}`)
	result, err := svc.Forward(context.Background(), c, account, body)
	require.NoError(t, err)
	require.NotNil(t, result)
	require.Equal(t, int32(2), attempts.Load(), "admission heartbeat must not prevent the existing WS recovery retry")
	require.Equal(t, "resp_wait_retry_ok", result.RequestID)
	require.Contains(t, rec.Body.String(), `"type":"response.completed"`)
}
