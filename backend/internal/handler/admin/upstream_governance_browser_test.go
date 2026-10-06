package admin

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	gov "github.com/Wei-Shaw/sub2api/internal/upstreamgovernance"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestGovernanceBrowserHTTPRejectsInjectedTargetsAndUnboundedControls(t *testing.T) {
	gin.SetMode(gin.TestMode)
	svc := gov.NewService(nil, nil, nil, nil, false)
	svc.SetBrowserAuthorizer(gov.NewBrowserAuthorizer(svc, nil, nil))
	h := NewUpstreamGovernanceHandler(svc)
	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: 7})
		c.Next()
	})
	r.POST("/sites/:id/browser-auth", h.StartBrowserAuthorization)
	r.POST("/sites/:id/browser-auth/:job_id/actions", h.BrowserAuthorizationAction)
	r.POST("/sites/:id/browser-auth/:job_id/complete", h.CompleteBrowserAuthorization)
	r.DELETE("/sites/:id/browser-auth/:job_id", h.CancelBrowserAuthorization)
	const job = "cb104bbf-5726-4b4c-a283-3a6c94cb82db"
	for _, tc := range []struct{ method, path, body string }{
		{"POST", "/sites/0/browser-auth", `{}`},
		{"POST", "/sites/1/browser-auth", `{"base_url":"http://127.0.0.1"}`},
		{"POST", "/sites/1/browser-auth", `{"expected_site_version":1.5}`},
		{"POST", "/sites/1/browser-auth", `{"expected_site_version":0}`},
		{"POST", "/sites/1/browser-auth", `{} {}`},
		{"POST", "/sites/1/browser-auth/" + job + "/actions", `{"type":"javascript","text":"alert(1)"}`},
		{"POST", "/sites/1/browser-auth/" + job + "/actions", `{"type":"pointer_down","x":-1}`},
		{"POST", "/sites/1/browser-auth/" + job + "/actions", `{"type":"pointer_down","x":1024}`},
		{"POST", "/sites/1/browser-auth/" + job + "/actions", `{"type":"key","key":"Control+O"}`},
		{"POST", "/sites/1/browser-auth/" + job + "/actions", `{"type":"text","text":"` + strings.Repeat("x", 4097) + `"}`},
		{"POST", "/sites/1/browser-auth/" + job + "/complete", `{"session_token":"injected-session"}`},
		{"POST", "/sites/1/browser-auth/not-a-job/actions", `{}`},
		{"DELETE", "/sites/1/browser-auth/not-a-job", ``},
	} {
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest(tc.method, tc.path, strings.NewReader(tc.body)))
		require.Equal(t, http.StatusBadRequest, w.Code, tc.path+": "+w.Body.String())
	}
}

func TestGovernanceBrowserHTTPRequiresActorIdentity(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	h := NewUpstreamGovernanceHandler(nil)
	r.POST("/sites/:id/browser-auth", h.StartBrowserAuthorization)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest("POST", "/sites/1/browser-auth", strings.NewReader(`{}`)))
	require.Equal(t, http.StatusUnauthorized, w.Code)
}
