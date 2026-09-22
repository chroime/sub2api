package service

import (
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/gin-gonic/gin"
)

// StartStreamingACK acknowledges an eligible SSE request once while waiting
// for its first upstream output. The caller owns protocol and account gates.
func StartStreamingACK(c *gin.Context, cfg config.GatewaySyntheticFirstResponseConfig, startedAt time.Time) func() {
	return StartOpenAISyntheticFirstResponse(c, cfg, startedAt)
}

func StreamingACKMs(c *gin.Context) *int {
	return OpenAISyntheticFirstResponseMs(c)
}

func StreamingACKCommitted(c *gin.Context) bool {
	return OpenAISyntheticFirstResponseCommitted(c)
}

// ApplyStreamingACKResult records the real flush time separately; the model's
// FirstTokenMs remains unchanged for scheduling and administrative reporting.
func ApplyStreamingACKResult(c *gin.Context, result *ForwardResult) {
	if result != nil {
		result.StreamingAckMs = StreamingACKMs(c)
	}
}
