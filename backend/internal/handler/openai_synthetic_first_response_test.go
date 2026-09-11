package handler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/ctxkey"
	"github.com/Wei-Shaw/sub2api/internal/service"
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

func syntheticFirstResponseEnabledAccount(groupIDs ...int64) *service.Account {
	return &service.Account{
		Platform: service.PlatformOpenAI,
		Extra: map[string]any{
			service.OpenAISyntheticFirstResponseEnabledExtraKey: true,
		},
		GroupIDs: groupIDs,
	}
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
	stop := h.startSyntheticFirstResponse(streamContext, true, time.Now(), syntheticFirstResponseEnabledAccount(), nil)
	t.Cleanup(stop)
	require.Eventually(t, func() bool { return streamRecorder.Body.String() == ":\n\n" }, time.Second, time.Millisecond)

	nonStreamContext, nonStreamRecorder := syntheticFirstResponseHandlerContext()
	stopNonStream := h.startSyntheticFirstResponse(nonStreamContext, false, time.Now(), syntheticFirstResponseEnabledAccount(), nil)
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
	stop := h.startSyntheticFirstResponse(c, true, time.Now(), syntheticFirstResponseEnabledAccount(), nil)
	t.Cleanup(stop)
	require.Eventually(t, func() bool { return recorder.Body.String() == ":\n\n" }, time.Second, time.Millisecond)

	h.handleStreamingAwareError(c, http.StatusBadGateway, "upstream_error", "upstream failed", false)

	require.Equal(t, http.StatusOK, recorder.Code)
	require.Contains(t, recorder.Body.String(), `"type":"response.failed"`)
	require.Contains(t, recorder.Body.String(), "upstream failed")
}

func TestStartSyntheticFirstResponseRequiresAccountOptInAndGroupMatch(t *testing.T) {
	h := &OpenAIGatewayHandler{cfg: &config.Config{Gateway: config.GatewayConfig{
		SyntheticFirstResponse: config.GatewaySyntheticFirstResponseConfig{
			Enabled:                  true,
			MinDelayMs:               5,
			MaxDelayMs:               5,
			UnderOneSecondPercent:    100,
			UnderOneSecondMaxDelayMs: 5,
		},
	}}}

	groupID := int64(7)
	account := syntheticFirstResponseEnabledAccount(groupID)

	matchingContext, matchingRecorder := syntheticFirstResponseHandlerContext()
	stop := h.startSyntheticFirstResponse(matchingContext, true, time.Now(), account, &groupID)
	t.Cleanup(stop)
	require.Eventually(t, func() bool { return matchingRecorder.Body.String() == ":\n\n" }, time.Second, time.Millisecond)

	disabledContext, disabledRecorder := syntheticFirstResponseHandlerContext()
	disabledAccount := &service.Account{Platform: service.PlatformOpenAI}
	stopDisabled := h.startSyntheticFirstResponse(disabledContext, true, time.Now(), disabledAccount, &groupID)
	t.Cleanup(stopDisabled)
	time.Sleep(15 * time.Millisecond)
	require.Empty(t, disabledRecorder.Body.String())

	mismatchContext, mismatchRecorder := syntheticFirstResponseHandlerContext()
	otherGroupID := int64(8)
	stopMismatch := h.startSyntheticFirstResponse(mismatchContext, true, time.Now(), account, &otherGroupID)
	t.Cleanup(stopMismatch)
	time.Sleep(15 * time.Millisecond)
	require.Empty(t, mismatchRecorder.Body.String())
}

func TestResetSyntheticFirstResponseForRetryCancelsUncommittedAck(t *testing.T) {
	c, recorder := syntheticFirstResponseHandlerContext()
	h := &OpenAIGatewayHandler{cfg: &config.Config{Gateway: config.GatewayConfig{
		SyntheticFirstResponse: config.GatewaySyntheticFirstResponseConfig{
			Enabled:                  true,
			MinDelayMs:               40,
			MaxDelayMs:               40,
			UnderOneSecondPercent:    100,
			UnderOneSecondMaxDelayMs: 40,
		},
	}}}
	stop := h.startSyntheticFirstResponse(c, true, time.Now(), syntheticFirstResponseEnabledAccount(), nil)
	require.NotNil(t, stop)
	writerSizeBeforeForward := service.OpenAICompactKeepaliveAdjustedWrittenSize(c)

	resetSyntheticFirstResponseForRetry(c, &stop, writerSizeBeforeForward)
	require.Nil(t, stop)
	time.Sleep(60 * time.Millisecond)
	require.Empty(t, recorder.Body.String())
}

func TestResetSyntheticFirstResponseForRetryAllowsARearm(t *testing.T) {
	c, recorder := syntheticFirstResponseHandlerContext()
	h := &OpenAIGatewayHandler{cfg: &config.Config{Gateway: config.GatewayConfig{
		SyntheticFirstResponse: config.GatewaySyntheticFirstResponseConfig{
			Enabled:                  true,
			MinDelayMs:               40,
			MaxDelayMs:               40,
			UnderOneSecondPercent:    100,
			UnderOneSecondMaxDelayMs: 40,
		},
	}}}
	stop := h.startSyntheticFirstResponse(c, true, time.Now(), syntheticFirstResponseEnabledAccount(), nil)
	require.NotNil(t, stop)
	writerSizeBeforeForward := service.OpenAICompactKeepaliveAdjustedWrittenSize(c)

	resetSyntheticFirstResponseForRetry(c, &stop, writerSizeBeforeForward)
	require.Nil(t, stop)

	rearmedStop := h.startSyntheticFirstResponse(c, true, time.Now(), syntheticFirstResponseEnabledAccount(), nil)
	t.Cleanup(rearmedStop)
	require.Eventually(t, func() bool { return recorder.Body.String() == ":\n\n" }, time.Second, time.Millisecond)
}
