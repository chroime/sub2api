package admin

import (
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestBillEditCredentialsRequiresAdministratorAndDisablesCaching(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, tc := range []struct {
		name, role, id string
		userID         int64
		wantStatus     int
	}{
		{name: "anonymous", id: "1", wantStatus: 401},
		{name: "ordinary user", role: "user", userID: 7, id: "1", wantStatus: 403},
		{name: "missing role", userID: 7, id: "1", wantStatus: 403},
		{name: "invalid instance", role: "admin", userID: 7, id: "invalid", wantStatus: 400},
	} {
		t.Run(tc.name, func(t *testing.T) {
			handler := &PaymentHandler{}
			engine := gin.New()
			engine.GET("/providers/:id/bill-credentials", func(c *gin.Context) {
				if tc.userID > 0 {
					c.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: tc.userID})
				}
				if tc.role != "" {
					c.Set(string(middleware.ContextKeyUserRole), tc.role)
				}
				handler.GetBillEditCredentials(c)
			})
			recorder := httptest.NewRecorder()
			engine.ServeHTTP(recorder, httptest.NewRequest("GET", "/providers/"+tc.id+"/bill-credentials", nil))
			require.Equal(t, tc.wantStatus, recorder.Code)
			require.Equal(t, "no-store", recorder.Header().Get("Cache-Control"))
			require.Equal(t, "no-cache", recorder.Header().Get("Pragma"))
		})
	}
}

func TestBillReceiptClaimRequiresAuthenticatedReviewer(t *testing.T) {
	gin.SetMode(gin.TestMode)
	handler := &PaymentHandler{}
	engine := gin.New()
	engine.POST("/providers/:id/bill-receipts/:receiptId/claim", handler.ClaimBillReceipt)
	request := httptest.NewRequest("POST", "/providers/1/bill-receipts/2/claim", strings.NewReader(`{"order_id":3,"reason":"verified"}`))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	engine.ServeHTTP(response, request)
	require.Equal(t, 401, response.Code)
}

func TestBillConnectionRejectsInvalidProviderIDBeforeAccess(t *testing.T) {
	gin.SetMode(gin.TestMode)
	handler := &PaymentHandler{}
	engine := gin.New()
	engine.POST("/providers/:id/bill-test", handler.TestBillConnection)
	response := httptest.NewRecorder()
	engine.ServeHTTP(response, httptest.NewRequest("POST", "/providers/invalid/bill-test", nil))
	require.Equal(t, 400, response.Code)
}
