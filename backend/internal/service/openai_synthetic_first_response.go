package service

import (
	"bufio"
	"errors"
	"hash/fnv"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/ctxkey"
	"github.com/gin-gonic/gin"
)

const openAISyntheticFirstResponseKey = "openai_synthetic_first_response"

type openAISyntheticFirstResponse struct {
	mu        sync.Mutex
	writer    gin.ResponseWriter
	startedAt time.Time
	stop      chan struct{}
	stopped   bool
	bytes     int
	ackMs     *int
}

// syntheticFirstResponseDelay returns a stable per-request delay. The wider
// tail is intentionally sparse so timer scheduling still has room to meet the
// configured sub-second SLO.
func syntheticFirstResponseDelay(seed string, cfg config.GatewaySyntheticFirstResponseConfig) time.Duration {
	hasher := fnv.New64a()
	_, _ = hasher.Write([]byte(strings.TrimSpace(seed)))
	hash := hasher.Sum64()

	low := cfg.MinDelayMs
	high := cfg.UnderOneSecondMaxDelayMs
	if int(hash%100) >= cfg.UnderOneSecondPercent {
		low = 1000
		if cfg.MinDelayMs > low {
			low = cfg.MinDelayMs
		}
		high = cfg.MaxDelayMs
	}
	if low <= 0 {
		low = 300
	}
	if high < low {
		high = low
	}
	span := uint64(high - low + 1)
	delayMs := low + int((hash/100)%span)
	return time.Duration(delayMs) * time.Millisecond
}

func syntheticFirstResponseSeed(c *gin.Context) string {
	if c == nil || c.Request == nil {
		return ""
	}
	if value, _ := c.Request.Context().Value(ctxkey.RequestID).(string); strings.TrimSpace(value) != "" {
		return value
	}
	if value, _ := c.Request.Context().Value(ctxkey.ClientRequestID).(string); strings.TrimSpace(value) != "" {
		return value
	}
	return c.Request.Method + " " + c.Request.URL.Path
}

// StartOpenAISyntheticFirstResponse starts a one-shot SSE comment ACK. The
// first normal response-body write wins the same mutex and cancels the ACK,
// which guarantees that fast upstream output is never delayed.
func StartOpenAISyntheticFirstResponse(c *gin.Context, cfg config.GatewaySyntheticFirstResponseConfig, startedAt time.Time) func() {
	if c == nil || c.Writer == nil || !cfg.Enabled {
		return func() {}
	}
	if existing, ok := c.Get(openAISyntheticFirstResponseKey); ok {
		if state, valid := existing.(*openAISyntheticFirstResponse); valid && state != nil {
			return state.Stop
		}
	}
	if startedAt.IsZero() {
		startedAt = time.Now()
	}

	originalWriter := c.Writer
	state := &openAISyntheticFirstResponse{
		writer:    originalWriter,
		startedAt: startedAt,
		stop:      make(chan struct{}),
	}
	header := originalWriter.Header()
	header.Set("Content-Type", "text/event-stream")
	header.Set("Cache-Control", "no-cache")
	header.Set("Connection", "keep-alive")
	header.Set("X-Accel-Buffering", "no")

	wrapper := &openAISyntheticFirstResponseWriter{ResponseWriter: originalWriter, state: state}
	c.Writer = wrapper
	c.Set(openAISyntheticFirstResponseKey, state)

	delay := syntheticFirstResponseDelay(syntheticFirstResponseSeed(c), cfg)
	go func() {
		timer := time.NewTimer(delay)
		defer timer.Stop()
		select {
		case <-state.stop:
			return
		case <-c.Request.Context().Done():
			state.Stop()
			return
		case <-timer.C:
			state.beat()
		}
	}()

	return func() {
		state.Stop()
		if current, ok := c.Writer.(*openAISyntheticFirstResponseWriter); ok && current == wrapper {
			c.Writer = originalWriter
		}
	}
}

func (s *openAISyntheticFirstResponse) beat() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.stopped {
		return
	}
	s.writer.WriteHeader(http.StatusOK)
	n, err := s.writer.Write([]byte(":\n\n"))
	s.bytes += n
	if err == nil {
		s.writer.Flush()
		ms := int(time.Since(s.startedAt).Milliseconds())
		s.ackMs = &ms
	}
	s.stopLocked()
}

func (s *openAISyntheticFirstResponse) stopLocked() {
	if s.stopped {
		return
	}
	s.stopped = true
	close(s.stop)
}

func (s *openAISyntheticFirstResponse) Stop() {
	s.mu.Lock()
	s.stopLocked()
	s.mu.Unlock()
}

// OpenAISyntheticFirstResponseMs returns the actual server-side ACK flush
// latency. Nil means real output or cancellation won before the timer.
func OpenAISyntheticFirstResponseMs(c *gin.Context) *int {
	state := openAISyntheticFirstResponseFromContext(c)
	if state == nil {
		return nil
	}
	state.mu.Lock()
	defer state.mu.Unlock()
	if state.ackMs == nil {
		return nil
	}
	value := *state.ackMs
	return &value
}

// OpenAISyntheticFirstResponseCommitted reports whether the one-shot comment
// has committed the downstream response as an SSE stream.
func OpenAISyntheticFirstResponseCommitted(c *gin.Context) bool {
	return OpenAISyntheticFirstResponseMs(c) != nil
}

func openAISyntheticFirstResponseBytes(c *gin.Context) int {
	state := openAISyntheticFirstResponseFromContext(c)
	if state == nil {
		return 0
	}
	state.mu.Lock()
	defer state.mu.Unlock()
	return state.bytes
}

func openAISyntheticFirstResponseFromContext(c *gin.Context) *openAISyntheticFirstResponse {
	if c == nil {
		return nil
	}
	value, ok := c.Get(openAISyntheticFirstResponseKey)
	if !ok {
		return nil
	}
	state, _ := value.(*openAISyntheticFirstResponse)
	return state
}

// ApplyOpenAISyntheticFirstResponseResult keeps the upstream TTFT intact and
// exposes the earlier downstream ACK latency through the existing usage field.
func ApplyOpenAISyntheticFirstResponseResult(c *gin.Context, result *OpenAIForwardResult) {
	if result == nil {
		return
	}
	if result.UpstreamFirstTokenMs == nil && result.FirstTokenMs != nil {
		value := *result.FirstTokenMs
		result.UpstreamFirstTokenMs = &value
	}
	ackMs := OpenAISyntheticFirstResponseMs(c)
	if ackMs != nil && (result.FirstTokenMs == nil || *ackMs < *result.FirstTokenMs) {
		value := *ackMs
		result.FirstTokenMs = &value
	}
}

type openAISyntheticFirstResponseWriter struct {
	gin.ResponseWriter
	state *openAISyntheticFirstResponse
}

func (w *openAISyntheticFirstResponseWriter) stopAndWrite(write func() (int, error)) (int, error) {
	w.state.mu.Lock()
	defer w.state.mu.Unlock()
	w.state.stopLocked()
	return write()
}

func (w *openAISyntheticFirstResponseWriter) Write(data []byte) (int, error) {
	return w.stopAndWrite(func() (int, error) { return w.ResponseWriter.Write(data) })
}

func (w *openAISyntheticFirstResponseWriter) WriteString(value string) (int, error) {
	return w.stopAndWrite(func() (int, error) { return w.ResponseWriter.WriteString(value) })
}

func (w *openAISyntheticFirstResponseWriter) WriteHeader(code int) {
	w.state.mu.Lock()
	defer w.state.mu.Unlock()
	if code >= http.StatusMultipleChoices {
		w.state.stopLocked()
	}
	w.ResponseWriter.WriteHeader(code)
}

func (w *openAISyntheticFirstResponseWriter) Flush() {
	w.state.mu.Lock()
	defer w.state.mu.Unlock()
	w.ResponseWriter.Flush()
}

func (w *openAISyntheticFirstResponseWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	w.state.Stop()
	if w.ResponseWriter == nil {
		return nil, nil, errors.New("response writer released")
	}
	return w.ResponseWriter.Hijack()
}

func (w *openAISyntheticFirstResponseWriter) Status() int {
	w.state.mu.Lock()
	defer w.state.mu.Unlock()
	return w.ResponseWriter.Status()
}

func (w *openAISyntheticFirstResponseWriter) Size() int {
	w.state.mu.Lock()
	defer w.state.mu.Unlock()
	return w.ResponseWriter.Size()
}

func (w *openAISyntheticFirstResponseWriter) Written() bool {
	w.state.mu.Lock()
	defer w.state.mu.Unlock()
	return w.ResponseWriter.Written()
}
