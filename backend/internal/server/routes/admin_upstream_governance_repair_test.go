package routes

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/handler"
	"github.com/Wei-Shaw/sub2api/internal/handler/admin"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestGovernanceRepairRoutesRequireAdministrator(t *testing.T) {
	gin.SetMode(gin.TestMode)
	cfg := &config.Config{JWT: config.JWTConfig{Secret: "repair-local-test-secret", ExpireHour: 1}}
	auth := service.NewAuthService(nil, nil, nil, nil, cfg, nil, nil, nil, nil, nil, nil, nil, nil)
	user := &service.User{ID: 7, Email: "fixture@example.test", Role: service.RoleUser, Status: service.StatusActive}
	users := service.NewUserService(governanceModelUserReader{user: user}, nil, nil, nil)
	router := gin.New()
	group := router.Group("/api/v1/admin", gin.HandlerFunc(middleware.NewAdminAuthMiddleware(auth, users, nil, nil)))
	registerUpstreamGovernanceRoutes(group, &handler.Handlers{Admin: &handler.AdminHandlers{UpstreamGovernance: admin.NewUpstreamGovernanceHandler(nil)}})
	token, err := auth.GenerateToken(t.Context(), user)
	require.NoError(t, err)
	paths := map[string]bool{
		"POST /api/v1/admin/upstream-governance/sites/:id/keys/:key_id/repairs":                    false,
		"GET /api/v1/admin/upstream-governance/sites/:id/keys/:key_id/repairs":                     false,
		"POST /api/v1/admin/upstream-governance/sites/:id/keys/:key_id/repairs/:repair_id/confirm": false,
		"POST /api/v1/admin/upstream-governance/sites/:id/keys/:key_id/repairs/:repair_id/abandon": false,
	}
	for _, route := range router.Routes() {
		key := route.Method + " " + route.Path
		if _, expected := paths[key]; !expected {
			continue
		}
		paths[key] = true
		path := strings.NewReplacer(":id", "1", ":key_id", "4", ":repair_id", "operation-1").Replace(route.Path)
		for _, authorized := range []bool{false, true} {
			request := httptest.NewRequest(route.Method, path, strings.NewReader(`{}`))
			want := http.StatusUnauthorized
			if authorized {
				request.Header.Set("Authorization", "Bearer "+token)
				want = http.StatusForbidden
			}
			response := httptest.NewRecorder()
			router.ServeHTTP(response, request)
			require.Equal(t, want, response.Code, key)
		}
	}
	for route, found := range paths {
		require.True(t, found, route)
	}
}
