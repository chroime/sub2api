package routes

import (
	"net/http"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/handler"
	servermiddleware "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestUserTokenRankingRouteIsRegisteredUnderAuthenticatedUserRoutes(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	handlers := &handler.Handlers{
		User:  handler.NewUserHandler(nil, nil, nil, nil, nil, nil),
		Usage: handler.NewUsageHandler(nil, nil, nil, nil),
	}
	passThroughAuth := servermiddleware.JWTAuthMiddleware(func(c *gin.Context) { c.Next() })
	passThroughAudit := servermiddleware.AuditLogMiddleware(func(c *gin.Context) { c.Next() })
	RegisterUserRoutes(router.Group("/api/v1"), handlers, passThroughAuth, passThroughAudit, nil, nil)

	found := false
	for _, route := range router.Routes() {
		if route.Method == http.MethodGet && route.Path == "/api/v1/user/token-ranking" {
			found = true
			break
		}
	}
	require.True(t, found)
}
