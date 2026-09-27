package middleware

import (
	"encoding/json"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestGovernanceConnectAuditOmitsAllCredentialBody(t *testing.T) {
	gin.SetMode(gin.TestMode)
	repo := &auditCaptureRepository{}
	svc := service.NewAuditLogService(repo, nil)
	svc.Start()
	r := gin.New()
	r.Use(gin.HandlerFunc(NewAuditLogMiddleware(svc)))
	r.POST("/api/v1/admin/upstream-governance/sites/:id/connect", func(c *gin.Context) {
		var in map[string]any
		require.NoError(t, c.ShouldBindJSON(&in))
		require.Equal(t, "password-canary", in["password"])
		c.JSON(200, gin.H{"ok": true})
	})
	req := httptest.NewRequest("POST", "/api/v1/admin/upstream-governance/sites/12/connect", strings.NewReader(`{"password":"password-canary","otp":"otp-canary","session_token":"session-canary"}`))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(httptest.NewRecorder(), req)
	svc.Stop()
	repo.mu.Lock()
	defer repo.mu.Unlock()
	require.Len(t, repo.logs, 1)
	raw, e := json.Marshal(repo.logs[0])
	require.NoError(t, e)
	require.NotContains(t, string(raw), "canary")
	require.Equal(t, "<credential-bearing body omitted>", repo.logs[0].RequestBody)
	require.Equal(t, 200, repo.logs[0].StatusCode)
}

func TestGovernanceLoginCredentialsAuditOmitsEditBodyAndRecordsReads(t *testing.T) {
	gin.SetMode(gin.TestMode)
	repo := &auditCaptureRepository{}
	svc := service.NewAuditLogService(repo, nil)
	svc.Start()
	router := gin.New()
	router.Use(gin.HandlerFunc(NewAuditLogMiddleware(svc)))
	router.PUT("/api/v1/admin/upstream-governance/sites/:id", func(c *gin.Context) {
		var input struct {
			Login struct{ Username, Password string } `json:"login_credentials"`
		}
		require.NoError(t, c.ShouldBindJSON(&input))
		require.Equal(t, "password-canary", input.Login.Password)
		c.JSON(200, gin.H{"ok": true})
	})
	router.GET("/api/v1/admin/upstream-governance/sites/:id/login-credentials", func(c *gin.Context) {
		c.JSON(200, gin.H{"username": "username-canary", "password": "password-canary"})
	})
	request := httptest.NewRequest("PUT", "/api/v1/admin/upstream-governance/sites/1", strings.NewReader(`{"login_credentials":{"username":"username-canary","password":"password-canary"}}`))
	request.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(httptest.NewRecorder(), request)
	router.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/api/v1/admin/upstream-governance/sites/1/login-credentials", nil))
	svc.Stop()
	repo.mu.Lock()
	defer repo.mu.Unlock()
	require.Len(t, repo.logs, 2)
	for _, entry := range repo.logs {
		raw, err := json.Marshal(entry)
		require.NoError(t, err)
		require.NotContains(t, string(raw), "canary")
		if entry.Method == "PUT" {
			require.Equal(t, "<credential-bearing body omitted>", entry.RequestBody)
		} else {
			require.Equal(t, "admin.upstream_governance.login_credentials.read", entry.Action)
		}
	}
}
