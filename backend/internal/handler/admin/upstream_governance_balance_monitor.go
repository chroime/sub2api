package admin

import (
	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	gov "github.com/Wei-Shaw/sub2api/internal/upstreamgovernance"
	"github.com/gin-gonic/gin"
)

func (h *UpstreamGovernanceHandler) ConfigureBalanceMonitor(c *gin.Context) {
	id, ok := governanceID(c, "id")
	if !ok {
		return
	}
	var input struct {
		Version int64 `json:"version"`
		gov.BalanceMonitorConfig
	}
	if !governanceBody(c, &input) {
		return
	}
	site, err := h.svc.ConfigureBalanceMonitor(c.Request.Context(), id, input.Version, input.BalanceMonitorConfig)
	if !governanceError(c, err) {
		response.Success(c, site)
	}
}
