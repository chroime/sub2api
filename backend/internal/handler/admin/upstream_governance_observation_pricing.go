package admin

import (
	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	gov "github.com/Wei-Shaw/sub2api/internal/upstreamgovernance"
	"github.com/gin-gonic/gin"
)

func (h *UpstreamGovernanceHandler) ObservationPolicy(c *gin.Context) {
	id, ok := governanceID(c, "id")
	if !ok {
		return
	}
	value, err := h.svc.ObservationPolicy(c.Request.Context(), id)
	if !governanceError(c, err) {
		response.Success(c, value)
	}
}

func (h *UpstreamGovernanceHandler) ConfigureObservationPolicy(c *gin.Context) {
	id, ok := governanceID(c, "id")
	if !ok {
		return
	}
	var input struct {
		Version int64                    `json:"version"`
		Policy  gov.ObservationPolicy    `json:"policy"`
	}
	if !governanceBody(c, &input) {
		return
	}
	value, err := h.svc.ConfigureObservationPolicy(c.Request.Context(), id, input.Version, input.Policy)
	if !governanceError(c, err) {
		response.Success(c, value)
	}
}

func (h *UpstreamGovernanceHandler) PricingPolicies(c *gin.Context) {
	id, ok := governanceID(c, "id")
	if !ok {
		return
	}
	value, err := h.svc.PricingPolicies(c.Request.Context(), id)
	if !governanceError(c, err) {
		response.Success(c, value)
	}
}

func (h *UpstreamGovernanceHandler) ConfigurePricingPolicies(c *gin.Context) {
	id, ok := governanceID(c, "id")
	if !ok {
		return
	}
	var input gov.PricingPoliciesConfiguration
	if !governanceBody(c, &input) {
		return
	}
	value, err := h.svc.ConfigurePricingPolicies(c.Request.Context(), id, input)
	if !governanceError(c, err) {
		response.Success(c, value)
	}
}
