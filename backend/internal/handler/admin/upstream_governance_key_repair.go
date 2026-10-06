package admin

import (
	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	gov "github.com/Wei-Shaw/sub2api/internal/upstreamgovernance"
	"github.com/gin-gonic/gin"
)

func (h *UpstreamGovernanceHandler) PrepareKeyRepair(c *gin.Context) {
	governanceNoStore(c)
	siteID, ok := governanceID(c, "id")
	if !ok {
		return
	}
	keyID, ok := governanceID(c, "key_id")
	if !ok {
		return
	}
	var input gov.PrepareKeyRepairInput
	if !governanceBody(c, &input) {
		return
	}
	repair, err := h.svc.PrepareKeyRepair(c.Request.Context(), siteID, keyID, input)
	if !governanceError(c, err) {
		response.Success(c, repair)
	}
}

func (h *UpstreamGovernanceHandler) LatestKeyRepair(c *gin.Context) {
	governanceNoStore(c)
	siteID, ok := governanceID(c, "id")
	if !ok {
		return
	}
	keyID, ok := governanceID(c, "key_id")
	if !ok {
		return
	}
	repair, err := h.svc.LatestKeyRepair(c.Request.Context(), siteID, keyID)
	if !governanceError(c, err) {
		response.Success(c, repair)
	}
}

func (h *UpstreamGovernanceHandler) ConfirmKeyRepair(c *gin.Context) {
	governanceNoStore(c)
	siteID, ok := governanceID(c, "id")
	if !ok {
		return
	}
	keyID, ok := governanceID(c, "key_id")
	if !ok {
		return
	}
	repairID := c.Param("repair_id")
	if repairID == "" {
		governanceError(c, gov.ErrInvalid)
		return
	}
	var input *struct{}
	if !governanceBody(c, &input) {
		return
	}
	if input == nil {
		governanceError(c, gov.ErrInvalid)
		return
	}
	repair, err := h.svc.ConfirmKeyRepair(c.Request.Context(), siteID, keyID, repairID)
	if !governanceError(c, err) {
		response.Success(c, repair)
	}
}

func (h *UpstreamGovernanceHandler) AbandonKeyRepair(c *gin.Context) {
	governanceNoStore(c)
	siteID, ok := governanceID(c, "id")
	if !ok {
		return
	}
	keyID, ok := governanceID(c, "key_id")
	if !ok {
		return
	}
	repairID := c.Param("repair_id")
	if repairID == "" {
		governanceError(c, gov.ErrInvalid)
		return
	}
	var input gov.AbandonKeyRepairInput
	if !governanceBody(c, &input) {
		return
	}
	repair, err := h.svc.AbandonKeyRepair(c.Request.Context(), siteID, keyID, repairID, input)
	if !governanceError(c, err) {
		response.Success(c, repair)
	}
}
