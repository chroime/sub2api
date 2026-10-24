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

func TestUserBulkEmailRoutesRequireAdministrator(t *testing.T) {
	gin.SetMode(gin.TestMode)
	cfg := &config.Config{JWT: config.JWTConfig{Secret: "bulk-email-local-test-secret", ExpireHour: 1}}
	auth := service.NewAuthService(nil, nil, nil, nil, cfg, nil, nil, nil, nil, nil, nil, nil, nil)
	user := &service.User{ID: 7, Email: "fixture@example.test", Role: service.RoleUser, Status: service.StatusActive}
	users := service.NewUserService(governanceModelUserReader{user: user}, nil, nil, nil)
	r := gin.New()
	group := r.Group("/api/v1/admin", gin.HandlerFunc(middleware.NewAdminAuthMiddleware(auth, users, nil, nil)))
	registerUserManagementRoutes(group, &handler.Handlers{Admin: &handler.AdminHandlers{User: admin.NewUserHandler(nil, nil, nil, nil, nil, nil, nil), UserAttribute: admin.NewUserAttributeHandler(nil)}})
	token, err := auth.GenerateToken(t.Context(), user)
	require.NoError(t, err)
	registered := map[string]bool{}
	for _, route := range r.Routes() {
		registered[route.Method+" "+route.Path] = true
	}
	for _, path := range []string{"/api/v1/admin/users/bulk-email", "/api/v1/admin/users/bulk-email/preview"} {
		require.True(t, registered["POST "+path])
		for _, authenticated := range []bool{false, true} {
			req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(`{"all":true,"event":"system.health_snapshot"}`))
			want := http.StatusUnauthorized
			if authenticated {
				req.Header.Set("Authorization", "Bearer "+token)
				want = http.StatusForbidden
			}
			response := httptest.NewRecorder()
			r.ServeHTTP(response, req)
			require.Equal(t, want, response.Code)
		}
	}
}
