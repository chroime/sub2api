package admin

import (
	"errors"
	"net/http"

	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	gov "github.com/Wei-Shaw/sub2api/internal/upstreamgovernance"
	"github.com/gin-gonic/gin"
)

func (h *UpstreamGovernanceHandler) AstrBotGroups(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	c.Header("Pragma", "no-cache")
	var query gov.AstrBotGroupsQuery
	if !governanceBody(c, &query) {
		return
	}
	result, err := h.svc.ListAstrBotGroups(c.Request.Context(), query)
	var queryErr *gov.AstrBotGroupsError
	if errors.As(err, &queryErr) {
		status := http.StatusBadGateway
		if queryErr.Code == "astrbot_not_configured" {
			status = http.StatusBadRequest
		}
		response.ErrorWithDetails(c, status, "AstrBot group list could not be loaded", queryErr.Code, nil)
		return
	}
	if !governanceError(c, err) {
		response.Success(c, result)
	}
}
