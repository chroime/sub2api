package handler

import (
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

// startStreamingACK is called only after account admission and immediately
// before forwarding a request whose downstream transport is known to be SSE.
func (h *GatewayHandler) startStreamingACK(c *gin.Context, stream bool, startedAt time.Time, account *service.Account, groupID *int64) func() {
	if !stream || h == nil || h.cfg == nil || c == nil || c.Request == nil || !account.SupportsStreamingACK() {
		return func() {}
	}
	if !h.gatewayService.IsStreamingACKEnabledForRequest(c.Request.Context(), account, groupID) {
		return func() {}
	}
	cfg := h.cfg.Gateway.SyntheticFirstResponse
	if h.gatewayService != nil {
		cfg = h.gatewayService.SyntheticFirstResponseConfig(c.Request.Context())
	}
	return service.StartStreamingACK(c, cfg, startedAt)
}

// Native Gemini also exposes a JSON-array streaming transport. Only explicit
// alt=sse requests may receive an SSE comment before the upstream responds.
func geminiStreamingACKEligible(c *gin.Context, stream bool) bool {
	return stream && c != nil && strings.EqualFold(c.Query("alt"), "sse")
}
