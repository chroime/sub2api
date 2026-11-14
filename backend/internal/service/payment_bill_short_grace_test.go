//go:build unit

package service

import (
	"context"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/payment/provider"
	"github.com/stretchr/testify/require"
)

func TestBillAmountExpiryReleasesWithoutGraceOrSync(t *testing.T) {
	s, id := newBillReceiptTestService(t)
	ctx := context.Background()
	start := time.Now().UTC().Truncate(time.Second).Add(-time.Minute)
	order := matchingBillLocalOrder(t, s, id, "SHORT_GRACE", start)
	_, err := s.entClient.PaymentOrder.UpdateOneID(order.ID).SetStatus(OrderStatusExpired).Save(ctx)
	require.NoError(t, err)
	require.NoError(t, billAmountReservationCheckAt(t, s, order.ExpiresAt))
	require.NoError(t, billAmountReservationCheckAt(t, s, order.ExpiresAt.Add(time.Second)))
	billAmountSuccessfulSyncFixture(t, s, order.ExpiresAt.Add(-time.Second))
	_, err = s.entClient.ExecContext(ctx, `UPDATE payment_bill_sync_state SET error_code='sync_failed'`)
	require.NoError(t, err)
	require.NoError(t, billAmountReservationCheckAt(t, s, order.ExpiresAt.Add(30*time.Second)))
}

func TestBillAmountExpiredReservationReleasesWithoutRewritingHistory(t *testing.T) {
	s, id := newBillReceiptTestService(t)
	ctx := context.Background()
	start := time.Now().UTC().Truncate(time.Second).Add(-2 * time.Minute)
	order := matchingBillLocalOrder(t, s, id, "OLD_LONG_GRACE", start)
	end := order.ExpiresAt.Add(30 * time.Second)
	billAmountSuccessfulSyncFixture(t, s, end)
	require.NoError(t, billAmountReservationCheckAt(t, s, end.Add(time.Second)))
	rows, err := s.entClient.QueryContext(ctx, `SELECT expires_at,reuse_after FROM payment_bill_amount_reservations WHERE order_id=$1`, order.ID)
	require.NoError(t, err)
	defer rows.Close()
	require.True(t, rows.Next())
	var expiry, recordedReuse time.Time
	require.NoError(t, rows.Scan(&expiry, &recordedReuse))
	require.True(t, expiry.Equal(order.ExpiresAt))
	require.True(t, recordedReuse.Equal(order.ExpiresAt.Add(2*time.Minute)), "the old issued reservation remains auditable")
}

func TestBillAmountExpiredReservationRetainsUnclaimedReceiptWithoutBlockingReuse(t *testing.T) {
	s, id := newBillReceiptTestService(t)
	ctx := context.Background()
	start := time.Now().UTC().Truncate(time.Second).Add(-2 * time.Minute)
	order := matchingBillLocalOrder(t, s, id, "SHORT_GRACE_REVIEW", start)
	_, err := s.entClient.PaymentOrder.UpdateOneID(order.ID).SetStatus(OrderStatusExpired).Save(ctx)
	require.NoError(t, err)
	for _, entry := range []provider.AlipayBillEntry{
		{AccountLogID: "SHORT_GRACE_A", AlipayOrderNo: "SHORT_GRACE_TRADE_A", Amount: "100.00", Direction: "收入", TransAt: start.Add(10 * time.Second)},
		{AccountLogID: "SHORT_GRACE_B", AlipayOrderNo: "SHORT_GRACE_TRADE_B", Amount: "100.00", Direction: "收入", TransAt: start.Add(20 * time.Second)},
	} {
		_, _, err = s.storeBillReceipt(ctx, id, "2088000000000001", entry)
		require.NoError(t, err)
	}
	now := time.Now().UTC()
	billAmountSuccessfulSyncFixture(t, s, now)
	require.NoError(t, billAmountReservationCheckAt(t, s, now), "an old review receipt must not extend amount occupancy")
	_, err = s.entClient.ExecContext(ctx, `UPDATE payment_bill_receipts SET status='ignored'`)
	require.NoError(t, err)
	require.NoError(t, billAmountReservationCheckAt(t, s, now))
}

func TestBillAmountNewReservationStoresNoAdditionalWait(t *testing.T) {
	s, id, _ := newBuiltinEasyPayServiceFixture(t, "https://merchant.example")
	ctx := context.Background()
	created, err := s.CreateBuiltinEasyPayOrder(ctx, builtinEasyPayTestParams(id, "https://merchant.example"))
	require.NoError(t, err)
	rows, err := s.entClient.QueryContext(ctx, `SELECT a.expires_at,a.reuse_after FROM payment_bill_amount_reservations a JOIN payment_epay_orders o ON o.id=a.epay_order_id WHERE o.trade_no=$1`, created.TradeNo)
	require.NoError(t, err)
	defer rows.Close()
	require.True(t, rows.Next())
	var expiry, reuse time.Time
	require.NoError(t, rows.Scan(&expiry, &reuse))
	require.Zero(t, reuse.Sub(expiry))
}
