package handler

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestBalancePrechargeWaitHeartbeatErrorsRemainSSE(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, tc := range []struct {
		name     string
		path     string
		write    func(*gin.Context)
		terminal string
	}{
		{"responses", "/v1/responses", func(c *gin.Context) {
			(&GatewayHandler{}).responsesErrorResponse(c, 429, "balance_precharge_wait_timeout", "wait expired")
		}, "event: response.failed"},
		{"chat", "/v1/chat/completions", func(c *gin.Context) {
			(&GatewayHandler{}).chatCompletionsErrorResponse(c, 429, "balance_precharge_wait_timeout", "wait expired")
		}, "data: "},
		{"gemini", "/v1beta/models/gemini:streamGenerateContent?alt=sse", func(c *gin.Context) { googleError(c, 429, "wait expired") }, "data: "},
		{"native responses", "/v1/responses", func(c *gin.Context) {
			(&OpenAIGatewayHandler{}).errorResponse(c, 429, "balance_precharge_wait_timeout", "wait expired")
		}, "event: response.failed"},
		{"native messages", "/v1/messages", func(c *gin.Context) {
			(&OpenAIGatewayHandler{}).anthropicErrorResponse(c, 429, "balance_precharge_wait_timeout", "wait expired")
		}, "event: error"},
		{"messages", "/v1/messages", func(c *gin.Context) {
			(&GatewayHandler{}).errorResponse(c, 429, "balance_precharge_wait_timeout", "wait expired")
		}, "event: error"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(recorder)
			c.Request = httptest.NewRequest(http.MethodPost, tc.path, nil)
			c.Header("Content-Type", "text/event-stream")
			written, err := fmt.Fprint(c.Writer, ": keepalive\n\n")
			require.NoError(t, err)
			recordGatewayStreamHeartbeat(c, written)
			c.Writer.Flush()
			tc.write(c)
			require.Equal(t, http.StatusOK, recorder.Code)
			require.Contains(t, recorder.Header().Get("Content-Type"), "text/event-stream")
			require.Contains(t, recorder.Body.String(), tc.terminal)
			require.Contains(t, recorder.Body.String(), "wait expired")
			require.NotContains(t, recorder.Body.String(), "\n\n{", "JSON must never be appended outside an SSE data frame")
		})
	}
}

func TestBalancePrechargeWaitHeartbeatIsNonsemanticAndStopsOnCancel(t *testing.T) {
	c, recorder := newBalancePrechargeHeartbeatContext()
	ctx, cancel := context.WithCancel(c.Request.Context())
	defer cancel()
	c.Request = c.Request.WithContext(ctx)
	started := false
	beat := balancePrechargeWaitHeartbeat(c, &started)
	require.NoError(t, beat())
	require.True(t, started)
	require.Equal(t, ": keepalive\n\n", recorder.Body.String())
	require.True(t, gatewayStreamHasOnlyHeartbeats(c))
	require.Equal(t, -1, service.OpenAICompactKeepaliveAdjustedWrittenSize(c), "admission heartbeat must not block upstream failover")
	require.False(t, service.IsResponseCommitted(c))
	require.Nil(t, service.StreamingACKMs(c), "queue heartbeat must not create an account ACK measurement")
	require.NoError(t, beat())
	cancel()
	require.ErrorIs(t, beat(), context.Canceled)
	require.Equal(t, ": keepalive\n\n: keepalive\n\n", recorder.Body.String())
}

func TestBalancePrechargeWaitContextLeavesNonSSEUntouched(t *testing.T) {
	c, recorder := newBalancePrechargeHeartbeatContext()
	ctx := c.Request.Context()
	started := false
	require.Equal(t, ctx, balancePrechargeWaitContext(ctx, c, false, &started))
	require.Equal(t, ctx, balancePrechargeWaitContext(ctx, c, true, nil))
	require.Empty(t, recorder.Body.String())
	require.False(t, started)
}

func TestBalancePrechargeWaitHeartbeatReturnsWriteError(t *testing.T) {
	c, _ := newBalancePrechargeHeartbeatContext()
	c.Writer = &prechargeWaitFailingWriter{ResponseWriter: c.Writer}
	started := false
	require.ErrorContains(t, balancePrechargeWaitHeartbeat(c, &started)(), "disconnected")
}

func TestBillingErrorDetailsPrechargeWaitTimeout(t *testing.T) {
	status, code, message, retryAfter := billingErrorDetails(fmt.Errorf("admission: %w", service.ErrBalancePrechargeWaitTimeout))
	require.Equal(t, http.StatusTooManyRequests, status)
	require.Equal(t, "balance_precharge_wait_timeout", code)
	require.Contains(t, message, "waiting")
	require.Equal(t, 1, retryAfter)
}

func TestBillingErrorDetailsQuotaExhaustedDuringPrechargeWait(t *testing.T) {
	status, code, message, retryAfter := billingErrorDetails(fmt.Errorf("admission: %w", service.ErrAPIKeyQuotaExhausted))
	require.Equal(t, http.StatusTooManyRequests, status)
	require.Equal(t, "rate_limit_exceeded", code)
	require.Contains(t, message, "额度已用完")
	require.Zero(t, retryAfter)
}

func newBalancePrechargeHeartbeatContext() (*gin.Context, *httptest.ResponseRecorder) {
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	return c, recorder
}

type prechargeWaitFailingWriter struct{ gin.ResponseWriter }

func (w *prechargeWaitFailingWriter) Write([]byte) (int, error) {
	return 0, errors.New("client disconnected")
}
