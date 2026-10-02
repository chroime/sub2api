package admin

import (
	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	gov "github.com/Wei-Shaw/sub2api/internal/upstreamgovernance"
	"github.com/gin-gonic/gin"
)

func (h *UpstreamGovernanceHandler) BalanceHealth(c *gin.Context) {
	id, ok := governanceID(c, "id")
	if !ok {
		return
	}
	value, err := h.svc.BalanceHealth(c.Request.Context(), id)
	if !governanceError(c, err) {
		response.Success(c, value)
	}
}

func (h *UpstreamGovernanceHandler) RechargePlan(c *gin.Context) {
	id, ok := governanceID(c, "id")
	if !ok {
		return
	}
	value, err := h.svc.RechargePlan(c.Request.Context(), id)
	if !governanceError(c, err) {
		response.Success(c, value)
	}
}

func (h *UpstreamGovernanceHandler) ConfigureRechargePlan(c *gin.Context) {
	id, ok := governanceID(c, "id")
	if !ok {
		return
	}
	var input struct {
		Version int64              `json:"version"`
		Policy  gov.RechargePolicy `json:"policy"`
	}
	if !governanceBody(c, &input) {
		return
	}
	value, err := h.svc.ConfigureRechargePlan(c.Request.Context(), id, input.Version, input.Policy)
	if !governanceError(c, err) {
		response.Success(c, value)
	}
}

func (h *UpstreamGovernanceHandler) EvaluateRechargePlan(c *gin.Context) {
	id, ok := governanceID(c, "id")
	if !ok {
		return
	}
	var input struct{}
	if !governanceBody(c, &input) {
		return
	}
	value, err := h.svc.EvaluateRechargePlan(c.Request.Context(), id)
	if !governanceError(c, err) {
		response.Success(c, value)
	}
}
