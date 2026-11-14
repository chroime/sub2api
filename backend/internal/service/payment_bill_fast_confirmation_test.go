//go:build unit

package service

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/payment/provider"
	"github.com/stretchr/testify/require"
)

func billClosedAtFixture(t *testing.T, s *PaymentService, local, external int64) time.Time {
	t.Helper()
	rows, err := s.entClient.QueryContext(context.Background(), `SELECT closed_at FROM payment_bill_amount_reservations WHERE order_id=$1 OR epay_order_id=$2`, local, external)
	require.NoError(t, err)
	defer rows.Close()
	require.True(t, rows.Next())
	var closed sql.NullTime
	require.NoError(t, rows.Scan(&closed))
	require.True(t, closed.Valid, "confirmation must atomically close the original amount window")
	return closed.Time
}

func TestBillAmountFastConfirmationClosesWindowAndReleasesImmediately(t *testing.T) {
	s, id := newBillReceiptTestService(t)
	ctx := context.Background()
	start := time.Now().UTC().Truncate(time.Second).Add(-20 * time.Second)
	o := matchingBillLocalOrder(t, s, id, "FAST_CONFIRMED", start)
	credit := alipayGuardFulfillment(t, s, o)
	r, _, err := s.storeBillReceipt(ctx, id, "2088000000000001", provider.AlipayBillEntry{AccountLogID: "FAST_LOG", AlipayOrderNo: "FAST_TRADE", Amount: "100.00", Direction: "收入", TransAt: start.Add(5 * time.Second)})
	require.NoError(t, err)
	before := time.Now().UTC()
	matched, err := s.reconcileBillReceiptThrough(ctx, r, before)
	require.NoError(t, err)
	require.True(t, matched)
	require.Equal(t, 100.0, *credit)
	closed := billClosedAtFixture(t, s, o.ID, 0)
	require.Zero(t, closed.Nanosecond())
	require.False(t, closed.Before(before), "never move the closing boundary back to the payer's timestamp")
	require.True(t, closed.Before(o.ExpiresAt))
	require.NoError(t, billAmountReservationCheckAt(t, s, time.Now().UTC()))
	billAmountSuccessfulSyncFixture(t, s, closed.Add(-time.Second))
	require.NoError(t, billAmountReservationCheckAt(t, s, closed))
	billAmountSuccessfulSyncFixture(t, s, closed)
	require.NoError(t, billAmountReservationCheckAt(t, s, closed), "confirmed payments must not wait for the original minute plus cooling delay")

	// Retrying fulfillment/claim cannot reopen or extend the ownership window.
	matched, err = s.reconcileBillReceiptThrough(ctx, r, time.Now().UTC())
	require.NoError(t, err)
	require.True(t, matched)
	require.True(t, closed.Equal(billClosedAtFixture(t, s, o.ID, 0)))
	require.Equal(t, 100.0, *credit)
	updated, err := s.entClient.PaymentOrder.Get(ctx, o.ID)
	require.NoError(t, err)
	require.True(t, updated.ExpiresAt.Equal(o.ExpiresAt), "original displayed deadline remains auditable")

	// The next window can start at the exclusive boundary even though the old
	// original deadline is later. Old receipts still identify only the old order.
	next := matchingBillLocalOrder(t, s, id, "NEXT_CONFIRMED", closed)
	target, reason, err := findBillAmountReceiptTarget(ctx, s.entClient, r)
	require.NoError(t, err)
	require.Empty(t, reason)
	require.Equal(t, o.ID, target.LocalID)
	boundary := *r
	boundary.TransAt = closed
	target, reason, err = findBillAmountReceiptTarget(ctx, s.entClient, &boundary)
	require.NoError(t, err)
	require.Empty(t, reason)
	require.Equal(t, next.ID, target.LocalID, "the closing second belongs exclusively to the new window")
}

func TestBillAmountConfirmedWindowExtraReceiptRequiresReview(t *testing.T) {
	s, id := newBillReceiptTestService(t)
	ctx := context.Background()
	start := time.Now().UTC().Truncate(time.Second).Add(-20 * time.Second)
	o := matchingBillLocalOrder(t, s, id, "FAST_EXTRA", start)
	alipayGuardFulfillment(t, s, o)
	entry := provider.AlipayBillEntry{AccountLogID: "FAST_FIRST_LOG", AlipayOrderNo: "FAST_FIRST_TRADE", Amount: "100.00", Direction: "收入", TransAt: start.Add(5 * time.Second)}
	r, _, err := s.storeBillReceipt(ctx, id, "2088000000000001", entry)
	require.NoError(t, err)
	matched, err := s.reconcileBillReceiptThrough(ctx, r, time.Now().UTC())
	require.NoError(t, err)
	require.True(t, matched)
	closed := billClosedAtFixture(t, s, o.ID, 0)
	billAmountSuccessfulSyncFixture(t, s, closed)
	entry.AccountLogID = "FAST_DUPLICATE_LOG"
	_, _, err = s.storeBillReceipt(ctx, id, r.ReceiverID, entry)
	require.NoError(t, err)
	require.NoError(t, billAmountReservationCheckAt(t, s, closed), "another accounting row for a claimed trade must not block reuse")
	entry.AccountLogID, entry.AlipayOrderNo = "FAST_SECOND_LOG", "FAST_SECOND_TRADE"
	entry.TransAt = start.Add(6 * time.Second)
	extra, _, err := s.storeBillReceipt(ctx, id, r.ReceiverID, entry)
	require.NoError(t, err)
	require.NoError(t, billAmountReservationCheckAt(t, s, closed), "an old extra receipt must not block a new payment")
	matched, err = s.reconcileBillReceiptThrough(ctx, extra, closed)
	require.NoError(t, err)
	require.False(t, matched, "an extra payment must never credit the completed order twice")
	_, err = s.entClient.ExecContext(ctx, `UPDATE payment_bill_receipts SET status='ignored' WHERE id=$1`, extra.ID)
	require.NoError(t, err)
	require.NoError(t, billAmountReservationCheckAt(t, s, closed))
}

func TestBillAmountHistoricalConfirmedPaymentSkipsCooling(t *testing.T) {
	s, id := newBillReceiptTestService(t)
	ctx := context.Background()
	start := time.Now().UTC().Truncate(time.Second).Add(-2 * time.Minute)
	o := matchingBillLocalOrder(t, s, id, "HISTORICAL_CONFIRMED", start)
	alipayGuardFulfillment(t, s, o)
	r, _, err := s.storeBillReceipt(ctx, id, "2088000000000001", provider.AlipayBillEntry{AccountLogID: "HIST_LOG", AlipayOrderNo: "HIST_TRADE", Amount: "100.00", Direction: "收入", TransAt: start.Add(5 * time.Second)})
	require.NoError(t, err)
	matched, err := s.reconcileBillReceiptThrough(ctx, r, time.Now().UTC())
	require.NoError(t, err)
	require.True(t, matched)
	// Simulate an already-confirmed record predating the additive migration.
	_, err = s.entClient.ExecContext(ctx, `UPDATE payment_bill_amount_reservations SET closed_at=NULL WHERE order_id=$1`, o.ID)
	require.NoError(t, err)
	now := time.Now().UTC()
	billAmountSuccessfulSyncFixture(t, s, now)
	require.NoError(t, billAmountReservationCheckAt(t, s, now))
}

func TestBillAmountFastExternalConfirmationDoesNotWaitForNotify(t *testing.T) {
	s, id, _ := newBuiltinEasyPayServiceFixture(t, "https://merchant.example")
	ctx := context.Background()
	created, err := s.CreateBuiltinEasyPayOrder(ctx, builtinEasyPayTestParams(id, "https://merchant.example"))
	require.NoError(t, err)
	o, err := findBuiltinEasyPayOrder(ctx, s.entClient, id, "", created.TradeNo)
	require.NoError(t, err)
	start := time.Now().UTC().Truncate(time.Second).Add(-20 * time.Second)
	_, err = s.entClient.ExecContext(ctx, `UPDATE payment_epay_orders SET created_at=$1,expires_at=$2 WHERE id=$3`, start, start.Add(time.Minute), o.ID)
	require.NoError(t, err)
	_, err = s.entClient.ExecContext(ctx, `UPDATE payment_bill_amount_reservations SET created_at=$1,expires_at=$2,reuse_after=$3 WHERE epay_order_id=$4`, start, start.Add(time.Minute), start.Add(3*time.Minute), o.ID)
	require.NoError(t, err)
	r, _, err := s.storeBillReceipt(ctx, id, "2088000000000001", provider.AlipayBillEntry{AccountLogID: "FAST_EXT_LOG", AlipayOrderNo: "FAST_EXT_TRADE", Amount: "100.00", Direction: "收入", TransAt: start.Add(5 * time.Second)})
	require.NoError(t, err)
	matched, err := s.reconcileBillReceiptThrough(ctx, r, time.Now().UTC())
	require.NoError(t, err)
	require.True(t, matched)
	closed := billClosedAtFixture(t, s, 0, o.ID)
	billAmountSuccessfulSyncFixture(t, s, closed)
	require.NoError(t, billAmountReservationCheckAt(t, s, closed))
	o, err = findBuiltinEasyPayOrder(ctx, s.entClient, id, "", created.TradeNo)
	require.NoError(t, err)
	require.Equal(t, "PAID", o.Status)
	require.Equal(t, "pending", o.NotifyStatus)
}
