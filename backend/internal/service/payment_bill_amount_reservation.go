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

// lockBillAmountReservation serializes allocation and blocks only payable orders.
// Historical windows remain available for delayed bill matching, but neither
// statement latency nor an unresolved old receipt extends amount occupancy.
func (s *PaymentService) lockBillAmountReservation(ctx context.Context, tx *dbent.Tx, receiver string, amountMinor int64, now time.Time) error {
	if err := s.lockBillAmountKey(ctx, tx, receiver, amountMinor); err != nil {
		return err
	}
	var occupiedUntil time.Time
	for _, query := range []string{
		`SELECT a.expires_at FROM payment_bill_amount_reservations a
		LEFT JOIN payment_orders o ON o.id=a.order_id
		LEFT JOIN payment_epay_orders e ON e.id=a.epay_order_id
		WHERE a.receiver_id=$1 AND a.amount_minor=$2 AND a.expires_at>$3
		AND a.user_cancelled_at IS NULL AND a.closed_at IS NULL
		AND (o.status='PENDING' OR e.status='PENDING')
		AND NOT EXISTS(SELECT 1 FROM payment_bill_claims c WHERE c.order_id=a.order_id OR c.epay_order_id=a.epay_order_id)
		ORDER BY a.expires_at DESC LIMIT 1`,
		`SELECT o.expires_at FROM payment_orders o
		WHERE o.provider_key='alipay_bill' AND o.provider_snapshot->>'receiver_id'=$1
		AND o.pay_amount=$2 AND o.expires_at>$3 AND o.status='PENDING'
		AND NOT EXISTS(SELECT 1 FROM payment_bill_amount_reservations r WHERE r.order_id=o.id)
		ORDER BY o.expires_at DESC LIMIT 1`,
		`SELECT o.expires_at FROM payment_epay_orders o
		WHERE o.receiver_id=$1 AND o.amount_minor=$2 AND o.expires_at>$3 AND o.status='PENDING'
		AND NOT EXISTS(SELECT 1 FROM payment_bill_amount_reservations r WHERE r.epay_order_id=o.id)
		ORDER BY o.expires_at DESC LIMIT 1`,
	} {
		var amount any = amountMinor
		if strings.Contains(query, "o.pay_amount") {
			amount = float64(amountMinor) / 100
		}
		rows, err := tx.Client().QueryContext(ctx, query, receiver, amount, now)
		if err != nil {
			return err
		}
		var expiry time.Time
		if rows.Next() {
			err = rows.Scan(&expiry)
		}
		rowsErr := rows.Err()
		rows.Close()
		if err != nil {
			return err
		}
		if rowsErr != nil {
			return rowsErr
		}
		if expiry.After(occupiedUntil) {
			occupiedUntil = expiry
		}
	}
	if now.Before(occupiedUntil) {
		seconds := int64(math.Ceil(occupiedUntil.Sub(now).Seconds()))
		return infraerrors.Conflict("ALIPAY_BILL_AMOUNT_BUSY", "this payment amount is in use until the current order is paid, cancelled, or expired").WithMetadata(map[string]string{"retry_after_seconds": strconv.FormatInt(seconds, 10)})
	}
	return nil
}

// closeBillAmountReservation runs inside the claim transaction with the amount
// lock held. Alipay timestamps have second precision: rounding confirmation UP
// keeps the entire confirmation second with the old order. The next reservation
// uses a separate payment window; this endpoint only preserves historical matching.
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
	if providerID <= 0 || receiver == "" || amountMinor <= 0 || (localID > 0) == (externalID > 0) || !expiresAt.After(createdAt) {
		return fmt.Errorf("invalid fixed QR amount reservation")
	}
	var local, external any
	if localID > 0 {
		local = localID
	} else {
		external = externalID
	}
	_, err := tx.Client().ExecContext(ctx, `INSERT INTO payment_bill_amount_reservations(provider_instance_id,receiver_id,amount_minor,order_id,epay_order_id,created_at,expires_at,reuse_after) VALUES($1,$2,$3,$4,$5,$6,$7,$8)`, providerID, receiver, amountMinor, local, external, createdAt, expiresAt, expiresAt)
	return err
}
