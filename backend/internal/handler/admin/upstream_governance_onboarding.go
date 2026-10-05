package admin

import (
	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	gov "github.com/Wei-Shaw/sub2api/internal/upstreamgovernance"
	"github.com/gin-gonic/gin"
)

func (h *UpstreamGovernanceHandler) Detect(c *gin.Context) {
	var input gov.DetectInput
	if !governanceBody(c, &input) {
		return
	}
	result, e := h.svc.Detect(c.Request.Context(), input)
	if !governanceError(c, e) {
		response.Success(c, result)
	}
}
