package admin

import (
	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

// GetStreamingACKSettings handles GET /api/v1/admin/settings/streaming-ack.
func (h *SettingHandler) GetStreamingACKSettings(c *gin.Context) {
	settings, err := h.settingService.GetStreamingACKSettings(c.Request.Context())
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, settings)
}

// UpdateStreamingACKSettings handles PUT /api/v1/admin/settings/streaming-ack.
func (h *SettingHandler) UpdateStreamingACKSettings(c *gin.Context) {
	var req struct {
		Enabled *bool `json:"enabled" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request: "+err.Error())
		return
	}
	settings := &service.StreamingACKSettings{Enabled: *req.Enabled}
	if err := h.settingService.SetStreamingACKSettings(c.Request.Context(), settings); err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, settings)
}
