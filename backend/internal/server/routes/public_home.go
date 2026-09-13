package routes

import (
	"github.com/Wei-Shaw/sub2api/internal/handler"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/gin-gonic/gin"
)

// Homepage summaries are always anonymous; console feature gates and user grants
// must not change this public projection or expose exclusive groups.
func RegisterPublicHomeRoutes(v1 *gin.RouterGroup, h *handler.Handlers, limiter *middleware.PanelRateLimiter) {
	public := v1.Group("/public/home")
	public.Use(limiter.PublicIP())
	public.GET("/channels", h.AvailableChannel.ListPublic)
	public.GET("/channel-monitors", h.ChannelMonitor.ListPublic)
	public.GET("/pricing", h.ModelPlaza.GetPublic)
}
