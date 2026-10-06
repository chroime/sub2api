package admin

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestGovernanceModelHTTPRejectsAmbiguousOrInjectedInputs(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h := NewUpstreamGovernanceHandler(nil) // A valid request would reach the service; invalid input must never do so.
	r := gin.New()
	r.POST("/sites/:id/model-policies", h.SaveModelPolicy)
	r.DELETE("/sites/:id/model-policies/:policy_id", h.DeleteModelPolicy)
	r.POST("/sites/:id/model-batches", h.StartModelBatch)
	r.POST("/sites/:id/model-batches/:batch_id/cancel", h.CancelModelBatch)
	r.GET("/sites/:id/model-runs", h.ModelRuns)
	r.GET("/sites/:id/model-stats", h.ModelStats)
	r.GET("/sites/:id/model-runs/:run_id", h.ModelRun)
	r.PUT("/sites/:id/model-runs/:run_id/review", h.ReviewModelRun)
	const batch = "cd207c56-3220-4c64-ae04-14403ebbb9df"
	for _, tc := range []struct{ method, path, body string }{
		{"POST", "/sites/0/model-batches", `{}`},
		{"POST", "/sites/1/model-batches", `{"request_id":"` + batch + `","api_key":"injected"}`},
		{"POST", "/sites/1/model-batches", `{"request_id":"` + batch + `","config":{"concurrency":1.5}}`},
		{"POST", "/sites/1/model-batches", `{"request_id":"00000000-0000-0000-0000-000000000000"}`},
		{"POST", "/sites/1/model-batches", `{"request_id":"` + batch + `"} {}`},
		{"POST", "/sites/1/model-policies", `{"id":2,"version":0}`},
		{"POST", "/sites/1/model-policies", `{"site_id":2}`},
		{"POST", "/sites/1/model-policies", `{"next_run_at":"2026-01-01T00:00:00Z"}`},
		{"DELETE", "/sites/1/model-policies/1?version=0", ``},
		{"DELETE", "/sites/1/model-policies/1?version=01", ``},
		{"GET", "/sites/1/model-runs?page_size=101", ``},
		{"GET", "/sites/1/model-stats?days=31", ``},
		{"GET", "/sites/1/model-stats?days=-1", ``},
		{"GET", "/sites/1/model-runs?batch_id=other-site", ``},
		{"GET", "/sites/1/model-runs/not-a-uuid", ``},
		{"POST", "/sites/1/model-batches/" + batch + "/cancel", `{"restart":true}`},
		{"PUT", "/sites/1/model-runs/" + batch + "/review", `{"review":"pass","reviewer_id":1}`},
		{"PUT", "/sites/1/model-runs/" + batch + "/review", `{"review":"guaranteed-real-model"}`},
		{"PUT", "/sites/1/model-runs/" + batch + "/review", `{"review":"pass","note":"` + strings.Repeat("x", 4001) + `"}`},
	} {
		t.Run(tc.method+tc.path+tc.body[:min(len(tc.body), 35)], func(t *testing.T) {
			w := httptest.NewRecorder()
			r.ServeHTTP(w, httptest.NewRequest(tc.method, tc.path, strings.NewReader(tc.body)))
			require.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
		})
	}
}
