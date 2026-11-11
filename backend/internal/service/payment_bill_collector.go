package service

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/ent/paymentorder"
	"github.com/Wei-Shaw/sub2api/ent/paymentproviderinstance"
	"github.com/Wei-Shaw/sub2api/internal/payment"
	"github.com/Wei-Shaw/sub2api/internal/payment/provider"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/google/uuid"
)

// storeBillReceipt ignores debits, and never logs raw upstream rows. A duplicate
// account-log ID is immutable: later scans cannot overwrite money already claimed.
func (s *PaymentService) storeBillReceipt(ctx context.Context, instanceID int64, receiver string, entry provider.AlipayBillEntry) (*PaymentBillReceipt, bool, error) {
	if entry.Direction != "收入" {
		return nil, false, nil
	}
	cents, err := billObservedAmountCents(entry.Amount)
	if err != nil {
		return nil, false, infraerrors.ServiceUnavailable("ALIPAY_BILL_INVALID_RESPONSE", "receipt amount is not supported")
	}
	if entry.AccountLogID == "" || len(entry.AccountLogID) > 128 || len(entry.AlipayOrderNo) > 128 || len(entry.MerchantOrderNo) > 128 || entry.TransAt.IsZero() {
		return nil, false, infraerrors.ServiceUnavailable("ALIPAY_BILL_INVALID_RESPONSE", "receipt identifiers are invalid")
	}
	tx, err := s.entClient.Tx(ctx)
	if err != nil {
		return nil, false, err
	}
	defer tx.Rollback()
	if _, _, err = s.lockBillProviderConfig(ctx, tx, instanceID, receiver); err != nil {
		return nil, false, err
	}
	if err = s.lockBillAmountKey(ctx, tx, receiver, cents); err != nil {
		return nil, false, err
	}
	result, err := tx.Client().ExecContext(ctx, `INSERT INTO payment_bill_receipts(provider_instance_id,receiver_id,account_log_id,alipay_order_no,merchant_order_no,amount_minor,trans_at,status,review_reason,created_at) VALUES($1,$2,$3,$4,$5,$6,$7,'review','awaiting_review',$8) ON CONFLICT(receiver_id,account_log_id) DO NOTHING`, instanceID, receiver, entry.AccountLogID, entry.AlipayOrderNo, entry.MerchantOrderNo, cents, entry.TransAt, time.Now().UTC())
	if err != nil {
		return nil, false, err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return nil, false, err
	}
	rows, err := tx.Client().QueryContext(ctx, `SELECT `+paymentBillReceiptColumns+` FROM payment_bill_receipts WHERE receiver_id=$1 AND account_log_id=$2`, receiver, entry.AccountLogID)
	if err != nil {
		return nil, false, err
	}
	defer rows.Close()
	if !rows.Next() {
		if rows.Err() != nil {
			return nil, false, rows.Err()
		}
		return nil, false, fmt.Errorf("inserted receipt could not be read")
	}
	receipt, err := scanPaymentBillReceipt(rows)
	rows.Close()
	if err != nil {
		return nil, false, err
	}
	if err = tx.Commit(); err != nil {
		return nil, false, err
	}
	return &receipt, affected == 1, nil
}

func (s *PaymentService) acquireBillSyncLease(ctx context.Context, receiver, owner string, now time.Time) (time.Time, error) {
	rows, err := s.entClient.QueryContext(ctx, `INSERT INTO payment_bill_sync_state(receiver_id,lease_until,lease_owner,updated_at) VALUES($1,$2,$3,$4) ON CONFLICT(receiver_id) DO UPDATE SET lease_until=EXCLUDED.lease_until,lease_owner=EXCLUDED.lease_owner,updated_at=EXCLUDED.updated_at WHERE payment_bill_sync_state.lease_until < $4 RETURNING cursor_at`, receiver, now.Add(2*time.Minute), owner, now)
	if err != nil {
		return time.Time{}, err
	}
	defer rows.Close()
	if !rows.Next() {
		if rows.Err() != nil {
			return time.Time{}, rows.Err()
		}
		return time.Time{}, infraerrors.Conflict("ALIPAY_BILL_SYNC_BUSY", "this receiving account is already being synchronized")
	}
	var cursor sql.NullTime
	if err = rows.Scan(&cursor); err != nil {
		return time.Time{}, err
	}
	return cursor.Time, nil
}

func (s *PaymentService) releaseBillSyncLease(receiver, owner string, cursor time.Time, success bool) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	now := time.Now().UTC()
	var checkpoint any
	if !cursor.IsZero() {
		checkpoint = cursor
	}
	code := ""
	if !success {
		code = "sync_failed"
	}
	_, err := s.entClient.ExecContext(ctx, `UPDATE payment_bill_sync_state SET cursor_at=COALESCE($1,cursor_at),lease_until=$6,error_code=$3,updated_at=$2 WHERE receiver_id=$4 AND lease_owner=$5`, checkpoint, now, code, receiver, owner, time.Unix(0, 0).UTC())
	if err != nil {
		slog.Warn("[AlipayBill] unable to release sync lease")
	}
}

func (s *PaymentService) SyncAlipayBills(ctx context.Context, id int64) (*PaymentBillSyncResult, error) {
	return s.syncAlipayBills(ctx, id, false)
}

func (s *PaymentService) syncAlipayBills(ctx context.Context, id int64, recentOnly bool) (*PaymentBillSyncResult, error) {
	_, cfg, err := s.billProviderConfig(ctx, id)
	if err != nil {
		return nil, err
	}
	reader, err := provider.NewAlipayBill(strconv.FormatInt(id, 10), cfg)
	if err != nil {
		return nil, err
	}
	return s.syncAlipayBillsWithReaderMode(ctx, id, strings.TrimSpace(cfg["receiverId"]), reader, recentOnly)
}

type billStatementReader interface {
	QueryBills(context.Context, time.Time, time.Time, int) (*provider.AlipayBillPage, error)
}

func (s *PaymentService) syncAlipayBillsWithReader(ctx context.Context, id int64, receiver string, reader billStatementReader) (*PaymentBillSyncResult, error) {
	return s.syncAlipayBillsWithReaderMode(ctx, id, receiver, reader, false)
}

func (s *PaymentService) syncAlipayBillsWithReaderMode(ctx context.Context, id int64, receiver string, reader billStatementReader, recentOnly bool) (*PaymentBillSyncResult, error) {
	ctx, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()
	now := time.Now().UTC().Truncate(time.Second)
	owner := uuid.NewString()
	cursor, err := s.acquireBillSyncLease(ctx, receiver, owner, now)
	if err != nil {
		return nil, err
	}
	successful := false
	defer func() { s.releaseBillSyncLease(receiver, owner, cursor, successful) }()
	result := &PaymentBillSyncResult{CheckedAt: now, receipts: make(map[int64]*PaymentBillReceipt)}
	// Catch up at most 32 one-hour windows per call. Advance the cursor only
	// after a complete page set, so partial API failures never skip money.
	for window := 0; window < 32; window++ {
		start, end := billSyncWindow(now, cursor)
		if err := s.collectBillWindow(ctx, id, receiver, reader, start, end, result); err != nil {
			return nil, err
		}
		cursor = end
		if !end.Before(now) {
			break
		}
	}
	// An independent durable cursor revisits the previous day's older rows.
	// Keeping each historical window to one hour avoids a busy account's
	// entire-day total repeatedly exceeding the per-window page bound.
	// The faster checkout loop needs the forward overlap only. Historical
	// recovery retains its regular cadence instead of being queried every 5s.
	if !recentOnly {
		if err := s.rescanBillWindow(ctx, id, receiver, owner, now, reader, result); err != nil {
			return nil, err
		}
	}
	if err := s.loadUnreconciledBillReceipts(ctx, receiver, result.receipts); err != nil {
		return nil, err
	}
	// All pages/windows must be persisted before amount matching, otherwise the
	// first row could claim an amount before a second conflicting row is seen.
	ids := make([]int64, 0, len(result.receipts))
	for id := range result.receipts {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	for _, id := range ids {
		receipt := result.receipts[id]
		matched, err := s.reconcileBillReceiptThrough(ctx, receipt, cursor)
		if err != nil {
			return nil, err
		}
		if matched {
			result.Matched++
		} else if receipt.Status == "review" {
			result.Review++
		}
	}
	result.receipts = nil
	successful = true
	return result, nil
}

func (s *PaymentService) collectBillWindow(ctx context.Context, id int64, receiver string, reader billStatementReader, start, end time.Time, result *PaymentBillSyncResult) error {
	for pageNo := 1; pageNo <= 20; pageNo++ {
		page, err := reader.QueryBills(ctx, start, end, pageNo)
		if err != nil {
			return err
		}
		if err = validateBillPage(page, pageNo); err != nil {
			return err
		}
		result.Fetched += len(page.Items)
		for _, entry := range page.Items {
			receipt, inserted, err := s.storeBillReceipt(ctx, id, receiver, entry)
			if err != nil {
				return err
			}
			if inserted {
				result.Inserted++
			}
			if receipt == nil {
				continue
			}
			result.receipts[receipt.ID] = receipt
		}
		if pageNo*1000 >= page.Total {
			return nil
		}
		if pageNo == 20 {
			return infraerrors.ServiceUnavailable("ALIPAY_BILL_QUERY_LIMIT", "statement hour has too many rows; sync has retained its cursor")
		}
	}
	return nil
}

func billRescanWindow(now, cursor time.Time) (time.Time, time.Time) {
	start := cursor
	if start.IsZero() || start.Before(now.Add(-24*time.Hour)) || !start.Before(now) {
		start = now.Add(-24 * time.Hour)
	}
	end := start.Add(time.Hour)
	if end.After(now) {
		end = now
	}
	return start, end
}

func (s *PaymentService) rescanBillWindow(ctx context.Context, id int64, receiver, owner string, now time.Time, reader billStatementReader, result *PaymentBillSyncResult) error {
	rows, err := s.entClient.QueryContext(ctx, `SELECT rescan_cursor_at FROM payment_bill_sync_state WHERE receiver_id=$1 AND lease_owner=$2`, receiver, owner)
	if err != nil {
		return err
	}
	var cursor sql.NullTime
	if !rows.Next() {
		err = rows.Err()
		rows.Close()
		if err != nil {
			return err
		}
		return infraerrors.Conflict("ALIPAY_BILL_SYNC_BUSY", "statement synchronization lease changed")
	}
	err = rows.Scan(&cursor)
	rows.Close()
	if err != nil {
		return err
	}
	start, end := billRescanWindow(now, cursor.Time)
	if err = s.collectBillWindow(ctx, id, receiver, reader, start, end, result); err != nil {
		return err
	}
	var next any
	// The forward query overlaps five minutes, so the historical sweep can
	// reset once it reaches that overlap instead of chasing a moving clock.
	if end.Before(now.Add(-5 * time.Minute)) {
		next = end
	}
	_, err = s.entClient.ExecContext(ctx, `UPDATE payment_bill_sync_state SET rescan_cursor_at=$1 WHERE receiver_id=$2 AND lease_owner=$3`, next, receiver, owner)
	return err
}

func (s *PaymentService) reconcileBillReceipt(ctx context.Context, r *PaymentBillReceipt) (bool, error) {
	through, err := successfulBillSyncThrough(ctx, s.entClient, r.ReceiverID)
	if err != nil {
		return false, err
	}
	return s.reconcileBillReceiptThrough(ctx, r, through)
}

func (s *PaymentService) reconcileBillReceiptThrough(ctx context.Context, r *PaymentBillReceipt, coveredThrough time.Time) (bool, error) {
	if r.Status == "matched" && r.EpayOrderID != nil {
		// External orders are recovered through the durable notification queue.
		return true, nil
	}
	if r.Status == "matched" && r.OrderID != nil {
		order, err := s.entClient.PaymentOrder.Get(ctx, *r.OrderID)
		if err != nil {
			return false, err
		}
		if order.Status == OrderStatusPaid || order.Status == OrderStatusRecharging || order.Status == OrderStatusFailed {
			if err := s.executeFulfillment(ctx, order.ID); err != nil {
				slog.Warn("[AlipayBill] fulfillment needs retry", "order_id", order.ID)
			}
		}
		return true, nil
	}
	if r.Status != "review" {
		return false, nil
	}
	reason := billReceiptMatchReason(r, nil, false)
	var order *dbent.PaymentOrder
	var epayOrder *BuiltinEasyPayOrder
	if r.MerchantOrderNo != "" && r.AlipayOrderNo != "" {
		var err error
		order, err = s.entClient.PaymentOrder.Query().Where(paymentorder.OutTradeNo(r.MerchantOrderNo)).Only(ctx)
		if err != nil && !dbent.IsNotFound(err) {
			return false, err
		}
		epayOrder, err = findBuiltinEasyPayReceiptOrder(ctx, s.entClient, r.MerchantOrderNo)
		if err != nil {
			return false, err
		}
		switch {
		case order != nil && epayOrder != nil:
			reason = "ambiguous_order_reference"
		case epayOrder != nil:
			reason = billEpayReceiptMatchReason(r, epayOrder, false)
		default:
			reason = billReceiptMatchReason(r, order, false)
		}
	}
	if reason != "" {
		if order == nil && epayOrder == nil && r.AlipayOrderNo != "" {
			matched, amountReason, err := s.reconcileBillAmountReceipt(ctx, r, coveredThrough)
			if err != nil {
				return false, err
			}
			if matched {
				return true, nil
			}
			// Keep legacy review reasons for receipts without a reservation.
			if amountReason != "" && amountReason != "amount_window_missing" {
				reason = amountReason
			}
		}
		_, err := s.entClient.ExecContext(ctx, `UPDATE payment_bill_receipts SET review_reason=$1 WHERE id=$2 AND status='review'`, reason, r.ID)
		return false, err
	}
	var claimErr error
	if epayOrder != nil {
		claimErr = s.claimBuiltinEasyPayReceipt(ctx, r.ReceiverID, r.ID, epayOrder.ID, "system", "official merchant_order_no matched gateway trade_no", false)
	} else {
		claimErr = s.claimBillReceipt(ctx, r.ReceiverID, r.ID, order.ID, "system", "official merchant_order_no matched", false)
	}
	if err := claimErr; err != nil {
		if infraerrors.Reason(err) == "ALIPAY_BILL_ALREADY_CLAIMED" {
			_, updateErr := s.entClient.ExecContext(ctx, `UPDATE payment_bill_receipts SET review_reason='duplicate_transaction' WHERE id=$1 AND status='review'`, r.ID)
			return false, updateErr
		}
		return false, err
	}
	if epayOrder != nil {
		return true, nil
	}
	if err := s.executeFulfillment(ctx, order.ID); err != nil {
		slog.Warn("[AlipayBill] fulfillment needs retry", "order_id", order.ID)
	}
	return true, nil
}

// Persisted rows are the reconciliation queue. A later page error can advance
// the query checkpoint without settling earlier receipts; restart/retry must
// still revisit those receipts even after they leave the five-minute overlap.
func (s *PaymentService) loadUnreconciledBillReceipts(ctx context.Context, receiver string, batch map[int64]*PaymentBillReceipt) error {
	rows, err := s.entClient.QueryContext(ctx, `SELECT `+paymentBillReceiptColumns+` FROM payment_bill_receipts r WHERE receiver_id=$1 AND status='review' AND (
		EXISTS(SELECT 1 FROM payment_bill_amount_reservations a WHERE a.receiver_id=r.receiver_id AND a.amount_minor=r.amount_minor AND a.created_at<=r.trans_at AND COALESCE(a.user_cancelled_at,a.closed_at,a.expires_at)>r.trans_at)
		OR EXISTS(SELECT 1 FROM payment_orders o WHERE o.out_trade_no=r.merchant_order_no AND r.merchant_order_no<>'')
		OR EXISTS(SELECT 1 FROM payment_epay_orders o WHERE o.trade_no=r.merchant_order_no AND r.merchant_order_no<>''))`, receiver)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		r, err := scanPaymentBillReceipt(rows)
		if err != nil {
			return err
		}
		batch[r.ID] = &r
	}
	return rows.Err()
}

// Source switching does not disable an instance. All enabled bill instances
// continue collecting, including instances no longer selected for new orders.
func (s *PaymentService) CollectAlipayBillReceipts(ctx context.Context) error {
	return s.collectAlipayBillReceipts(ctx, false)
}

// CollectActiveAlipayBillReceipts refreshes recent checkouts without increasing
// statement requests for idle providers or running another historical sweep.
func (s *PaymentService) CollectActiveAlipayBillReceipts(ctx context.Context) error {
	return s.collectAlipayBillReceipts(ctx, true)
}

func (s *PaymentService) collectAlipayBillReceipts(ctx context.Context, recentOnly bool) error {
	// A committed claim survives restarts even if the upstream row is no
	// longer inside the lookback window, or its provider is now disabled.
	if err := s.recoverClaimedBillOrders(ctx); err != nil {
		return err
	}
	instances, err := s.entClient.PaymentProviderInstance.Query().Where(paymentproviderinstance.ProviderKey(payment.TypeAlipayBill)).All(ctx)
	if err != nil {
		return err
	}
	seen := map[string]bool{}
	now := time.Now().UTC()
	for _, inst := range instances {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		var eligible bool
		if recentOnly {
			eligible, err = s.shouldCollectActiveAlipayBillProvider(ctx, inst, now)
		} else {
			eligible, err = s.shouldCollectAlipayBillProvider(ctx, inst, now)
		}
		if err != nil {
			return err
		}
		if !eligible {
			continue
		}
		cfg, err := s.configService.decryptProviderConfig(inst.ProviderKey, inst.Config)
		if err != nil {
			continue
		}
		receiver := strings.TrimSpace(cfg["receiverId"])
		if receiver == "" || seen[receiver] {
			continue
		}
		if _, err = s.syncAlipayBills(ctx, inst.ID, recentOnly); err != nil {
			if infraerrors.Reason(err) != "ALIPAY_BILL_SYNC_BUSY" {
				slog.Warn("[AlipayBill] receipt sync failed", "provider_instance_id", inst.ID, "reason", infraerrors.Reason(err))
			}
		} else {
			seen[receiver] = true
		}
	}
	return nil
}

// Pending/reconciling checkouts get a short polling cadence until two minutes
// after the payment deadline. Settled/cancelled orders remain active only until
// a successful statement covers their effective ownership window; after that
// the normal historical collector handles late receipts.
func (s *PaymentService) shouldCollectActiveAlipayBillProvider(ctx context.Context, inst *dbent.PaymentProviderInstance, now time.Time) (bool, error) {
	rows, err := s.entClient.QueryContext(ctx, `SELECT EXISTS (
		SELECT 1 FROM payment_epay_orders o
		LEFT JOIN payment_bill_amount_reservations a ON a.epay_order_id=o.id
		LEFT JOIN payment_bill_sync_state b ON b.receiver_id=o.receiver_id
		WHERE o.provider_instance_id=$1 AND o.expires_at>$2 AND (
			o.status IN ('PENDING','EXPIRED') OR (o.status='PAID' AND
				(b.cursor_at IS NULL OR b.cursor_at<COALESCE(a.user_cancelled_at,a.closed_at,o.expires_at) OR b.error_code<>''))
		)
	) OR EXISTS (
		SELECT 1 FROM payment_orders o
		LEFT JOIN payment_bill_amount_reservations a ON a.order_id=o.id
		LEFT JOIN payment_bill_sync_state b ON b.receiver_id=o.provider_snapshot->>'receiver_id'
		WHERE o.provider_key='alipay_bill' AND o.provider_instance_id=$3 AND o.expires_at>$2 AND (
			o.status IN ('PENDING','EXPIRED','PAID','RECHARGING','FAILED') OR (o.status IN ('CANCELLED','COMPLETED') AND
				(b.cursor_at IS NULL OR b.cursor_at<COALESCE(a.user_cancelled_at,a.closed_at,o.expires_at) OR b.error_code<>''))
		)
	)`, inst.ID, now.Add(-2*time.Minute), strconv.FormatInt(inst.ID, 10))
	if err != nil {
		return false, fmt.Errorf("check active fixed QR payments: %w", err)
	}
	defer rows.Close()
	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return false, err
		}
		return false, fmt.Errorf("active fixed QR check returned no result")
	}
	var active bool
	if err := rows.Scan(&active); err != nil {
		return false, err
	}
	return active, rows.Err()
}

func (s *PaymentService) shouldCollectAlipayBillProvider(ctx context.Context, inst *dbent.PaymentProviderInstance, now time.Time) (bool, error) {
	if inst.Enabled {
		return true, nil
	}
	// Disabling new payments must not hide an already-issued fixed QR's
	// receipt. Keep a bounded recovery window for delayed bills and manual
	// review after expiry, without continually scanning old disabled accounts.
	rows, err := s.entClient.QueryContext(ctx, `SELECT EXISTS (
		SELECT 1 FROM payment_epay_orders WHERE provider_instance_id=$1
		AND status IN ('PENDING','EXPIRED') AND expires_at>$2
	) OR EXISTS (
		SELECT 1 FROM payment_orders WHERE provider_key='alipay_bill'
		AND provider_instance_id=$3 AND status IN ('PENDING','EXPIRED') AND expires_at>$2
	)`, inst.ID, now.Add(-24*time.Hour), strconv.FormatInt(inst.ID, 10))
	if err != nil {
		return false, fmt.Errorf("check gateway receipt recovery eligibility: %w", err)
	}
	defer rows.Close()
	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return false, err
		}
		return false, fmt.Errorf("gateway receipt recovery eligibility returned no result")
	}
	var eligible bool
	if err := rows.Scan(&eligible); err != nil {
		return false, err
	}
	return eligible, rows.Err()
}

var billObservedAmountPattern = regexp.MustCompile(`^(0|[1-9][0-9]{0,15})(\.[0-9]{1,2})?$`)

func billObservedAmountCents(raw string) (int64, error) {
	if !billObservedAmountPattern.MatchString(raw) {
		return 0, fmt.Errorf("invalid receipt amount")
	}
	parts := strings.SplitN(raw, ".", 2)
	whole, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil {
		return 0, err
	}
	fraction := "00"
	if len(parts) == 2 {
		fraction = parts[1]
		if len(fraction) == 1 {
			fraction += "0"
		}
	}
	cents, _ := strconv.ParseInt(fraction, 10, 64)
	amount := whole*100 + cents
	if amount <= 0 {
		return 0, fmt.Errorf("nonpositive receipt amount")
	}
	return amount, nil
}

func validateBillPage(page *provider.AlipayBillPage, expectedPage int) error {
	if page == nil || page.Page != expectedPage || page.Total < 0 {
		return infraerrors.ServiceUnavailable("ALIPAY_BILL_INVALID_RESPONSE", "invalid bill page")
	}
	remaining := page.Total - (expectedPage-1)*1000
	if remaining < 0 {
		remaining = 0
	}
	if remaining > 1000 {
		remaining = 1000
	}
	if len(page.Items) != remaining {
		return infraerrors.ServiceUnavailable("ALIPAY_BILL_INVALID_RESPONSE", "incomplete bill page; cursor retained")
	}
	return nil
}

func (s *PaymentService) recoverClaimedBillOrders(ctx context.Context) error {
	rows, err := s.entClient.QueryContext(ctx, `SELECT o.id FROM payment_bill_claims c JOIN payment_orders o ON o.id=c.order_id WHERE o.status IN ('PAID','FAILED','RECHARGING') ORDER BY o.updated_at,o.id LIMIT 20`)
	if err != nil {
		return err
	}
	var ids []int64
	for rows.Next() {
		var id int64
		if err = rows.Scan(&id); err != nil {
			rows.Close()
			return err
		}
		ids = append(ids, id)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	for _, id := range ids {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if err = s.executeFulfillment(ctx, id); err != nil {
			slog.Warn("[AlipayBill] claimed order fulfillment needs retry", "order_id", id)
		}
	}
	return nil
}
