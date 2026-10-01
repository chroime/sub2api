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
		Version int64                 `json:"version"`
		Policy  gov.ObservationPolicy `json:"policy"`
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

// Pointers distinguish omitted/null values from intentional zero/false. The
// outer strict decoder rejects all client-supplied authoritative fields.
type governancePricingDraftInput struct {
	Enabled                  *bool            `json:"enabled"`
	Mode                     *gov.PricingMode `json:"mode"`
	MinMargin                *float64         `json:"min_margin"`
	SafetyBuffer             *float64         `json:"safety_buffer"`
	DecreaseStabilitySeconds *int64           `json:"decrease_stability_seconds"`
	MaxIncreasePercent       *float64         `json:"max_increase_percent"`
}

func (p *governancePricingDraftInput) draft(c *gin.Context) (gov.PricingPolicyDraft, bool) {
	if p == nil || p.Enabled == nil || p.Mode == nil || p.MinMargin == nil || p.SafetyBuffer == nil || p.DecreaseStabilitySeconds == nil || p.MaxIncreasePercent == nil {
		governanceError(c, gov.ErrInvalid)
		return gov.PricingPolicyDraft{}, false
	}
	return gov.PricingPolicyDraft{Enabled: *p.Enabled, Mode: *p.Mode, MinMargin: *p.MinMargin, SafetyBuffer: *p.SafetyBuffer, DecreaseStabilitySeconds: *p.DecreaseStabilitySeconds, MaxIncreasePercent: *p.MaxIncreasePercent}, true
}

func (h *UpstreamGovernanceHandler) PreviewPricingPolicy(c *gin.Context) {
	id, ok := governanceID(c, "id")
	if !ok {
		return
	}
	groupID, ok := governanceID(c, "group_id")
	if !ok {
		return
	}
	var input struct {
		Policy *governancePricingDraftInput `json:"policy"`
	}
	if !governanceBody(c, &input) {
		return
	}
	draft, ok := input.Policy.draft(c)
	if !ok {
		return
	}
	value, err := h.svc.PreviewPricingPolicy(c.Request.Context(), id, groupID, draft)
	if !governanceError(c, err) {
		response.Success(c, value)
	}
}

func (h *UpstreamGovernanceHandler) ConfigurePricingPolicy(c *gin.Context) {
	id, ok := governanceID(c, "id")
	if !ok {
		return
	}
	groupID, ok := governanceID(c, "group_id")
	if !ok {
		return
	}
	var input struct {
		Policy      *governancePricingDraftInput `json:"policy"`
		Fingerprint string                       `json:"fingerprint"`
	}
	if !governanceBody(c, &input) {
		return
	}
	draft, ok := input.Policy.draft(c)
	if !ok {
		return
	}
	value, err := h.svc.ConfigurePricingPolicy(c.Request.Context(), id, groupID, draft, input.Fingerprint)
	if !governanceError(c, err) {
		response.Success(c, value)
	}
}

func (h *UpstreamGovernanceHandler) ConfigurePricingNotifications(c *gin.Context) {
	id, ok := governanceID(c, "id")
	if !ok {
		return
	}
	var input struct {
		Version       int64                          `json:"version"`
		Notifications *gov.PricingNotificationPolicy `json:"notifications"`
	}
	if !governanceBody(c, &input) {
		return
	}
	if input.Version <= 0 || input.Notifications == nil {
		governanceError(c, gov.ErrInvalid)
		return
	}
	value, err := h.svc.ConfigurePricingNotifications(c.Request.Context(), id, input.Version, *input.Notifications)
	if !governanceError(c, err) {
		response.Success(c, value)
	}
}
