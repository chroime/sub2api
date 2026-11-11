package service

import (
	"context"
	"database/sql"
	"log/slog"
	"time"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/internal/payment"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
)

type billReceiptClaimMode uint8

const (
	billClaimOfficialReference billReceiptClaimMode = iota
	billClaimManual
	billClaimReservedAmount
)

type billAmountReceiptTarget struct {
	LocalID, ExternalID  int64
	CreatedAt, ExpiresAt time.Time
	EffectiveEnd         time.Time
}

// PaymentOrderUsesBillAmountMatching exposes a policy marker, never credentials.
// Settlement additionally requires the immutable reservation stored at creation.
func PaymentOrderUsesBillAmountMatching(order *dbent.PaymentOrder) bool {
	if order == nil || psStringValue(order.ProviderKey) != payment.TypeAlipayBill {
		return false
	}
	enabled, _ := order.ProviderSnapshot["bill_amount_matching"].(bool)
	return enabled
}

func findBillAmountReceiptTarget(ctx context.Context, client *dbent.Client, r *PaymentBillReceipt) (*billAmountReceiptTarget, string, error) {
	if r == nil || r.TransAt.IsZero() || r.AlipayOrderNo == "" {
		return nil, "missing_transaction_id", nil
	}
	amount, err := billObservedAmountCents(r.Amount)
	if err != nil {
		return nil, "amount_mismatch", nil
	}
	// Official account logs have second precision. The exclusive deadline
	// prevents the expiration second from becoming a valid new payment.
	rows, err := client.QueryContext(ctx, `SELECT order_id,epay_order_id,created_at,expires_at,user_cancelled_at,closed_at FROM payment_bill_amount_reservations WHERE receiver_id=$1 AND amount_minor=$2 AND created_at<$3 AND COALESCE(user_cancelled_at,closed_at,expires_at)>$4 ORDER BY id LIMIT 2`, r.ReceiverID, amount, r.TransAt.Truncate(time.Second).Add(time.Second), r.TransAt)
	if err != nil {
		return nil, "", err
	}
	defer rows.Close()
	var target *billAmountReceiptTarget
	for rows.Next() {
		if target != nil {
			return nil, "amount_window_ambiguous", nil
		}
		var local, external sql.NullInt64
		var closed sql.NullTime
		var userCancelled sql.NullTime
		target = &billAmountReceiptTarget{}
		if err = rows.Scan(&local, &external, &target.CreatedAt, &target.ExpiresAt, &userCancelled, &closed); err != nil {
			return nil, "", err
		}
		target.LocalID = local.Int64
		target.ExternalID = external.Int64
		target.EffectiveEnd = target.ExpiresAt
		if closed.Valid {
			target.EffectiveEnd = closed.Time
		}
		if userCancelled.Valid {
			target.EffectiveEnd = userCancelled.Time
		}
	}
	if err = rows.Err(); err != nil {
		return nil, "", err
	}
	if target == nil {
		return nil, "amount_window_missing", nil
	}
	return target, "", nil
}

func checkBillAmountReceiptTarget(ctx context.Context, client *dbent.Client, r *PaymentBillReceipt, localID, externalID int64, createdAt, expiresAt, coveredThrough time.Time) error {
	target, reason, err := findBillAmountReceiptTarget(ctx, client, r)
	if err != nil {
		return err
	}
	if reason == "" && (target.LocalID != localID || target.ExternalID != externalID || !target.CreatedAt.Equal(createdAt) || !target.ExpiresAt.Equal(expiresAt)) {
		reason = "amount_window_ambiguous"
	}
	// A complete observed batch covering the payment's whole second suffices;
	// the remaining original payment minute is not a confirmation delay.
	paymentSecondEnd := r.TransAt.Truncate(time.Second).Add(time.Second)
	if reason == "" && (paymentSecondEnd.After(time.Now().UTC()) || coveredThrough.Before(paymentSecondEnd)) {
		reason = "amount_window_pending"
	}
	if reason == "" {
		amount, _ := billObservedAmountCents(r.Amount)
		rows, e := client.QueryContext(ctx, `SELECT COUNT(DISTINCT CASE WHEN alipay_order_no='' THEN account_log_id ELSE alipay_order_no END) FROM payment_bill_receipts WHERE receiver_id=$1 AND amount_minor=$2 AND trans_at>=$3 AND trans_at<$4`, r.ReceiverID, amount, createdAt.Truncate(time.Second), target.EffectiveEnd)
		if e != nil {
			return e
		}
		var count int
		if rows.Next() {
			err = rows.Scan(&count)
		}
		rowsErr := rows.Err()
		rows.Close()
		if err != nil {
			return err
		}
		if rowsErr != nil {
			return rowsErr
		}
		if count != 1 {
			reason = "amount_receipts_ambiguous"
		}
	}
	if reason != "" {
		return infraerrors.Conflict("ALIPAY_BILL_MATCH_REJECTED", "receipt does not identify one reserved payment window").WithMetadata(map[string]string{"reason": reason})
	}
	return nil
}

func billLocalReceiptReasonForMode(r *PaymentBillReceipt, order *dbent.PaymentOrder, mode billReceiptClaimMode) string {
	if mode != billClaimReservedAmount {
		return billReceiptMatchReason(r, order, mode == billClaimManual)
	}
	if reason := billReceiptMatchReason(r, order, true); reason != "" {
		return reason
	}
	if !PaymentOrderUsesBillAmountMatching(order) {
		return "amount_window_missing"
	}
	if order.Status == OrderStatusCancelled {
		return "amount_window_cancelled"
	}
	if !r.TransAt.Before(order.ExpiresAt.Truncate(time.Second)) {
		return "amount_window_expired"
	}
	return ""
}

func billExternalReceiptReasonForMode(r *PaymentBillReceipt, order *BuiltinEasyPayOrder, mode billReceiptClaimMode) string {
	if mode != billClaimReservedAmount {
		return billEpayReceiptMatchReason(r, order, mode == billClaimManual)
	}
	if reason := billEpayReceiptMatchReason(r, order, true); reason != "" {
		return reason
	}
	if !r.TransAt.Before(order.ExpiresAt.Truncate(time.Second)) {
		return "amount_window_expired"
	}
	return ""
}

// reconcileBillAmountReceipt is used only when the official reference names no
// platform order. It never creates reservations for historical orders/receipts.
func (s *PaymentService) reconcileBillAmountReceipt(ctx context.Context, r *PaymentBillReceipt, coveredThrough time.Time) (bool, string, error) {
	target, reason, err := findBillAmountReceiptTarget(ctx, s.entClient, r)
	if err != nil || reason != "" {
		return false, reason, err
	}
	const evidence = "unique reserved receiver and exact amount; official payment time inside 60-second window"
	if target.LocalID > 0 {
		err = s.claimBillReceiptWithMode(ctx, r.ReceiverID, r.ID, target.LocalID, "system", evidence, billClaimReservedAmount, coveredThrough)
	} else {
		err = s.claimBuiltinEasyPayReceiptWithMode(ctx, r.ReceiverID, r.ID, target.ExternalID, "system", evidence, billClaimReservedAmount, coveredThrough)
	}
	if err != nil {
		if infraerrors.Reason(err) == "ALIPAY_BILL_ALREADY_CLAIMED" {
			return false, "duplicate_transaction", nil
		}
		if infraerrors.Reason(err) == "ALIPAY_BILL_MATCH_REJECTED" {
			// Metadata carries a stable review reason without exposing input data.
			return false, billAmountRejectionReason(err), nil
		}
		return false, "", err
	}
	if target.LocalID > 0 {
		if err = s.executeFulfillment(ctx, target.LocalID); err != nil {
			// The committed PAID claim is recoverable by the normal collector.
			slog.Warn("[AlipayBill] reserved amount fulfillment needs retry", "order_id", target.LocalID)
			return true, "", nil
		}
	}
	return true, "", nil
}

func successfulBillSyncThrough(ctx context.Context, client *dbent.Client, receiver string) (time.Time, error) {
	rows, err := client.QueryContext(ctx, `SELECT cursor_at FROM payment_bill_sync_state WHERE receiver_id=$1 AND error_code=''`, receiver)
	if err != nil {
		return time.Time{}, err
	}
	defer rows.Close()
	var through sql.NullTime
	if rows.Next() {
		if err = rows.Scan(&through); err != nil {
			return time.Time{}, err
		}
	}
	return through.Time, rows.Err()
}

func billAmountRejectionReason(err error) string {
	if metadata := infraerrors.FromError(err).Metadata; metadata != nil {
		if reason := metadata["reason"]; reason != "" {
			return reason
		}
	}
	return "amount_window_ambiguous"
}

func (s *PaymentService) lockBillReceiptAmount(ctx context.Context, tx *dbent.Tx, r *PaymentBillReceipt) error {
	cents, err := billObservedAmountCents(r.Amount)
	if err != nil {
		return infraerrors.BadRequest("ALIPAY_BILL_INVALID_AMOUNT", "invalid observed amount")
	}
	return s.lockBillAmountKey(ctx, tx, r.ReceiverID, cents)
}

func billClaimModeLabel(mode billReceiptClaimMode) string {
	switch mode {
	case billClaimManual:
		return "manual"
	case billClaimReservedAmount:
		return "reserved_amount"
	default:
		return "official_reference"
	}
}
