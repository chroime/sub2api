//go:build unit

package service

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/payment/provider"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/stretchr/testify/require"
)

func TestBillUserCancelImmediatelyReleasesWithoutStatementSync(t *testing.T) {
	s, id := newBillReceiptTestService(t)
	ctx := context.Background()
	start := time.Now().UTC().Truncate(time.Second).Add(-10 * time.Second)
	o := matchingBillLocalOrder(t, s, id, "USER_CANCEL_NOW", start)
	// An observed but not yet confirmed old payment must stay in review rather
	// than locking the amount again or being attributed to a replacement order.
	r, _, err := s.storeBillReceipt(ctx, id, "2088000000000001", provider.AlipayBillEntry{AccountLogID: "USER_CANCEL_LOG", AlipayOrderNo: "USER_CANCEL_TRADE", Amount: "100.00", Direction: "收入", TransAt: start.Add(time.Second)})
	require.NoError(t, err)
	// Avoid the unrelated provider factory in the original cancellation path.
	_, err = s.entClient.PaymentOrder.UpdateOneID(o.ID).SetPaymentType("").Save(ctx)
	require.NoError(t, err)
	before := time.Now().UTC()
	outcome, err := s.CancelOrder(ctx, o.ID, o.UserID)
	require.NoError(t, err)
	require.Equal(t, checkPaidResultCancelled, outcome)
	require.NoError(t, billAmountReservationCheckAt(t, s, time.Now().UTC()), "explicit cancellation must not wait for the old minute, sync, or an old review receipt")
	rows, err := s.entClient.QueryContext(ctx, `SELECT user_cancelled_at,expires_at FROM payment_bill_amount_reservations WHERE order_id=$1`, o.ID)
	require.NoError(t, err)
	require.True(t, rows.Next())
	var cancelled sql.NullTime
	var expiry time.Time
	require.NoError(t, rows.Scan(&cancelled, &expiry))
	rows.Close()
	require.True(t, cancelled.Valid)
	require.False(t, cancelled.Time.Before(before))
	require.True(t, cancelled.Time.Before(o.ExpiresAt))
	require.True(t, expiry.Equal(o.ExpiresAt))
	next := matchingBillLocalOrder(t, s, id, "USER_CANCEL_NEXT", cancelled.Time.Truncate(time.Second))
	target, reason, err := findBillAmountReceiptTarget(ctx, s.entClient, r)
	require.NoError(t, err)
	require.Empty(t, reason)
	require.Equal(t, o.ID, target.LocalID)
	boundary := *r
	boundary.TransAt = cancelled.Time.Truncate(time.Second)
	_, reason, err = findBillAmountReceiptTarget(ctx, s.entClient, &boundary)
	require.NoError(t, err)
	if cancelled.Time.Nanosecond() != 0 {
		require.Equal(t, "amount_window_ambiguous", reason, "the cancellation second must never guess old/new ownership")
	}
	boundary.TransAt = cancelled.Time.Truncate(time.Second).Add(time.Second)
	target, reason, err = findBillAmountReceiptTarget(ctx, s.entClient, &boundary)
	require.NoError(t, err)
	require.Empty(t, reason)
	require.Equal(t, next.ID, target.LocalID)
}

func TestBillUserCancelDoesNotChangeAdminOrExpiryLocks(t *testing.T) {
	for _, status := range []string{OrderStatusCancelled, OrderStatusExpired} {
		t.Run(status, func(t *testing.T) {
			s, id := newBillReceiptTestService(t)
			ctx := context.Background()
			start := time.Now().UTC().Truncate(time.Second).Add(-10 * time.Second)
			o := matchingBillLocalOrder(t, s, id, "NON_USER_CANCEL", start)
			_, err := s.entClient.PaymentOrder.UpdateOneID(o.ID).SetStatus(status).Save(ctx)
			require.NoError(t, err)
			err = billAmountReservationCheckAt(t, s, time.Now().UTC())
			require.Error(t, err)
			require.Contains(t, []string{"ALIPAY_BILL_AMOUNT_BUSY", "ALIPAY_BILL_CANCELLED_AMOUNT_WAIT"}, infraerrors.Reason(err))
		})
	}
}

func TestBillUserCancelCannotReleaseAnotherUsersOrPaidOrder(t *testing.T) {
	s, id := newBillReceiptTestService(t)
	ctx := context.Background()
	o := matchingBillLocalOrder(t, s, id, "USER_CANCEL_AUTH", time.Now().UTC().Truncate(time.Second))
	_, err := s.CancelOrder(ctx, o.ID, o.UserID+1)
	require.Equal(t, "FORBIDDEN", infraerrors.Reason(err))
	_, err = s.entClient.PaymentOrder.UpdateOneID(o.ID).SetStatus(OrderStatusPaid).Save(ctx)
	require.NoError(t, err)
	_, err = s.CancelOrder(ctx, o.ID, o.UserID)
	require.Equal(t, "INVALID_STATUS", infraerrors.Reason(err))
	rows, err := s.entClient.QueryContext(ctx, `SELECT user_cancelled_at FROM payment_bill_amount_reservations WHERE order_id=$1`, o.ID)
	require.NoError(t, err)
	defer rows.Close()
	require.True(t, rows.Next())
	var cancelled sql.NullTime
	require.NoError(t, rows.Scan(&cancelled))
	require.False(t, cancelled.Valid)
}

func TestBillUserCancelReleaseSurvivesManualClaim(t *testing.T) {
	s, id := newBillReceiptTestService(t)
	ctx := context.Background()
	start := time.Now().UTC().Truncate(time.Second).Add(-10 * time.Second)
	o := matchingBillLocalOrder(t, s, id, "USER_CANCEL_CLAIM", start)
	_, err := s.CancelOrder(ctx, o.ID, o.UserID)
	require.NoError(t, err)
	r, _, err := s.storeBillReceipt(ctx, id, "2088000000000001", provider.AlipayBillEntry{AccountLogID: "CANCEL_CLAIM_LOG", AlipayOrderNo: "CANCEL_CLAIM_TRADE", Amount: "100.00", Direction: "收入", TransAt: start.Add(time.Second)})
	require.NoError(t, err)
	require.NoError(t, s.claimBillReceipt(ctx, r.ReceiverID, r.ID, o.ID, "admin:1", "isolated verified old payment", true))
	require.NoError(t, billAmountReservationCheckAt(t, s, time.Now().UTC()), "reviewing the old payment must not reopen a released amount slot")
}

func TestBillUserCancelLegacyOrderReleasesWithoutEnrollingHistory(t *testing.T) {
	s, id := newBillReceiptTestService(t)
	ctx := context.Background()
	o := billTestOrder(t, s, id, "LEGACY_USER_CANCEL")
	_, err := s.CancelOrder(ctx, o.ID, o.UserID)
	require.NoError(t, err)
	require.NoError(t, billAmountReservationCheckAt(t, s, time.Now().UTC()))
	rows, err := s.entClient.QueryContext(ctx, `SELECT count(*) FROM payment_bill_amount_reservations`)
	require.NoError(t, err)
	defer rows.Close()
	require.True(t, rows.Next())
	var count int
	require.NoError(t, rows.Scan(&count))
	require.Zero(t, count)
}

func TestBillUserCancelMigrationBackfillsOnlyVerifiedUserActions(t *testing.T) {
	s, id := newBillReceiptTestService(t)
	ctx := context.Background()
	start := time.Now().UTC().Truncate(time.Second).Add(-10 * time.Second)
	user := matchingBillLocalOrder(t, s, id, "BACKFILL_USER_CANCEL", start)
	admin := matchingBillLocalOrder(t, s, id, "BACKFILL_ADMIN_CANCEL", start)
	for _, fixture := range []struct {
		id       int64
		operator string
	}{{user.ID, "user:" + strconv.FormatInt(user.UserID, 10)}, {admin.ID, "admin"}} {
		_, err := s.entClient.PaymentOrder.UpdateOneID(fixture.id).SetStatus(OrderStatusCancelled).Save(ctx)
		require.NoError(t, err)
		_, err = s.entClient.PaymentAuditLog.Create().SetOrderID(strconv.FormatInt(fixture.id, 10)).SetAction("ORDER_CANCELLED").SetOperator(fixture.operator).SetDetail("fixture").SetCreatedAt(start.Add(5 * time.Second)).Save(ctx)
		require.NoError(t, err)
	}
	migration, err := os.ReadFile(filepath.Join("..", "..", "migrations", "286_payment_bill_user_cancel_release.sql"))
	require.NoError(t, err)
	query := string(migration)[strings.Index(string(migration), "UPDATE payment_bill_amount_reservations"):]
	_, err = s.entClient.ExecContext(ctx, query)
	require.NoError(t, err)
	rows, err := s.entClient.QueryContext(ctx, `SELECT order_id,user_cancelled_at FROM payment_bill_amount_reservations ORDER BY order_id`)
	require.NoError(t, err)
	defer rows.Close()
	for rows.Next() {
		var orderID int64
		var cancelled sql.NullTime
		require.NoError(t, rows.Scan(&orderID, &cancelled))
		require.Equal(t, orderID == user.ID, cancelled.Valid)
	}
	require.NoError(t, rows.Err())
}
