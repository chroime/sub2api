//go:build unit

package service

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/internal/payment/provider"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/stretchr/testify/require"
)

// SQLite cannot ALTER COLUMN DROP NOT NULL. Rebuild only the empty test claims
// table to mirror migration 283; the PostgreSQL test runs the actual migration.
func applyBuiltinEasyPayTestSchema(t *testing.T, client *dbent.Client) {
	t.Helper()
	ctx := context.Background()
	migration, err := os.ReadFile(filepath.Join("..", "..", "migrations", "283_payment_epay_orders.sql"))
	require.NoError(t, err)
	creation := strings.SplitN(string(migration), "ALTER TABLE payment_bill_receipts", 2)[0]
	creation = strings.ReplaceAll(creation, "BIGSERIAL PRIMARY KEY", "INTEGER PRIMARY KEY AUTOINCREMENT")
	creation = strings.ReplaceAll(creation, "TIMESTAMPTZ", "DATETIME")
	creation = strings.ReplaceAll(creation, "NOW()", "CURRENT_TIMESTAMP")
	_, err = client.ExecContext(ctx, creation)
	require.NoError(t, err)
	_, err = client.ExecContext(ctx, `ALTER TABLE payment_bill_receipts ADD COLUMN epay_order_id BIGINT REFERENCES payment_epay_orders(id) ON DELETE RESTRICT`)
	require.NoError(t, err)
	_, err = client.ExecContext(ctx, `DROP TABLE payment_bill_claims`)
	require.NoError(t, err)
	_, err = client.ExecContext(ctx, `CREATE TABLE payment_bill_claims (
		receipt_id BIGINT PRIMARY KEY REFERENCES payment_bill_receipts(id) ON DELETE RESTRICT,
		receiver_id VARCHAR(32) NOT NULL, alipay_order_no VARCHAR(128) NOT NULL CHECK(alipay_order_no<>''),
		order_id BIGINT UNIQUE REFERENCES payment_orders(id) ON DELETE RESTRICT,
		epay_order_id BIGINT UNIQUE REFERENCES payment_epay_orders(id) ON DELETE RESTRICT,
		actor VARCHAR(100) NOT NULL, reason VARCHAR(500) NOT NULL, created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
		UNIQUE(receiver_id,alipay_order_no), CHECK((order_id IS NOT NULL) <> (epay_order_id IS NOT NULL))
	)`)
	require.NoError(t, err)
}

func epayReceiptTestOrder(t *testing.T, s *PaymentService, instanceID int64, tradeNo string) int64 {
	t.Helper()
	now := time.Now().UTC()
	rows, err := s.entClient.QueryContext(context.Background(), `INSERT INTO payment_epay_orders
		(provider_instance_id,out_trade_no,trade_no,amount_minor,receiver_id,app_id,qr_rule_id,qr_url,name,notify_url,callback_key_encrypted,created_at,expires_at,updated_at)
		VALUES($1,$2,$3,10000,'2088000000000001','app-1','qr-100','https://qr.alipay.com/test','external order','https://merchant.invalid/notify','encrypted-fixture',$4,$5,$6) RETURNING id`,
		instanceID, "merchant-"+tradeNo, tradeNo, now.Add(-time.Minute), now.Add(time.Hour), now)
	require.NoError(t, err)
	defer rows.Close()
	require.True(t, rows.Next())
	var id int64
	require.NoError(t, rows.Scan(&id))
	return id
}

func TestBuiltinEasyPayClaimCommitsNotificationWithoutLocalCredit(t *testing.T) {
	s, instanceID := newBillReceiptTestService(t)
	ctx := context.Background()
	orderID := epayReceiptTestOrder(t, s, instanceID, "EPAY_FIXED_1")
	receipt, _, err := s.storeBillReceipt(ctx, instanceID, "2088000000000001", provider.AlipayBillEntry{AccountLogID: "EPAY_LOG_1", AlipayOrderNo: "EPAY_ALI_1", Amount: "100.00", Direction: "收入", TransAt: time.Now()})
	require.NoError(t, err)
	require.NoError(t, s.ClaimBuiltinEasyPayReceipt(ctx, instanceID, receipt.ID, orderID, "admin:1", "merchant verified this payment"))
	require.NoError(t, s.ClaimBuiltinEasyPayReceipt(ctx, instanceID, receipt.ID, orderID, "admin:1", "retry"))
	rows, err := s.entClient.QueryContext(ctx, `SELECT status,alipay_order_no,notify_status,notify_attempts FROM payment_epay_orders WHERE id=$1`, orderID)
	require.NoError(t, err)
	require.True(t, rows.Next())
	var status, transaction, notify string
	var attempts int
	require.NoError(t, rows.Scan(&status, &transaction, &notify, &attempts))
	rows.Close()
	require.Equal(t, "PAID", status)
	require.Equal(t, "EPAY_ALI_1", transaction)
	require.Equal(t, "pending", notify)
	require.Zero(t, attempts)
	localOrders, err := s.entClient.PaymentOrder.Query().Count(ctx)
	require.NoError(t, err)
	require.Zero(t, localOrders, "external claims must never create or fulfill local balance orders")
	page, err := s.ListAlipayBillReceipts(ctx, instanceID, 1, 20, "matched")
	require.NoError(t, err)
	require.Len(t, page.Items, 1)
	require.Nil(t, page.Items[0].OrderID)
	require.NotNil(t, page.Items[0].EpayOrderID)
	require.Equal(t, orderID, *page.Items[0].EpayOrderID)
}

func TestBuiltinEasyPayClaimSharesLocalReceiptAndTransactionExclusivity(t *testing.T) {
	for _, first := range []string{"local", "external"} {
		t.Run(first, func(t *testing.T) {
			s, instanceID := newBillReceiptTestService(t)
			ctx := context.Background()
			local := billTestOrder(t, s, instanceID, "LOCAL_TARGET")
			external := epayReceiptTestOrder(t, s, instanceID, "EXTERNAL_TARGET")
			entry := provider.AlipayBillEntry{AccountLogID: "SHARED_LOG", AlipayOrderNo: "SHARED_ALI", Amount: "100.00", Direction: "收入", TransAt: time.Now()}
			receipt, _, err := s.storeBillReceipt(ctx, instanceID, "2088000000000001", entry)
			require.NoError(t, err)
			if first == "local" {
				require.NoError(t, s.claimBillReceipt(ctx, receipt.ReceiverID, receipt.ID, local.ID, "admin:1", "verified", true))
				err = s.ClaimBuiltinEasyPayReceipt(ctx, instanceID, receipt.ID, external, "admin:1", "duplicate")
			} else {
				require.NoError(t, s.ClaimBuiltinEasyPayReceipt(ctx, instanceID, receipt.ID, external, "admin:1", "verified"))
				err = s.claimBillReceipt(ctx, receipt.ReceiverID, receipt.ID, local.ID, "admin:1", "duplicate", true)
			}
			require.Equal(t, "ALIPAY_BILL_ALREADY_CLAIMED", infraerrors.Reason(err))
			entry.AccountLogID = "ANOTHER_LOG_SAME_TRANSACTION"
			duplicate, _, err := s.storeBillReceipt(ctx, instanceID, receipt.ReceiverID, entry)
			require.NoError(t, err)
			if first == "local" {
				err = s.ClaimBuiltinEasyPayReceipt(ctx, instanceID, duplicate.ID, external, "admin:1", "duplicate")
			} else {
				err = s.claimBillReceipt(ctx, receipt.ReceiverID, duplicate.ID, local.ID, "admin:1", "duplicate", true)
			}
			require.Equal(t, "ALIPAY_BILL_ALREADY_CLAIMED", infraerrors.Reason(err))
		})
	}
}

func TestBuiltinEasyPayAutomaticReceiptNeedsGatewayReference(t *testing.T) {
	s, instanceID := newBillReceiptTestService(t)
	ctx := context.Background()
	orderID := epayReceiptTestOrder(t, s, instanceID, "EPAY_STABLE_TRADE")
	entry := provider.AlipayBillEntry{AccountLogID: "AUTO_LOG", AlipayOrderNo: "AUTO_ALI", Amount: "100.00", Direction: "收入", TransAt: time.Now()}
	receipt, _, err := s.storeBillReceipt(ctx, instanceID, "2088000000000001", entry)
	require.NoError(t, err)
	matched, err := s.reconcileBillReceipt(ctx, receipt)
	require.NoError(t, err)
	require.False(t, matched, "amount and time alone do not identify a gateway order")
	entry.AccountLogID, entry.AlipayOrderNo, entry.MerchantOrderNo = "VERIFIED_LOG", "VERIFIED_ALI", "EPAY_STABLE_TRADE"
	verified, _, err := s.storeBillReceipt(ctx, instanceID, "2088000000000001", entry)
	require.NoError(t, err)
	matched, err = s.reconcileBillReceipt(ctx, verified)
	require.NoError(t, err)
	require.True(t, matched)
	page, err := s.ListAlipayBillReceipts(ctx, instanceID, 1, 20, "matched")
	require.NoError(t, err)
	require.Len(t, page.Items, 1)
	require.Equal(t, orderID, *page.Items[0].EpayOrderID)
}

func TestBuiltinEasyPayClaimRejectsAmountReceiverAndForeignReferences(t *testing.T) {
	for _, tc := range []struct {
		name, amount, receiver, reference string
	}{
		{name: "wrong amount", amount: "100.01", receiver: "2088000000000001"},
		{name: "wrong receiver", amount: "100.00", receiver: "2088000000000002"},
		{name: "known local order", amount: "100.00", receiver: "2088000000000001", reference: "FOREIGN_LOCAL"},
		{name: "known gateway order", amount: "100.00", receiver: "2088000000000001", reference: "FOREIGN_EPAY"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, instanceID := newBillReceiptTestService(t)
			ctx := context.Background()
			orderID := epayReceiptTestOrder(t, s, instanceID, "RIGHT_EPAY")
			billTestOrder(t, s, instanceID, "FOREIGN_LOCAL")
			epayReceiptTestOrder(t, s, instanceID, "FOREIGN_EPAY")
			receipt, _, err := s.storeBillReceipt(ctx, instanceID, "2088000000000001", provider.AlipayBillEntry{AccountLogID: "REJECT_LOG", AlipayOrderNo: "REJECT_ALI", MerchantOrderNo: tc.reference, Amount: tc.amount, Direction: "收入", TransAt: time.Now()})
			require.NoError(t, err)
			if tc.receiver != "2088000000000001" {
				_, err = s.entClient.ExecContext(ctx, `UPDATE payment_epay_orders SET receiver_id=$1 WHERE id=$2`, tc.receiver, orderID)
				require.NoError(t, err)
			}
			err = s.ClaimBuiltinEasyPayReceipt(ctx, instanceID, receipt.ID, orderID, "admin:1", "verified")
			require.Equal(t, "ALIPAY_BILL_MATCH_REJECTED", infraerrors.Reason(err))
			order, err := getBuiltinEasyPayOrderForReceipt(ctx, s.entClient, orderID)
			require.NoError(t, err)
			require.Equal(t, "PENDING", order.Status)
			require.Equal(t, "none", order.NotifyStatus)
		})
	}
}

func TestBuiltinEasyPayClaimRetryKeepsDeliveredNotification(t *testing.T) {
	s, instanceID := newBillReceiptTestService(t)
	ctx := context.Background()
	orderID := epayReceiptTestOrder(t, s, instanceID, "DELIVERED_EPAY")
	receipt, _, err := s.storeBillReceipt(ctx, instanceID, "2088000000000001", provider.AlipayBillEntry{AccountLogID: "DELIVERED_LOG", AlipayOrderNo: "DELIVERED_ALI", Amount: "100.00", Direction: "收入", TransAt: time.Now()})
	require.NoError(t, err)
	require.NoError(t, s.ClaimBuiltinEasyPayReceipt(ctx, instanceID, receipt.ID, orderID, "admin:1", "verified"))
	_, err = s.entClient.ExecContext(ctx, `UPDATE payment_epay_orders SET notify_status='delivered',notify_attempts=2,notify_next_at=NULL WHERE id=$1`, orderID)
	require.NoError(t, err)
	require.NoError(t, s.ClaimBuiltinEasyPayReceipt(ctx, instanceID, receipt.ID, orderID, "admin:1", "retry"))
	order, err := getBuiltinEasyPayOrderForReceipt(ctx, s.entClient, orderID)
	require.NoError(t, err)
	require.Equal(t, "delivered", order.NotifyStatus)
	require.Equal(t, 2, order.NotifyAttempts)
	require.Nil(t, order.NotifyNextAt)
}

func TestBuiltinEasyPayClaimPreventsOrdinaryAlipayCredit(t *testing.T) {
	s, instanceID := newBillReceiptTestService(t)
	ctx := context.Background()
	orderID := epayReceiptTestOrder(t, s, instanceID, "EXTERNAL_OWNER")
	local := alipayGuardLegacyOrder(t, s, instanceID, "LATE_LOCAL")
	credited := alipayGuardFulfillment(t, s, local)
	receipt, _, err := s.storeBillReceipt(ctx, instanceID, "2088000000000001", provider.AlipayBillEntry{AccountLogID: "NULL_LOCAL_TARGET", AlipayOrderNo: "EXTERNAL_PAID_ALI", Amount: "100.00", Direction: "收入", TransAt: time.Now()})
	require.NoError(t, err)
	require.NoError(t, s.ClaimBuiltinEasyPayReceipt(ctx, instanceID, receipt.ID, orderID, "admin:1", "verified"))
	err = s.toPaid(ctx, local, "EXTERNAL_PAID_ALI", 100, "alipay")
	require.Equal(t, "ALIPAY_BILL_ALREADY_CLAIMED", infraerrors.Reason(err))
	require.Zero(t, *credited)
}
