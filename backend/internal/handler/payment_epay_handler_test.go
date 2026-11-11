//go:build unit

package handler

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/ent/enttest"
	"github.com/Wei-Shaw/sub2api/internal/payment"
	"github.com/Wei-Shaw/sub2api/internal/payment/provider"
	"github.com/Wei-Shaw/sub2api/internal/repository"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	_ "modernc.org/sqlite"
)

func TestBuiltinEasyPayParameterParsingRejectsDuplicateAndConflictingValues(t *testing.T) {
	for _, tc := range []struct {
		name, query, body string
		ok                bool
	}{
		{"legacy query and body", "act=order&pid=1", "act=order&pid=1", true},
		{"duplicate query", "pid=1&pid=1", "", false},
		{"duplicate body", "", "pid=1&pid=1", false},
		{"conflicting transports", "pid=1", "pid=2", false},
		{"malformed escaping", "pid=%xx", "", false},
		{"oversized", "", strings.Repeat("a", builtinEasyPayMaxRequestBytes+1), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest(http.MethodPost, "/api.php?"+tc.query, strings.NewReader(tc.body))
			c.Request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			_, err := readBuiltinEasyPayParams(c)
			if tc.ok {
				require.NoError(t, err)
			} else {
				require.Error(t, err)
			}
		})
	}
}

func TestBuiltinEasyPayUnsupportedActionsReturnProtocolError(t *testing.T) {
	h := &PaymentHandler{}
	router := gin.New()
	router.POST("/api.php", h.BuiltinEasyPayAPI)
	for _, action := range []string{"refund", "orders", "", "balance"} {
		w := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/api.php", strings.NewReader("act="+action))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		router.ServeHTTP(w, req)
		require.Equal(t, http.StatusOK, w.Code)
		var result map[string]any
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &result))
		require.Equal(t, float64(-1), result["code"])
		require.NotContains(t, result, "data")
	}
}

func TestBuiltinEasyPayExistingClientInteroperability(t *testing.T) {
	ctx := context.Background()
	db, err := sql.Open("sqlite", fmt.Sprintf("file:%s?mode=memory&cache=shared&_pragma=foreign_keys(1)", t.Name()))
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	client := enttest.NewClient(t, enttest.WithOptions(dbent.Driver(entsql.OpenDB(dialect.SQLite, db))))
	t.Cleanup(func() { _ = client.Close() })
	for _, name := range []string{"282_payment_bill_receipts.sql", "283_payment_epay_orders.sql", "284_payment_bill_amount_reservations.sql", "285_payment_bill_amount_closure.sql", "286_payment_bill_user_cancel_release.sql"} {
		migration, err := os.ReadFile(filepath.Join("..", "..", "migrations", name))
		require.NoError(t, err)
		schema := strings.ReplaceAll(string(migration), "BIGSERIAL PRIMARY KEY", "INTEGER PRIMARY KEY AUTOINCREMENT")
		schema = strings.ReplaceAll(schema, "TIMESTAMPTZ", "DATETIME")
		schema = strings.ReplaceAll(schema, "NOW()", "CURRENT_TIMESTAMP")
		if name == "282_payment_bill_receipts.sql" {
			// SQLite cannot drop NOT NULL later; create the final nullable target.
			schema = strings.ReplaceAll(schema, "order_id BIGINT NOT NULL UNIQUE", "order_id BIGINT UNIQUE")
		}
		if name == "283_payment_epay_orders.sql" {
			// PostgreSQL migration constraints are covered by isolated PG tests.
			schema = strings.SplitN(schema, "ALTER TABLE payment_bill_receipts", 2)[0] + `
				ALTER TABLE payment_bill_receipts ADD COLUMN epay_order_id BIGINT REFERENCES payment_epay_orders(id);
				ALTER TABLE payment_bill_claims ADD COLUMN epay_order_id BIGINT REFERENCES payment_epay_orders(id);`
		}
		_, err = client.ExecContext(ctx, schema)
		require.NoError(t, err)
	}
	settings := repository.NewSettingRepository(client)
	require.NoError(t, settings.Set(ctx, service.SettingPaymentEnabled, "true"))
	encryptionKey := []byte("0123456789abcdef0123456789abcdef")
	const merchantKey = "abcdef0123456789abcdef0123456789"
	config := map[string]string{"appId": "2021001234567890", "receiverId": "2088000000000001", "privateKey": "private-placeholder", "publicKey": "public-placeholder", "epayEnabled": "true", "epayKey": merchantKey, "epayNotifyOrigins": `["https://merchant.example"]`, "qrRules": `[{"id":"ten","amount":"10.00","qr_url":"https://qr.alipay.com/ten","enabled":true}]`}
	raw, err := json.Marshal(config)
	require.NoError(t, err)
	encrypted, err := payment.Encrypt(string(raw), encryptionKey)
	require.NoError(t, err)
	instance, err := client.PaymentProviderInstance.Create().SetProviderKey(payment.TypeAlipayBill).SetName("builtin gateway").SetConfig(encrypted).SetSupportedTypes("alipay").SetEnabled(true).SetPaymentMode("qrcode").Save(ctx)
	require.NoError(t, err)
	configSvc := service.NewPaymentConfigService(client, settings, encryptionKey)
	paymentSvc := service.NewPaymentService(client, payment.NewRegistry(), nil, nil, nil, configSvc, nil, nil, nil)
	h := NewPaymentHandler(paymentSvc, configSvc)
	router := gin.New()
	router.POST("/mapi.php", h.BuiltinEasyPayCreate)
	router.GET("/api.php", h.BuiltinEasyPayAPI)
	router.POST("/api.php", h.BuiltinEasyPayAPI)
	server := httptest.NewServer(router)
	defer server.Close()
	pid := strconv.FormatInt(instance.ID, 10)
	merchant, err := provider.NewEasyPay("merchant", map[string]string{"pid": pid, "pkey": merchantKey, "apiBase": server.URL, "notifyUrl": "https://merchant.example/notify", "returnUrl": "https://merchant.example/return"})
	require.NoError(t, err)
	request := payment.CreatePaymentRequest{OrderID: "EXTERNAL_1", Amount: "10.00", PaymentType: "alipay", Subject: "External purchase"}
	created, err := merchant.CreatePayment(ctx, request)
	require.NoError(t, err)
	require.Equal(t, "https://qr.alipay.com/ten", created.QRCode)
	require.True(t, strings.HasPrefix(created.TradeNo, "epay_"))
	queried, err := merchant.QueryOrder(ctx, request.OrderID)
	require.NoError(t, err)
	require.Equal(t, payment.ProviderStatusPending, queried.Status)
	require.Equal(t, 10.0, queried.Amount)
	// Changing the source and disabling new payments must not strand existing
	// merchant queries. No internal user/order/balance is created for this sale.
	_, err = client.PaymentProviderInstance.UpdateOneID(instance.ID).SetEnabled(false).Save(ctx)
	require.NoError(t, err)
	require.NoError(t, settings.Set(ctx, service.SettingPaymentVisibleMethodAlipaySource, service.VisibleMethodSourceEasyPayAlipay))
	queried, err = merchant.QueryOrder(ctx, request.OrderID)
	require.NoError(t, err)
	require.Equal(t, payment.ProviderStatusPending, queried.Status)
	_, err = client.ExecContext(ctx, `UPDATE payment_epay_orders SET status='PAID',alipay_order_no='OFFICIAL_TX',paid_at=$1 WHERE trade_no=$2`, time.Now().UTC(), created.TradeNo)
	require.NoError(t, err)
	notification := &payment.PaymentNotification{OrderID: request.OrderID, TradeNo: created.TradeNo, Amount: 10, Status: payment.NotificationStatusSuccess}
	require.NoError(t, merchant.ConfirmNotification(ctx, notification), "current EasyPay client must confirm against our authenticated query endpoint")
	queried, err = merchant.QueryOrder(ctx, request.OrderID)
	require.NoError(t, err)
	require.Equal(t, payment.ProviderStatusPaid, queried.Status)
	count, err := client.PaymentOrder.Query().Count(ctx)
	require.NoError(t, err)
	require.Zero(t, count)
	_, err = merchant.CreatePayment(ctx, request)
	require.Error(t, err, "a paid gateway order must not present the QR for a second payment")
	_, err = merchant.Refund(ctx, payment.RefundRequest{OrderID: request.OrderID, TradeNo: created.TradeNo, Amount: "10.00"})
	require.Error(t, err)
}
