package admin

import (
	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

// ListSortOrder GET /api/v1/admin/channel-monitors/sort-order.
// Always includes disabled monitors and ignores list pagination and filters.
func (h *ChannelMonitorHandler) ListSortOrder(c *gin.Context) {
	items, err := h.monitorService.ListSortOrder(c.Request.Context())
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, items)
}

// UpdateSortOrder PUT /api/v1/admin/channel-monitors/sort-order.
func (h *ChannelMonitorHandler) UpdateSortOrder(c *gin.Context) {
	var req struct {
		Updates []service.ChannelMonitorSortOrderUpdate `json:"updates" binding:"required,min=1"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request: "+err.Error())
		return
	}
	if err := h.monitorService.UpdateSortOrders(c.Request.Context(), req.Updates); err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, gin.H{"message": "Sort order updated successfully"})
}
