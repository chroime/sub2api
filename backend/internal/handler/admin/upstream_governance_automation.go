package admin

import (
	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	gov "github.com/Wei-Shaw/sub2api/internal/upstreamgovernance"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

func (h *UpstreamGovernanceHandler) Automation(c *gin.Context) {
	id, ok := governanceID(c, "id")
	if !ok {
		return
	}
	value, err := h.svc.GetAutomation(c.Request.Context(), id)
	if !governanceError(c, err) {
		response.Success(c, value)
	}
}

func (h *UpstreamGovernanceHandler) ConfigureAutomation(c *gin.Context) {
	id, ok := governanceID(c, "id")
	if !ok {
		return
	}
	var input gov.AutomationConfig
	if !governanceBody(c, &input) {
		return
	}
	value, err := h.svc.ConfigureAutomation(c.Request.Context(), id, input)
	if !governanceError(c, err) {
		response.Success(c, value)
	}
}

func (h *UpstreamGovernanceHandler) Reconciliation(c *gin.Context) {
	id, ok := governanceID(c, "id")
	if !ok {
		return
	}
	value, err := h.svc.Reconciliation(c.Request.Context(), id)
	if !governanceError(c, err) {
		response.Success(c, value)
	}
}

func (h *UpstreamGovernanceHandler) PreviewReconciliation(c *gin.Context) {
	id, ok := governanceID(c, "id")
	if !ok {
		return
	}
	var input struct{}
	if !governanceBody(c, &input) {
		return
	}
	value, err := h.svc.PreviewReconciliation(c.Request.Context(), id)
	if !governanceError(c, err) {
		response.Success(c, value)
	}
}

func (h *UpstreamGovernanceHandler) ApplyReconciliation(c *gin.Context) {
	id, ok := governanceID(c, "id")
	if !ok {
		return
	}
	previewID := c.Param("preview_id")
	if _, err := uuid.Parse(previewID); err != nil {
		governanceError(c, gov.ErrInvalid)
		return
	}
	var input struct {
		BindingIDs []int64 `json:"binding_ids"`
	}
	if !governanceBody(c, &input) {
		return
	}
	if len(input.BindingIDs) == 0 || len(input.BindingIDs) > 100 {
		governanceError(c, gov.ErrInvalid)
		return
	}
	seen := make(map[int64]bool, len(input.BindingIDs))
	for _, bindingID := range input.BindingIDs {
		if bindingID <= 0 || seen[bindingID] {
			governanceError(c, gov.ErrInvalid)
			return
		}
		seen[bindingID] = true
	}
	value, err := h.svc.ApplyReconciliation(c.Request.Context(), id, previewID, input.BindingIDs)
	if !governanceError(c, err) {
		response.Success(c, value)
	}
}
