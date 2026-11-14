//go:build unit

package service

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/internal/payment/provider"
	"github.com/stretchr/testify/require"
)

func TestBillReceiptMatchRequiresOrderIdentity(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	pk, instance := "alipay_bill", "7"
	order := &dbent.PaymentOrder{ID: 11, OutTradeNo: "SITE_ORDER_11", ProviderKey: &pk, ProviderInstanceID: &instance, PayAmount: 100, Status: OrderStatusPending, CreatedAt: now.Add(-time.Minute), ExpiresAt: now.Add(time.Minute), ProviderSnapshot: map[string]any{"provider_key": pk, "receiver_id": "2088000000000001"}}
	receipt := &PaymentBillReceipt{ReceiverID: "2088000000000001", AccountLogID: "LOG1", AlipayOrderNo: "ALI1", Amount: "100.00", TransAt: now}
	require.Equal(t, "missing_order_reference", billReceiptMatchReason(receipt, order, false), "one same-amount pending order does not prove payment ownership")
	receipt.MerchantOrderNo = order.OutTradeNo
	require.Empty(t, billReceiptMatchReason(receipt, order, false))
	receipt.Amount = "100.01"
	require.Equal(t, "amount_mismatch", billReceiptMatchReason(receipt, order, false), "no one-cent tolerance in fixed amount matching")
	receipt.Amount = "100.00"
	receipt.ReceiverID = "2088000000000002"
	require.Equal(t, "receiver_mismatch", billReceiptMatchReason(receipt, order, false))
	receipt.ReceiverID = "2088000000000001"
	order.Status = OrderStatusExpired
	require.Equal(t, "expired_order", billReceiptMatchReason(receipt, order, false))
	require.Empty(t, billReceiptMatchReason(receipt, order, true), "an administrator may explicitly settle an expired order after verifying the receipt")
	receipt.TransAt = order.CreatedAt.Add(-time.Minute)
	require.Equal(t, "outside_order_window", billReceiptMatchReason(receipt, order, true))
	receipt.TransAt = now
	receipt.AlipayOrderNo = ""
	require.Equal(t, "missing_transaction_id", billReceiptMatchReason(receipt, order, true))
}

type billQueryFixture func(context.Context, time.Time, time.Time, int) (*provider.AlipayBillPage, error)

func (f billQueryFixture) QueryBills(ctx context.Context, start, end time.Time, page int) (*provider.AlipayBillPage, error) {
	return f(ctx, start, end, page)
}

func TestBillSyncPersistsAmbiguousIncomeWithoutChoosingCustomer(t *testing.T) {
	s, inst := newBillReceiptTestService(t)
	ctx := context.Background()
	a := billTestOrder(t, s, inst, "CUSTOMER_A")
	b := billTestOrder(t, s, inst, "CUSTOMER_B")
	entry := provider.AlipayBillEntry{AccountLogID: "REAL_FORMAT_LOG", AlipayOrderNo: "REAL_FORMAT_TRADE", Amount: "100.00", Direction: "收入", TransAt: time.Now().UTC().Truncate(time.Second).Add(-time.Second)}
	reader := billQueryFixture(func(_ context.Context, start, end time.Time, page int) (*provider.AlipayBillPage, error) {
		result := &provider.AlipayBillPage{Page: page}
		if !entry.TransAt.Before(start) && entry.TransAt.Before(end) {
			result.Items = []provider.AlipayBillEntry{entry}
			result.Total = 1
		}
		return result, nil
	})
	result, err := s.syncAlipayBillsWithReader(ctx, inst, "2088000000000001", reader)
	require.NoError(t, err)
	require.Equal(t, 1, result.Inserted)
	require.Zero(t, result.Matched)
	for _, id := range []int64{a.ID, b.ID} {
		order, err := s.entClient.PaymentOrder.Get(ctx, id)
		require.NoError(t, err)
		require.Equal(t, OrderStatusPending, order.Status)
	}
	page, err := s.ListAlipayBillReceipts(ctx, inst, 1, 20, "review")
	require.NoError(t, err)
	require.Equal(t, 1, page.Total)
	require.Equal(t, "missing_order_reference", page.Items[0].ReviewReason)
	result, err = s.syncAlipayBillsWithReader(ctx, inst, "2088000000000001", reader)
	require.NoError(t, err)
	require.Zero(t, result.Inserted)
}

func TestBillSyncCheckpointSurvivesLaterQueryFailure(t *testing.T) {
	s, inst := newBillReceiptTestService(t)
	ctx := context.Background()
	var completed time.Time
	calls := 0
	reader := billQueryFixture(func(_ context.Context, start, end time.Time, page int) (*provider.AlipayBillPage, error) {
		calls++
		if calls == 1 {
			completed = end
			return &provider.AlipayBillPage{Page: page}, nil
		}
		return nil, errors.New("test upstream unavailable")
	})
	_, err := s.syncAlipayBillsWithReader(ctx, inst, "2088000000000001", reader)
	require.Error(t, err)
	require.Equal(t, 2, calls)
	cursor, err := s.acquireBillSyncLease(ctx, "2088000000000001", "check", time.Now().UTC())
	require.NoError(t, err)
	require.True(t, completed.Equal(cursor))
}

func TestBillProviderResponseNeverReturnsStoredKeys(t *testing.T) {
	s, _ := newBillReceiptTestService(t)
	response, err := s.configService.ProviderInstanceResponseForAdmin(&dbent.PaymentProviderInstance{ID: 1, ProviderKey: "alipay_bill", SupportedTypes: "alipay", Config: `{"receiverId":"2088000000000001","privateKey":"private","publicKey":"verify","appAuthToken":"token"}`})
	require.NoError(t, err)
	require.Equal(t, "2088000000000001", response.Config["receiverId"])
	require.NotContains(t, response.Config, "privateKey")
	require.NotContains(t, response.Config, "publicKey")
	require.NotContains(t, response.Config, "appAuthToken")
}

func newBillReceiptTestService(t *testing.T) (*PaymentService, int64) {
	t.Helper()
	client := newPaymentConfigServiceTestClient(t)
	migration, err := os.ReadFile(filepath.Join("..", "..", "migrations", "282_payment_bill_receipts.sql"))
	require.NoError(t, err)
	schema := strings.ReplaceAll(string(migration), "BIGSERIAL PRIMARY KEY", "INTEGER PRIMARY KEY AUTOINCREMENT")
	schema = strings.ReplaceAll(schema, "TIMESTAMPTZ", "DATETIME")
	schema = strings.ReplaceAll(schema, "NOW()", "CURRENT_TIMESTAMP")
	_, err = client.ExecContext(context.Background(), schema)
	require.NoError(t, err)
	applyBuiltinEasyPayTestSchema(t, client)
	amountSchema, err := os.ReadFile(filepath.Join("..", "..", "migrations", "284_payment_bill_amount_reservations.sql"))
	require.NoError(t, err)
	amountSQL := strings.ReplaceAll(string(amountSchema), "BIGSERIAL PRIMARY KEY", "INTEGER PRIMARY KEY AUTOINCREMENT")
	amountSQL = strings.ReplaceAll(amountSQL, "TIMESTAMPTZ", "DATETIME")
	amountSQL = strings.ReplaceAll(amountSQL, "NOW()", "CURRENT_TIMESTAMP")
	// Apply migration 290's deadline constraint to the SQLite fixture.
	amountSQL = strings.ReplaceAll(amountSQL, "reuse_after > expires_at", "reuse_after >= expires_at")
	_, err = client.ExecContext(context.Background(), amountSQL)
	require.NoError(t, err)
	closureSchema, err := os.ReadFile(filepath.Join("..", "..", "migrations", "285_payment_bill_amount_closure.sql"))
	require.NoError(t, err)
	_, err = client.ExecContext(context.Background(), strings.ReplaceAll(string(closureSchema), "TIMESTAMPTZ", "DATETIME"))
	require.NoError(t, err)
	userCancelSchema, err := os.ReadFile(filepath.Join("..", "..", "migrations", "286_payment_bill_user_cancel_release.sql"))
	require.NoError(t, err)
	_, err = client.ExecContext(context.Background(), strings.ReplaceAll(string(userCancelSchema), "TIMESTAMPTZ", "DATETIME"))
	require.NoError(t, err)
	inst, err := client.PaymentProviderInstance.Create().SetProviderKey("alipay_bill").SetName("Bill test").SetConfig(`{"receiverId":"2088000000000001"}`).Save(context.Background())
	require.NoError(t, err)
	return &PaymentService{entClient: client, configService: NewPaymentConfigService(client, nil, nil)}, inst.ID
}

func billTestOrder(t *testing.T, s *PaymentService, inst int64, name string) *dbent.PaymentOrder {
	t.Helper()
	ctx := context.Background()
	user, err := s.entClient.User.Create().SetEmail(name + "@test.invalid").SetPasswordHash("not-a-real-hash").SetUsername(name).Save(ctx)
	require.NoError(t, err)
	o, err := s.entClient.PaymentOrder.Create().SetUserID(user.ID).SetUserEmail(user.Email).SetUserName(name).SetAmount(100).SetPayAmount(100).SetRechargeCode(name).SetOutTradeNo(name).SetPaymentType("alipay").SetPaymentTradeNo("").SetProviderKey("alipay_bill").SetProviderInstanceID(strconv.FormatInt(inst, 10)).SetProviderSnapshot(map[string]any{"receiver_id": "2088000000000001", "provider_key": "alipay_bill", "currency": "CNY"}).SetCreatedAt(time.Now().Add(-time.Minute)).SetExpiresAt(time.Now().Add(time.Hour)).SetClientIP("127.0.0.1").SetSrcHost("test.invalid").Save(ctx)
	require.NoError(t, err)
	return o
}

func TestBillReceiptStoreAndClaimsAreTransactionWide(t *testing.T) {
	s, inst := newBillReceiptTestService(t)
	ctx := context.Background()
	first := billTestOrder(t, s, inst, "FIRST")
	second := billTestOrder(t, s, inst, "SECOND")
	entry := provider.AlipayBillEntry{AccountLogID: "LOG1", AlipayOrderNo: "ALI1", MerchantOrderNo: first.OutTradeNo, Amount: "100.00", Direction: "收入", TransAt: time.Now().Truncate(time.Second)}
	r, inserted, err := s.storeBillReceipt(ctx, inst, "2088000000000001", entry)
	require.NoError(t, err)
	require.True(t, inserted)
	other, inserted, err := s.storeBillReceipt(ctx, inst, "2088000000000001", entry)
	require.NoError(t, err)
	require.False(t, inserted)
	require.Equal(t, r.ID, other.ID)
	require.NoError(t, s.claimBillReceipt(ctx, r.ReceiverID, r.ID, first.ID, "admin:1", "verified transfer", true))
	require.NoError(t, s.claimBillReceipt(ctx, r.ReceiverID, r.ID, first.ID, "admin:1", "retry", true))
	require.Error(t, s.claimBillReceipt(ctx, r.ReceiverID, r.ID, second.ID, "admin:1", "wrong order", true))
	entry.AccountLogID = "LOG2" // A second accounting row for the very same transaction.
	row2, inserted, err := s.storeBillReceipt(ctx, inst, "2088000000000001", entry)
	require.NoError(t, err)
	require.True(t, inserted)
	require.Error(t, s.claimBillReceipt(ctx, row2.ReceiverID, row2.ID, second.ID, "admin:1", "duplicate transaction", true))
	a, err := s.entClient.PaymentOrder.Get(ctx, first.ID)
	require.NoError(t, err)
	require.Equal(t, OrderStatusPaid, a.Status)
	b, err := s.entClient.PaymentOrder.Get(ctx, second.ID)
	require.NoError(t, err)
	require.Equal(t, OrderStatusPending, b.Status)
	logs, err := s.entClient.PaymentAuditLog.Query().All(ctx)
	require.NoError(t, err)
	require.Len(t, logs, 1)
}

func TestBillReceiptStoreDeduplicatesAcrossProviderInstances(t *testing.T) {
	s, inst := newBillReceiptTestService(t)
	ctx := context.Background()
	other, err := s.entClient.PaymentProviderInstance.Create().SetProviderKey("alipay_bill").SetName("Same receiver").SetConfig(`{"receiverId":"2088000000000001"}`).Save(ctx)
	require.NoError(t, err)
	entry := provider.AlipayBillEntry{AccountLogID: "LOG_SHARED", AlipayOrderNo: "ALI_SHARED", Amount: "100.00", Direction: "收入", TransAt: time.Now()}
	a, inserted, err := s.storeBillReceipt(ctx, inst, "2088000000000001", entry)
	require.NoError(t, err)
	require.True(t, inserted)
	b, inserted, err := s.storeBillReceipt(ctx, other.ID, "2088000000000001", entry)
	require.NoError(t, err)
	require.False(t, inserted)
	require.Equal(t, a.ID, b.ID)
	entry.Amount = "-100.00"
	entry.Direction = "支出"
	entry.AccountLogID = "OUTGOING"
	r, _, err := s.storeBillReceipt(ctx, inst, "2088000000000001", entry)
	require.NoError(t, err)
	require.Nil(t, r)
}

func TestBillReceiptMatchDoesNotUseForeignOrderReference(t *testing.T) {
	now := time.Now().UTC()
	pk := "alipay_bill"
	order := &dbent.PaymentOrder{ID: 12, OutTradeNo: "RIGHT_ORDER", ProviderKey: &pk, PayAmount: 10, Status: OrderStatusPending, CreatedAt: now.Add(-time.Minute), ExpiresAt: now.Add(time.Minute), ProviderSnapshot: map[string]any{"provider_key": pk, "receiver_id": "2088000000000001"}}
	r := &PaymentBillReceipt{ReceiverID: "2088000000000001", AlipayOrderNo: "ALI1", MerchantOrderNo: "OTHER_ORDER", Amount: "10.00", TransAt: now}
	require.Equal(t, "no_matching_order", billReceiptMatchReason(r, order, false))
	require.Empty(t, billReceiptMatchReason(r, order, true))
	order.Status = OrderStatusCompleted
	require.Equal(t, "invalid_order_state", billReceiptMatchReason(r, order, true))
}

func TestBillSyncWindowOverlapsWithoutSkippingBacklog(t *testing.T) {
	now := time.Date(2026, 11, 10, 10, 0, 0, 0, time.UTC)
	start, end := billSyncWindow(now, time.Time{})
	require.Equal(t, now.Add(-24*time.Hour), start)
	require.Equal(t, start.Add(time.Hour), end)
	cursor := now.Add(-10 * time.Minute)
	start, end = billSyncWindow(now, cursor)
	require.Equal(t, cursor.Add(-5*time.Minute), start)
	require.Equal(t, now, end)
	start, end = billSyncWindow(now, now.Add(-90*24*time.Hour))
	require.Equal(t, now.Add(-31*24*time.Hour), start)
	require.Equal(t, start.Add(time.Hour), end)
}

func TestBillPageCompletionRejectsMissingRows(t *testing.T) {
	require.NoError(t, validateBillPage(&provider.AlipayBillPage{Page: 1, Total: 0}, 1))
	require.Error(t, validateBillPage(&provider.AlipayBillPage{Page: 2, Total: 1001}, 2))
	require.Error(t, validateBillPage(&provider.AlipayBillPage{Page: 1, Total: 1001, Items: make([]provider.AlipayBillEntry, 900)}, 1))
	require.NoError(t, validateBillPage(&provider.AlipayBillPage{Page: 2, Total: 1001, Items: make([]provider.AlipayBillEntry, 1)}, 2))
}

func TestBillHistoricalRescanUsesBoundedIndependentWindows(t *testing.T) {
	now := time.Date(2026, 11, 10, 12, 0, 0, 0, time.UTC)
	start, end := billRescanWindow(now, time.Time{})
	require.Equal(t, now.Add(-24*time.Hour), start)
	require.Equal(t, time.Hour, end.Sub(start))
	nextStart, nextEnd := billRescanWindow(now.Add(time.Minute), end)
	require.Equal(t, end, nextStart)
	require.Equal(t, time.Hour, nextEnd.Sub(nextStart))
	start, end = billRescanWindow(now, now.Add(-time.Minute))
	require.Equal(t, now, end)
	require.Equal(t, time.Minute, end.Sub(start))
}

func TestBillReceiptLargeUnrelatedIncomeDoesNotBlockSync(t *testing.T) {
	s, inst := newBillReceiptTestService(t)
	r, inserted, err := s.storeBillReceipt(context.Background(), inst, "2088000000000001", provider.AlipayBillEntry{AccountLogID: "BIG_RECEIPT", AlipayOrderNo: "BIG_TRADE", Amount: "1000001.25", Direction: "收入", TransAt: time.Now()})
	require.NoError(t, err)
	require.True(t, inserted)
	require.Equal(t, "1000001.25", r.Amount)
}

func TestBillSyncFailureRetainsCompletedCursor(t *testing.T) {
	s, _ := newBillReceiptTestService(t)
	now := time.Now().UTC().Truncate(time.Second)
	_, err := s.acquireBillSyncLease(context.Background(), "2088000000000001", "owner", now)
	require.NoError(t, err)
	cursor := now.Add(-4 * time.Hour)
	s.releaseBillSyncLease("2088000000000001", "owner", cursor, false)
	stored, err := s.acquireBillSyncLease(context.Background(), "2088000000000001", "next", now.Add(time.Second))
	require.NoError(t, err)
	require.True(t, cursor.Equal(stored))
}

func TestBillReceiptCannotReuseOtherProviderTransaction(t *testing.T) {
	s, inst := newBillReceiptTestService(t)
	ctx := context.Background()
	old := billTestOrder(t, s, inst, "OLD_OFFICIAL")
	_, err := s.entClient.PaymentOrder.UpdateOneID(old.ID).SetProviderKey("alipay").SetStatus(OrderStatusCompleted).SetPaymentTradeNo("ALREADY_PAID").Save(ctx)
	require.NoError(t, err)
	newOrder := billTestOrder(t, s, inst, "NEW_FIXED")
	r, _, err := s.storeBillReceipt(ctx, inst, "2088000000000001", provider.AlipayBillEntry{AccountLogID: "REUSED_LOG", AlipayOrderNo: "ALREADY_PAID", Amount: "100.00", Direction: "收入", TransAt: time.Now()})
	require.NoError(t, err)
	require.Error(t, s.claimBillReceipt(ctx, r.ReceiverID, r.ID, newOrder.ID, "admin:1", "review", true))
	r, _, err = s.storeBillReceipt(ctx, inst, "2088000000000001", provider.AlipayBillEntry{AccountLogID: "FOREIGN_REF_LOG", AlipayOrderNo: "FRESH_TRADE", MerchantOrderNo: old.OutTradeNo, Amount: "100.00", Direction: "收入", TransAt: time.Now()})
	require.NoError(t, err)
	require.Error(t, s.claimBillReceipt(ctx, r.ReceiverID, r.ID, newOrder.ID, "admin:1", "review", true))
	current, err := s.entClient.PaymentOrder.Get(ctx, newOrder.ID)
	require.NoError(t, err)
	require.Equal(t, OrderStatusPending, current.Status)
}
