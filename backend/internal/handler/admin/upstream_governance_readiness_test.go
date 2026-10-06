package admin

import (
	"context"
	"net/http/httptest"
	"testing"

	gov "github.com/Wei-Shaw/sub2api/internal/upstreamgovernance"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type readinessHandlerStore struct {
	gov.Store
}

func (readinessHandlerStore) GetSite(context.Context, int64) (*gov.Site, error) {
	return &gov.Site{ID: 1, Name: "Fixture", Platform: "sub2api", BaseURL: "https://fixture.example", Version: 1, Status: "disconnected"}, nil
}

func (readinessHandlerStore) LatestSnapshot(context.Context, int64) (*gov.Snapshot, error) {
	return nil, gov.ErrNotFound
}

func (readinessHandlerStore) ListBindings(context.Context, int64) ([]gov.Binding, error) {
	return nil, nil
}

func (readinessHandlerStore) ListManagedKeys(context.Context, int64) ([]gov.ManagedKey, error) {
	return nil, nil
}

func (readinessHandlerStore) GetAutomation(context.Context, int64) (gov.AutomationConfig, error) {
	return gov.DefaultAutomationConfig(), nil
}

func (readinessHandlerStore) ListPricingPolicies(context.Context, int64) (gov.PricingPoliciesConfiguration, error) {
	return gov.PricingPoliciesConfiguration{Notifications: gov.PricingNotificationPolicy{}}, nil
}

func TestGovernanceReadinessRouteReturnsNonSecretChecklist(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h := NewUpstreamGovernanceHandler(gov.NewService(readinessHandlerStore{}, nil, nil, nil, false))
	r := gin.New()
	r.GET("/sites/:id/readiness", h.Readiness)

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest("GET", "/sites/1/readiness", nil))
	require.Equal(t, 200, w.Code)
	require.Contains(t, w.Body.String(), `"site_id":1`)
	require.Contains(t, w.Body.String(), `"checks"`)
	require.Contains(t, w.Body.String(), `"key":"authorization"`)
	require.NotContains(t, w.Body.String(), "session_cipher")
	require.NotContains(t, w.Body.String(), "password")
}

func TestGovernanceReadinessRouteValidatesIDs(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h := NewUpstreamGovernanceHandler(nil)
	r := gin.New()
	r.GET("/sites/:id/readiness", h.Readiness)
	for _, path := range []string{"/sites/0/readiness", "/sites/01/readiness", "/sites/-1/readiness"} {
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest("GET", path, nil))
		require.Equal(t, 400, w.Code, path)
	}
}
