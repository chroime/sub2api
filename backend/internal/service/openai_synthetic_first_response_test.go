package service

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/ctxkey"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func syntheticFirstResponseTestContext(t *testing.T, requestID string) (*gin.Context, *httptest.ResponseRecorder) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	req := httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	req = req.WithContext(context.WithValue(req.Context(), ctxkey.RequestID, requestID))
	c.Request = req
	return c, recorder
}

func TestSyntheticFirstResponseDelayMeetsConfiguredSLO(t *testing.T) {
	cfg := config.GatewaySyntheticFirstResponseConfig{
		Enabled:                  true,
		MinDelayMs:               600,
		MaxDelayMs:               1500,
		UnderOneSecondPercent:    90,
		UnderOneSecondMaxDelayMs: 900,
	}

	subSecond := 0
	tail := 0
	for i := 0; i < 10_000; i++ {
		delay := syntheticFirstResponseDelay("request-"+strconv.Itoa(i), cfg)
		require.GreaterOrEqual(t, delay, 600*time.Millisecond)
		require.LessOrEqual(t, delay, 1500*time.Millisecond)
		if delay < time.Second {
			subSecond++
			require.GreaterOrEqual(t, delay, 600*time.Millisecond)
			require.LessOrEqual(t, delay, 900*time.Millisecond)
		} else {
			tail++
			require.GreaterOrEqual(t, delay, time.Second)
		}
	}
	// The deterministic hash bucket is expected to be approximately 90/10;
	// allow a small sampling tolerance while enforcing each bucket's bounds above.
	require.GreaterOrEqual(t, subSecond, 8_850)
	require.LessOrEqual(t, subSecond, 9_150)
	require.GreaterOrEqual(t, tail, 850)
	require.LessOrEqual(t, tail, 1_150)
}

func TestSyntheticFirstResponseWritesOneCommentForSlowStream(t *testing.T) {
	c, recorder := syntheticFirstResponseTestContext(t, "slow-request")
	cfg := config.GatewaySyntheticFirstResponseConfig{
		Enabled:                  true,
		MinDelayMs:               5,
		MaxDelayMs:               5,
		UnderOneSecondPercent:    100,
		UnderOneSecondMaxDelayMs: 5,
	}

	stop := StartOpenAISyntheticFirstResponse(c, cfg, time.Now())
	t.Cleanup(stop)
	require.Eventually(t, func() bool { return OpenAISyntheticFirstResponseMs(c) != nil }, time.Second, time.Millisecond)
	time.Sleep(15 * time.Millisecond)
	require.Equal(t, ":\n\n", recorder.Body.String())
	require.NotNil(t, OpenAISyntheticFirstResponseMs(c))
	require.Equal(t, -1, OpenAICompactKeepaliveAdjustedWrittenSize(c))
}

func TestSyntheticFirstResponseDoesNotDelayFastOutput(t *testing.T) {
	c, recorder := syntheticFirstResponseTestContext(t, "fast-request")
	cfg := config.GatewaySyntheticFirstResponseConfig{
		Enabled:                  true,
		MinDelayMs:               40,
		MaxDelayMs:               40,
		UnderOneSecondPercent:    100,
		UnderOneSecondMaxDelayMs: 40,
	}

	stop := StartOpenAISyntheticFirstResponse(c, cfg, time.Now())
	t.Cleanup(stop)
	started := time.Now()
	_, err := c.Writer.WriteString("data: {\"type\":\"response.output_text.delta\",\"delta\":\"fast\"}\n\n")
	require.NoError(t, err)
	c.Writer.Flush()
	require.Less(t, time.Since(started), 20*time.Millisecond)
	time.Sleep(60 * time.Millisecond)
	require.NotContains(t, recorder.Body.String(), "\n:\n")
	require.False(t, strings.HasPrefix(recorder.Body.String(), ":\n\n"))
	require.Nil(t, OpenAISyntheticFirstResponseMs(c))
	firstTokenMs := 2
	result := &OpenAIForwardResult{FirstTokenMs: &firstTokenMs}
	ApplyOpenAISyntheticFirstResponseResult(c, result)
	require.Equal(t, 2, *result.FirstTokenMs)
	require.Nil(t, result.StreamingAckMs)
}

func TestApplyOpenAISyntheticFirstResponsePreservesUpstreamTTFT(t *testing.T) {
	c, _ := syntheticFirstResponseTestContext(t, "result-request")
	cfg := config.GatewaySyntheticFirstResponseConfig{
		Enabled:                  true,
		MinDelayMs:               5,
		MaxDelayMs:               5,
		UnderOneSecondPercent:    100,
		UnderOneSecondMaxDelayMs: 5,
	}
	stop := StartOpenAISyntheticFirstResponse(c, cfg, time.Now())
	t.Cleanup(stop)
	require.Eventually(t, func() bool { return OpenAISyntheticFirstResponseMs(c) != nil }, time.Second, time.Millisecond)
	upstream := 22_580
	result := &OpenAIForwardResult{FirstTokenMs: &upstream}

	ApplyOpenAISyntheticFirstResponseResult(c, result)

	require.Equal(t, 22_580, *result.FirstTokenMs)
	require.Equal(t, 22_580, *result.UpstreamFirstTokenMs)
	require.Equal(t, 22_580, *result.SchedulerFirstTokenMs())
	require.NotNil(t, result.StreamingAckMs)
	require.Equal(t, *OpenAISyntheticFirstResponseMs(c), *result.StreamingAckMs)
}

func TestApplyOpenAISyntheticFirstResponseDoesNotInventTTFT(t *testing.T) {
	c, _ := syntheticFirstResponseTestContext(t, "ack-without-output")
	ackMs := 700
	c.Set(openAISyntheticFirstResponseKey, &openAISyntheticFirstResponse{ackMs: &ackMs})
	result := &OpenAIForwardResult{}

	ApplyOpenAISyntheticFirstResponseResult(c, result)

	require.Nil(t, result.FirstTokenMs)
	require.Nil(t, result.UpstreamFirstTokenMs)
	require.Nil(t, result.SchedulerFirstTokenMs())
	require.Equal(t, 700, *result.StreamingAckMs)
}

type syntheticAckFlushTimingWriter struct {
	gin.ResponseWriter
	flushedAt time.Time
}

func (w *syntheticAckFlushTimingWriter) Flush() {
	w.ResponseWriter.Flush()
	w.flushedAt = time.Now()
}

func TestSyntheticFirstResponseRecordsActualFlushLatency(t *testing.T) {
	c, recorder := syntheticFirstResponseTestContext(t, "actual-flush")
	startedAt := time.Now().Add(-250 * time.Millisecond)
	writer := &syntheticAckFlushTimingWriter{ResponseWriter: c.Writer}
	state := &openAISyntheticFirstResponse{writer: writer, startedAt: startedAt, stop: make(chan struct{})}
	c.Set(openAISyntheticFirstResponseKey, state)

	state.beat()

	ackMs := OpenAISyntheticFirstResponseMs(c)
	require.NotNil(t, ackMs)
	require.False(t, writer.flushedAt.IsZero())
	require.GreaterOrEqual(t, *ackMs, int(writer.flushedAt.Sub(startedAt).Milliseconds()))
	require.LessOrEqual(t, *ackMs, int(time.Since(startedAt).Milliseconds()))
	require.Equal(t, ":\n\n", recorder.Body.String())
}
