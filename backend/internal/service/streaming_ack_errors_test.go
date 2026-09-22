package service

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestStreamingACKErrorsStayFramedAfterConfirmation(t *testing.T) {
	for _, test := range []struct {
		name, path, event string
		write             func(*gin.Context)
	}{
		{"claude", "/v1/messages", "event: error", func(c *gin.Context) { writeAnthropicError(c, 400, "invalid_request_error", "bad request") }},
		{"chat", "/v1/chat/completions", "data:", func(c *gin.Context) { writeGatewayCCError(c, 400, "invalid_request_error", "bad request") }},
		{"responses", "/v1/responses", "event: response.failed", func(c *gin.Context) { writeResponsesError(c, 400, "invalid_request_error", "bad request") }},
		{"gemini", "/v1beta/models/gemini:streamGenerateContent", "data:", func(c *gin.Context) { _ = (&GeminiMessagesCompatService{}).writeGoogleError(c, 400, "bad request") }},
		{"antigravity", "/v1/messages", "event: error", func(c *gin.Context) {
			_ = (&AntigravityGatewayService{}).writeClaudeError(c, 400, "invalid_request_error", "bad request")
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			c, recorder := syntheticFirstResponseTestContext(t, test.name)
			c.Request.URL.Path = test.path
			state := &openAISyntheticFirstResponse{writer: c.Writer, startedAt: time.Now(), stop: make(chan struct{})}
			c.Set(openAISyntheticFirstResponseKey, state)
			state.beat()
			test.write(c)
			require.Equal(t, http.StatusOK, recorder.Code)
			require.Contains(t, recorder.Body.String(), test.event)
			require.NotContains(t, recorder.Body.String(), ":\n\n{", "a JSON error must not follow an ACK without SSE framing")
			for _, line := range strings.Split(recorder.Body.String(), "\n") {
				if strings.HasPrefix(line, "data: ") && line != "data: [DONE]" {
					require.True(t, json.Valid([]byte(strings.TrimPrefix(line, "data: "))))
				}
			}
		})
	}
}

func TestStreamingACKFastErrorKeepsHTTPStatusAndJSON(t *testing.T) {
	c, recorder := syntheticFirstResponseTestContext(t, "fast-error")
	c.Request.URL.Path = "/v1/messages"
	stop := StartStreamingACK(c, config.GatewaySyntheticFirstResponseConfig{
		Enabled: true, MinDelayMs: 30, MaxDelayMs: 30, UnderOneSecondPercent: 100, UnderOneSecondMaxDelayMs: 30,
	}, time.Now())
	t.Cleanup(stop)
	writeAnthropicError(c, http.StatusBadRequest, "invalid_request_error", "bad request")
	time.Sleep(50 * time.Millisecond)
	stop()
	require.Equal(t, http.StatusBadRequest, recorder.Code)
	require.Contains(t, recorder.Header().Get("Content-Type"), "application/json")
	require.True(t, json.Valid(recorder.Body.Bytes()))
	require.Nil(t, StreamingACKMs(c))
}

func TestStreamingErrorsAfterAdmissionHeartbeat(t *testing.T) {
	for _, path := range []string{"/v1/responses", "/v1/chat/completions", "/v1/messages", "/v1beta/models/gemini:streamGenerateContent"} {
		for _, dataError := range []bool{false, true} {
			t.Run(path+map[bool]string{false: "/json", true: "/data"}[dataError], func(t *testing.T) {
				c, recorder := syntheticFirstResponseTestContext(t, "queued-error")
				c.Request.URL.Path = path
				c.Header("Content-Type", "text/event-stream")
				_, err := c.Writer.WriteString(": keepalive\n\n")
				require.NoError(t, err)
				c.Writer.Flush()
				if dataError {
					writeStreamingACKDataError(c, http.StatusBadGateway, "application/json", []byte("{\n  \"error\": {\"message\":\"upstream failed\"}\n}"))
				} else {
					writeStreamingACKJSONError(c, http.StatusBadGateway, gin.H{"error": gin.H{"message": "upstream failed"}})
				}
				require.Equal(t, http.StatusOK, recorder.Code)
				require.Contains(t, recorder.Header().Get("Content-Type"), "text/event-stream")
				require.Contains(t, recorder.Body.String(), "data: ")
				require.NotContains(t, recorder.Body.String(), "\n\n{")
				require.True(t, IsResponseCommitted(c))
				require.Nil(t, StreamingACKMs(c))
				if path == "/v1/responses" {
					require.Contains(t, recorder.Body.String(), "event: response.failed")
				}
				for _, line := range strings.Split(recorder.Body.String(), "\n") {
					if strings.HasPrefix(line, "data: ") && line != "data: [DONE]" {
						require.True(t, json.Valid([]byte(strings.TrimPrefix(line, "data: "))))
					}
				}
			})
		}
	}
}
