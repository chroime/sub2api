package admin

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"

	gov "github.com/Wei-Shaw/sub2api/internal/upstreamgovernance"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type pricingAdminHandlerStore struct{ gov.Store }

func (pricingAdminHandlerStore) PreviewPricingPolicy(_ context.Context, siteID, groupID int64, d gov.PricingPolicyDraft) (gov.PricingPolicyPreview, error) {
	target := .3385
	return gov.PricingPolicyPreview{LocalGroupID: groupID, CurrentSale: .3, TargetSale: &target, Policy: d, Fingerprint: "fixture", Reason: "price_ready"}, nil
}
func (pricingAdminHandlerStore) SavePricingPolicy(_ context.Context, _, _ int64, _ gov.PricingPolicyDraft, _ string) (gov.PricingPoliciesConfiguration, error) {
	return gov.PricingPoliciesConfiguration{}, gov.ErrConflict
}
func (pricingAdminHandlerStore) SavePricingNotifications(_ context.Context, _, _ int64, _ gov.PricingNotificationPolicy) (gov.PricingPoliciesConfiguration, error) {
	return gov.PricingPoliciesConfiguration{}, gov.ErrConflict
}

func TestGovernancePricingAdminRejectsClientAuthoritativeFields(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h := NewUpstreamGovernanceHandler(nil)
	for _, input := range []string{
		`{"policy":{"baseline_cost":0.01}}`,
		`{"policy":{"manual_owner":false}}`,
		`{"policy":{"active_cost":0.01}}`,
		`{"policy":{"sources":[]}}`,
		`{"policy":{"min_margin":null}}`,
		`{"policy":null}`,
		`{}`,
	} {
		r := gin.New()
		r.POST("/sites/:id/pricing-policies/:group_id/preview", h.PreviewPricingPolicy)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest("POST", "/sites/1/pricing-policies/12/preview", strings.NewReader(input)))
		require.Equal(t, 400, w.Code, input)
	}
}

func TestGovernancePricingAdminPreviewContractAndConflictStatus(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h := NewUpstreamGovernanceHandler(gov.NewService(pricingAdminHandlerStore{}, nil, nil, nil, false))
	r := gin.New()
	r.POST("/sites/:id/pricing-policies/:group_id/preview", h.PreviewPricingPolicy)
	r.PUT("/sites/:id/pricing-policies/:group_id", h.ConfigurePricingPolicy)
	r.PUT("/sites/:id/pricing-notifications", h.ConfigurePricingNotifications)
	body := `{"policy":{"enabled":true,"mode":"target_margin","min_margin":0.25,"safety_buffer":0.1,"decrease_stability_seconds":60,"max_increase_percent":20}}`
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest("POST", "/sites/1/pricing-policies/12/preview", strings.NewReader(body)))
	require.Equal(t, 200, w.Code)
	require.Contains(t, w.Body.String(), `"target_sale":0.3385`)
	require.Contains(t, w.Body.String(), `"local_group_id":12`)
	w = httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest("PUT", "/sites/1/pricing-policies/12", strings.NewReader(strings.TrimSuffix(body, "}")+`,"fingerprint":"stale"}`)))
	require.Equal(t, 409, w.Code)
	w = httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest("PUT", "/sites/1/pricing-notifications", strings.NewReader(`{"version":1,"notifications":{"enabled":false,"recipients":[]}}`)))
	require.Equal(t, 409, w.Code)
}

func TestGovernancePricingNotificationsRejectsPolicyPayload(t *testing.T) {
	h := NewUpstreamGovernanceHandler(nil)
	r := gin.New()
	r.PUT("/sites/:id/pricing-notifications", h.ConfigurePricingNotifications)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest("PUT", "/sites/1/pricing-notifications", strings.NewReader(`{"version":1,"notifications":{},"policies":[]}`)))
	require.Equal(t, 400, w.Code)
}
