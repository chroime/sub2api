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
	require.Len(t, routes, 64)
	registered := map[string]bool{}
	for _, route := range routes {
		require.Contains(t, route.Path, "/api/v1/admin/upstream-governance/")
		registered[route.Method+" "+route.Path] = true
	}
	for _, expected := range []string{
		"GET /api/v1/admin/upstream-governance/import-templates",
		"PUT /api/v1/admin/upstream-governance/import-templates",
		"GET /api/v1/admin/upstream-governance/workbench",
		"GET /api/v1/admin/upstream-governance/sites/:id/timeline",
		"GET /api/v1/admin/upstream-governance/sites/:id/auth-status",
		"GET /api/v1/admin/upstream-governance/sites/:id/readiness",
		"POST /api/v1/admin/upstream-governance/sites/:id/browser-auth",
		"GET /api/v1/admin/upstream-governance/sites/:id/browser-auth/:job_id",
		"POST /api/v1/admin/upstream-governance/sites/:id/browser-auth/:job_id/actions",
		"POST /api/v1/admin/upstream-governance/sites/:id/browser-auth/:job_id/complete",
		"DELETE /api/v1/admin/upstream-governance/sites/:id/browser-auth/:job_id",
		"GET /api/v1/admin/upstream-governance/sites/:id/model-policies",
		"POST /api/v1/admin/upstream-governance/sites/:id/model-policies",
		"DELETE /api/v1/admin/upstream-governance/sites/:id/model-policies/:policy_id",
		"POST /api/v1/admin/upstream-governance/sites/:id/model-batches",
		"POST /api/v1/admin/upstream-governance/sites/:id/model-batches/:batch_id/cancel",
		"GET /api/v1/admin/upstream-governance/sites/:id/model-runs",
		"GET /api/v1/admin/upstream-governance/sites/:id/model-stats",
		"GET /api/v1/admin/upstream-governance/sites/:id/model-runs/:run_id",
		"PUT /api/v1/admin/upstream-governance/sites/:id/model-runs/:run_id/review",
		"GET /api/v1/admin/upstream-governance/sites/:id/login-credentials",
		"PUT /api/v1/admin/upstream-governance/sites/:id/balance-monitor",
		"GET /api/v1/admin/upstream-governance/sites/:id/balance-health",
		"GET /api/v1/admin/upstream-governance/sites/:id/recharge-plan",
		"PUT /api/v1/admin/upstream-governance/sites/:id/recharge-plan",
		"POST /api/v1/admin/upstream-governance/sites/:id/recharge-plan/evaluate",
		"GET /api/v1/admin/upstream-governance/sites/:id/automation",
		"PUT /api/v1/admin/upstream-governance/sites/:id/automation",
		"GET /api/v1/admin/upstream-governance/sites/:id/observation-policy",
		"PUT /api/v1/admin/upstream-governance/sites/:id/observation-policy",
		"GET /api/v1/admin/upstream-governance/sites/:id/pricing-policies",
		"PUT /api/v1/admin/upstream-governance/sites/:id/pricing-policies",
		"POST /api/v1/admin/upstream-governance/sites/:id/pricing-policies/:group_id/preview",
		"PUT /api/v1/admin/upstream-governance/sites/:id/pricing-policies/:group_id",
		"PUT /api/v1/admin/upstream-governance/sites/:id/pricing-notifications",
		"GET /api/v1/admin/upstream-governance/sites/:id/reconciliation",
		"POST /api/v1/admin/upstream-governance/sites/:id/reconcile-preview",
		"POST /api/v1/admin/upstream-governance/sites/:id/reconcile-previews/:preview_id/apply",
		"GET /api/v1/admin/upstream-governance/model-templates",
		"PUT /api/v1/admin/upstream-governance/model-templates",
		"POST /api/v1/admin/upstream-governance/sites/detect",
		"GET /api/v1/admin/upstream-governance/sites/:id/keys",
		"POST /api/v1/admin/upstream-governance/sites/:id/keys/audit",
		"POST /api/v1/admin/upstream-governance/sites/:id/keys/:key_id/repairs",
		"GET /api/v1/admin/upstream-governance/sites/:id/keys/:key_id/repairs",
		"POST /api/v1/admin/upstream-governance/sites/:id/keys/:key_id/repairs/:repair_id/confirm",
		"POST /api/v1/admin/upstream-governance/sites/:id/keys/:key_id/repairs/:repair_id/abandon",
		"POST /api/v1/admin/upstream-governance/sites/:id/keys",
		"POST /api/v1/admin/upstream-governance/sites/:id/keys/:key_id/reveal",
	} {
		require.True(t, registered[expected], expected)
	}
}
