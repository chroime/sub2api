package handler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/ctxkey"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func syntheticFirstResponseHandlerContext() (*gin.Context, *httptest.ResponseRecorder) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	req := httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	req = req.WithContext(context.WithValue(req.Context(), ctxkey.RequestID, "handler-request"))
	c.Request = req
	return c, recorder
}

func TestStartSyntheticFirstResponseOnlyForStreamingRequests(t *testing.T) {
	h := &OpenAIGatewayHandler{cfg: &config.Config{Gateway: config.GatewayConfig{
		SyntheticFirstResponse: config.GatewaySyntheticFirstResponseConfig{
			Enabled:                  true,
			MinDelayMs:               5,
			MaxDelayMs:               5,
			UnderOneSecondPercent:    100,
			UnderOneSecondMaxDelayMs: 5,
		},
	}}}

	streamContext, streamRecorder := syntheticFirstResponseHandlerContext()
	stop := h.startSyntheticFirstResponse(streamContext, true, time.Now())
	t.Cleanup(stop)
	require.Eventually(t, func() bool { return streamRecorder.Body.String() == ":\n\n" }, time.Second, time.Millisecond)

	nonStreamContext, nonStreamRecorder := syntheticFirstResponseHandlerContext()
	stopNonStream := h.startSyntheticFirstResponse(nonStreamContext, false, time.Now())
	t.Cleanup(stopNonStream)
	time.Sleep(15 * time.Millisecond)
	require.Empty(t, nonStreamRecorder.Body.String())
}

func TestSyntheticFirstResponseConvertsLaterResponsesFailureToSSE(t *testing.T) {
	h := &OpenAIGatewayHandler{cfg: &config.Config{Gateway: config.GatewayConfig{
		SyntheticFirstResponse: config.GatewaySyntheticFirstResponseConfig{
			Enabled:                  true,
			MinDelayMs:               5,
			MaxDelayMs:               5,
			UnderOneSecondPercent:    100,
			UnderOneSecondMaxDelayMs: 5,
		},
	}}}
	c, recorder := syntheticFirstResponseHandlerContext()
	stop := h.startSyntheticFirstResponse(c, true, time.Now())
	t.Cleanup(stop)
	require.Eventually(t, func() bool { return recorder.Body.String() == ":\n\n" }, time.Second, time.Millisecond)

	h.handleStreamingAwareError(c, http.StatusBadGateway, "upstream_error", "upstream failed", false)

	require.Equal(t, http.StatusOK, recorder.Code)
	require.Contains(t, recorder.Body.String(), `"type":"response.failed"`)
	require.Contains(t, recorder.Body.String(), "upstream failed")
}
