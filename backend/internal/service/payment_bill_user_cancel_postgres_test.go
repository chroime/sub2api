//go:build unit

package service

import (
	"context"
	"database/sql"
	"strconv"
	"testing"
	"time"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/internal/payment"
	"github.com/Wei-Shaw/sub2api/internal/payment/provider"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/stretchr/testify/require"
)

func userCancelledAtPostgresFixture(t *testing.T, s *PaymentService, id int64) sql.NullTime {
	t.Helper()
	rows, err := s.entClient.QueryContext(context.Background(), `SELECT user_cancelled_at FROM payment_bill_amount_reservations WHERE order_id=$1`, id)
	require.NoError(t, err)
	defer rows.Close()
	require.True(t, rows.Next())
	var at sql.NullTime
	require.NoError(t, rows.Scan(&at))
	return at
}

func billPostgresFixtureCount(t *testing.T, s *PaymentService, query string, args ...any) int {
	t.Helper()
	rows, err := s.entClient.QueryContext(context.Background(), query, args...)
	require.NoError(t, err)
	defer rows.Close()
	require.True(t, rows.Next())
	var count int
	require.NoError(t, rows.Scan(&count))
	return count
}

func billPostgresCancelFixtureProvider(s *PaymentService) {
	s.registry = payment.NewRegistry()
	s.loadBalancer = payment.NewDefaultLoadBalancer(s.entClient, s.configService.encryptionKey)
}

func TestBillPostgresUserCancelImmediatelyReusesAmountWithOneNextOwner(t *testing.T) {
	s, id := newBillAmountPostgresService(t)
	billPostgresCancelFixtureProvider(s)
	ctx := context.Background()
	_, cfg, err := s.billProviderConfig(ctx, id)
	require.NoError(t, err)
	start := time.Now().UTC().Truncate(time.Second).Add(-20 * time.Second)
	old := matchingBillLocalOrder(t, s, id, "PG_USER_CANCEL_PREVIOUS", start)
	before := time.Now().UTC()
	outcome, err := s.CancelOrder(ctx, old.ID, old.UserID)
	require.NoError(t, err)
	require.Equal(t, checkPaidResultCancelled, outcome)
	cancelled := userCancelledAtPostgresFixture(t, s, old.ID)
	require.True(t, cancelled.Valid)
	require.False(t, cancelled.Time.Before(before))
	require.True(t, cancelled.Time.Before(old.ExpiresAt))
	require.Equal(t, 0, billPostgresFixtureCount(t, s, `SELECT count(*) FROM payment_bill_sync_state`), "immediate release must not depend on a previous successful statement query")
	require.Equal(t, 1, billPostgresFixtureCount(t, s, `SELECT count(*) FROM payment_audit_logs WHERE order_id=$1 AND action='ORDER_CANCELLED' AND operator=$2`, strconv.FormatInt(old.ID, 10), "user:"+strconv.FormatInt(old.UserID, 10)))

	// The payment happened before cancellation but its statement becomes visible
	// only now. It must stay with the cancelled window without keeping the amount
	// locked for a new checkout.
	receipt, _, err := s.storeBillReceipt(ctx, id, "2088000000000001", provider.AlipayBillEntry{AccountLogID: "PG_USER_CANCEL_OLD_LOG", AlipayOrderNo: "PG_USER_CANCEL_OLD_TRADE", Amount: "100.00", Direction: "收入", TransAt: start.Add(5 * time.Second)})
	require.NoError(t, err)
	a, err := s.entClient.User.Create().SetEmail("user-cancel-next-a@test.invalid").SetUsername("user-cancel-next-a").SetPasswordHash("fixture").Save(ctx)
	require.NoError(t, err)
	b, err := s.entClient.User.Create().SetEmail("user-cancel-next-b@test.invalid").SetUsername("user-cancel-next-b").SetPasswordHash("fixture").Save(ctx)
	require.NoError(t, err)
	type result struct {
		order *dbent.PaymentOrder
		err   error
	}
	gate := make(chan struct{})
	results := make(chan result, 2)
	for _, user := range []*dbent.User{a, b} {
		go func(u *dbent.User) {
			<-gate
			o, e := createBillAmountLocalFixture(ctx, s, id, cfg, u, 100)
			results <- result{o, e}
		}(user)
	}
	close(gate)
	var next *dbent.PaymentOrder
	busy := 0
	for i := 0; i < 2; i++ {
		r := <-results
		if r.err == nil {
			require.Nil(t, next, "the amount must have exactly one next owner")
			next = r.order
		} else {
			require.Equal(t, "ALIPAY_BILL_AMOUNT_BUSY", infraerrors.Reason(r.err))
			busy++
		}
	}
	require.NotNil(t, next)
	require.Equal(t, 1, busy)
	require.True(t, next.CreatedAt.Before(old.ExpiresAt), "active cancellation releases the amount before the original minute ends")
	target, reason, err := findBillAmountReceiptTarget(ctx, s.entClient, receipt)
	require.NoError(t, err)
	require.Empty(t, reason)
	require.Equal(t, old.ID, target.LocalID)
	matched, err := s.reconcileBillReceiptThrough(ctx, receipt, time.Now().UTC())
	require.NoError(t, err)
	require.False(t, matched)
	next, err = s.entClient.PaymentOrder.Get(ctx, next.ID)
	require.NoError(t, err)
	require.Equal(t, OrderStatusPending, next.Status)
	require.Empty(t, next.PaymentTradeNo)
	require.Zero(t, billPostgresFixtureCount(t, s, `SELECT count(*) FROM payment_bill_claims WHERE order_id=$1`, next.ID))
	old, err = s.entClient.PaymentOrder.Get(ctx, old.ID)
	require.NoError(t, err)
	require.Equal(t, OrderStatusCancelled, old.Status)
	require.True(t, old.ExpiresAt.Equal(start.Add(time.Minute)), "the original payment deadline remains auditable")
}

func TestBillPostgresUserCancelSameSecondReceiptRemainsAmbiguous(t *testing.T) {
	s, id := newBillAmountPostgresService(t)
	billPostgresCancelFixtureProvider(s)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Second)
	start := now.Add(-20 * time.Second)
	old := matchingBillLocalOrder(t, s, id, "PG_USER_CANCEL_BOUNDARY_OLD", start)
	_, err := s.CancelOrder(ctx, old.ID, old.UserID)
	require.NoError(t, err)
	// Align isolated fixture timestamps to a known fractional cancellation point
	// without sleeping. New checkouts can start in the same second immediately.
	boundary := now.Add(-10 * time.Second)
	_, err = s.entClient.ExecContext(ctx, `UPDATE payment_bill_amount_reservations SET user_cancelled_at=$1 WHERE order_id=$2`, boundary.Add(750*time.Millisecond), old.ID)
	require.NoError(t, err)
	next := matchingBillLocalOrder(t, s, id, "PG_USER_CANCEL_BOUNDARY_NEW", boundary)
	receipt, _, err := s.storeBillReceipt(ctx, id, "2088000000000001", provider.AlipayBillEntry{AccountLogID: "PG_USER_CANCEL_BOUNDARY_LOG", AlipayOrderNo: "PG_USER_CANCEL_BOUNDARY_TRADE", Amount: "100.00", Direction: "收入", TransAt: boundary})
	require.NoError(t, err)
	target, reason, err := findBillAmountReceiptTarget(ctx, s.entClient, receipt)
	require.NoError(t, err)
	require.Nil(t, target)
	require.Equal(t, "amount_window_ambiguous", reason)
	matched, err := s.reconcileBillReceiptThrough(ctx, receipt, now)
	require.NoError(t, err)
	require.False(t, matched, "second-precision statements cannot prove who paid within a shared cancellation second")
	require.Zero(t, billPostgresFixtureCount(t, s, `SELECT count(*) FROM payment_bill_claims`))
	updated, err := s.entClient.PaymentOrder.Get(ctx, next.ID)
	require.NoError(t, err)
	require.Equal(t, OrderStatusPending, updated.Status)
	before := *receipt
	before.TransAt = boundary.Add(-time.Second)
	target, reason, err = findBillAmountReceiptTarget(ctx, s.entClient, &before)
	require.NoError(t, err)
	require.Empty(t, reason)
	require.Equal(t, old.ID, target.LocalID)
	after := *receipt
	after.TransAt = boundary.Add(time.Second)
	target, reason, err = findBillAmountReceiptTarget(ctx, s.entClient, &after)
	require.NoError(t, err)
	require.Empty(t, reason)
	require.Equal(t, next.ID, target.LocalID)
}

func TestBillPostgresUserCancelAndClaimSerialize(t *testing.T) {
	s, id := newBillAmountPostgresService(t)
	billPostgresCancelFixtureProvider(s)
	ctx := context.Background()
	start := time.Now().UTC().Truncate(time.Second).Add(-20 * time.Second)
	order := matchingBillLocalOrder(t, s, id, "PG_USER_CANCEL_CLAIM_RACE", start)
	receipt, _, err := s.storeBillReceipt(ctx, id, "2088000000000001", provider.AlipayBillEntry{AccountLogID: "PG_USER_CANCEL_RACE_LOG", AlipayOrderNo: "PG_USER_CANCEL_RACE_TRADE", Amount: "100.00", Direction: "收入", TransAt: start.Add(5 * time.Second)})
	require.NoError(t, err)
	type result struct {
		kind, outcome string
		err           error
	}
	gate := make(chan struct{})
	results := make(chan result, 2)
	go func() {
		<-gate
		outcome, e := s.CancelOrder(ctx, order.ID, order.UserID)
		results <- result{kind: "cancel", outcome: outcome, err: e}
	}()
	go func() {
		<-gate
		e := s.claimBillReceiptWithMode(ctx, receipt.ReceiverID, receipt.ID, order.ID, "system", "isolated concurrent cancellation", billClaimReservedAmount, time.Now().UTC())
		results <- result{kind: "claim", err: e}
	}()
	close(gate)
	var cancel, claim result
	for i := 0; i < 2; i++ {
		r := <-results
		if r.kind == "cancel" {
			cancel = r
		} else {
			claim = r
		}
	}
	updated, err := s.entClient.PaymentOrder.Get(ctx, order.ID)
	require.NoError(t, err)
	marker := userCancelledAtPostgresFixture(t, s, order.ID)
	claims := billPostgresFixtureCount(t, s, `SELECT count(*) FROM payment_bill_claims WHERE order_id=$1`, order.ID)
	switch updated.Status {
	case OrderStatusPaid:
		require.NoError(t, claim.err)
		require.Equal(t, 1, claims)
		require.False(t, marker.Valid, "a paid order must never be marked as voluntarily released")
		if cancel.err != nil {
			require.Equal(t, "INVALID_STATUS", infraerrors.Reason(cancel.err))
		} else {
			require.Equal(t, checkPaidResultAlreadyPaid, cancel.outcome)
		}
	case OrderStatusCancelled:
		require.NoError(t, cancel.err)
		require.Equal(t, checkPaidResultCancelled, cancel.outcome)
		require.True(t, marker.Valid)
		require.Zero(t, claims)
		require.Equal(t, "ALIPAY_BILL_MATCH_REJECTED", infraerrors.Reason(claim.err))
	default:
		t.Fatalf("unexpected concurrent state %q", updated.Status)
	}
}

func TestBillPostgresUserCancelledManualClaimDoesNotReopenAmount(t *testing.T) {
	s, id := newBillAmountPostgresService(t)
	billPostgresCancelFixtureProvider(s)
	ctx := context.Background()
	_, cfg, err := s.billProviderConfig(ctx, id)
	require.NoError(t, err)
	start := time.Now().UTC().Truncate(time.Second).Add(-20 * time.Second)
	old := matchingBillLocalOrder(t, s, id, "PG_USER_CANCEL_RESTORED", start)
	_, err = s.CancelOrder(ctx, old.ID, old.UserID)
	require.NoError(t, err)
	marker := userCancelledAtPostgresFixture(t, s, old.ID)
	require.True(t, marker.Valid)
	receipt, _, err := s.storeBillReceipt(ctx, id, "2088000000000001", provider.AlipayBillEntry{AccountLogID: "PG_USER_CANCEL_RESTORED_LOG", AlipayOrderNo: "PG_USER_CANCEL_RESTORED_TRADE", Amount: "100.00", Direction: "收入", TransAt: start.Add(5 * time.Second)})
	require.NoError(t, err)
	require.NoError(t, s.claimBillReceipt(ctx, receipt.ReceiverID, receipt.ID, old.ID, "admin:1", "verified pre-cancellation receipt", true))
	old, err = s.entClient.PaymentOrder.Get(ctx, old.ID)
	require.NoError(t, err)
	require.Equal(t, OrderStatusPaid, old.Status)
	require.True(t, marker.Time.Equal(userCancelledAtPostgresFixture(t, s, old.ID).Time))
	require.NoError(t, billAmountReservationCheckAt(t, s, time.Now().UTC()), "manual confirmation must not reopen an already released reservation or require sync")
	user, err := s.entClient.User.Create().SetEmail("user-cancel-restored-next@test.invalid").SetUsername("user-cancel-restored-next").SetPasswordHash("fixture").Save(ctx)
	require.NoError(t, err)
	_, err = createBillAmountLocalFixture(ctx, s, id, cfg, user, 100)
	require.NoError(t, err)
}

func TestBillPostgresAdminCancelAndTimeoutDoNotImmediatelyRelease(t *testing.T) {
	for _, kind := range []string{"admin", "timeout"} {
		t.Run(kind, func(t *testing.T) {
			s, id := newBillAmountPostgresService(t)
			billPostgresCancelFixtureProvider(s)
			ctx := context.Background()
			start := time.Now().UTC().Truncate(time.Second).Add(-20 * time.Second)
			if kind == "timeout" {
				start = time.Now().UTC().Truncate(time.Second).Add(-70 * time.Second)
			}
			order := matchingBillLocalOrder(t, s, id, "PG_NONUSER_CANCEL_"+kind, start)
			wantStatus := OrderStatusCancelled
			wantReason := "ALIPAY_BILL_CANCELLED_AMOUNT_WAIT"
			if kind == "admin" {
				_, err := s.AdminCancelOrder(ctx, order.ID)
				require.NoError(t, err)
			} else {
				n, err := s.ExpireTimedOutOrders(ctx)
				require.NoError(t, err)
				require.Equal(t, 1, n)
				wantStatus = OrderStatusExpired
				wantReason = "ALIPAY_BILL_AMOUNT_BUSY"
			}
			updated, err := s.entClient.PaymentOrder.Get(ctx, order.ID)
			require.NoError(t, err)
			require.Equal(t, wantStatus, updated.Status)
			require.False(t, userCancelledAtPostgresFixture(t, s, order.ID).Valid)
			require.Equal(t, wantReason, infraerrors.Reason(billAmountReservationCheckAt(t, s, time.Now().UTC())))
		})
	}
}
