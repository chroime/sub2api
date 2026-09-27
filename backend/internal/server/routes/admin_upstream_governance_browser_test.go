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

func TestGovernanceBrowserRoutesDenyPublicAndNonAdminUsers(t *testing.T) {
	gin.SetMode(gin.TestMode)
	cfg := &config.Config{JWT: config.JWTConfig{Secret: "browser-auth-local-fixture-secret", ExpireHour: 1}}
	auth := service.NewAuthService(nil, nil, nil, nil, cfg, nil, nil, nil, nil, nil, nil, nil, nil)
	user := &service.User{ID: 7, Email: "fixture@example.test", Role: service.RoleUser, Status: service.StatusActive}
	users := service.NewUserService(governanceModelUserReader{user: user}, nil, nil, nil)
	r := gin.New()
	g := r.Group("/api/v1/admin", gin.HandlerFunc(middleware.NewAdminAuthMiddleware(auth, users, nil, nil)))
	registerUpstreamGovernanceRoutes(g, &handler.Handlers{Admin: &handler.AdminHandlers{UpstreamGovernance: admin.NewUpstreamGovernanceHandler(nil)}})
	token, err := auth.GenerateToken(t.Context(), user)
	require.NoError(t, err)
	count := 0
	for _, route := range r.Routes() {
		if !strings.Contains(route.Path, "/browser-auth") && !strings.HasSuffix(route.Path, "/auth-status") {
			continue
		}
		count++
		path := strings.NewReplacer(":job_id", "cb104bbf-5726-4b4c-a283-3a6c94cb82db", ":id", "1").Replace(route.Path)
		for _, authenticated := range []bool{false, true} {
			req := httptest.NewRequest(route.Method, path, strings.NewReader(`{}`))
			want := http.StatusUnauthorized
			if authenticated {
				req.Header.Set("Authorization", "Bearer "+token)
				want = http.StatusForbidden
			}
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)
			require.Equal(t, want, w.Code, route.Method+" "+path)
		}
	}
	require.Equal(t, 6, count)
}
