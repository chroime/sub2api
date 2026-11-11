//go:build unit

package service

import (
	"context"
	"strconv"
	"testing"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/internal/payment"
	"github.com/stretchr/testify/require"
)

func TestBuildPaymentOrderProviderSnapshot_ExcludesSensitiveConfig(t *testing.T) {
	t.Parallel()

	sel := &payment.InstanceSelection{
		InstanceID:     "12",
		ProviderKey:    payment.TypeWxpay,
		SupportedTypes: "wxpay,wxpay_direct",
		PaymentMode:    "popup",
		Config: map[string]string{
			"privateKey": "secret",
			"apiV3Key":   "secret-v3",
			"appId":      "wx-app-id",
		},
	}

	snapshot := buildPaymentOrderProviderSnapshot(sel, CreateOrderRequest{})
	require.Equal(t, map[string]any{
		"schema_version":       2,
		"provider_instance_id": "12",
		"provider_key":         payment.TypeWxpay,
		"payment_mode":         "popup",
		"merchant_app_id":      "wx-app-id",
		"currency":             "CNY",
	}, snapshot)
	require.NotContains(t, snapshot, "config")
	require.NotContains(t, snapshot, "privateKey")
	require.NotContains(t, snapshot, "apiV3Key")
	require.NotContains(t, snapshot, "supported_types")
	require.NotContains(t, snapshot, "instance_name")
	require.NotContains(t, snapshot, "merchant_id")
}

func TestCreateOrderInTx_WritesProviderSnapshot(t *testing.T) {
	ctx := context.Background()
	client := newPaymentConfigServiceTestClient(t)

	user, err := client.User.Create().
		SetEmail("snapshot@example.com").
		SetPasswordHash("hash").
		SetUsername("snapshot-user").
		Save(ctx)
	require.NoError(t, err)

	instance, err := client.PaymentProviderInstance.Create().
		SetProviderKey(payment.TypeAlipay).
		SetName("Primary Alipay").
		SetConfig(`{"secretKey":"do-not-copy"}`).
		SetSupportedTypes("alipay,alipay_direct").
		SetPaymentMode("redirect").
		SetEnabled(true).
		Save(ctx)
	require.NoError(t, err)

	svc := &PaymentService{entClient: client}
	order, err := svc.createOrderInTx(
		ctx,
		CreateOrderRequest{
			UserID:      user.ID,
			PaymentType: payment.TypeAlipay,
			OrderType:   payment.OrderTypeBalance,
			ClientIP:    "127.0.0.1",
			SrcHost:     "app.example.com",
		},
		&User{
			ID:       user.ID,
			Email:    user.Email,
			Username: user.Username,
		},
		nil,
		&PaymentConfig{
			MaxPendingOrders: 3,
			OrderTimeoutMin:  30,
		},
		88,
		88,
		0,
		88,
		0,
		&payment.InstanceSelection{
			InstanceID:     strconv.FormatInt(instance.ID, 10),
			ProviderKey:    payment.TypeAlipay,
			SupportedTypes: "alipay,alipay_direct",
			PaymentMode:    "redirect",
			Config: map[string]string{
				"secretKey": "do-not-copy",
			},
		},
	)
	require.NoError(t, err)
	require.Equal(t, strconv.FormatInt(instance.ID, 10), valueOrEmpty(order.ProviderInstanceID))
	require.Equal(t, payment.TypeAlipay, valueOrEmpty(order.ProviderKey))
	require.Equal(t, float64(2), order.ProviderSnapshot["schema_version"])
	require.Equal(t, strconv.FormatInt(instance.ID, 10), order.ProviderSnapshot["provider_instance_id"])
	require.Equal(t, payment.TypeAlipay, order.ProviderSnapshot["provider_key"])
	require.Equal(t, "redirect", order.ProviderSnapshot["payment_mode"])
	require.NotContains(t, order.ProviderSnapshot, "config")
	require.NotContains(t, order.ProviderSnapshot, "secretKey")
	require.NotContains(t, order.ProviderSnapshot, "supported_types")
	require.NotContains(t, order.ProviderSnapshot, "instance_name")
}

func TestBuildPaymentOrderProviderSnapshot_UsesWxpayJSAPIAppIDForOpenIDOrders(t *testing.T) {
	t.Parallel()

	snapshot := buildPaymentOrderProviderSnapshot(&payment.InstanceSelection{
		InstanceID:  "88",
		ProviderKey: payment.TypeWxpay,
		Config: map[string]string{
			"appId":   "wx-open-app",
			"mpAppId": "wx-mp-app",
			"mchId":   "mch-88",
		},
		PaymentMode: "jsapi",
	}, CreateOrderRequest{OpenID: "openid-123"})

	require.Equal(t, "wx-mp-app", snapshot["merchant_app_id"])
	require.Equal(t, "mch-88", snapshot["merchant_id"])
	require.Equal(t, "CNY", snapshot["currency"])
}

func TestBuildPaymentOrderProviderSnapshot_IncludesAlipayMerchantIdentity(t *testing.T) {
	t.Parallel()

	snapshot := buildPaymentOrderProviderSnapshot(&payment.InstanceSelection{
		InstanceID:  "21",
		ProviderKey: payment.TypeAlipay,
		Config: map[string]string{
			"appId":      "alipay-app-21",
			"privateKey": "secret",
		},
		PaymentMode: "redirect",
	}, CreateOrderRequest{})

	require.Equal(t, "alipay-app-21", snapshot["merchant_app_id"])
	require.NotContains(t, snapshot, "privateKey")
}

func TestBuildPaymentOrderProviderSnapshot_IncludesEasyPayMerchantIdentity(t *testing.T) {
	t.Parallel()

	snapshot := buildPaymentOrderProviderSnapshot(&payment.InstanceSelection{
		InstanceID:  "66",
		ProviderKey: payment.TypeEasyPay,
		Config: map[string]string{
			"pid":  "easypay-merchant-66",
			"pkey": "secret",
		},
		PaymentMode: "popup",
	}, CreateOrderRequest{PaymentType: payment.TypeAlipay})

	require.Equal(t, "easypay-merchant-66", snapshot["merchant_id"])
	require.NotContains(t, snapshot, "pkey")
}

func TestBuildPaymentOrderProviderSnapshot_IncludesProviderCurrency(t *testing.T) {
	t.Parallel()

	stripeSnapshot := buildPaymentOrderProviderSnapshot(&payment.InstanceSelection{
		InstanceID:  "77",
		ProviderKey: payment.TypeStripe,
		Config: map[string]string{
			"currency": "hkd",
		},
	}, CreateOrderRequest{})
	require.Equal(t, "HKD", stripeSnapshot["currency"])

	airwallexSnapshot := buildPaymentOrderProviderSnapshot(&payment.InstanceSelection{
		InstanceID:  "78",
		ProviderKey: payment.TypeAirwallex,
		Config: map[string]string{
			"currency":  "usd",
			"accountId": "acct-78",
		},
	}, CreateOrderRequest{})
	require.Equal(t, "USD", airwallexSnapshot["currency"])
	require.Equal(t, "acct-78", airwallexSnapshot["merchant_id"])
}

func valueOrEmpty(v *string) string {
	if v == nil {
		return ""
	}
	return *v
}

func TestBuildPaymentOrderProviderSnapshot_AlipayBillIdentity(t *testing.T) {
	t.Parallel()
	snapshot := buildPaymentOrderProviderSnapshot(&payment.InstanceSelection{
		InstanceID: "88", ProviderKey: "alipay_bill", PaymentMode: "qrcode",
		Config: map[string]string{
			"appId": "app-fixed", "receiverId": "2088000000000001", "privateKey": "private-secret", "appAuthToken": "token-secret", "qrRules": "[]",
		},
	}, CreateOrderRequest{PaymentType: "alipay"})
	require.Equal(t, "app-fixed", snapshot["merchant_app_id"])
	require.Equal(t, "2088000000000001", snapshot["receiver_id"])
	require.Equal(t, "CNY", snapshot["currency"])
	require.NotContains(t, snapshot, "privateKey")
	require.NotContains(t, snapshot, "appAuthToken")
	require.NotContains(t, snapshot, "qrRules")
}

func TestValidateProviderSnapshotMetadata_AlipayBillRequiresPinnedIdentity(t *testing.T) {
	t.Parallel()
	order := &dbent.PaymentOrder{ProviderSnapshot: map[string]any{
		"schema_version": 2, "provider_instance_id": "88", "provider_key": "alipay_bill", "merchant_app_id": "app-fixed", "receiver_id": "2088000000000001", "currency": "CNY",
	}}
	for _, tc := range []struct {
		name      string
		metadata  map[string]string
		wantError bool
	}{
		{name: "correct identity", metadata: map[string]string{"app_id": "app-fixed", "receiver_id": "2088000000000001"}},
		{name: "missing identity", wantError: true},
		{name: "wrong app", metadata: map[string]string{"app_id": "other-app", "receiver_id": "2088000000000001"}, wantError: true},
		{name: "wrong receiver", metadata: map[string]string{"app_id": "app-fixed", "receiver_id": "2088000000000002"}, wantError: true},
		{name: "missing receiver", metadata: map[string]string{"app_id": "app-fixed"}, wantError: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := validateProviderSnapshotMetadata(order, "alipay_bill", tc.metadata)
			if tc.wantError {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestAlipayBillSourceSwitchKeepsExistingOrderProvider(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	client := newPaymentConfigServiceTestClient(t)
	old, err := client.PaymentProviderInstance.Create().SetProviderKey("easypay").SetName("Old Alipay").
		SetConfig("{}").SetSupportedTypes("alipay").SetEnabled(true).Save(ctx)
	require.NoError(t, err)
	bill, err := client.PaymentProviderInstance.Create().SetProviderKey("alipay_bill").SetName("Fixed Alipay").
		SetConfig("{}").SetSupportedTypes("alipay").SetEnabled(true).Save(ctx)
	require.NoError(t, err)
	configService := &PaymentConfigService{entClient: client, settingRepo: &paymentConfigSettingRepoStub{values: map[string]string{
		SettingPaymentVisibleMethodAlipaySource: "alipay_bill",
	}}}
	selected, err := configService.resolveEnabledVisibleMethodInstance(ctx, "alipay")
	require.NoError(t, err)
	require.Equal(t, bill.ID, selected.ID)
	id, key := strconv.FormatInt(old.ID, 10), "easypay"
	order := &dbent.PaymentOrder{ID: 10, PaymentType: "alipay", ProviderInstanceID: &id, ProviderKey: &key, ProviderSnapshot: map[string]any{
		"schema_version": 2, "provider_instance_id": id, "provider_key": "easypay", "merchant_id": "old-merchant",
	}}
	svc := &PaymentService{entClient: client, configService: configService}
	resolved, err := svc.resolveSnapshotOrderProviderInstance(ctx, order, psOrderProviderSnapshot(order))
	require.NoError(t, err)
	require.Equal(t, old.ID, resolved.ID)
	require.Equal(t, "easypay", expectedNotificationProviderKeyForOrder(nil, order, resolved.ProviderKey))
}
