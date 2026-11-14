//go:build unit

package service

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/internal/payment"
	"github.com/Wei-Shaw/sub2api/internal/payment/epay"
	"github.com/Wei-Shaw/sub2api/internal/payment/provider"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/google/uuid"
	"github.com/lib/pq"
	"github.com/stretchr/testify/require"
)

// A separate schema lets each test use the real migrations without sharing
// fixtures. The external runner still must supply a disposable database.
func newBillAmountPostgresService(t *testing.T) (*PaymentService, int64) {
	t.Helper()
	dsn := os.Getenv("ALIPAY_BILL_TEST_DSN")
	if dsn == "" {
		t.Skip("requires isolated ALIPAY_BILL_TEST_DSN")
	}
	if strings.HasPrefix(dsn, "postgres://") || strings.HasPrefix(dsn, "postgresql://") {
		var err error
		dsn, err = pq.ParseURL(dsn)
		require.NoError(t, err)
	}
	admin, err := sql.Open("postgres", dsn)
	require.NoError(t, err)
	var database string
	require.NoError(t, admin.QueryRow(`SELECT current_database()`).Scan(&database))
	require.True(t, strings.HasPrefix(database, "sub2api_billtest_"), "isolated database required")
	schema := "bill_amount_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	_, err = admin.Exec(`CREATE SCHEMA ` + pq.QuoteIdentifier(schema))
	require.NoError(t, err)
	db, err := sql.Open("postgres", dsn+" search_path="+schema)
	require.NoError(t, err)
	db.SetMaxOpenConns(8)
	ctx := context.Background()
	client := dbent.NewClient(dbent.Driver(entsql.OpenDB(dialect.Postgres, db)))
	t.Cleanup(func() {
		_ = client.Close()
		_, _ = admin.Exec(`DROP SCHEMA ` + pq.QuoteIdentifier(schema) + ` CASCADE`)
		_ = admin.Close()
	})
	require.NoError(t, client.Schema.Create(ctx))
	for _, name := range []string{"282_payment_bill_receipts.sql", "283_payment_epay_orders.sql", "284_payment_bill_amount_reservations.sql", "285_payment_bill_amount_closure.sql", "286_payment_bill_user_cancel_release.sql", "290_payment_bill_amount_no_cooldown.sql"} {
		migration, err := os.ReadFile(filepath.Join("..", "..", "migrations", name))
		require.NoError(t, err)
		_, err = db.Exec(string(migration))
		require.NoError(t, err)
	}
	service := &PaymentService{entClient: client, configService: NewPaymentConfigService(client, nil, nil)}
	service.configService.encryptionKey = []byte("0123456789abcdef0123456789abcdef")
	service.configService.settingRepo = &paymentConfigSettingRepoStub{values: map[string]string{SettingPaymentEnabled: "true", SettingOrderTimeoutMinutes: "30"}}
	cfg := map[string]string{"appId": "app-fixture", "receiverId": "2088000000000001", "privateKey": "fixture-private", "publicKey": "fixture-public", "epayEnabled": "true", "epayKey": builtinEasyPayTestKey, "epayNotifyOrigins": `["https://merchant.example"]`,
		"qrRules": `[{"id":"hundred","amount":"100.00","qr_url":"https://qr.alipay.com/hundred","enabled":true},{"id":"fifty","amount":"50.00","qr_url":"https://qr.alipay.com/fifty","enabled":true}]`}
	stored, err := service.configService.encryptProviderConfig(payment.TypeAlipayBill, cfg)
	require.NoError(t, err)
	inst, err := client.PaymentProviderInstance.Create().SetProviderKey(payment.TypeAlipayBill).SetName("Reservation fixture").SetConfig(stored).SetEnabled(true).SetSupportedTypes("alipay").SetPaymentMode("qrcode").Save(ctx)
	require.NoError(t, err)
	return service, inst.ID
}

func TestBillPostgresAmountConcurrentLocalAndExternalAcrossProviders(t *testing.T) {
	s, id := newBillAmountPostgresService(t)
	ctx := context.Background()
	inst, cfg, err := s.billProviderConfig(ctx, id)
	require.NoError(t, err)
	second, err := s.entClient.PaymentProviderInstance.Create().SetProviderKey(payment.TypeAlipayBill).SetName("Same receiver").SetConfig(inst.Config).SetEnabled(true).SetSupportedTypes("alipay").SetPaymentMode("qrcode").Save(ctx)
	require.NoError(t, err)
	user, err := s.entClient.User.Create().SetEmail("concurrent-amount@test.invalid").SetUsername("concurrent").SetPasswordHash("fixture").Save(ctx)
	require.NoError(t, err)
	gate := make(chan struct{})
	results := make(chan error, 2)
	go func() {
		<-gate
		_, err := createBillAmountLocalFixture(ctx, s, id, cfg, user, 100)
		results <- err
	}()
	go func() {
		<-gate
		_, err := s.CreateBuiltinEasyPayOrder(ctx, builtinEasyPayTestParams(second.ID, "https://merchant.example"))
		results <- err
	}()
	close(gate)
	success, busy := 0, 0
	for i := 0; i < 2; i++ {
		err := <-results
		if err == nil {
			success++
		} else if infraerrors.Reason(err) == "ALIPAY_BILL_AMOUNT_BUSY" {
			busy++
		} else {
			t.Fatalf("unexpected concurrent result: %v", err)
		}
	}
	require.Equal(t, 1, success)
	require.Equal(t, 1, busy)
	// The lock is scoped by exact amount, not provider or the whole receiver.
	params := builtinEasyPayTestParams(second.ID, "https://merchant.example")
	params["out_trade_no"], params["money"] = "OTHER_AMOUNT", "50.00"
	params["sign"] = epay.Sign(params, builtinEasyPayTestKey)
	_, err = s.CreateBuiltinEasyPayOrder(ctx, params)
	require.NoError(t, err)
}

func TestBillPostgresAmountRollbackDoesNotConsumeSlot(t *testing.T) {
	s, id := newBillAmountPostgresService(t)
	ctx := context.Background()
	user, err := s.entClient.User.Create().SetEmail("rollback@test.invalid").SetUsername("rollback").SetPasswordHash("fixture").Save(ctx)
	require.NoError(t, err)
	tx, err := s.entClient.Tx(ctx)
	require.NoError(t, err)
	now := time.Now().UTC()
	require.NoError(t, s.lockBillAmountReservation(ctx, tx, "2088000000000001", 10000, now))
	order, err := tx.PaymentOrder.Create().SetUserID(user.ID).SetUserEmail(user.Email).SetUserName(user.Username).SetAmount(100).SetPayAmount(100).SetRechargeCode("ROLLBACK").SetOutTradeNo("ROLLBACK").SetPaymentType("alipay").SetPaymentTradeNo("").SetCreatedAt(now).SetExpiresAt(now.Add(time.Minute)).SetClientIP("127.0.0.1").SetSrcHost("fixture.invalid").Save(ctx)
	require.NoError(t, err)
	require.NoError(t, s.insertBillAmountReservation(ctx, tx, id, "2088000000000001", 10000, order.ID, 0, now, now.Add(time.Minute)))
	require.NoError(t, tx.Rollback())
	_, err = s.CreateBuiltinEasyPayOrder(ctx, builtinEasyPayTestParams(id, "https://merchant.example"))
	require.NoError(t, err, "rollback releases transaction lock and does not persist a reservation")
}

func TestBillPostgresAmountConcurrentDelayedReceiptClaimIsIdempotent(t *testing.T) {
	s, id := newBillAmountPostgresService(t)
	ctx := context.Background()
	start := time.Now().UTC().Truncate(time.Second).Add(-2 * time.Minute)
	order := matchingBillLocalOrder(t, s, id, "PG_DELAYED_MATCH", start)
	_, err := s.entClient.PaymentOrder.UpdateOneID(order.ID).SetStatus(OrderStatusExpired).Save(ctx)
	require.NoError(t, err)
	receipt, _, err := s.storeBillReceipt(ctx, id, "2088000000000001", provider.AlipayBillEntry{
		AccountLogID: "PG_DELAYED_LOG", AlipayOrderNo: "PG_DELAYED_TRADE", MerchantOrderNo: "STATIC_CODE_REFERENCE", Amount: "100.00", Direction: "收入", TransAt: start.Add(20 * time.Second),
	})
	require.NoError(t, err)
	claim := func() error {
		return s.claimBillReceiptWithMode(ctx, receipt.ReceiverID, receipt.ID, order.ID, "system", "PG reserved amount fixture", billClaimReservedAmount, time.Now().UTC())
	}
	gate := make(chan struct{})
	results := make(chan error, 2)
	for i := 0; i < 2; i++ {
		go func() { <-gate; results <- claim() }()
	}
	close(gate)
	successes := 0
	for i := 0; i < 2; i++ {
		err := <-results
		if err == nil {
			successes++
		} else {
			require.Contains(t, []string{"ALIPAY_BILL_MATCH_REJECTED", "ALIPAY_BILL_ALREADY_CLAIMED"}, infraerrors.Reason(err))
		}
	}
	require.GreaterOrEqual(t, successes, 1)
	require.NoError(t, claim(), "retry after the committed claim must be idempotent")
	order, err = s.entClient.PaymentOrder.Get(ctx, order.ID)
	require.NoError(t, err)
	require.Equal(t, OrderStatusPaid, order.Status, "payment in the original window remains valid when the bill arrives after expiry")
	require.Equal(t, receipt.AlipayOrderNo, order.PaymentTradeNo)
	rows, err := s.entClient.QueryContext(ctx, `SELECT COUNT(*) FROM payment_bill_claims WHERE order_id=$1`, order.ID)
	require.NoError(t, err)
	defer rows.Close()
	require.True(t, rows.Next())
	var count int
	require.NoError(t, rows.Scan(&count))
	require.Equal(t, 1, count)
}

func TestBillPostgresAmountUncertainReceiptsRemainReview(t *testing.T) {
	for _, kind := range []string{"wrong_amount", "late", "multiple"} {
		t.Run(kind, func(t *testing.T) {
			s, id := newBillAmountPostgresService(t)
			ctx := context.Background()
			start := time.Now().UTC().Truncate(time.Second).Add(-2 * time.Minute)
			order := matchingBillLocalOrder(t, s, id, "PG_REVIEW_"+kind, start)
			entry := provider.AlipayBillEntry{AccountLogID: "PG_REVIEW_LOG", AlipayOrderNo: "PG_REVIEW_TRADE", Amount: "100.00", Direction: "收入", TransAt: start.Add(20 * time.Second)}
			if kind == "wrong_amount" {
				entry.Amount = "100.01"
			}
			if kind == "late" {
				entry.TransAt = order.ExpiresAt
			}
			receipt, _, err := s.storeBillReceipt(ctx, id, "2088000000000001", entry)
			require.NoError(t, err)
			receipts := []*PaymentBillReceipt{receipt}
			if kind == "multiple" {
				entry.AccountLogID, entry.AlipayOrderNo = "PG_SECOND_LOG", "PG_SECOND_TRADE"
				entry.TransAt = entry.TransAt.Add(time.Second)
				other, _, err := s.storeBillReceipt(ctx, id, "2088000000000001", entry)
				require.NoError(t, err)
				receipts = append(receipts, other)
			}
			for _, receipt := range receipts {
				matched, err := s.reconcileBillReceiptThrough(ctx, receipt, time.Now().UTC())
				require.NoError(t, err)
				require.False(t, matched)
			}
			page, err := s.ListAlipayBillReceipts(ctx, id, 1, 20, "review")
			require.NoError(t, err)
			require.Len(t, page.Items, len(receipts))
			for _, item := range page.Items {
				if kind == "multiple" {
					require.Equal(t, "amount_receipts_ambiguous", item.ReviewReason)
				} else {
					require.Equal(t, "missing_order_reference", item.ReviewReason)
				}
			}
			order, err = s.entClient.PaymentOrder.Get(ctx, order.ID)
			require.NoError(t, err)
			require.Equal(t, OrderStatusPending, order.Status)
		})
	}
}

func TestBillPostgresAmountCancelledSlotHasSingleNextOwner(t *testing.T) {
	s, id := newBillAmountPostgresService(t)
	ctx := context.Background()
	_, cfg, err := s.billProviderConfig(ctx, id)
	require.NoError(t, err)
	now := time.Now().UTC().Truncate(time.Second)
	start := now.Add(-90 * time.Second)
	old := matchingBillLocalOrder(t, s, id, "CANCELLED_PREVIOUS_OWNER", start)
	_, err = s.entClient.PaymentOrder.UpdateOneID(old.ID).SetStatus(OrderStatusCancelled).Save(ctx)
	require.NoError(t, err)
	billAmountSuccessfulSyncFixture(t, s, now)
	a, err := s.entClient.User.Create().SetEmail("next-a@test.invalid").SetUsername("next-a").SetPasswordHash("fixture").Save(ctx)
	require.NoError(t, err)
	b, err := s.entClient.User.Create().SetEmail("next-b@test.invalid").SetUsername("next-b").SetPasswordHash("fixture").Save(ctx)
	require.NoError(t, err)
	gate := make(chan struct{})
	results := make(chan error, 2)
	for _, u := range []*dbent.User{a, b} {
		go func(user *dbent.User) {
			<-gate
			_, e := createBillAmountLocalFixture(ctx, s, id, cfg, user, 100)
			results <- e
		}(u)
	}
	close(gate)
	success, busy := 0, 0
	for i := 0; i < 2; i++ {
		e := <-results
		if e == nil {
			success++
		} else {
			require.Equal(t, "ALIPAY_BILL_AMOUNT_BUSY", infraerrors.Reason(e))
			busy++
		}
	}
	require.Equal(t, 1, success)
	require.Equal(t, 1, busy)
	// Late statement visibility must keep the old transaction in its original
	// cancelled window rather than credit whichever user obtained the new slot.
	r, _, err := s.storeBillReceipt(ctx, id, "2088000000000001", provider.AlipayBillEntry{AccountLogID: "CANCELLED_OLD_LOG", AlipayOrderNo: "CANCELLED_OLD_TRADE", Amount: "100.00", Direction: "收入", TransAt: start.Add(20 * time.Second)})
	require.NoError(t, err)
	matched, err := s.reconcileBillReceiptThrough(ctx, r, now)
	require.NoError(t, err)
	require.False(t, matched)
	page, err := s.ListAlipayBillReceipts(ctx, id, 1, 20, "review")
	require.NoError(t, err)
	require.Len(t, page.Items, 1)
	require.Nil(t, page.Items[0].OrderID)
	require.Equal(t, "amount_window_cancelled", page.Items[0].ReviewReason)
	old, err = s.entClient.PaymentOrder.Get(ctx, old.ID)
	require.NoError(t, err)
	require.Equal(t, OrderStatusCancelled, old.Status)
}
