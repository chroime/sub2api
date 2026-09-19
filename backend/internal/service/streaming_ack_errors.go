package service

import (
	"bytes"
	"encoding/json"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
)

// StopStreamingACKCommitted synchronizes with the timer before choosing an
// HTTP or SSE error. Checking commitment without stopping leaves a race where
// an ACK can commit status 200 between the check and a normal JSON error.
func StopStreamingACKCommitted(c *gin.Context) bool {
	state := openAISyntheticFirstResponseFromContext(c)
	if state == nil {
		return false
	}
	state.mu.Lock()
	defer state.mu.Unlock()
	state.stopLocked()
	return state.bytes > 0
}

func writeStreamingACKJSONError(c *gin.Context, status int, payload any) {
	if stopStreamingErrorWritersCommitted(c) {
		data, err := json.Marshal(payload)
		if err != nil {
			data = []byte(`{"error":{"type":"upstream_error","message":"Upstream request failed"}}`)
		}
		writeStreamingACKErrorFrame(c, status, data)
		return
	}
	if openAISyntheticFirstResponseFromContext(c) != nil {
		c.Header("Content-Type", "application/json; charset=utf-8")
	}
	c.JSON(status, payload)
}

func writeStreamingACKDataError(c *gin.Context, status int, contentType string, data []byte) {
	if stopStreamingErrorWritersCommitted(c) {
		writeStreamingACKErrorFrame(c, status, data)
		return
	}
	if openAISyntheticFirstResponseFromContext(c) != nil {
		c.Header("Content-Type", contentType)
	}
	c.Data(status, contentType, data)
}

// Admission and slot-wait heartbeats may have committed SSE without starting an
// account ACK. Stop asynchronous writers before inspecting the shared response,
// then preserve SSE framing for every already-started streaming transport.
func stopStreamingErrorWritersCommitted(c *gin.Context) bool {
	ackCommitted := StopStreamingACKCommitted(c)
	compactCommitted := StopOpenAICompactSSEKeepaliveCommitted(c)
	return ackCommitted || compactCommitted || (c != nil && c.Writer != nil && c.Writer.Written() && strings.Contains(c.Writer.Header().Get("Content-Type"), "text/event-stream"))
}

func writeStreamingACKErrorFrame(c *gin.Context, status int, data []byte) {
	// Flatten pretty-printed passthrough errors into one valid SSE data line.
	var compact bytes.Buffer
	if err := json.Compact(&compact, data); err != nil {
		fallback, _ := json.Marshal(gin.H{"error": gin.H{
			"code": status, "type": "upstream_error", "message": http.StatusText(status),
		}})
		data = fallback
	} else {
		data = compact.Bytes()
	}
	path := ""
	if c.Request != nil && c.Request.URL != nil {
		path = strings.TrimRight(c.Request.URL.Path, "/")
	}
	frame := ""
	switch {
	case strings.HasSuffix(path, "/responses"), strings.Contains(path, "/responses/"):
		var envelope map[string]json.RawMessage
		_ = json.Unmarshal(data, &envelope)
		errValue := envelope["error"]
		if len(errValue) == 0 || string(errValue) == "null" {
			errValue = json.RawMessage(`{"code":"upstream_error","message":"Upstream request failed"}`)
		}
		failed, _ := json.Marshal(gin.H{"type": "response.failed", "sequence_number": 0,
			"response": gin.H{"object": "response", "status": "failed", "error": errValue}})
		frame = "event: response.failed\ndata: " + string(failed) + "\n\n"
	case strings.HasSuffix(path, "/chat/completions"):
		frame = "data: " + string(data) + "\n\ndata: [DONE]\n\n"
	case strings.Contains(path, ":streamGenerateContent"):
		frame = "data: " + string(data) + "\n\n"
	default:
		frame = "event: error\ndata: " + string(data) + "\n\n"
	}
	c.Header("Content-Type", "text/event-stream")
	errType := gjson.GetBytes(data, "error.type").String()
	if errType == "" {
		errType = "upstream_error"
	}
	MarkOpsStreamError(c, errType, gjson.GetBytes(data, "error.message").String(), status)
	MarkResponseCommitted(c)
	_, _ = c.Writer.WriteString(frame)
	c.Writer.Flush()
}
