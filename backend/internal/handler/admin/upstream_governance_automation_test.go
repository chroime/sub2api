package admin

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestGovernanceAutomationHTTPRejectsAmbiguousApply(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h := NewUpstreamGovernanceHandler(nil)
	router := gin.New()
	router.POST("/sites/:id/reconcile-previews/:preview_id/apply", h.ApplyReconciliation)
	path := "/sites/1/reconcile-previews/688db180-3f7e-4ca2-b350-d0e0a20cd6c5/apply"
	for _, body := range []string{
		`{}`, `{"binding_ids":[]}`, `{"binding_ids":[0]}`,
		`{"binding_ids":[4,4]}`, `{"binding_ids":[4],"force":true}`,
		`{"binding_ids":[4.5]}`, `{"binding_ids":[4]} {}`,
	} {
		t.Run(body, func(t *testing.T) {
			w := httptest.NewRecorder()
			router.ServeHTTP(w, httptest.NewRequest(http.MethodPost, path, strings.NewReader(body)))
			require.Equal(t, http.StatusBadRequest, w.Code)
		})
	}
	for _, invalidPath := range []string{
		"/sites/0/reconcile-previews/688db180-3f7e-4ca2-b350-d0e0a20cd6c5/apply",
		"/sites/1/reconcile-previews/not-a-preview/apply",
	} {
		w := httptest.NewRecorder()
		router.ServeHTTP(w, httptest.NewRequest(http.MethodPost, invalidPath, strings.NewReader(`{"binding_ids":[4]}`)))
		require.Equal(t, http.StatusBadRequest, w.Code)
	}
}

func TestGovernanceAutomationPreviewCannotSmuggleAccountChanges(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h := NewUpstreamGovernanceHandler(nil)
	router := gin.New()
	router.POST("/sites/:id/reconcile-preview", h.PreviewReconciliation)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/sites/1/reconcile-preview", strings.NewReader(`{"rate_multiplier":0.01,"account_id":5}`)))
	require.Equal(t, http.StatusBadRequest, w.Code)
}
