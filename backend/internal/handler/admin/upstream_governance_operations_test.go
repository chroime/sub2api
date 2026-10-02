package admin

import (
	"context"
	"net/http/httptest"
	"testing"
	"time"

	gov "github.com/Wei-Shaw/sub2api/internal/upstreamgovernance"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type operationsHandlerStore struct{ gov.Store }

func (operationsHandlerStore) ReadWorkbench(_ context.Context, q gov.OperationsQuery, now time.Time) (gov.WorkbenchResult, error) {
	return gov.WorkbenchResult{Items: []gov.WorkbenchItem{{SiteID: q.SiteID}}, Page: q.Page, PageSize: q.PageSize, Total: 1, EvaluatedAt: now}, nil
}
func (operationsHandlerStore) ReadTimeline(_ context.Context, q gov.OperationsQuery, now time.Time) (gov.TimelineResult, error) {
	return gov.TimelineResult{Items: []gov.TimelineItem{{SiteID: q.SiteID, Kind: q.Kind}}, Page: q.Page, PageSize: q.PageSize, Total: 1, EvaluatedAt: now}, nil
}

func TestGovernanceOperationsQueryValidationAndReadContracts(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h := NewUpstreamGovernanceHandler(gov.NewService(operationsHandlerStore{}, nil, nil, nil, false))
	r := gin.New()
	r.GET("/workbench", h.Workbench)
	r.GET("/sites/:id/timeline", h.Timeline)
	for _, url := range []string{"/workbench?site_id=0", "/workbench?site_id=01", "/workbench?site_id=-1", "/workbench?page=0", "/workbench?page_size=101", "/sites/1/timeline?kind=secret-canary", "/sites/01/timeline"} {
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest("GET", url, nil))
		require.Equal(t, 400, w.Code, url)
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest("GET", "/workbench?site_id=3&page=2&page_size=10", nil))
	require.Equal(t, 200, w.Code)
	require.Contains(t, w.Body.String(), `"site_id":3`)
	require.Contains(t, w.Body.String(), `"page":2`)
	require.Contains(t, w.Body.String(), `"page_size":10`)
	w = httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest("GET", "/sites/4/timeline?kind=notification", nil))
	require.Equal(t, 200, w.Code)
	require.Contains(t, w.Body.String(), `"site_id":4`)
	require.Contains(t, w.Body.String(), `"kind":"notification"`)
}
