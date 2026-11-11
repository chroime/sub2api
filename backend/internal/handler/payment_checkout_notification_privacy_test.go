//go:build unit

package handler

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/ent/enttest"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestCheckoutInfoNeverExposesPaymentAdminNotificationRecipients(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db, err := sql.Open("sqlite", "file:"+t.Name()+"?mode=memory&cache=shared")
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	_, err = db.Exec("PRAGMA foreign_keys = ON")
	require.NoError(t, err)
	client := enttest.NewClient(t, enttest.WithOptions(dbent.Driver(entsql.OpenDB(dialect.SQLite, db))))
	t.Cleanup(func() { _ = client.Close() })
	configService := service.NewPaymentConfigService(client, &checkoutRechargeAmountSettings{values: map[string]string{
		"PAYMENT_ADMIN_RECHARGE_NOTIFY_ENABLED": "true",
		"PAYMENT_ADMIN_RECHARGE_NOTIFY_EMAILS":  `["private-admin@example.test"]`,
	}}, nil)
	h := NewPaymentHandler(nil, configService)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodGet, "/api/v1/payment/checkout-info", nil)
	h.GetCheckoutInfo(ctx)
	require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
	var payload struct {
		Data map[string]json.RawMessage `json:"data"`
	}
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &payload))
	for _, key := range []string{"admin_recharge_notify_enabled", "admin_recharge_notify_emails"} {
		_, exists := payload.Data[key]
		require.False(t, exists, "administrator notification settings are not checkout data")
	}
	require.NotContains(t, recorder.Body.String(), "private-admin@example.test")
}

func TestUserPaymentConfigNeverExposesPaymentAdminNotificationRecipients(t *testing.T) {
	gin.SetMode(gin.TestMode)
	configService := service.NewPaymentConfigService(nil, &checkoutRechargeAmountSettings{values: map[string]string{
		"PAYMENT_ADMIN_RECHARGE_NOTIFY_ENABLED": "true",
		"PAYMENT_ADMIN_RECHARGE_NOTIFY_EMAILS":  `["private-admin@example.test"]`,
		service.SettingPaymentEnabled:           "true",
		service.SettingMinRechargeAmount:        "10",
		service.SettingHelpText:                 "Existing customer payment instructions",
	}}, nil)
	h := NewPaymentHandler(nil, configService)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodGet, "/api/v1/payment/config", nil)
	h.GetPaymentConfig(ctx)
	require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
	var payload struct {
		Data map[string]json.RawMessage `json:"data"`
	}
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &payload))
	for _, key := range []string{"admin_recharge_notify_enabled", "admin_recharge_notify_emails"} {
		_, exists := payload.Data[key]
		require.False(t, exists, "administrator notification settings are not customer payment configuration")
	}
	require.NotContains(t, recorder.Body.String(), "private-admin@example.test")
	require.JSONEq(t, "true", string(payload.Data["enabled"]))
	require.JSONEq(t, "10", string(payload.Data["min_amount"]))
	require.JSONEq(t, `"Existing customer payment instructions"`, string(payload.Data["help_text"]))
	for _, key := range []string{"max_amount", "daily_limit", "order_timeout_minutes", "max_pending_orders", "enabled_payment_types", "balance_disabled", "balance_recharge_multiplier", "subscription_usd_to_cny_rate", "recharge_fee_rate", "recharge_bonus_tiers", "recharge_bonus_mode", "recharge_bonus_notice", "load_balance_strategy", "product_name_prefix", "product_name_suffix", "help_image_url", "cancel_rate_limit_enabled", "cancel_rate_limit_max", "cancel_rate_limit_window", "cancel_rate_limit_unit", "cancel_rate_limit_window_mode", "alipay_force_qrcode", "alipay_mobile_precreate_deep_link"} {
		_, exists := payload.Data[key]
		require.True(t, exists, "existing public field %s must remain available", key)
	}
}
