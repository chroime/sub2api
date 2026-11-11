package service

import (
	"context"
	"database/sql"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	"entgo.io/ent/dialect"
	dbent "github.com/Wei-Shaw/sub2api/ent"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
)

const (
	billAmountOrderTTL   = time.Minute
	billAmountReuseDelay = 2 * time.Minute
	billAmountSyncAge    = 2 * time.Minute
)

// Provider IDs deliberately are not part of this key. All providers and both
// order sources using one receiving account must serialize the same amount.
func (s *PaymentService) lockBillAmountKey(ctx context.Context, tx *dbent.Tx, receiver string, amountMinor int64) error {
	if receiver == "" || strings.TrimSpace(receiver) != receiver || amountMinor <= 0 {
		return infraerrors.BadRequest("ALIPAY_BILL_INVALID_AMOUNT", "invalid receiving account or payment amount")
	}
	if s.entClient.Driver().Dialect() != dialect.Postgres {
		return nil
	}
	_, err := tx.Client().ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1, 0))`, "alipay_amount:"+receiver+":"+strconv.FormatInt(amountMinor, 10))
	return err
}

// lockBillAmountReservation must be called before creating the order, and its
// transaction must also insert the immutable reservation before committing.
func (s *PaymentService) lockBillAmountReservation(ctx context.Context, tx *dbent.Tx, receiver string, amountMinor int64, now time.Time) error {
	if err := s.lockBillAmountKey(ctx, tx, receiver, amountMinor); err != nil {
		return err
	}
	client := tx.Client()
	var lastExpiry, reuseAfter time.Time
	cancelledBlocker := false
	rows, err := client.QueryContext(ctx, `SELECT a.expires_at,a.reuse_after,a.closed_at,CASE WHEN o.status='CANCELLED' THEN 1 ELSE 0 END,
		EXISTS(SELECT 1 FROM payment_bill_claims c WHERE c.order_id=a.order_id OR c.epay_order_id=a.epay_order_id)
		FROM payment_bill_amount_reservations a LEFT JOIN payment_orders o ON o.id=a.order_id
		WHERE a.receiver_id=$1 AND a.amount_minor=$2 AND a.user_cancelled_at IS NULL`, receiver, amountMinor)
	if err != nil {
		return err
	}
	for rows.Next() {
		var expiry, recordedReuse time.Time
		var closed sql.NullTime
		var cancelled int
		var confirmed bool
		if err = rows.Scan(&expiry, &recordedReuse, &closed, &cancelled, &confirmed); err != nil {
			break
		}
		if closed.Valid {
			expiry = closed.Time
		}
		if expiry.After(lastExpiry) {
			lastExpiry = expiry
		}
		effectiveReuse := recordedReuse
		// Confirmed claims permanently close amount ownership; fulfillment or
		// callback retries must not reopen it. Old confirmed rows with no closure
		// keep their original end, but also skip the extra cooling delay.
		// Cancelled orders still retain their full original payment window.
		if confirmed || cancelled == 1 {
			effectiveReuse = expiry
		}
		if effectiveReuse.After(reuseAfter) || (effectiveReuse.Equal(reuseAfter) && cancelled == 0) {
			reuseAfter = effectiveReuse
			cancelledBlocker = cancelled == 1
		}
	}
	rowsErr := rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	if rowsErr != nil {
		return rowsErr
	}
	// Previously issued static codes have no reservation history. They still
	// block new checkout during their old payment/cooling window, but are never
	// retroactively granted an automatic amount-matching reservation.
	for _, query := range []string{
		`SELECT o.expires_at FROM payment_orders o WHERE o.provider_key='alipay_bill' AND o.provider_snapshot->>'receiver_id'=$1 AND o.pay_amount=$2 AND o.status IN ('PENDING','EXPIRED','CANCELLED','FAILED') AND NOT EXISTS(SELECT 1 FROM payment_bill_amount_reservations r WHERE r.order_id=o.id)
		AND NOT EXISTS(SELECT 1 FROM payment_audit_logs l WHERE l.order_id=CAST(o.id AS TEXT) AND l.action='ORDER_CANCELLED' AND l.operator='user:' || CAST(o.user_id AS TEXT)) ORDER BY o.expires_at DESC LIMIT 1`,
		`SELECT o.expires_at FROM payment_epay_orders o WHERE o.receiver_id=$1 AND o.amount_minor=$2 AND o.status IN ('PENDING','EXPIRED') AND NOT EXISTS(SELECT 1 FROM payment_bill_amount_reservations r WHERE r.epay_order_id=o.id) ORDER BY o.expires_at DESC LIMIT 1`,
	} {
		var amount any = amountMinor
		if strings.Contains(query, "o.pay_amount") {
			amount = float64(amountMinor) / 100
		}
		rows, err := client.QueryContext(ctx, query, receiver, amount)
		if err != nil {
			return err
		}
		var legacyExpiry time.Time
		if rows.Next() {
			err = rows.Scan(&legacyExpiry)
		}
		rowsErr := rows.Err()
		rows.Close()
		if err != nil {
			return err
		}
		if rowsErr != nil {
			return rowsErr
		}
		if legacyExpiry.After(lastExpiry) {
			lastExpiry = legacyExpiry
		}
		if !legacyExpiry.IsZero() && !legacyExpiry.Add(billAmountReuseDelay).Before(reuseAfter) {
			reuseAfter = legacyExpiry.Add(billAmountReuseDelay)
			cancelledBlocker = false
		}
	}
	if now.Before(reuseAfter) {
		seconds := int64(math.Ceil(reuseAfter.Sub(now).Seconds()))
		if cancelledBlocker {
			return infraerrors.Conflict("ALIPAY_BILL_CANCELLED_AMOUNT_WAIT", "the cancelled order retains its original payment window; retry after the deadline and bill sync").WithMetadata(map[string]string{"retry_after_seconds": strconv.FormatInt(seconds, 10)})
		}
		return infraerrors.Conflict("ALIPAY_BILL_AMOUNT_BUSY", "this payment amount is reserved; retry after the payment and cooling window").WithMetadata(map[string]string{"retry_after_seconds": strconv.FormatInt(seconds, 10)})
	}
	if lastExpiry.IsZero() {
		return nil
	}
	rows, err = client.QueryContext(ctx, `SELECT cursor_at,error_code,updated_at FROM payment_bill_sync_state WHERE receiver_id=$1`, receiver)
	if err != nil {
		return err
	}
	var cursor sql.NullTime
	var syncError string
	var updated time.Time
	found := rows.Next()
	if found {
		err = rows.Scan(&cursor, &syncError, &updated)
	}
	rowsErr = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	if rowsErr != nil {
		return rowsErr
	}
	// Acquiring a sync lease also refreshes updated_at. The durable cursor must
	// itself be recent, so starting an in-flight query cannot make stale state
	// appear to be a newly successful sync.
	if !found || !cursor.Valid || cursor.Time.Before(lastExpiry) || cursor.Time.Before(now.Add(-billAmountSyncAge)) || syncError != "" || updated.Before(now.Add(-billAmountSyncAge)) {
		return infraerrors.Conflict("ALIPAY_BILL_SYNC_REQUIRED", "the previous amount reservation is awaiting a successful recent bill sync")
	}
	rows, err = client.QueryContext(ctx, `SELECT COALESCE(MAX(CASE WHEN o.status='CANCELLED' THEN 1 ELSE 0 END),0),COUNT(*)
		FROM payment_bill_amount_reservations a
		LEFT JOIN payment_orders o ON o.id=a.order_id
		JOIN payment_bill_receipts r ON r.receiver_id=a.receiver_id AND r.amount_minor=a.amount_minor
			AND r.trans_at>=a.created_at AND r.trans_at<COALESCE(a.closed_at,a.expires_at)
		WHERE a.receiver_id=$1 AND a.amount_minor=$2 AND a.user_cancelled_at IS NULL AND r.status='review'
			AND (o.status='CANCELLED' OR a.closed_at IS NOT NULL
				OR EXISTS(SELECT 1 FROM payment_bill_claims c WHERE c.order_id=a.order_id OR c.epay_order_id=a.epay_order_id))
			AND r.order_id IS NULL AND r.epay_order_id IS NULL
			AND NOT EXISTS(SELECT 1 FROM payment_bill_claims c WHERE c.receipt_id=r.id OR (r.alipay_order_no<>'' AND c.alipay_order_no=r.alipay_order_no))
	`, receiver, amountMinor)
	if err != nil {
		return err
	}
	var cancelledReview, unresolved int
	if rows.Next() {
		err = rows.Scan(&cancelledReview, &unresolved)
	} else if rows.Err() == nil {
		err = fmt.Errorf("cancelled bill receipt query returned no result")
	}
	rowsErr = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	if rowsErr != nil {
		return rowsErr
	}
	if unresolved > 0 {
		if cancelledReview > 0 {
			return infraerrors.Conflict("ALIPAY_BILL_CANCELLED_PAYMENT_REVIEW", "a receipt in a cancelled payment window needs review before reusing this amount")
		}
		return infraerrors.Conflict("ALIPAY_BILL_PAYMENT_REVIEW", "an additional receipt in a confirmed payment window needs review before reusing this amount")
	}
	return nil
}

// closeBillAmountReservation runs inside the claim transaction with the amount
// lock held. Alipay timestamps have second precision: rounding confirmation UP
// keeps the entire confirmation second with the old order. The next reservation
// waits for successful statement coverage of this exclusive endpoint.
func closeBillAmountReservation(ctx context.Context, client *dbent.Client, localID, externalID int64, confirmedAt time.Time) error {
	rows, err := client.QueryContext(ctx, `SELECT created_at,expires_at,closed_at FROM payment_bill_amount_reservations WHERE order_id=$1 OR epay_order_id=$2`, localID, externalID)
	if err != nil {
		return err
	}
	var created, expiry time.Time
	var existing sql.NullTime
	found := rows.Next()
	if found {
		err = rows.Scan(&created, &expiry, &existing)
	}
	rowsErr := rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	if rowsErr != nil {
		return rowsErr
	}
	if !found || existing.Valid {
		return nil
	}
	closed := confirmedAt.UTC().Truncate(time.Second)
	if closed.Before(confirmedAt) {
		closed = closed.Add(time.Second)
	}
	if closed.After(expiry) {
		closed = expiry
	}
	if !closed.After(created) {
		return fmt.Errorf("bill confirmation precedes its amount reservation")
	}
	_, err = client.ExecContext(ctx, `UPDATE payment_bill_amount_reservations SET closed_at=$1 WHERE (order_id=$2 OR epay_order_id=$3) AND closed_at IS NULL`, closed, localID, externalID)
	return err
}

func (s *PaymentService) insertBillAmountReservation(ctx context.Context, tx *dbent.Tx, providerID int64, receiver string, amountMinor, localID, externalID int64, createdAt, expiresAt time.Time) error {
	if providerID <= 0 || receiver == "" || amountMinor <= 0 || (localID > 0) == (externalID > 0) || expiresAt.Sub(createdAt) != billAmountOrderTTL {
		return fmt.Errorf("invalid fixed QR amount reservation")
	}
	var local, external any
	if localID > 0 {
		local = localID
	} else {
		external = externalID
	}
	_, err := tx.Client().ExecContext(ctx, `INSERT INTO payment_bill_amount_reservations(provider_instance_id,receiver_id,amount_minor,order_id,epay_order_id,created_at,expires_at,reuse_after) VALUES($1,$2,$3,$4,$5,$6,$7,$8)`, providerID, receiver, amountMinor, local, external, createdAt, expiresAt, expiresAt.Add(billAmountReuseDelay))
	return err
}
