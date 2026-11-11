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

func TestBillPostgresFastClaimClosesLocalAndExternalWindows(t *testing.T) {
	for _, source := range []string{"local", "external"} {
		t.Run(source, func(t *testing.T) {
			s, id := newBillAmountPostgresService(t)
			ctx := context.Background()
			start := time.Now().UTC().Truncate(time.Second).Add(-20 * time.Second)
			expires := start.Add(time.Minute)
			var localID, externalID int64
			if source == "local" {
				localID = matchingBillLocalOrder(t, s, id, "PG_FAST_LOCAL", start).ID
			} else {
				externalID = epayReceiptTestOrder(t, s, id, "PG_FAST_EXTERNAL")
				_, err := s.entClient.ExecContext(ctx, `UPDATE payment_epay_orders SET created_at=$1,expires_at=$2 WHERE id=$3`, start, expires, externalID)
				require.NoError(t, err)
				reserveBillMatchingFixture(t, s, id, 0, externalID, start)
			}
			r, _, err := s.storeBillReceipt(ctx, id, "2088000000000001", provider.AlipayBillEntry{
				AccountLogID: "PG_FAST_LOG", AlipayOrderNo: "PG_FAST_TRADE", MerchantOrderNo: "STATIC_CODE_REFERENCE", Amount: "100.00", Direction: "收入", TransAt: start.Add(5 * time.Second),
			})
			require.NoError(t, err)
			claim := func(through time.Time) error {
				if localID > 0 {
					return s.claimBillReceiptWithMode(ctx, r.ReceiverID, r.ID, localID, "system", "isolated early confirmation", billClaimReservedAmount, through)
				}
				return s.claimBuiltinEasyPayReceiptWithMode(ctx, r.ReceiverID, r.ID, externalID, "system", "isolated early confirmation", billClaimReservedAmount, through)
			}
			before := time.Now().UTC()
			require.NoError(t, claim(before), "one observed receipt can confirm before the original minute ends")
			after := time.Now().UTC()
			closed := billClosedAtFixture(t, s, localID, externalID)
			require.Zero(t, closed.Nanosecond())
			require.False(t, closed.Before(before), "closure must round up the claim instant, never the earlier payment instant")
			require.False(t, closed.After(after.Truncate(time.Second).Add(time.Second)))
			require.True(t, closed.Before(expires))
			if localID > 0 {
				o, err := s.entClient.PaymentOrder.Get(ctx, localID)
				require.NoError(t, err)
				require.Equal(t, OrderStatusPaid, o.Status)
				require.Equal(t, r.AlipayOrderNo, o.PaymentTradeNo)
				require.True(t, expires.Equal(o.ExpiresAt))
			} else {
				o, err := getBuiltinEasyPayOrderForReceipt(ctx, s.entClient, externalID)
				require.NoError(t, err)
				require.Equal(t, "PAID", o.Status)
				require.Equal(t, "pending", o.NotifyStatus, "callback delivery is independent of amount release")
				require.True(t, expires.Equal(o.ExpiresAt))
			}
			require.Equal(t, "ALIPAY_BILL_AMOUNT_BUSY", infraerrors.Reason(billAmountReservationCheckAt(t, s, closed.Add(-time.Nanosecond))))
			billAmountSuccessfulSyncFixture(t, s, closed.Add(-time.Second))
			require.Equal(t, "ALIPAY_BILL_SYNC_REQUIRED", infraerrors.Reason(billAmountReservationCheckAt(t, s, closed)))
			billAmountSuccessfulSyncFixture(t, s, closed)
			require.NoError(t, billAmountReservationCheckAt(t, s, closed), "confirmed amount can be reused without the old two-minute cooling delay")
			require.NoError(t, claim(closed))
			require.True(t, closed.Equal(billClosedAtFixture(t, s, localID, externalID)), "an idempotent retry must not extend or reopen the window")
		})
	}
}

func TestBillPostgresFastClosedSlotHasOneNextOwnerAndRetainsOldReceipts(t *testing.T) {
	s, id := newBillAmountPostgresService(t)
	ctx := context.Background()
	_, cfg, err := s.billProviderConfig(ctx, id)
	require.NoError(t, err)
	now := time.Now().UTC().Truncate(time.Second)
	start := now.Add(-20 * time.Second)
	old := matchingBillLocalOrder(t, s, id, "PG_FAST_PREVIOUS", start)
	r, _, err := s.storeBillReceipt(ctx, id, "2088000000000001", provider.AlipayBillEntry{
		AccountLogID: "PG_FAST_PREVIOUS_LOG", AlipayOrderNo: "PG_FAST_PREVIOUS_TRADE", Amount: "100.00", Direction: "收入", TransAt: start.Add(5 * time.Second),
	})
	require.NoError(t, err)
	require.NoError(t, s.claimBillReceiptWithMode(ctx, r.ReceiverID, r.ID, old.ID, "system", "isolated fast claim", billClaimReservedAmount, time.Now().UTC()))
	// Move only this isolated fixture's confirmation instant into the past to
	// exercise the next real creation without sleeping across a clock second.
	closed := start.Add(10 * time.Second)
	_, err = s.entClient.ExecContext(ctx, `UPDATE payment_bill_amount_reservations SET closed_at=$1 WHERE order_id=$2`, closed, old.ID)
	require.NoError(t, err)
	// Coverage stops at the closure: the later original deadline is irrelevant.
	_, err = s.entClient.ExecContext(ctx, `INSERT INTO payment_bill_sync_state(receiver_id,cursor_at,lease_until,lease_owner,error_code,updated_at) VALUES($1,$2,$3,'fixture','',$4)`, r.ReceiverID, closed, time.Unix(0, 0).UTC(), now)
	require.NoError(t, err)
	a, err := s.entClient.User.Create().SetEmail("fast-next-a@test.invalid").SetUsername("fast-next-a").SetPasswordHash("fixture").Save(ctx)
	require.NoError(t, err)
	b, err := s.entClient.User.Create().SetEmail("fast-next-b@test.invalid").SetUsername("fast-next-b").SetPasswordHash("fixture").Save(ctx)
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
			o, e := createBillAmountLocalFixture(ctx, s, id, cfg, u, 100)
			results <- outcome{o, e}
		}(user)
	}
	close(gate)
	var next *dbent.PaymentOrder
	busy := 0
	for i := 0; i < 2; i++ {
		result := <-results
		if result.err == nil {
			require.Nil(t, next, "only one next owner may obtain the released amount")
			next = result.order
		} else {
			require.Equal(t, "ALIPAY_BILL_AMOUNT_BUSY", infraerrors.Reason(result.err))
			busy++
		}
	}
	require.NotNil(t, next)
	require.Equal(t, 1, busy)
	require.False(t, next.CreatedAt.Before(closed))
	require.True(t, next.CreatedAt.Before(old.ExpiresAt), "next checkout must not wait for the old minute's deadline")
	require.NoError(t, s.claimBillReceiptWithMode(ctx, r.ReceiverID, r.ID, old.ID, "system", "old receipt retry", billClaimReservedAmount, now))
	require.True(t, closed.Equal(billClosedAtFixture(t, s, old.ID, 0)))

	// A separately observed old-window payment still belongs exclusively to the
	// old closed interval, even when its bill arrives after the next checkout.
	extra, _, err := s.storeBillReceipt(ctx, id, r.ReceiverID, provider.AlipayBillEntry{AccountLogID: "PG_FAST_EXTRA_LOG", AlipayOrderNo: "PG_FAST_EXTRA_TRADE", Amount: "100.00", Direction: "收入", TransAt: start.Add(6 * time.Second)})
	require.NoError(t, err)
	target, reason, err := findBillAmountReceiptTarget(ctx, s.entClient, extra)
	require.NoError(t, err)
	require.Empty(t, reason)
	require.Equal(t, old.ID, target.LocalID)
	matched, err := s.reconcileBillReceiptThrough(ctx, extra, time.Now().UTC())
	require.NoError(t, err)
	require.False(t, matched)
	next, err = s.entClient.PaymentOrder.Get(ctx, next.ID)
	require.NoError(t, err)
	require.Equal(t, OrderStatusPending, next.Status)
	require.Empty(t, next.PaymentTradeNo)
	rows, err := s.entClient.QueryContext(ctx, `SELECT count(*) FROM payment_bill_claims WHERE order_id=$1`, next.ID)
	require.NoError(t, err)
	require.True(t, rows.Next())
	var count int
	require.NoError(t, rows.Scan(&count))
	rows.Close()
	require.Zero(t, count)

	// Align the next fixture to the actual shared second boundary to verify
	// half-open ownership even though the old original expiry overlaps it.
	_, err = s.entClient.ExecContext(ctx, `UPDATE payment_orders SET created_at=$1,expires_at=$2 WHERE id=$3`, closed, closed.Add(time.Minute), next.ID)
	require.NoError(t, err)
	_, err = s.entClient.ExecContext(ctx, `UPDATE payment_bill_amount_reservations SET created_at=$1,expires_at=$2,reuse_after=$3 WHERE order_id=$4`, closed, closed.Add(time.Minute), closed.Add(3*time.Minute), next.ID)
	require.NoError(t, err)
	boundary := *r
	boundary.TransAt = closed.Add(-time.Second)
	target, reason, err = findBillAmountReceiptTarget(ctx, s.entClient, &boundary)
	require.NoError(t, err)
	require.Empty(t, reason)
	require.Equal(t, old.ID, target.LocalID)
	boundary.TransAt = closed
	target, reason, err = findBillAmountReceiptTarget(ctx, s.entClient, &boundary)
	require.NoError(t, err)
	require.Empty(t, reason)
	require.Equal(t, next.ID, target.LocalID, "closure second is exclusive to the new owner")
}
