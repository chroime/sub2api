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
	if !stream || h == nil || h.cfg == nil || !account.IsOpenAISyntheticFirstResponseEnabledForGroup(groupID) {
		return func() {}
	}
	return service.StartOpenAISyntheticFirstResponse(c, h.cfg.Gateway.SyntheticFirstResponse, startedAt)
}

// resetSyntheticFirstResponseForRetry cancels an uncommitted ACK before a
// failover attempt. Once a synthetic ACK or any semantic response bytes have
// reached the client, the response cannot be retracted and the existing state
// is intentionally preserved.
func resetSyntheticFirstResponseForRetry(c *gin.Context, stop *func(), writerSizeBeforeForward int) {
	if stop == nil || *stop == nil {
		return
	}
	if service.OpenAISyntheticFirstResponseCommitted(c) {
		return
	}
	if service.OpenAICompactKeepaliveAdjustedWrittenSize(c) != writerSizeBeforeForward {
		return
	}
	(*stop)()
	*stop = nil
}
