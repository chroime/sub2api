package middleware

import (
	"bytes"
	"net/http"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

// DownstreamUsageProjection rewrites standard usage fields in responses sent
// to an API-key client. It sits after API-key authentication so the request
// context already contains the group's adjustment. The underlying gateway
// services keep writing and recording the original upstream usage separately.
func DownstreamUsageProjection() gin.HandlerFunc {
	return func(c *gin.Context) {
		if !isDownstreamUsageResponsePath(c) {
			c.Next()
			return
		}
		adjustment, ok := service.DownstreamUsageAdjustmentFromContext(c.Request.Context())
		if !ok || !service.DownstreamUsageAdjustmentEnabled(adjustment) {
			c.Next()
			return
		}

		original := c.Writer
		projector := &downstreamUsageResponseWriter{
			ResponseWriter: original,
			adjustment:     adjustment,
		}
		c.Writer = projector
		defer func() {
			projector.finish()
			c.Writer = original
		}()
		c.Next()
	}
}

func isDownstreamUsageResponsePath(c *gin.Context) bool {
	if c == nil || c.Request == nil || c.Request.Method != http.MethodPost || c.Request.URL == nil {
		return false
	}
	path := strings.TrimRight(c.Request.URL.Path, "/")
	if strings.HasSuffix(path, "/messages/count_tokens") || strings.Contains(path, "/responses/input_tokens") {
		return false
	}
	return strings.HasSuffix(path, "/messages") ||
		strings.HasSuffix(path, "/chat/completions") ||
		strings.HasSuffix(path, "/responses") ||
		strings.Contains(path, "/responses/")
}

type downstreamUsageResponseWriter struct {
	gin.ResponseWriter
	adjustment service.TokenUsageAdjustment

	mode     downstreamResponseMode
	buffer   bytes.Buffer
	pending  bytes.Buffer
	closed   bool
	eligible bool
}

type downstreamResponseMode uint8

const (
	downstreamModeUnknown downstreamResponseMode = iota
	downstreamModeJSON
	downstreamModeSSE
	downstreamModeRaw
)

func (w *downstreamUsageResponseWriter) Write(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	w.ensureMode(p)
	switch w.mode {
	case downstreamModeJSON:
		_, _ = w.buffer.Write(p)
		return len(p), nil
	case downstreamModeSSE:
		_, _ = w.pending.Write(p)
		return len(p), w.flushCompleteSSEFrames()
	default:
		return w.ResponseWriter.Write(p)
	}
}

func (w *downstreamUsageResponseWriter) WriteString(value string) (int, error) {
	return w.Write([]byte(value))
}

func (w *downstreamUsageResponseWriter) Flush() {
	w.ensureMode(nil)
	if w.mode == downstreamModeJSON {
		// JSON responses are buffered until the handler returns so a serializer
		// split across multiple Write calls cannot bypass projection.
		return
	}
	if w.mode == downstreamModeSSE {
		_ = w.flushCompleteSSEFrames()
	}
	w.ResponseWriter.Flush()
}

func (w *downstreamUsageResponseWriter) ensureMode(firstWrite []byte) {
	if w.mode != downstreamModeUnknown {
		return
	}
	contentType := strings.ToLower(w.Header().Get("Content-Type"))
	switch {
	case strings.Contains(contentType, "text/event-stream"):
		w.mode = downstreamModeSSE
	case strings.Contains(contentType, "application/json") || strings.Contains(contentType, "+json"):
		w.mode = downstreamModeJSON
	case len(firstWrite) > 0:
		trimmed := bytes.TrimSpace(firstWrite)
		if bytes.HasPrefix(trimmed, []byte("data:")) || bytes.HasPrefix(trimmed, []byte("event:")) {
			w.mode = downstreamModeSSE
		} else if bytes.HasPrefix(trimmed, []byte("{")) || bytes.HasPrefix(trimmed, []byte("[")) {
			w.mode = downstreamModeJSON
		} else {
			w.mode = downstreamModeRaw
		}
	default:
		// Keep the mode unknown until the first body bytes or content type are
		// available. Flush with no body still delegates to the wrapped writer.
		return
	}
}

func (w *downstreamUsageResponseWriter) flushCompleteSSEFrames() error {
	for {
		frame, delimiterLen := takeCompleteSSEFrame(w.pending.Bytes())
		if delimiterLen == 0 {
			return nil
		}
		w.pending.Next(len(frame) + delimiterLen)
		projected := projectSSEFrame(frame, w.adjustment, &w.eligible)
		if delimiterLen == 4 {
			projected = append(projected, '\r', '\n', '\r', '\n')
		} else {
			projected = append(projected, '\n', '\n')
		}
		if _, err := w.ResponseWriter.Write(projected); err != nil {
			return err
		}
	}
}

func takeCompleteSSEFrame(data []byte) ([]byte, int) {
	lf := bytes.Index(data, []byte("\n\n"))
	crlf := bytes.Index(data, []byte("\r\n\r\n"))
	if lf < 0 && crlf < 0 {
		return nil, 0
	}
	if crlf >= 0 && (lf < 0 || crlf < lf) {
		return data[:crlf], 4
	}
	return data[:lf], 2
}

func projectSSEFrame(frame []byte, adjustment service.TokenUsageAdjustment, eligible *bool) []byte {
	var result bytes.Buffer
	for len(frame) > 0 {
		line, newlineLen := takeSSELine(frame)
		frame = frame[len(line)+newlineLen:]
		if bytes.HasPrefix(line, []byte("data:")) {
			prefix := line[:5]
			payload := bytes.TrimSpace(line[5:])
			projected, changed := service.ProjectDownstreamUsageJSONWithEligibility(payload, adjustment, eligible)
			if changed {
				result.Write(prefix)
				result.WriteByte(' ')
				result.Write(projected)
			} else {
				result.Write(line)
			}
		} else {
			result.Write(line)
		}
		if newlineLen == 2 {
			result.WriteString("\r\n")
		} else if newlineLen == 1 {
			result.WriteByte('\n')
		}
	}
	return result.Bytes()
}

func takeSSELine(data []byte) ([]byte, int) {
	if idx := bytes.IndexByte(data, '\n'); idx >= 0 {
		line := data[:idx]
		if len(line) > 0 && line[len(line)-1] == '\r' {
			return line[:len(line)-1], 2
		}
		return line, 1
	}
	return data, 0
}

func (w *downstreamUsageResponseWriter) finish() {
	if w.closed {
		return
	}
	w.closed = true
	switch w.mode {
	case downstreamModeJSON:
		body := w.buffer.Bytes()
		if projected, changed := service.ProjectDownstreamUsageJSON(body, w.adjustment); changed {
			_, _ = w.ResponseWriter.Write(projected)
		} else if len(body) > 0 {
			_, _ = w.ResponseWriter.Write(body)
		}
	case downstreamModeSSE:
		_ = w.flushCompleteSSEFrames()
		if w.pending.Len() > 0 {
			// A malformed or truncated final frame should remain visible to the
			// client; valid gateway frames always end in a blank line and have
			// already been projected above.
			_, _ = w.ResponseWriter.Write(w.pending.Bytes())
			w.pending.Reset()
		}
	}
}

var _ gin.ResponseWriter = (*downstreamUsageResponseWriter)(nil)
var _ http.Flusher = (*downstreamUsageResponseWriter)(nil)
