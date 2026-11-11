//go:build unit

package admin

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestClaimBillReceiptRequiresExactlyOnePositiveTarget(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, body := range []string{
		`{"reason":"verified"}`,
		`{"order_id":1,"epay_order_id":2,"reason":"verified"}`,
		`{"epay_order_id":-2,"reason":"verified"}`,
		`{"order_id":-1,"epay_order_id":2,"reason":"verified"}`,
	} {
		t.Run(body, func(t *testing.T) {
			handler := NewPaymentHandler(nil, nil)
			router := gin.New()
			router.POST("/providers/:id/bill-receipts/:receiptId/claim", func(c *gin.Context) {
				c.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: 1})
				handler.ClaimBillReceipt(c)
			})
			request := httptest.NewRequest(http.MethodPost, "/providers/1/bill-receipts/2/claim", strings.NewReader(body))
			request.Header.Set("Content-Type", "application/json")
			recorder := httptest.NewRecorder()
			router.ServeHTTP(recorder, request)
			require.Equal(t, http.StatusBadRequest, recorder.Code)
		})
	}
}
