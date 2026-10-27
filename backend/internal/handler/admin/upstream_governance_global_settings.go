package admin

import (
	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	gov "github.com/Wei-Shaw/sub2api/internal/upstreamgovernance"
	"github.com/gin-gonic/gin"
)

// GlobalSettings returns the single administrator-wide governance configuration.
// Credentials are write-only, including plaintext values awaiting migration.
func (h *UpstreamGovernanceHandler) GlobalSettings(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	c.Header("Pragma", "no-cache")
	value, err := h.svc.GetGlobalSettings(c.Request.Context())
	if !governanceError(c, err) {
		value.AstrBot.APIKey = ""
		response.Success(c, value)
	}
}

func (h *UpstreamGovernanceHandler) SaveGlobalSettings(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	c.Header("Pragma", "no-cache")
	var input gov.GlobalSettings
	if !governanceBody(c, &input) {
		return
	}
	value, err := h.svc.SaveGlobalSettings(c.Request.Context(), input)
	if !governanceError(c, err) {
		value.AstrBot.APIKey = ""
		response.Success(c, value)
	}
}
