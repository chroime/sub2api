package routes

import (
	"github.com/Wei-Shaw/sub2api/internal/handler"
	"github.com/gin-gonic/gin"
)

func registerAccountQualityRoutes(admin *gin.RouterGroup, h *handler.Handlers) {
	if h.Admin.AccountQuality == nil {
		return
	}
	q := admin.Group("/account-quality")
	v := h.Admin.AccountQuality
	q.GET("/overview", v.Overview)
	q.GET("/policies", v.Policies)
	q.POST("/policies", v.SavePolicy)
	q.PUT("/policies/:id", v.SavePolicy)
	q.DELETE("/policies/:id", v.DeletePolicy)
	q.POST("/bindings", v.Bind)
	q.POST("/bindings/:id/run", v.RunNow)
	q.POST("/bindings/:id/control", v.Control)
	q.POST("/accounts/summaries", v.Summaries)
	q.GET("/accounts/:id", v.Detail)
	q.GET("/runs", v.Runs)
	q.GET("/runs/:id", v.Run)
	q.POST("/runs/:id/cancel", v.Cancel)
	q.GET("/settings", v.Settings)
	q.PUT("/settings", v.SaveSettings)
}
