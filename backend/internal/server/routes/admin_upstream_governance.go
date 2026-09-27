package routes

import (
	"github.com/Wei-Shaw/sub2api/internal/handler"
	"github.com/gin-gonic/gin"
)

func registerUpstreamGovernanceRoutes(admin *gin.RouterGroup, h *handler.Handlers) {
	if h.Admin.UpstreamGovernance == nil {
		return
	}
	api := h.Admin.UpstreamGovernance
	g := admin.Group("/upstream-governance")
	g.GET("/model-templates", api.ModelTemplates)
	g.PUT("/model-templates", api.SaveModelTemplates)
	g.GET("/sites", api.List)
	g.POST("/sites", api.Create)
	g.POST("/sites/detect", api.Detect)
	g.PUT("/sites/:id", api.Update)
	g.GET("/sites/:id/login-credentials", api.LoginCredentials)
	g.PUT("/sites/:id/balance-monitor", api.ConfigureBalanceMonitor)
	g.DELETE("/sites/:id", api.Delete)
	g.POST("/sites/:id/connect", api.Connect)
	g.POST("/sites/:id/sync", api.Sync)
	g.GET("/sites/:id/catalog", api.Catalog)
	g.GET("/sites/:id/bindings", api.Bindings)
	g.GET("/sites/:id/keys", api.Keys)
	g.POST("/sites/:id/keys", api.CreateKeys)
	g.POST("/sites/:id/keys/:key_id/reveal", api.RevealKey)
	g.POST("/sites/:id/previews", api.Preview)
	g.POST("/sites/:id/previews/:preview_id/apply", api.Apply)
	g.GET("/sites/:id/events", api.Events)
	g.POST("/sites/:id/events/:event_id/ack", api.Acknowledge)
	g.GET("/sites/:id/checks", api.Checks)
	g.POST("/sites/:id/bindings/:binding_id/check", api.Probe)
	g.PUT("/sites/:id/bindings/:binding_id/monitor", api.Monitor)
}
