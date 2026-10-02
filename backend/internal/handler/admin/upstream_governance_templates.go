package admin

import (
	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	gov "github.com/Wei-Shaw/sub2api/internal/upstreamgovernance"
	"github.com/gin-gonic/gin"
)

func (h *UpstreamGovernanceHandler) ModelTemplates(c *gin.Context) {
	value, err := h.svc.ModelTemplates(c.Request.Context())
	if governanceError(c, err) {
		return
	}
	response.Success(c, value)
}

func (h *UpstreamGovernanceHandler) SaveModelTemplates(c *gin.Context) {
	var input gov.ModelTemplates
	if !governanceBody(c, &input) {
		return
	}
	value, err := h.svc.SaveModelTemplates(c.Request.Context(), input)
	if governanceError(c, err) {
		return
	}
	response.Success(c, value)
}
