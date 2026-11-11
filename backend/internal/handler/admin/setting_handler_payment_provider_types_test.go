//go:build unit

package admin

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestPaymentProviderTypesSettingsRoundTripKeepsAlipayBill(t *testing.T) {
	h, repo := newStepUpSwitchTestHandler(t, map[string]string{
		service.SettingEnabledPaymentTypes: "easypay,alipay",
	})
	h.paymentConfigService = service.NewPaymentConfigService(nil, repo, nil)
	want := []string{"easypay", "alipay", "alipay_bill"}

	saved := doUpdateSettings(t, h, map[string]any{"payment_enabled_types": want}, nil)
	require.Equal(t, http.StatusOK, saved.Code)
	require.Equal(t, "easypay,alipay,alipay_bill", repo.values[service.SettingEnabledPaymentTypes])
	var response struct {
		Data struct {
			EnabledTypes []string `json:"payment_enabled_types"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(saved.Body.Bytes(), &response))
	require.Equal(t, want, response.Data.EnabledTypes, "the save response must keep the provider selection shown by the admin")

	loaded := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(loaded)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/v1/admin/settings", nil)
	h.GetSettings(c)
	require.Equal(t, http.StatusOK, loaded.Code)
	require.NoError(t, json.Unmarshal(loaded.Body.Bytes(), &response))
	require.Equal(t, want, response.Data.EnabledTypes, "reloading settings must not collapse a provider key into a visible method")

	repeated := doUpdateSettings(t, h, map[string]any{"payment_enabled_types": response.Data.EnabledTypes}, nil)
	require.Equal(t, http.StatusOK, repeated.Code)
	require.Equal(t, "easypay,alipay,alipay_bill", repo.values[service.SettingEnabledPaymentTypes])
	checkout, err := h.paymentConfigService.GetPaymentConfig(context.Background())
	require.NoError(t, err)
	require.Equal(t, []string{"easypay", "alipay"}, checkout.EnabledTypes, "checkout still exposes canonical visible methods")
}

func TestPaymentProviderTypesSettingsGetKeepsBillWithoutEnablingOfficial(t *testing.T) {
	h, repo := newStepUpSwitchTestHandler(t, map[string]string{
		service.SettingEnabledPaymentTypes: "alipay_bill",
	})
	h.paymentConfigService = service.NewPaymentConfigService(nil, repo, nil)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/v1/admin/settings", nil)
	h.GetSettings(c)
	require.Equal(t, http.StatusOK, recorder.Code)
	var response struct {
		Data struct {
			EnabledTypes []string `json:"payment_enabled_types"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &response))
	require.Equal(t, []string{"alipay_bill"}, response.Data.EnabledTypes)
}

func TestPaymentProviderTypesAdminConfigKeepsProviderKeys(t *testing.T) {
	_, repo := newStepUpSwitchTestHandler(t, map[string]string{
		service.SettingEnabledPaymentTypes: " alipay_bill , alipay_direct , alipay , wxpay_direct , stripe ",
	})
	h := NewPaymentHandler(nil, service.NewPaymentConfigService(nil, repo, nil))
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/v1/admin/payment/config", nil)
	h.GetConfig(c)
	require.Equal(t, http.StatusOK, recorder.Code)
	var response struct {
		Data struct {
			EnabledTypes []string `json:"enabled_payment_types"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &response))
	require.Equal(t, []string{"alipay_bill", "alipay", "wxpay", "stripe"}, response.Data.EnabledTypes)
}
