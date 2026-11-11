//go:build unit

package routes

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/handler"
	adminhandler "github.com/Wei-Shaw/sub2api/internal/handler/admin"
	"github.com/Wei-Shaw/sub2api/internal/pkg/adminaccess"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestBillEditCredentialsRouteRejectsAnonymousAndReadOnlyPaymentKeys(t *testing.T) {
	gin.SetMode(gin.TestMode)
	settings := service.NewSettingService(&scopedPaymentRateSettings{}, nil)
	settings.SetScopedAdminKeyRepository(&scopedRouteKeys{})
	key, _, err := settings.CreateScopedAdminKey(context.Background(), service.ScopedAdminKeyInput{
		Name: "payment report reader", Enabled: true,
		Policy: adminaccess.Policy{GlobalPermissions: []string{"payment.read"}},
	}, 9)
	require.NoError(t, err)
	auth := middleware.NewAdminAuthMiddleware(nil, service.NewUserService(&scopedRouteUsers{}, nil, nil, nil), settings, nil)
	router := gin.New()
	noop := func(c *gin.Context) { c.Next() }
	RegisterPaymentRoutes(router.Group("/api/v1"), &handler.PaymentHandler{}, &handler.PaymentWebhookHandler{}, &adminhandler.PaymentHandler{},
		middleware.JWTAuthMiddleware(noop), auth, middleware.AuditLogMiddleware(noop), nil, nil, nil)
	for _, tc := range []struct {
		key  string
		want int
	}{{"", http.StatusUnauthorized}, {key, http.StatusForbidden}} {
		request := httptest.NewRequest(http.MethodGet, "/api/v1/admin/payment/providers/invalid/bill-credentials", nil)
		if tc.key != "" {
			request.Header.Set("x-api-key", tc.key)
		}
		recorder := httptest.NewRecorder()
		router.ServeHTTP(recorder, request)
		require.Equal(t, tc.want, recorder.Code, "authorization must reject before parsing the provider identity")
	}
}
