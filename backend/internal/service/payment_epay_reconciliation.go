package service

import (
	"context"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
)

// ClaimBuiltinEasyPayReceipt records an external merchant's payment. The durable
// notification queue handles delivery; no local balance fulfillment runs here.
func (s *PaymentService) ClaimBuiltinEasyPayReceipt(ctx context.Context, providerID, receiptID, orderID int64, actor, reason string) error {
	if strings.TrimSpace(reason) == "" || utf8.RuneCountInString(reason) > 500 {
		return infraerrors.BadRequest("ALIPAY_BILL_REVIEW_REASON_REQUIRED", "record how the payment was verified (up to 500 characters)")
	}
	if receiptID <= 0 || orderID <= 0 {
		return infraerrors.BadRequest("VALIDATION_ERROR", "receipt and order IDs must be positive")
	}
	_, cfg, err := s.billProviderConfig(ctx, providerID)
	if err != nil {
		return err
	}
	order, err := getBuiltinEasyPayOrderForReceipt(ctx, s.entClient, orderID)
	if err != nil {
		return err
	}
	if order.ProviderInstanceID != providerID {
		return infraerrors.NotFound("NOT_FOUND", "gateway order not found for this provider")
	}
	return s.claimBuiltinEasyPayReceipt(ctx, strings.TrimSpace(cfg["receiverId"]), receiptID, orderID, actor, strings.TrimSpace(reason), true)
}

func getBuiltinEasyPayOrderForReceipt(ctx context.Context, client *dbent.Client, id int64) (*BuiltinEasyPayOrder, error) {
	rows, err := client.QueryContext(ctx, `SELECT `+builtinEasyPayOrderColumns+` FROM payment_epay_orders WHERE id=$1`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return nil, err
		}
		return nil, infraerrors.NotFound("NOT_FOUND", "gateway order not found")
	}
	order, err := scanBuiltinEasyPayOrder(rows)
	return &order, err
}

func findBuiltinEasyPayReceiptOrder(ctx context.Context, client *dbent.Client, reference string) (*BuiltinEasyPayOrder, error) {
	rows, err := client.QueryContext(ctx, `SELECT `+builtinEasyPayOrderColumns+` FROM payment_epay_orders WHERE trade_no=$1`, reference)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	if !rows.Next() {
		return nil, rows.Err()
	}
	order, err := scanBuiltinEasyPayOrder(rows)
	return &order, err
}

func billEpayReceiptMatchReason(receipt *PaymentBillReceipt, order *BuiltinEasyPayOrder, manual bool) string {
	if receipt == nil || receipt.AlipayOrderNo == "" {
		return "missing_transaction_id"
	}
	if !manual && receipt.MerchantOrderNo == "" {
		return "missing_order_reference"
	}
	if order == nil || (!manual && receipt.MerchantOrderNo != order.TradeNo) {
		return "no_matching_order"
	}
	if order.ReceiverID == "" || receipt.ReceiverID != order.ReceiverID {
		return "receiver_mismatch"
	}
	amount, err := billObservedAmountCents(receipt.Amount)
	if err != nil || amount != order.AmountMinor {
		return "amount_mismatch"
	}
	switch order.Status {
	case "PENDING":
	case "EXPIRED":
		if !manual {
			return "expired_order"
		}
	default:
		return "invalid_order_state"
	}
	if receipt.TransAt.IsZero() || receipt.TransAt.Before(order.CreatedAt.Truncate(time.Second)) || (!manual && receipt.TransAt.After(order.ExpiresAt)) {
		return "outside_order_window"
	}
	return ""
}

func (s *PaymentService) claimBuiltinEasyPayReceipt(ctx context.Context, receiver string, receiptID, orderID int64, actor, reason string, manual bool) error {
	mode := billClaimOfficialReference
	if manual {
		mode = billClaimManual
	}
	return s.claimBuiltinEasyPayReceiptWithMode(ctx, receiver, receiptID, orderID, actor, reason, mode, time.Time{})
}

func (s *PaymentService) claimBuiltinEasyPayReceiptWithMode(ctx context.Context, receiver string, receiptID, orderID int64, actor, reason string, mode billReceiptClaimMode, coveredThrough time.Time) error {
	tx, err := s.entClient.Tx(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	client := tx.Client()
	rows, err := client.QueryContext(ctx, `SELECT `+paymentBillReceiptColumns+` FROM payment_bill_receipts WHERE id=$1 AND receiver_id=$2`, receiptID, receiver)
	if err != nil {
		return err
	}
	if !rows.Next() {
		rowsErr := rows.Err()
		rows.Close()
		if rowsErr != nil {
			return rowsErr
		}
		return infraerrors.NotFound("NOT_FOUND", "receipt not found")
	}
	receipt, err := scanPaymentBillReceipt(rows)
	rows.Close()
	if err != nil {
		return err
	}
	if receipt.Status == "matched" {
		if receipt.EpayOrderID != nil && *receipt.EpayOrderID == orderID && receipt.OrderID == nil {
			return nil
		}
		return infraerrors.Conflict("ALIPAY_BILL_ALREADY_CLAIMED", "receipt already belongs to another order")
	}
	if receipt.Status != "review" {
		return infraerrors.Conflict("ALIPAY_BILL_NOT_REVIEWABLE", "receipt is not available for review")
	}
	if err := s.lockBillReceiptAmount(ctx, tx, &receipt); err != nil {
		return err
	}
	if err := s.lockAlipayTransaction(ctx, tx, receipt.AlipayOrderNo); err != nil {
		return err
	}
	order, err := getBuiltinEasyPayOrderForReceipt(ctx, client, orderID)
	if err != nil {
		return err
	}
	if rejection := billExternalReceiptReasonForMode(&receipt, order, mode); rejection != "" {
		return infraerrors.Conflict("ALIPAY_BILL_MATCH_REJECTED", "receipt cannot be assigned to this gateway order").WithMetadata(map[string]string{"reason": rejection})
	}
	if mode == billClaimReservedAmount {
		if err := checkBillAmountReceiptTarget(ctx, client, &receipt, 0, order.ID, order.CreatedAt, order.ExpiresAt, coveredThrough); err != nil {
			return err
		}
	}
	if err := ensureBillTransactionUnclaimed(ctx, client, receipt.AlipayOrderNo, 0, orderID); err != nil {
		return err
	}
	if err := ensureBillReceiptReferenceTarget(ctx, client, receipt.MerchantOrderNo, 0, orderID); err != nil {
		return err
	}
	now := time.Now().UTC()
	claim, err := client.ExecContext(ctx, `INSERT INTO payment_bill_claims(receipt_id,receiver_id,alipay_order_no,epay_order_id,actor,reason,created_at)
		VALUES($1,$2,$3,$4,$5,$6,$7) ON CONFLICT DO NOTHING`, receiptID, receiver, receipt.AlipayOrderNo, orderID, actor, reason, now)
	if err != nil {
		return err
	}
	count, err := claim.RowsAffected()
	if err != nil {
		return err
	}
	if count != 1 {
		return infraerrors.Conflict("ALIPAY_BILL_ALREADY_CLAIMED", "this receipt, transaction, or order has already been claimed")
	}
	updated, err := client.ExecContext(ctx, `UPDATE payment_epay_orders SET status='PAID',alipay_order_no=$1,paid_at=$2,updated_at=$3,
		notify_status='pending',notify_next_at=$3 WHERE id=$4 AND status=$5`, receipt.AlipayOrderNo, receipt.TransAt, now, orderID, order.Status)
	if err != nil {
		return err
	}
	count, err = updated.RowsAffected()
	if err != nil {
		return err
	}
	if count != 1 {
		return infraerrors.Conflict("ALIPAY_BILL_ORDER_CHANGED", "gateway order changed during reconciliation")
	}
	updated, err = client.ExecContext(ctx, `UPDATE payment_bill_receipts SET status='matched',review_reason='',order_id=NULL,epay_order_id=$1 WHERE id=$2 AND status='review'`, orderID, receiptID)
	if err != nil {
		return err
	}
	count, err = updated.RowsAffected()
	if err != nil {
		return err
	}
	if count != 1 {
		return infraerrors.Conflict("ALIPAY_BILL_ALREADY_CLAIMED", "receipt was claimed concurrently")
	}
	if err = closeBillAmountReservation(ctx, client, 0, orderID, time.Now().UTC()); err != nil {
		return err
	}
	return tx.Commit()
}

func ensureBillTransactionUnclaimed(ctx context.Context, client *dbent.Client, tradeNo string, localOrderID, epayOrderID int64) error {
	rows, err := client.QueryContext(ctx, `SELECT
		EXISTS(SELECT 1 FROM payment_orders WHERE payment_trade_no=$1 AND id<>$2)
		OR EXISTS(SELECT 1 FROM payment_epay_orders WHERE alipay_order_no=$1 AND id<>$3)
		OR EXISTS(SELECT 1 FROM payment_bill_claims WHERE alipay_order_no=$1 AND (COALESCE(order_id,0)<>$2 OR COALESCE(epay_order_id,0)<>$3))`, tradeNo, localOrderID, epayOrderID)
	if err != nil {
		return err
	}
	defer rows.Close()
	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return err
		}
		return fmt.Errorf("payment transaction ownership query returned no result")
	}
	var used bool
	if err := rows.Scan(&used); err != nil {
		return err
	}
	if used {
		return infraerrors.Conflict("ALIPAY_BILL_ALREADY_CLAIMED", "transaction already belongs to another payment order")
	}
	return rows.Err()
}

func ensureBillReceiptReferenceTarget(ctx context.Context, client *dbent.Client, reference string, localOrderID, epayOrderID int64) error {
	if reference == "" {
		return nil
	}
	rows, err := client.QueryContext(ctx, `SELECT
		EXISTS(SELECT 1 FROM payment_orders WHERE out_trade_no=$1 AND id<>$2)
		OR EXISTS(SELECT 1 FROM payment_epay_orders WHERE trade_no=$1 AND id<>$3)`, reference, localOrderID, epayOrderID)
	if err != nil {
		return err
	}
	defer rows.Close()
	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return err
		}
		return fmt.Errorf("payment order reference query returned no result")
	}
	var other bool
	if err := rows.Scan(&other); err != nil {
		return err
	}
	if other {
		return infraerrors.Conflict("ALIPAY_BILL_MATCH_REJECTED", "official transaction reference belongs to another order")
	}
	return rows.Err()
}
