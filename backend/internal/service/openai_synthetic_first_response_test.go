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
	require.Eventually(t, func() bool { return recorder.Body.String() == ":\n\n" }, time.Second, time.Millisecond)
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
}

func TestApplyOpenAISyntheticFirstResponsePreservesUpstreamTTFT(t *testing.T) {
	c, recorder := syntheticFirstResponseTestContext(t, "result-request")
	cfg := config.GatewaySyntheticFirstResponseConfig{
		Enabled:                  true,
		MinDelayMs:               5,
		MaxDelayMs:               5,
		UnderOneSecondPercent:    100,
		UnderOneSecondMaxDelayMs: 5,
	}
	stop := StartOpenAISyntheticFirstResponse(c, cfg, time.Now())
	t.Cleanup(stop)
	require.Eventually(t, func() bool { return recorder.Body.String() == ":\n\n" }, time.Second, time.Millisecond)
	upstream := 22_580
	result := &OpenAIForwardResult{FirstTokenMs: &upstream}

	ApplyOpenAISyntheticFirstResponseResult(c, result)

	require.Less(t, *result.FirstTokenMs, 100)
	require.Equal(t, 22_580, *result.UpstreamFirstTokenMs)
	require.Equal(t, 22_580, *result.SchedulerFirstTokenMs())
}
