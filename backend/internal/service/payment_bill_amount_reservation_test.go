//go:build unit

package service

import (
	"context"
	"encoding/json"
	"strconv"
	"testing"
	"time"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/internal/payment"
	"github.com/Wei-Shaw/sub2api/internal/payment/epay"
	"github.com/Wei-Shaw/sub2api/internal/payment/provider"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/stretchr/testify/require"
)

func TestBillAmountCreationConfiguredExpiryAndExclusiveAmount(t *testing.T) {
	s, id, _ := newBuiltinEasyPayServiceFixture(t, "https://merchant.example")
	ctx := context.Background()
	params := builtinEasyPayTestParams(id, "https://merchant.example")
	_, err := s.CreateBuiltinEasyPayOrder(ctx, params)
	require.NoError(t, err)
	order, err := findBuiltinEasyPayOrder(ctx, s.entClient, id, params["out_trade_no"], "")
	require.NoError(t, err)
	require.Equal(t, 30*time.Minute, order.ExpiresAt.Sub(order.CreatedAt), "fixed QR checkout must use the configured timeout")
	require.Zero(t, order.CreatedAt.Nanosecond())
	require.Zero(t, order.ExpiresAt.Nanosecond())
	params["out_trade_no"] = "COMPETING_ORDER"
	params["sign"] = epay.Sign(params, builtinEasyPayTestKey)
	_, err = s.CreateBuiltinEasyPayOrder(ctx, params)
	require.Equal(t, "ALIPAY_BILL_AMOUNT_BUSY", infraerrors.Reason(err), "another order must not share the same receiving amount")
}

func TestBillAmountLocalCreationOwnsConfiguredWindow(t *testing.T) {
	s, id, cfg := newBuiltinEasyPayServiceFixture(t, "https://merchant.example")
	ctx := context.Background()
	user, err := s.entClient.User.Create().SetEmail("amount-local@test.invalid").SetPasswordHash("fixture").SetUsername("amount-local").Save(ctx)
	require.NoError(t, err)
	order, err := createBillAmountLocalFixture(ctx, s, id, cfg, user, 100)
	require.NoError(t, err)
	require.Equal(t, 30*time.Minute, order.ExpiresAt.Sub(order.CreatedAt))
	require.Zero(t, order.CreatedAt.Nanosecond())
	require.Zero(t, order.ExpiresAt.Nanosecond())
	require.Equal(t, true, order.ProviderSnapshot["bill_amount_matching"])
	params := builtinEasyPayTestParams(id, "https://merchant.example")
	_, err = s.CreateBuiltinEasyPayOrder(ctx, params)
	require.Equal(t, "ALIPAY_BILL_AMOUNT_BUSY", infraerrors.Reason(err))
	_, err = s.entClient.PaymentOrder.UpdateOneID(order.ID).SetStatus(OrderStatusCancelled).Save(ctx)
	require.NoError(t, err)
	_, err = s.CreateBuiltinEasyPayOrder(ctx, params)
	require.NoError(t, err, "a cancelled order must release its amount immediately")
}

func billAmountReservationCheckAt(t *testing.T, s *PaymentService, at time.Time) error {
	t.Helper()
	tx, err := s.entClient.Tx(context.Background())
	require.NoError(t, err)
	defer tx.Rollback()
	return s.lockBillAmountReservation(context.Background(), tx, "2088000000000001", 10000, at)
}

func billAmountSuccessfulSyncFixture(t *testing.T, s *PaymentService, through time.Time) {
	t.Helper()
	_, err := s.entClient.ExecContext(context.Background(), `INSERT INTO payment_bill_sync_state(receiver_id,cursor_at,lease_until,lease_owner,error_code,updated_at) VALUES($1,$2,$3,'fixture','',$2) ON CONFLICT(receiver_id) DO UPDATE SET cursor_at=$2,error_code='',updated_at=$2`, "2088000000000001", through, time.Unix(0, 0).UTC())
	require.NoError(t, err)
}

func TestBillAmountCancelledLocalReleasesWithoutStatementSync(t *testing.T) {
	s, id := newBillReceiptTestService(t)
	ctx := context.Background()
	start := time.Now().UTC().Truncate(time.Second)
	o := matchingBillLocalOrder(t, s, id, "CANCELLED_RELEASE", start)
	_, err := s.entClient.PaymentOrder.UpdateOneID(o.ID).SetStatus(OrderStatusCancelled).Save(ctx)
	require.NoError(t, err)
	err = billAmountReservationCheckAt(t, s, start.Add(30*time.Second))
	require.NoError(t, err)
	require.NoError(t, billAmountReservationCheckAt(t, s, o.ExpiresAt))
	billAmountSuccessfulSyncFixture(t, s, o.ExpiresAt.Add(-time.Second))
	require.NoError(t, billAmountReservationCheckAt(t, s, o.ExpiresAt))
	billAmountSuccessfulSyncFixture(t, s, o.ExpiresAt)
	require.NoError(t, billAmountReservationCheckAt(t, s, o.ExpiresAt), "cancelled unpaid local order should not require another two minutes")
	rows, err := s.entClient.QueryContext(ctx, `SELECT created_at,expires_at,reuse_after FROM payment_bill_amount_reservations WHERE order_id=$1`, o.ID)
	require.NoError(t, err)
	defer rows.Close()
	require.True(t, rows.Next())
	var created, expiry, reuse time.Time
	require.NoError(t, rows.Scan(&created, &expiry, &reuse))
	require.True(t, created.Equal(start))
	require.True(t, expiry.Equal(o.ExpiresAt))
	require.True(t, reuse.Equal(o.ExpiresAt.Add(2*time.Minute)), "recorded original policy/history must remain unchanged")
}

func TestBillAmountCancelledReceiptStaysInReviewWithoutBlockingReuse(t *testing.T) {
	s, id := newBillReceiptTestService(t)
	ctx := context.Background()
	start := time.Now().UTC().Truncate(time.Second).Add(-2 * time.Minute)
	o := matchingBillLocalOrder(t, s, id, "CANCELLED_RECEIPT", start)
	_, err := s.entClient.PaymentOrder.UpdateOneID(o.ID).SetStatus(OrderStatusCancelled).Save(ctx)
	require.NoError(t, err)
	receipt, _, err := s.storeBillReceipt(ctx, id, "2088000000000001", provider.AlipayBillEntry{AccountLogID: "CANCELLED_LOG", AlipayOrderNo: "CANCELLED_TRADE", Amount: "100.00", Direction: "收入", TransAt: start.Add(20 * time.Second)})
	require.NoError(t, err)
	now := time.Now().UTC().Truncate(time.Second)
	billAmountSuccessfulSyncFixture(t, s, now)
	require.NoError(t, billAmountReservationCheckAt(t, s, now))
	matched, err := s.reconcileBillReceiptThrough(ctx, receipt, now)
	require.NoError(t, err)
	require.False(t, matched, "cancelled payment must not auto-credit a replacement order")
	page, err := s.ListAlipayBillReceipts(ctx, id, 1, 20, "review")
	require.NoError(t, err)
	require.Nil(t, page.Items[0].OrderID)
	_, err = s.entClient.ExecContext(ctx, `UPDATE payment_bill_receipts SET status='ignored' WHERE id=$1`, receipt.ID)
	require.NoError(t, err)
	require.NoError(t, billAmountReservationCheckAt(t, s, now), "review resolution releases the cancellation hold")
}

func TestBillAmountCancelledExceptionDoesNotHideOtherReservation(t *testing.T) {
	s, id := newBillReceiptTestService(t)
	ctx := context.Background()
	start := time.Now().UTC().Truncate(time.Second)
	matchingBillLocalOrder(t, s, id, "ORDINARY_BLOCKER", start)
	cancelled := matchingBillLocalOrder(t, s, id, "NEWER_CANCELLED", start.Add(10*time.Second))
	_, err := s.entClient.PaymentOrder.UpdateOneID(cancelled.ID).SetStatus(OrderStatusCancelled).Save(ctx)
	require.NoError(t, err)
	err = billAmountReservationCheckAt(t, s, start.Add(50*time.Second))
	require.Equal(t, "ALIPAY_BILL_AMOUNT_BUSY", infraerrors.Reason(err))
	require.Equal(t, "10", infraerrors.FromError(err).Metadata["retry_after_seconds"], "the remaining active order must retain its own deadline")
}

func createBillAmountLocalFixture(ctx context.Context, s *PaymentService, id int64, cfg map[string]string, user *dbent.User, amount float64) (*dbent.PaymentOrder, error) {
	return s.createOrderInTx(ctx, CreateOrderRequest{UserID: user.ID, PaymentType: "alipay", OrderType: "balance", ClientIP: "127.0.0.1", SrcHost: "fixture.invalid"},
		&User{ID: user.ID, Email: user.Email, Username: user.Username}, nil,
		&PaymentConfig{MaxPendingOrders: 100, OrderTimeoutMin: 30}, amount, amount, 0, amount, 0,
		&payment.InstanceSelection{InstanceID: strconv.FormatInt(id, 10), ProviderKey: payment.TypeAlipayBill, Config: cfg, PaymentMode: "qrcode"})
}

func TestBillAmountReuseAtExpiryIgnoresStatementLatency(t *testing.T) {
	s, id, _ := newBuiltinEasyPayServiceFixture(t, "https://merchant.example")
	ctx := context.Background()
	_, err := s.CreateBuiltinEasyPayOrder(ctx, builtinEasyPayTestParams(id, "https://merchant.example"))
	require.NoError(t, err)
	order, err := findBuiltinEasyPayOrder(ctx, s.entClient, id, "MERCHANT_ORDER_1", "")
	require.NoError(t, err)
	check := func(at time.Time) error {
		tx, err := s.entClient.Tx(ctx)
		require.NoError(t, err)
		defer tx.Rollback()
		return s.lockBillAmountReservation(ctx, tx, order.ReceiverID, order.AmountMinor, at)
	}
	err = check(order.ExpiresAt.Add(-time.Second))
	require.Equal(t, "ALIPAY_BILL_AMOUNT_BUSY", infraerrors.Reason(err))
	require.Equal(t, "1", infraerrors.FromError(err).Metadata["retry_after_seconds"])
	require.NoError(t, check(order.ExpiresAt))
	_, err = s.entClient.ExecContext(ctx, `INSERT INTO payment_bill_sync_state(receiver_id,cursor_at,lease_until,lease_owner,error_code,updated_at) VALUES($1,$2,$3,'fixture','sync_failed',$2)`, order.ReceiverID, order.ExpiresAt.Add(-3*time.Minute), time.Unix(0, 0).UTC())
	require.NoError(t, err)
	require.NoError(t, check(order.ExpiresAt.Add(time.Second)), "failed or stale statement sync must not keep an expired amount locked")
}

func TestBillAmountLegacyOrderPreventsOverlappingNewCheckout(t *testing.T) {
	s, id, _ := newBuiltinEasyPayServiceFixture(t, "https://merchant.example")
	old := billTestOrder(t, s, id, "LEGACY_AMOUNT")
	_, err := s.CreateBuiltinEasyPayOrder(context.Background(), builtinEasyPayTestParams(id, "https://merchant.example"))
	require.Equal(t, "ALIPAY_BILL_AMOUNT_BUSY", infraerrors.Reason(err))
	require.Greater(t, old.ExpiresAt.Sub(time.Now()), time.Minute)
	rows, err := s.entClient.QueryContext(context.Background(), `SELECT COUNT(*) FROM payment_bill_amount_reservations`)
	require.NoError(t, err)
	defer rows.Close()
	require.True(t, rows.Next())
	var count int
	require.NoError(t, rows.Scan(&count))
	require.Zero(t, count, "legacy history must not be opted into amount matching")
}

func TestBillAmountPolicyLeavesOrdinaryProviderTimeoutUnchanged(t *testing.T) {
	s, _, _ := newBuiltinEasyPayServiceFixture(t, "https://merchant.example")
	ctx := context.Background()
	user, err := s.entClient.User.Create().SetEmail("ordinary-timeout@test.invalid").SetPasswordHash("fixture").SetUsername("ordinary").Save(ctx)
	require.NoError(t, err)
	before := time.Now().UTC()
	order, err := s.createOrderInTx(ctx,
		CreateOrderRequest{UserID: user.ID, PaymentType: "alipay", OrderType: "balance", ClientIP: "127.0.0.1", SrcHost: "fixture.invalid"},
		&User{ID: user.ID, Email: user.Email, Username: user.Username}, nil,
		&PaymentConfig{MaxPendingOrders: 10, OrderTimeoutMin: 30}, 100, 100, 0, 100, 0,
		&payment.InstanceSelection{InstanceID: "1", ProviderKey: payment.TypeAlipay, Config: map[string]string{"appId": "ordinary"}})
	require.NoError(t, err)
	require.WithinDuration(t, before.Add(30*time.Minute), order.ExpiresAt, time.Second)
	require.NotContains(t, order.ProviderSnapshot, "bill_amount_matching")
}

func TestBillAmountSameAmountDifferentReceiverIsIndependent(t *testing.T) {
	s, id, cfg := newBuiltinEasyPayServiceFixture(t, "https://merchant.example")
	ctx := context.Background()
	_, err := s.CreateBuiltinEasyPayOrder(ctx, builtinEasyPayTestParams(id, "https://merchant.example"))
	require.NoError(t, err)
	cfg["receiverId"] = "2088000000000002"
	stored, err := s.configService.encryptProviderConfig(payment.TypeAlipayBill, cfg)
	require.NoError(t, err)
	second, err := s.entClient.PaymentProviderInstance.Create().SetProviderKey(payment.TypeAlipayBill).SetName("Other receiver").SetEnabled(true).SetConfig(stored).SetSupportedTypes("alipay").SetPaymentMode("qrcode").Save(ctx)
	require.NoError(t, err)
	_, err = s.CreateBuiltinEasyPayOrder(ctx, builtinEasyPayTestParams(second.ID, "https://merchant.example"))
	require.NoError(t, err)
}

func TestBillAmountCompletedPaymentKeepsReservationAndResponseDeadline(t *testing.T) {
	s, id, _ := newBuiltinEasyPayServiceFixture(t, "https://merchant.example")
	ctx := context.Background()
	params := builtinEasyPayTestParams(id, "https://merchant.example")
	created, err := s.CreateBuiltinEasyPayOrder(ctx, params)
	require.NoError(t, err)
	var response map[string]any
	data, err := json.Marshal(created)
	require.NoError(t, err)
	require.NoError(t, json.Unmarshal(data, &response))
	parsed, err := time.Parse(time.RFC3339Nano, response["expires_at"].(string))
	require.NoError(t, err)
	require.True(t, parsed.Equal(created.ExpiresAt))
	_, err = s.entClient.ExecContext(ctx, `UPDATE payment_epay_orders SET status='PAID',paid_at=$1,alipay_order_no='SETTLED_FIXTURE' WHERE trade_no=$2`, time.Now().UTC(), created.TradeNo)
	require.NoError(t, err)
	params["out_trade_no"] = "NEXT_PAYMENT"
	params["sign"] = epay.Sign(params, builtinEasyPayTestKey)
	_, err = s.CreateBuiltinEasyPayOrder(ctx, params)
	require.NoError(t, err, "a confirmed payment must release the amount without another sync")
}
