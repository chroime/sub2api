package admin

import (
	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/gin-gonic/gin"
)

// Readiness is a read-only, non-secret integration checklist for one upstream.
// Each optional capability reports its own read_failed state instead of making
// the whole overview unavailable when one backing table cannot be read.
func (h *UpstreamGovernanceHandler) Readiness(c *gin.Context) {
	id, ok := governanceID(c, "id")
	if !ok {
		return
	}
	value, err := h.svc.Readiness(c.Request.Context(), id)
	if !governanceError(c, err) {
		response.Success(c, value)
	}
}
