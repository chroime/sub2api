package admin

import (
	"strconv"

	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

func (h *SettingHandler) GetBalancePrechargeSettings(c *gin.Context) {
	settings, err := h.settingService.GetBalancePrechargeSettings(c.Request.Context())
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, settings)
}

func (h *SettingHandler) UpdateBalancePrechargeSettings(c *gin.Context) {
	var req struct {
		Enabled   *bool    `json:"enabled" binding:"required"`
		Threshold *float64 `json:"threshold" binding:"required"`
		Amount    *float64 `json:"amount" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request: "+err.Error())
		return
	}
	settings := service.BalancePrechargeSettings{Enabled: *req.Enabled, Threshold: *req.Threshold, Amount: *req.Amount}
	if err := h.settingService.SetBalancePrechargeSettings(c.Request.Context(), settings); err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, settings)
}

func balancePrechargeGroupID(c *gin.Context) (int64, bool) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id <= 0 {
		response.BadRequest(c, "Invalid group ID")
		return 0, false
	}
	return id, true
}

func (h *SettingHandler) GetGroupBalancePrechargeSettings(c *gin.Context) {
	id, ok := balancePrechargeGroupID(c)
	if !ok {
		return
	}
	h.respondGroupBalancePrechargeSettings(c, id)
}

func (h *SettingHandler) respondGroupBalancePrechargeSettings(c *gin.Context, groupID int64) {
	ctx := c.Request.Context()
	settings, err := h.settingService.GetGroupBalancePrechargeSettings(ctx, groupID)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	global, err := h.settingService.GetBalancePrechargeSettings(ctx)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	effective, err := h.settingService.GetEffectiveBalancePrechargeSettings(ctx, groupID)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, gin.H{"group_id": groupID, "settings": settings, "global": global, "effective": effective})
}

func (h *SettingHandler) UpdateGroupBalancePrechargeSettings(c *gin.Context) {
	id, ok := balancePrechargeGroupID(c)
	if !ok {
		return
	}
	var req struct {
		Mode      string   `json:"mode" binding:"required"`
		Threshold *float64 `json:"threshold" binding:"required"`
		Amount    *float64 `json:"amount" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request: "+err.Error())
		return
	}
	settings := service.GroupBalancePrechargeSettings{Mode: req.Mode, Threshold: *req.Threshold, Amount: *req.Amount}
	if err := h.settingService.SetGroupBalancePrechargeSettings(c.Request.Context(), id, settings); err != nil {
		response.ErrorFrom(c, err)
		return
	}
	h.respondGroupBalancePrechargeSettings(c, id)
}
