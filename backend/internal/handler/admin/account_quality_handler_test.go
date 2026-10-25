//go:build unit

package admin

import (
	aq "github.com/Wei-Shaw/sub2api/internal/accountquality"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"net/http/httptest"
	"strings"
	"testing"
)

func qualityTestContext(body string, authenticated bool) (*gin.Context, *httptest.ResponseRecorder) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest("POST", "/api/v1/admin/account-quality/bindings", strings.NewReader(body))
	ctx.Request.Header.Set("Content-Type", "application/json")
	if authenticated {
		ctx.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: 42})
	}
	return ctx, recorder
}
func TestAccountQualityMutationRequiresAdministratorIdentity(t *testing.T) {
	handler := NewAccountQualityHandler(nil)
	ctx, recorder := qualityTestContext(`{"policy_id":1,"account_ids":[1]}`, false)
	handler.Bind(ctx)
	require.Equal(t, 401, recorder.Code)
}
func TestAccountQualityRejectsMalformedOrAdditionalMutationPayload(t *testing.T) {
	handler := NewAccountQualityHandler(nil)
	for _, body := range []string{`{"policy_id":1,"account_ids":[1],"credentials":{"api_key":"not-a-real-key"}}`, `{"policy_id":1,"account_ids":[1]} {"auto_isolate":true}`, `{"policy_id":`, strings.Repeat("x", 300000)} {
		t.Run(body[:min(25, len(body))], func(t *testing.T) {
			ctx, recorder := qualityTestContext(body, true)
			handler.Bind(ctx)
			require.Equal(t, 400, recorder.Code)
			require.NotContains(t, recorder.Body.String(), "not-a-real-key")
		})
	}
}
func TestAccountQualityControlValidatesVersionAndReasonBeforeStorage(t *testing.T) {
	handler := NewAccountQualityHandler(&aq.Service{Store: &aq.Store{}})
	for _, body := range []string{`{"version":0,"action":"release","reason":"reviewed"}`, `{"version":1,"action":"release","reason":""}`} {
		ctx, recorder := qualityTestContext(body, true)
		ctx.Params = gin.Params{{Key: "id", Value: "1"}}
		handler.Control(ctx)
		require.Equal(t, 400, recorder.Code)
	}
}
func TestAccountQualitySettingsRejectsInvalidBudgetsBeforeStorage(t *testing.T) {
	handler := NewAccountQualityHandler(&aq.Service{Store: &aq.Store{}})
	ctx, recorder := qualityTestContext(`{"version":1,"concurrency":21,"daily_request_limit":100,"notifications":{"reminder_seconds":60}}`, true)
	handler.SaveSettings(ctx)
	require.Equal(t, 400, recorder.Code)
}
func TestAccountQualitySettingsDefaultsHaveNoExternalDelivery(t *testing.T) {
	settings := aq.DefaultSettings()
	require.False(t, settings.Notifications.EmailEnabled)
	require.False(t, settings.Notifications.QQEnabled)
	require.Empty(t, settings.Notifications.EmailRecipients)
	require.Empty(t, settings.Notifications.QQTargets)
}
