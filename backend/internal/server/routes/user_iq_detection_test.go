package routes

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/handler"
	servermiddleware "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	gov "github.com/Wei-Shaw/sub2api/internal/upstreamgovernance"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestUserIQDetectionRouteIsAuthenticatedAndRegistered(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	handlers := &handler.Handlers{User: handler.NewUserHandler(nil, nil, nil, nil, nil, nil)}
	passThroughAuth := servermiddleware.JWTAuthMiddleware(func(c *gin.Context) { c.Next() })
	passThroughAudit := servermiddleware.AuditLogMiddleware(func(c *gin.Context) { c.Next() })
	var limiter *servermiddleware.PanelRateLimiter
	RegisterUserRoutes(router.Group("/api/v1"), handlers, passThroughAuth, passThroughAudit, nil, limiter)

	found := false
	for _, route := range router.Routes() {
		if route.Method == http.MethodGet && route.Path == "/api/v1/user/iq-detection" {
			found = true
			break
		}
	}
	require.True(t, found)

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/api/v1/user/iq-detection", nil)
	router.ServeHTTP(recorder, request)
	require.Equal(t, http.StatusUnauthorized, recorder.Code)
}

func TestUserIQDetectionRouteReturnsSafeEmptyProjectionForAuthenticatedUser(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	handlers := &handler.Handlers{User: handler.NewUserHandler(nil, nil, nil, nil, nil, nil)}
	passThroughAuth := servermiddleware.JWTAuthMiddleware(func(c *gin.Context) {
		c.Set(string(servermiddleware.ContextKeyUser), servermiddleware.AuthSubject{UserID: 7})
		c.Next()
	})
	passThroughAudit := servermiddleware.AuditLogMiddleware(func(c *gin.Context) { c.Next() })
	var limiter *servermiddleware.PanelRateLimiter
	RegisterUserRoutes(router.Group("/api/v1"), handlers, passThroughAuth, passThroughAudit, nil, limiter)

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/api/v1/user/iq-detection", nil)
	router.ServeHTTP(recorder, request)
	require.Equal(t, http.StatusOK, recorder.Code)
	var envelope struct {
		Code int             `json:"code"`
		Data gov.IQDashboard `json:"data"`
	}
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &envelope))
	require.Zero(t, envelope.Code)
	require.Equal(t, 21, envelope.Data.StandardAnswer)
	require.Equal(t, 24, envelope.Data.WindowHours)
	require.NotNil(t, envelope.Data.CandyResults)
	require.NotNil(t, envelope.Data.PelicanWorks)
	require.NotNil(t, envelope.Data.Timeline)
	require.NotContains(t, recorder.Body.String(), "request_body")
}

func TestUserIQDetectionRouteRejectsInvalidQueryParameters(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, query := range []string{"?hours=", "?hours=1&hours=2", "?hours=abc", "?hours=0", "?hours=-1", "?hours=169", "?hours=1.5", "?hours=999999999999999999999999999999999999", "?limit=", "?limit=1&limit=2", "?limit=abc", "?limit=0", "?limit=-1", "?limit=21"} {
		router := gin.New()
		handlers := &handler.Handlers{User: handler.NewUserHandler(nil, nil, nil, nil, nil, nil)}
		passThroughAuth := servermiddleware.JWTAuthMiddleware(func(c *gin.Context) {
			c.Set(string(servermiddleware.ContextKeyUser), servermiddleware.AuthSubject{UserID: 7})
			c.Next()
		})
		passThroughAudit := servermiddleware.AuditLogMiddleware(func(c *gin.Context) { c.Next() })
		RegisterUserRoutes(router.Group("/api/v1"), handlers, passThroughAuth, passThroughAudit, nil, nil)

		recorder := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodGet, "/api/v1/user/iq-detection"+query, nil)
		router.ServeHTTP(recorder, request)
		require.Equal(t, http.StatusBadRequest, recorder.Code, query)
	}
}
