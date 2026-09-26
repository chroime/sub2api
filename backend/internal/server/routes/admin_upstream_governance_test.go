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
	require.Len(t, routes, 22)
	registered := map[string]bool{}
	for _, route := range routes {
		require.Contains(t, route.Path, "/api/v1/admin/upstream-governance/")
		registered[route.Method+" "+route.Path] = true
	}
	for _, expected := range []string{
		"PUT /api/v1/admin/upstream-governance/sites/:id/balance-monitor",
		"GET /api/v1/admin/upstream-governance/model-templates",
		"PUT /api/v1/admin/upstream-governance/model-templates",
		"POST /api/v1/admin/upstream-governance/sites/detect",
		"GET /api/v1/admin/upstream-governance/sites/:id/keys",
		"POST /api/v1/admin/upstream-governance/sites/:id/keys",
		"POST /api/v1/admin/upstream-governance/sites/:id/keys/:key_id/reveal",
	} {
		require.True(t, registered[expected], expected)
	}
}
