package handler

import (
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

func (h *OpenAIGatewayHandler) startSyntheticFirstResponse(c *gin.Context, stream bool, startedAt time.Time) func() {
	if !stream || h == nil || h.cfg == nil {
		return func() {}
	}
	return service.StartOpenAISyntheticFirstResponse(c, h.cfg.Gateway.SyntheticFirstResponse, startedAt)
}
