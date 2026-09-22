package admin

import (
	"strconv"

	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

func (h *SettingHandler) SetBalancePrechargeReconciliationService(s *service.BalancePrechargeReconciliationService) {
	h.balancePrechargeReconciliation = s
}

func (h *SettingHandler) ListBalancePrechargeReviews(c *gin.Context) {
	limit, err := strconv.Atoi(c.DefaultQuery("limit", "20"))
	if err != nil {
		response.ErrorFrom(c, service.ErrBalancePrechargeReviewInvalid)
		return
	}
	offset, err := strconv.Atoi(c.DefaultQuery("offset", "0"))
	if err != nil {
		response.ErrorFrom(c, service.ErrBalancePrechargeReviewInvalid)
		return
	}
	items, total, err := h.balancePrechargeReconciliation.List(c.Request.Context(), c.Query("status"), limit, offset)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, gin.H{"items": items, "total": total})
}

func (h *SettingHandler) ResolveBalancePrechargeReview(c *gin.Context) {
	actorID := getAdminIDFromContext(c)
	if actorID <= 0 {
		response.Unauthorized(c, "Authenticated administrator required")
		return
	}
	var req struct {
		Action     string   `json:"action" binding:"required"`
		ActualCost *float64 `json:"actual_cost" binding:"required"`
		Note       string   `json:"note" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		response.ErrorFrom(c, service.ErrBalancePrechargeReviewInvalid)
		return
	}
	result, err := h.balancePrechargeReconciliation.Resolve(c.Request.Context(), &service.BalancePrechargeResolutionCommand{
		PrechargeID: c.Param("id"), ActorID: actorID, Action: req.Action, ActualCost: *req.ActualCost, Note: req.Note,
	})
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, result)
}
