//go:build unit

package service

import (
	"context"
	"testing"
	"time"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/internal/payment/provider"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/stretchr/testify/require"
)

func TestBillPostgresShortGraceHasOneNextOwnerAndRetainsLateReceipt(t *testing.T) {
	s, id := newBillAmountPostgresService(t)
	ctx := context.Background()
	_, cfg, err := s.billProviderConfig(ctx, id)
	require.NoError(t, err)
	// This helper deliberately stores the historical two-minute reuse policy.
	// The current policy must safely shorten its allocation wait without edits.
	now := time.Now().UTC().Truncate(time.Second)
	start := now.Add(-95 * time.Second)
	old := matchingBillLocalOrder(t, s, id, "PG_SHORT_GRACE_OLD", start)
	_, err = s.entClient.PaymentOrder.UpdateOneID(old.ID).SetStatus(OrderStatusExpired).Save(ctx)
	require.NoError(t, err)
	billAmountSuccessfulSyncFixture(t, s, old.ExpiresAt.Add(30*time.Second))
	a, err := s.entClient.User.Create().SetEmail("short-grace-a@test.invalid").SetUsername("short-grace-a").SetPasswordHash("fixture").Save(ctx)
	require.NoError(t, err)
	b, err := s.entClient.User.Create().SetEmail("short-grace-b@test.invalid").SetUsername("short-grace-b").SetPasswordHash("fixture").Save(ctx)
	require.NoError(t, err)
	type outcome struct {
		order *dbent.PaymentOrder
		err   error
	}
	gate := make(chan struct{})
	results := make(chan outcome, 2)
	for _, user := range []*dbent.User{a, b} {
		go func(u *dbent.User) {
			<-gate
			order, err := createBillAmountLocalFixture(ctx, s, id, cfg, u, 100)
			results <- outcome{order: order, err: err}
		}(user)
	}
	close(gate)
	var next *dbent.PaymentOrder
	busy := 0
	for range 2 {
		result := <-results
		if result.err == nil {
			require.Nil(t, next)
			next = result.order
		} else {
			require.Equal(t, "ALIPAY_BILL_AMOUNT_BUSY", infraerrors.Reason(result.err))
			busy++
		}
	}
	require.NotNil(t, next)
	require.Equal(t, 1, busy)
	require.True(t, next.CreatedAt.Before(old.ExpiresAt.Add(2*time.Minute)), "next owner must not wait for the old recorded 120-second policy")
	// A delayed old-window statement must still identify that historical order,
	// never the new user who obtained the same amount after thirty seconds.
	receipt, _, err := s.storeBillReceipt(ctx, id, "2088000000000001", provider.AlipayBillEntry{AccountLogID: "PG_SHORT_GRACE_OLD_LOG", AlipayOrderNo: "PG_SHORT_GRACE_OLD_TRADE", Amount: "100.00", Direction: "收入", TransAt: start.Add(20 * time.Second)})
	require.NoError(t, err)
	target, reason, err := findBillAmountReceiptTarget(ctx, s.entClient, receipt)
	require.NoError(t, err)
	require.Empty(t, reason)
	require.Equal(t, old.ID, target.LocalID)
	require.NoError(t, s.claimBillReceiptWithMode(ctx, receipt.ReceiverID, receipt.ID, old.ID, "system", "isolated late payment", billClaimReservedAmount, now))
	next, err = s.entClient.PaymentOrder.Get(ctx, next.ID)
	require.NoError(t, err)
	require.Equal(t, OrderStatusPending, next.Status)
	require.Empty(t, next.PaymentTradeNo)
}

func TestBillPostgresShortGraceRetainsAmbiguousPaymentReview(t *testing.T) {
	s, id := newBillAmountPostgresService(t)
	ctx := context.Background()
	start := time.Now().UTC().Truncate(time.Second).Add(-95 * time.Second)
	order := matchingBillLocalOrder(t, s, id, "PG_SHORT_GRACE_AMBIGUOUS", start)
	_, err := s.entClient.PaymentOrder.UpdateOneID(order.ID).SetStatus(OrderStatusExpired).Save(ctx)
	require.NoError(t, err)
	for _, entry := range []provider.AlipayBillEntry{
		{AccountLogID: "PG_SHORT_GRACE_AMBIGUOUS_A", AlipayOrderNo: "PG_SHORT_GRACE_AMBIGUOUS_TRADE_A", Amount: "100.00", Direction: "收入", TransAt: start.Add(10 * time.Second)},
		{AccountLogID: "PG_SHORT_GRACE_AMBIGUOUS_B", AlipayOrderNo: "PG_SHORT_GRACE_AMBIGUOUS_TRADE_B", Amount: "100.00", Direction: "收入", TransAt: start.Add(20 * time.Second)},
	} {
		_, _, err = s.storeBillReceipt(ctx, id, "2088000000000001", entry)
		require.NoError(t, err)
	}
	billAmountSuccessfulSyncFixture(t, s, time.Now().UTC())
	require.Equal(t, "ALIPAY_BILL_PAYMENT_REVIEW", infraerrors.Reason(billAmountReservationCheckAt(t, s, time.Now().UTC())))
}
