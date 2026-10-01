package admin

import (
	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	gov "github.com/Wei-Shaw/sub2api/internal/upstreamgovernance"
	"github.com/gin-gonic/gin"
)

func (h *UpstreamGovernanceHandler) ImportTemplates(c *gin.Context) {
	value, err := h.svc.ImportTemplates(c.Request.Context())
	if !governanceError(c, err) {
		response.Success(c, value)
	}
}

func (h *UpstreamGovernanceHandler) SaveImportTemplates(c *gin.Context) {
	var input gov.ImportTemplates
	if !governanceBody(c, &input) {
		return
	}
	value, err := h.svc.SaveImportTemplates(c.Request.Context(), input)
	if !governanceError(c, err) {
		response.Success(c, value)
	}
}
