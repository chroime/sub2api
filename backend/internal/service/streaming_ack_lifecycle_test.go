package service

import (
	"net/http"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestStreamingACKRestartPreservesCommittedAcknowledgement(t *testing.T) {
	c, recorder := syntheticFirstResponseTestContext(t, "committed-retry")
	state := &openAISyntheticFirstResponse{writer: c.Writer, startedAt: time.Now().Add(-600 * time.Millisecond), stop: make(chan struct{})}
	c.Set(openAISyntheticFirstResponseKey, state)
	state.beat()
	before := *StreamingACKMs(c)
	stop := StartStreamingACK(c, config.GatewaySyntheticFirstResponseConfig{
		Enabled: true, MinDelayMs: 1000, MaxDelayMs: 1000, UnderOneSecondPercent: 100, UnderOneSecondMaxDelayMs: 1000,
	}, time.Now())
	t.Cleanup(stop)
	require.True(t, StreamingACKCommitted(c), "retry must not replace an already committed ACK")
	require.Equal(t, before, *StreamingACKMs(c))
	require.Equal(t, -1, OpenAICompactKeepaliveAdjustedWrittenSize(c))
	writeResponsesError(c, 400, "bad_request", "bad request")
	require.NotContains(t, recorder.Body.String(), ":\n\n{")
	require.Contains(t, recorder.Body.String(), "event: response.failed")
}

type streamingACKBlockedHeaderWriter struct {
	gin.ResponseWriter
	entered chan struct{}
	release chan struct{}
}

func (w *streamingACKBlockedHeaderWriter) WriteHeader(code int) {
	close(w.entered)
	<-w.release
	w.ResponseWriter.WriteHeader(code)
}

func TestStreamingACKHeaderUpdatesDoNotMutateTimerHeaders(t *testing.T) {
	c, _ := syntheticFirstResponseTestContext(t, "headers")
	originalHeaders := c.Writer.Header()
	blocked := &streamingACKBlockedHeaderWriter{ResponseWriter: c.Writer, entered: make(chan struct{}), release: make(chan struct{})}
	c.Writer = blocked
	stop := StartStreamingACK(c, config.GatewaySyntheticFirstResponseConfig{
		Enabled: true, MinDelayMs: 5, MaxDelayMs: 5, UnderOneSecondPercent: 100, UnderOneSecondMaxDelayMs: 5,
	}, time.Now())
	t.Cleanup(stop)
	defer close(blocked.release)
	select {
	case <-blocked.entered:
	case <-time.After(time.Second):
		t.Fatal("ACK writer was never entered")
	}
	c.Header("X-Upstream-ID", "late-header")
	require.Empty(t, originalHeaders.Get("X-Upstream-ID"), "request writes must not mutate the map being read by the ACK timer")
}

func TestStreamingACKPreservesHeadersWhenFastOutputWins(t *testing.T) {
	c, recorder := syntheticFirstResponseTestContext(t, "fast-headers")
	stop := StartStreamingACK(c, config.GatewaySyntheticFirstResponseConfig{
		Enabled: true, MinDelayMs: 1000, MaxDelayMs: 1000, UnderOneSecondPercent: 100, UnderOneSecondMaxDelayMs: 1000,
	}, time.Now())
	t.Cleanup(stop)
	c.Header("X-Upstream-ID", "request-123")
	c.Writer.WriteHeader(http.StatusOK)
	_, err := c.Writer.WriteString("data: hello\n\n")
	require.NoError(t, err)
	c.Writer.Flush()
	require.Equal(t, "request-123", recorder.Result().Header.Get("X-Upstream-ID"))
	require.Nil(t, StreamingACKMs(c))
}
