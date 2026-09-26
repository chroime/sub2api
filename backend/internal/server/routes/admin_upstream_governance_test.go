package routes

import (
	"github.com/Wei-Shaw/sub2api/internal/handler"
	"github.com/Wei-Shaw/sub2api/internal/handler/admin"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestGovernanceRoutesRegisteredUnderAdmin(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	h := &handler.Handlers{Admin: &handler.AdminHandlers{UpstreamGovernance: admin.NewUpstreamGovernanceHandler(nil)}}
	registerUpstreamGovernanceRoutes(r.Group("/api/v1/admin"), h)
	routes := r.Routes()
	require.Len(t, routes, 15)
	for _, route := range routes {
		require.Contains(t, route.Path, "/api/v1/admin/upstream-governance/sites")
	}
}
