package admin

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestGovernanceRechargeEvaluationUsesObservedBalanceOnly(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h := NewUpstreamGovernanceHandler(nil)
	router := gin.New()
	router.POST("/sites/:id/recharge-plan/evaluate", h.EvaluateRechargePlan)
	for _, body := range []string{
		`{"balance":0}`, `{"execute":true}`, `{"amount_minor":10000}`, `{} {}`,
	} {
		w := httptest.NewRecorder()
		router.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/sites/1/recharge-plan/evaluate", strings.NewReader(body)))
		require.Equal(t, http.StatusBadRequest, w.Code, body)
	}
}

func TestGovernanceRechargeHTTPRejectsFractionalMinorAmounts(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h := NewUpstreamGovernanceHandler(nil)
	router := gin.New()
	router.PUT("/sites/:id/recharge-plan", h.ConfigureRechargePlan)
	for _, body := range []string{
		`{"version":0,"policy":{"amount_minor":10.5}}`,
		`{"version":0,"policy":{"daily_budget_minor":10.5}}`,
		`{"version":0,"policy":{},"payment_token":"not-a-payment-api"}`,
	} {
		w := httptest.NewRecorder()
		router.ServeHTTP(w, httptest.NewRequest(http.MethodPut, "/sites/1/recharge-plan", strings.NewReader(body)))
		require.Equal(t, http.StatusBadRequest, w.Code, body)
	}
}
