package handler

import (
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

func (h *OpenAIGatewayHandler) startSyntheticFirstResponse(
	c *gin.Context,
	stream bool,
	startedAt time.Time,
	account *service.Account,
	groupID *int64,
) func() {
	if !stream || h == nil || h.cfg == nil || !account.IsStreamingACKEnabledForGroup(groupID) {
		return func() {}
	}
	cfg := h.cfg.Gateway.SyntheticFirstResponse
	if h.gatewayService != nil {
		cfg = h.gatewayService.SyntheticFirstResponseConfig(c.Request.Context())
	}
	return service.StartStreamingACK(c, cfg, startedAt)
}

// resetSyntheticFirstResponseForRetry cancels an uncommitted ACK before a
// failover attempt. Once a synthetic ACK or any semantic response bytes have
// reached the client, the response cannot be retracted and the existing state
// is intentionally preserved.
func resetSyntheticFirstResponseForRetry(c *gin.Context, stop *func(), writerSizeBeforeForward int) {
	if stop == nil || *stop == nil {
		return
	}
	// Stop under the timer's mutex before inspecting commitment. Otherwise an
	// ACK can land between the check and cleanup and lose its state on retry.
	if service.StopStreamingACKCommitted(c) {
		return
	}
	if service.OpenAICompactKeepaliveAdjustedWrittenSize(c) != writerSizeBeforeForward {
		return
	}
	(*stop)()
	*stop = nil
}
