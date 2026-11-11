//go:build unit

package service

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/payment"
	"github.com/Wei-Shaw/sub2api/internal/payment/epay"
	"github.com/Wei-Shaw/sub2api/internal/payment/provider"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/stretchr/testify/require"
)

const builtinEasyPayTestKey = "0123456789abcdef0123456789abcdef"

func newBuiltinEasyPayServiceFixture(t *testing.T, origin string) (*PaymentService, int64, map[string]string) {
	t.Helper()
	s, id := newBillReceiptTestService(t)
	s.configService.encryptionKey = []byte("0123456789abcdef0123456789abcdef")
	s.configService.settingRepo = &paymentConfigSettingRepoStub{values: map[string]string{SettingPaymentEnabled: "true", SettingOrderTimeoutMinutes: "30"}}
	origins, err := json.Marshal([]string{origin})
	require.NoError(t, err)
	cfg := map[string]string{"appId": "2021001234567890", "receiverId": "2088000000000001", "privateKey": "private-secret", "publicKey": "public-secret",
		"qrRules": `[{"id":"hundred","amount":"100.00","qr_url":"https://qr.alipay.com/hundred","enabled":true}]`, "epayEnabled": "true", "epayKey": builtinEasyPayTestKey, "epayNotifyOrigins": string(origins)}
	stored, err := s.configService.encryptProviderConfig(payment.TypeAlipayBill, cfg)
	require.NoError(t, err)
	_, err = s.entClient.PaymentProviderInstance.UpdateOneID(id).SetConfig(stored).SetEnabled(true).SetSupportedTypes("alipay").SetPaymentMode("qrcode").Save(context.Background())
	require.NoError(t, err)
	return s, id, cfg
}

func builtinEasyPayTestParams(id int64, origin string) map[string]string {
	params := map[string]string{"pid": strconv.FormatInt(id, 10), "type": "alipay", "out_trade_no": "MERCHANT_ORDER_1", "notify_url": origin + "/notify", "return_url": origin + "/return", "name": "External purchase", "money": "100.00", "sign_type": "MD5", "param": "user-123"}
	params["sign"] = epay.Sign(params, builtinEasyPayTestKey)
	return params
}

func TestBuiltinEasyPayExternalOrdersAreExactSeparateAndIdempotent(t *testing.T) {
	s, id, _ := newBuiltinEasyPayServiceFixture(t, "https://merchant.example")
	ctx := context.Background()
	params := builtinEasyPayTestParams(id, "https://merchant.example")
	first, err := s.CreateBuiltinEasyPayOrder(ctx, params)
	require.NoError(t, err)
	require.Equal(t, "https://qr.alipay.com/hundred", first.QRCode)
	require.True(t, strings.HasPrefix(first.TradeNo, "epay_"))
	second, err := s.CreateBuiltinEasyPayOrder(ctx, params)
	require.NoError(t, err)
	require.Equal(t, first, second)
	localCount, err := s.entClient.PaymentOrder.Query().Count(ctx)
	require.NoError(t, err)
	require.Zero(t, localCount)
	list, err := s.ListBuiltinEasyPayOrders(ctx, id, 1, 20, "", "")
	require.NoError(t, err)
	require.Equal(t, 1, list.Total)
	raw, err := json.Marshal(list)
	require.NoError(t, err)
	for _, private := range []string{builtinEasyPayTestKey, "private-secret", "https://qr.alipay.com", "callback_key_encrypted", "receiver_id", "notify_url"} {
		require.NotContains(t, string(raw), private)
	}
	for _, field := range []string{"money", "name", "notify_url", "return_url", "param"} {
		copy := map[string]string{}
		for k, v := range params {
			copy[k] = v
		}
		copy[field] += "1"
		if field == "money" {
			copy[field] = "100.01"
		}
		copy["sign"] = epay.Sign(copy, builtinEasyPayTestKey)
		_, err := s.CreateBuiltinEasyPayOrder(ctx, copy)
		require.Error(t, err, field)
	}
	params["out_trade_no"] = "UNCONFIGURED_AMOUNT"
	params["money"] = "99.99"
	params["sign"] = epay.Sign(params, builtinEasyPayTestKey)
	_, err = s.CreateBuiltinEasyPayOrder(ctx, params)
	require.Equal(t, "ALIPAY_BILL_QR_NO_MATCH", infraerrors.Reason(err))
}

func TestBuiltinEasyPayRejectsSignaturesTypesAndUnlistedCallbacks(t *testing.T) {
	s, id, _ := newBuiltinEasyPayServiceFixture(t, "https://merchant.example")
	for _, tc := range []struct{ field, value string }{{"sign", strings.Repeat("0", 32)}, {"type", "wxpay"}, {"notify_url", "https://merchant.example.evil.test/notify"}, {"notify_url", "https://merchant.example/notify?tenant=x"}, {"notify_url", "https://merchant.example/notify?"}, {"return_url", "https://other.example/return"}, {"trade_status", "TRADE_SUCCESS"}, {"cid", "another-channel"}} {
		params := builtinEasyPayTestParams(id, "https://merchant.example")
		params[tc.field] = tc.value
		if tc.field != "sign" {
			params["sign"] = epay.Sign(params, builtinEasyPayTestKey)
		}
		_, err := s.CreateBuiltinEasyPayOrder(context.Background(), params)
		require.Error(t, err, tc.field)
	}
	list, err := s.ListBuiltinEasyPayOrders(context.Background(), id, 1, 20, "", "")
	require.NoError(t, err)
	require.Zero(t, list.Total)
}

func TestBuiltinEasyPayQueriesSurviveDisablementAndExpiryCannotCreateNewAttempt(t *testing.T) {
	s, id, _ := newBuiltinEasyPayServiceFixture(t, "https://merchant.example")
	ctx := context.Background()
	params := builtinEasyPayTestParams(id, "https://merchant.example")
	created, err := s.CreateBuiltinEasyPayOrder(ctx, params)
	require.NoError(t, err)
	_, err = s.entClient.PaymentProviderInstance.UpdateOneID(id).SetEnabled(false).Save(ctx)
	require.NoError(t, err)
	s.configService.settingRepo = &paymentConfigSettingRepoStub{values: map[string]string{SettingPaymentEnabled: "false"}}
	query := map[string]string{"act": "order", "pid": params["pid"], "key": builtinEasyPayTestKey, "out_trade_no": params["out_trade_no"]}
	result, err := s.QueryBuiltinEasyPayOrder(ctx, query)
	require.NoError(t, err)
	require.Zero(t, result.Status)
	require.Equal(t, created.TradeNo, result.TradeNo)
	repeated, err := s.CreateBuiltinEasyPayOrder(ctx, params)
	require.NoError(t, err)
	require.Equal(t, created.TradeNo, repeated.TradeNo)
	params["out_trade_no"] = "NEW_ORDER"
	params["sign"] = epay.Sign(params, builtinEasyPayTestKey)
	_, err = s.CreateBuiltinEasyPayOrder(ctx, params)
	require.Equal(t, "BUILTIN_EPAY_DISABLED", infraerrors.Reason(err))
	query["key"] = "incorrect"
	_, err = s.QueryBuiltinEasyPayOrder(ctx, query)
	require.Equal(t, "BUILTIN_EPAY_AUTH_FAILED", infraerrors.Reason(err))
	query["key"] = builtinEasyPayTestKey
	query["pid"] = strconv.FormatInt(id+1, 10)
	_, err = s.QueryBuiltinEasyPayOrder(ctx, query)
	require.Equal(t, "BUILTIN_EPAY_AUTH_FAILED", infraerrors.Reason(err))
	_, err = s.entClient.ExecContext(ctx, `UPDATE payment_epay_orders SET expires_at=$1 WHERE trade_no=$2`, time.Now().Add(-time.Minute), created.TradeNo)
	require.NoError(t, err)
	params = builtinEasyPayTestParams(id, "https://merchant.example")
	_, err = s.CreateBuiltinEasyPayOrder(ctx, params)
	require.Equal(t, "BUILTIN_EPAY_ORDER_EXPIRED", infraerrors.Reason(err))
}

func TestBuiltinEasyPayNotificationsRetryPersistentlyAndUseFrozenSecret(t *testing.T) {
	var attempts atomic.Int32
	var captured atomic.Value
	callback := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		captured.Store(r.URL.RawQuery)
		if attempts.Add(1) == 1 {
			w.WriteHeader(http.StatusBadGateway)
			return
		}
		_, _ = w.Write([]byte("success"))
	}))
	defer callback.Close()
	s, id, cfg := newBuiltinEasyPayServiceFixture(t, callback.URL)
	ctx := context.Background()
	params := builtinEasyPayTestParams(id, callback.URL)
	created, err := s.CreateBuiltinEasyPayOrder(ctx, params)
	require.NoError(t, err)
	o, err := findBuiltinEasyPayOrder(ctx, s.entClient, id, params["out_trade_no"], "")
	require.NoError(t, err)
	receipt, _, err := s.storeBillReceipt(ctx, id, "2088000000000001", provider.AlipayBillEntry{AccountLogID: "EPAY_LOG", AlipayOrderNo: "ALIPAY_TX", Amount: "100.00", Direction: "收入", TransAt: time.Now().UTC()})
	require.NoError(t, err)
	require.NoError(t, s.ClaimBuiltinEasyPayReceipt(ctx, id, receipt.ID, o.ID, "admin:1", "verified external purchase"))
	// Direct fixture mutation simulates restored historical configuration. The
	// notification must use the order's frozen key even if current keys differ.
	cfg["epayKey"] = "fedcba9876543210fedcba9876543210"
	stored, err := s.configService.encryptProviderConfig(payment.TypeAlipayBill, cfg)
	require.NoError(t, err)
	_, err = s.entClient.PaymentProviderInstance.UpdateOneID(id).SetConfig(stored).SetEnabled(false).Save(ctx)
	require.NoError(t, err)
	_, err = s.entClient.ExecContext(ctx, `UPDATE payment_epay_orders SET notify_lease_owner='other-worker',notify_lease_until=$1 WHERE id=$2`, time.Now().Add(time.Minute), o.ID)
	require.NoError(t, err)
	require.NoError(t, s.RetryBuiltinEasyPayNotifications(ctx))
	require.Zero(t, attempts.Load(), "an active delivery lease must not be stolen")
	_, err = s.entClient.ExecContext(ctx, `UPDATE payment_epay_orders SET notify_lease_until=$1 WHERE id=$2`, time.Now().Add(-time.Second), o.ID)
	require.NoError(t, err)
	require.NoError(t, s.RetryBuiltinEasyPayNotifications(ctx))
	require.Equal(t, int32(1), attempts.Load())
	o, err = findBuiltinEasyPayOrder(ctx, s.entClient, id, params["out_trade_no"], "")
	require.NoError(t, err)
	require.Equal(t, "PAID", o.Status)
	require.Equal(t, "pending", o.NotifyStatus)
	require.NotNil(t, o.NotifyNextAt)
	require.NoError(t, s.RetryBuiltinEasyPayNotifications(ctx))
	require.Equal(t, int32(1), attempts.Load(), "backoff must prevent immediate redelivery")
	_, err = s.entClient.ExecContext(ctx, `UPDATE payment_epay_orders SET notify_next_at=$1 WHERE id=$2`, time.Now().Add(-time.Second), o.ID)
	require.NoError(t, err)
	require.NoError(t, s.RetryBuiltinEasyPayNotifications(ctx))
	require.Equal(t, int32(2), attempts.Load())
	merchant, err := provider.NewEasyPay("external", map[string]string{"pid": params["pid"], "pkey": builtinEasyPayTestKey, "apiBase": "https://gateway.example", "notifyUrl": params["notify_url"], "returnUrl": params["return_url"]})
	require.NoError(t, err)
	notification, err := merchant.VerifyNotification(ctx, captured.Load().(string), nil)
	require.NoError(t, err)
	require.Equal(t, created.TradeNo, notification.TradeNo)
	require.Equal(t, payment.NotificationStatusSuccess, notification.Status)
	o, err = findBuiltinEasyPayOrder(ctx, s.entClient, id, params["out_trade_no"], "")
	require.NoError(t, err)
	require.Equal(t, "delivered", o.NotifyStatus)
	require.Equal(t, 2, o.NotifyAttempts)
	require.NoError(t, s.RetryBuiltinEasyPayNotifications(ctx))
	require.Equal(t, int32(2), attempts.Load())
	_, err = s.CreateBuiltinEasyPayOrder(ctx, params)
	require.Equal(t, "BUILTIN_EPAY_ORDER_PAID", infraerrors.Reason(err))
}

func TestBuiltinEasyPayNotificationsDoNotFollowRedirects(t *testing.T) {
	var redirected atomic.Int32
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { redirected.Add(1); _, _ = w.Write([]byte("success")) }))
	defer target.Close()
	callback := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, target.URL, http.StatusFound) }))
	defer callback.Close()
	s, id, _ := newBuiltinEasyPayServiceFixture(t, callback.URL)
	ctx := context.Background()
	params := builtinEasyPayTestParams(id, callback.URL)
	_, err := s.CreateBuiltinEasyPayOrder(ctx, params)
	require.NoError(t, err)
	o, err := findBuiltinEasyPayOrder(ctx, s.entClient, id, params["out_trade_no"], "")
	require.NoError(t, err)
	require.Equal(t, "callback_http_status", s.sendBuiltinEasyPayNotification(ctx, o))
	require.Zero(t, redirected.Load())
}
