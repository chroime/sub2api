package service

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/ent/paymentorder"
	"github.com/Wei-Shaw/sub2api/internal/payment"
	"github.com/Wei-Shaw/sub2api/internal/payment/provider"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
)

// PaymentBillReceipt intentionally excludes counterparty, memo, and raw response
// data. The admin API exposes only the information needed to reconcile receipts.
type PaymentBillReceipt struct {
	ID                 int64     `json:"id"`
	ProviderInstanceID int64     `json:"-"`
	ReceiverID         string    `json:"-"`
	AccountLogID       string    `json:"account_log_id"`
	AlipayOrderNo      string    `json:"alipay_order_no"`
	MerchantOrderNo    string    `json:"merchant_order_no"`
	Amount             string    `json:"amount"`
	TransAt            time.Time `json:"trans_at"`
	Status             string    `json:"status"`
	ReviewReason       string    `json:"review_reason"`
	OrderID            *int64    `json:"order_id,omitempty"`
	EpayOrderID        *int64    `json:"epay_order_id,omitempty"`
}

type PaymentBillReceiptPage struct {
	Items    []PaymentBillReceipt `json:"items"`
	Total    int                  `json:"total"`
	Page     int                  `json:"page"`
	PageSize int                  `json:"page_size"`
}

type PaymentBillTestResult struct {
	Success    bool      `json:"success"`
	CheckedAt  time.Time `json:"checked_at"`
	EntryCount int       `json:"entry_count"`
	HasMore    bool      `json:"has_more"`
}

type PaymentBillSyncResult struct {
	Fetched   int       `json:"fetched"`
	Inserted  int       `json:"inserted"`
	Matched   int       `json:"matched"`
	Review    int       `json:"review"`
	CheckedAt time.Time `json:"checked_at"`
	receipts  map[int64]*PaymentBillReceipt
}

func (s *PaymentService) billProviderConfig(ctx context.Context, id int64) (*dbent.PaymentProviderInstance, map[string]string, error) {
	return s.loadBillProviderConfig(ctx, id, true)
}

func (s *PaymentService) loadBillProviderConfig(ctx context.Context, id int64, requireReceiver bool) (*dbent.PaymentProviderInstance, map[string]string, error) {
	inst, err := s.entClient.PaymentProviderInstance.Get(ctx, id)
	if err != nil {
		if dbent.IsNotFound(err) {
			return nil, nil, infraerrors.NotFound("NOT_FOUND", "payment provider not found")
		}
		return nil, nil, err
	}
	if inst.ProviderKey != payment.TypeAlipayBill {
		return nil, nil, infraerrors.BadRequest("ALIPAY_BILL_PROVIDER_REQUIRED", "select an Alipay fixed-code provider")
	}
	cfg, err := s.configService.decryptProviderConfig(inst.ProviderKey, inst.Config)
	if err != nil {
		return nil, nil, err
	}
	if requireReceiver && strings.TrimSpace(cfg["receiverId"]) == "" {
		return nil, nil, infraerrors.BadRequest("ALIPAY_BILL_RECEIVER_REQUIRED", "configure the receiving Alipay account ID")
	}
	return inst, cfg, nil
}

func (s *PaymentService) TestAlipayBillConnection(ctx context.Context, id int64) (*PaymentBillTestResult, error) {
	_, cfg, err := s.loadBillProviderConfig(ctx, id, false)
	if err != nil {
		return nil, err
	}
	reader, err := provider.NewAlipayBillQueryClient(strconv.FormatInt(id, 10), cfg)
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC().Truncate(time.Second)
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	page, err := reader.QueryBills(ctx, now.Add(-24*time.Hour), now, 1)
	if err != nil {
		return nil, err
	}
	return &PaymentBillTestResult{Success: true, CheckedAt: now, EntryCount: len(page.Items), HasMore: page.Total > len(page.Items)}, nil
}

// billReceiptMatchReason never treats amount/time as ownership evidence.
// Manual review can recover expired/cancelled orders, but cannot override the
// receiver, exact amount, transaction uniqueness, or completed-order checks.
func billReceiptMatchReason(receipt *PaymentBillReceipt, order *dbent.PaymentOrder, manual bool) string {
	if receipt == nil || receipt.AlipayOrderNo == "" {
		return "missing_transaction_id"
	}
	if !manual && receipt.MerchantOrderNo == "" {
		return "missing_order_reference"
	}
	if order == nil || (!manual && receipt.MerchantOrderNo != order.OutTradeNo) {
		return "no_matching_order"
	}
	receiver, _ := order.ProviderSnapshot["receiver_id"].(string)
	if psStringValue(order.ProviderKey) != payment.TypeAlipayBill || receiver == "" || receiver != receipt.ReceiverID || PaymentOrderCurrency(order) != "CNY" {
		return "receiver_mismatch"
	}
	actual, err := provider.ParseAlipayBillAmountCents(receipt.Amount)
	expected, expectedErr := provider.ParseAlipayBillAmountCents(strconv.FormatFloat(order.PayAmount, 'f', 2, 64))
	if err != nil || expectedErr != nil || actual != expected {
		return "amount_mismatch"
	}
	switch order.Status {
	case OrderStatusPending:
	case OrderStatusExpired, OrderStatusCancelled:
		if !manual {
			return "expired_order"
		}
	default:
		return "invalid_order_state"
	}
	// Alipay timestamps have whole-second precision.
	if receipt.TransAt.IsZero() || receipt.TransAt.Before(order.CreatedAt.Truncate(time.Second)) || (!manual && receipt.TransAt.After(order.ExpiresAt)) {
		return "outside_order_window"
	}
	return ""
}

func billSyncWindow(now, cursor time.Time) (time.Time, time.Time) {
	start := now.Add(-24 * time.Hour)
	if !cursor.IsZero() {
		start = cursor.Add(-5 * time.Minute)
	}
	if start.Before(now.Add(-31 * 24 * time.Hour)) {
		start = now.Add(-31 * 24 * time.Hour)
	}
	if start.After(now) {
		start = now.Add(-5 * time.Minute)
	}
	end := start.Add(time.Hour)
	if end.After(now) {
		end = now
	}
	return start, end
}

const paymentBillReceiptColumns = `id,provider_instance_id,receiver_id,account_log_id,alipay_order_no,merchant_order_no,amount_minor,trans_at,status,review_reason,order_id,epay_order_id`

func scanPaymentBillReceipt(rows *sql.Rows) (PaymentBillReceipt, error) {
	var r PaymentBillReceipt
	var amount int64
	var order, epayOrder sql.NullInt64
	err := rows.Scan(&r.ID, &r.ProviderInstanceID, &r.ReceiverID, &r.AccountLogID, &r.AlipayOrderNo, &r.MerchantOrderNo, &amount, &r.TransAt, &r.Status, &r.ReviewReason, &order, &epayOrder)
	r.Amount = fmt.Sprintf("%d.%02d", amount/100, amount%100)
	if order.Valid {
		r.OrderID = &order.Int64
	}
	if epayOrder.Valid {
		r.EpayOrderID = &epayOrder.Int64
	}
	return r, err
}

func (s *PaymentService) ListAlipayBillReceipts(ctx context.Context, id int64, page, size int, status string) (*PaymentBillReceiptPage, error) {
	_, cfg, err := s.billProviderConfig(ctx, id)
	if err != nil {
		return nil, err
	}
	size, page = applyPagination(size, page)
	if page > 100000 {
		return nil, infraerrors.BadRequest("VALIDATION_ERROR", "page is too large")
	}
	if status != "" && status != "review" && status != "matched" && status != "ignored" {
		return nil, infraerrors.BadRequest("VALIDATION_ERROR", "invalid receipt status")
	}
	receiver := strings.TrimSpace(cfg["receiverId"])
	result := &PaymentBillReceiptPage{Items: []PaymentBillReceipt{}, Page: page, PageSize: size}
	rows, err := s.entClient.QueryContext(ctx, `SELECT COUNT(*) FROM payment_bill_receipts WHERE receiver_id=$1 AND ($2='' OR status=$2)`, receiver, status)
	if err != nil {
		return nil, err
	}
	if rows.Next() {
		err = rows.Scan(&result.Total)
	}
	rowsErr := rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	if rowsErr != nil {
		return nil, rowsErr
	}
	rows, err = s.entClient.QueryContext(ctx, `SELECT `+paymentBillReceiptColumns+` FROM payment_bill_receipts WHERE receiver_id=$1 AND ($2='' OR status=$2) ORDER BY trans_at DESC,id DESC LIMIT $3 OFFSET $4`, receiver, status, size, (page-1)*size)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		r, err := scanPaymentBillReceipt(rows)
		if err != nil {
			return nil, err
		}
		result.Items = append(result.Items, r)
	}
	return result, rows.Err()
}

func (s *PaymentService) ClaimAlipayBillReceipt(ctx context.Context, providerID, receiptID, orderID int64, actor, reason string) error {
	if strings.TrimSpace(reason) == "" || utf8.RuneCountInString(reason) > 500 {
		return infraerrors.BadRequest("ALIPAY_BILL_REVIEW_REASON_REQUIRED", "record how the payment was verified (up to 500 characters)")
	}
	_, cfg, err := s.billProviderConfig(ctx, providerID)
	if err != nil {
		return err
	}
	if receiptID <= 0 || orderID <= 0 {
		return infraerrors.BadRequest("VALIDATION_ERROR", "receipt and order IDs must be positive")
	}
	if err = s.claimBillReceipt(ctx, strings.TrimSpace(cfg["receiverId"]), receiptID, orderID, actor, strings.TrimSpace(reason), true); err != nil {
		return err
	}
	return s.executeFulfillment(ctx, orderID)
}

// claimBillReceipt commits the unique transaction claim and order's PAID state
// together. Fulfillment is separately idempotent and recoverable after a crash.
func (s *PaymentService) claimBillReceipt(ctx context.Context, receiver string, receiptID, orderID int64, actor, reason string, manual bool) error {
	mode := billClaimOfficialReference
	if manual {
		mode = billClaimManual
	}
	return s.claimBillReceiptWithMode(ctx, receiver, receiptID, orderID, actor, reason, mode, time.Time{})
}

func (s *PaymentService) claimBillReceiptWithMode(ctx context.Context, receiver string, receiptID, orderID int64, actor, reason string, mode billReceiptClaimMode, coveredThrough time.Time) error {
	tx, err := s.entClient.Tx(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	client := tx.Client()
	rows, err := client.QueryContext(ctx, `SELECT `+paymentBillReceiptColumns+` FROM payment_bill_receipts WHERE id=$1 AND receiver_id=$2`, receiptID, receiver)
	if err != nil {
		return err
	}
	if !rows.Next() {
		e := rows.Err()
		rows.Close()
		if e != nil {
			return e
		}
		return infraerrors.NotFound("NOT_FOUND", "receipt not found")
	}
	receipt, err := scanPaymentBillReceipt(rows)
	rows.Close()
	if err != nil {
		return err
	}
	// Retrying a committed claim is safe; a different target order is not.
	if receipt.Status == "matched" {
		if receipt.OrderID != nil && *receipt.OrderID == orderID && receipt.EpayOrderID == nil {
			return nil
		}
		return infraerrors.Conflict("ALIPAY_BILL_ALREADY_CLAIMED", "receipt already belongs to another order")
	}
	if receipt.Status != "review" {
		return infraerrors.Conflict("ALIPAY_BILL_NOT_REVIEWABLE", "receipt is not available for review")
	}
	// Manual confirmation can change a cancelled order back to PAID. Serialize
	// that transition with amount reuse just like automatic confirmation.
	if err := s.lockBillReceiptAmount(ctx, tx, &receipt); err != nil {
		return err
	}
	if err := s.lockAlipayTransaction(ctx, tx, receipt.AlipayOrderNo); err != nil {
		return err
	}
	order, err := tx.PaymentOrder.Get(ctx, orderID)
	if err != nil {
		if dbent.IsNotFound(err) {
			return infraerrors.NotFound("NOT_FOUND", "order not found")
		}
		return err
	}
	if rejection := billLocalReceiptReasonForMode(&receipt, order, mode); rejection != "" {
		return infraerrors.Conflict("ALIPAY_BILL_MATCH_REJECTED", "receipt cannot be assigned to this order").WithMetadata(map[string]string{"reason": rejection})
	}
	if mode == billClaimReservedAmount {
		if err := checkBillAmountReceiptTarget(ctx, client, &receipt, order.ID, 0, order.CreatedAt, order.ExpiresAt, coveredThrough); err != nil {
			return err
		}
	}
	// Account logs also include transactions settled by our other Alipay
	// integrations. They must never be reused to fund a new fixed-QR order.
	if err := ensureBillTransactionUnclaimed(ctx, client, receipt.AlipayOrderNo, orderID, 0); err != nil {
		return err
	}
	if err := ensureBillReceiptReferenceTarget(ctx, client, receipt.MerchantOrderNo, orderID, 0); err != nil {
		return err
	}
	now := time.Now().UTC()
	claim, err := client.ExecContext(ctx, `INSERT INTO payment_bill_claims(receipt_id,receiver_id,alipay_order_no,order_id,actor,reason,created_at) VALUES($1,$2,$3,$4,$5,$6,$7) ON CONFLICT DO NOTHING`, receiptID, receiver, receipt.AlipayOrderNo, orderID, actor, reason, now)
	if err != nil {
		return err
	}
	n, err := claim.RowsAffected()
	if err != nil {
		return err
	}
	if n != 1 {
		return infraerrors.Conflict("ALIPAY_BILL_ALREADY_CLAIMED", "this receipt, transaction, or order has already been claimed")
	}
	count, err := tx.PaymentOrder.Update().Where(paymentorder.ID(orderID), paymentorder.Status(order.Status)).SetStatus(OrderStatusPaid).SetPaymentTradeNo(receipt.AlipayOrderNo).SetPaidAt(receipt.TransAt).ClearFailedAt().ClearFailedReason().Save(ctx)
	if err != nil {
		return err
	}
	if count != 1 {
		return infraerrors.Conflict("ALIPAY_BILL_ORDER_CHANGED", "order changed during reconciliation")
	}
	result, err := client.ExecContext(ctx, `UPDATE payment_bill_receipts SET status='matched',review_reason='',order_id=$1,epay_order_id=NULL WHERE id=$2 AND status='review'`, orderID, receiptID)
	if err != nil {
		return err
	}
	countRows, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if countRows != 1 {
		return infraerrors.Conflict("ALIPAY_BILL_ALREADY_CLAIMED", "receipt was claimed concurrently")
	}
	details := map[string]any{"receipt_id": receiptID, "trade_no": receipt.AlipayOrderNo, "reason": reason, "manual": mode == billClaimManual, "match_mode": billClaimModeLabel(mode), "previous_status": order.Status}
	detail, _ := json.Marshal(details)
	if _, err = tx.PaymentAuditLog.Create().SetOrderID(strconv.FormatInt(orderID, 10)).SetAction("BILL_PAYMENT_CONFIRMED").SetOperator(actor).SetDetail(string(detail)).Save(ctx); err != nil {
		return err
	}
	if err = closeBillAmountReservation(ctx, client, orderID, 0, time.Now().UTC()); err != nil {
		return err
	}
	return tx.Commit()
}
