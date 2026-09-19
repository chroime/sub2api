package repository

import (
	"context"
	"database/sql"
	"errors"
	"strings"
)

func (r *usageBillingRepository) RenewBalancePrechargeLease(ctx context.Context, id, owner string) (bool, error) {
	if strings.TrimSpace(id) == "" || strings.TrimSpace(owner) == "" {
		return false, errors.New("invalid precharge owner")
	}
	// Recovery may have queued review evidence during a database outage while
	// this owner remained alive. Permit its heartbeat to resume; only an explicit
	// owner completion or terminal hold revokes renewal. The ended marker also
	// fences a renewal that races EndBalancePrechargeLease under the row lock.
	result, err := r.db.ExecContext(ctx, `UPDATE balance_precharges SET lease_expires_at=NOW()+INTERVAL '2 minutes'
   WHERE id=$1 AND lease_owner=$2 AND state='reserved' AND lease_ended_at IS NULL`, id, owner)
	if err != nil {
		return false, err
	}
	n, err := result.RowsAffected()
	return n == 1, err
}

func (r *usageBillingRepository) EndBalancePrechargeLease(ctx context.Context, id, owner string) error {
	_, err := r.db.ExecContext(ctx, `UPDATE balance_precharges SET lease_expires_at=NOW(),lease_ended_at=NOW()
   WHERE id=$1 AND lease_owner=$2 AND state='reserved'`, id, owner)
	return err
}

func (r *usageBillingRepository) RecoverBalancePrecharges(ctx context.Context, limit int) (int, error) {
	if limit < 1 || limit > 1000 {
		return 0, errors.New("invalid precharge recovery batch size")
	}
	// Discover without holding ledger locks. Every monetary path orders locks as
	// user -> hold -> review; recovery follows the same order to avoid deadlocks.
	rows, err := r.db.QueryContext(ctx, `SELECT id,user_id FROM balance_precharges p
  WHERE state='reserved' AND ((lease_owner IS NOT NULL AND lease_expires_at<=NOW())
  OR (lease_owner IS NULL AND recovery_after<=NOW()))
  AND NOT EXISTS(SELECT 1 FROM balance_precharge_reviews r WHERE r.precharge_id=p.id)
  ORDER BY COALESCE(lease_expires_at,recovery_after),id LIMIT $1`, limit)
	if err != nil {
		return 0, err
	}
	type candidate struct {
		id   string
		user int64
	}
	candidates := make([]candidate, 0, limit)
	for rows.Next() {
		var c candidate
		if err = rows.Scan(&c.id, &c.user); err != nil {
			_ = rows.Close()
			return 0, err
		}
		candidates = append(candidates, c)
	}
	err = rows.Err()
	_ = rows.Close()
	if err != nil {
		return 0, err
	}
	recovered := 0
	for _, c := range candidates {
		count, err := r.recoverBalancePrecharge(ctx, c.id, c.user)
		if err != nil {
			return recovered, err
		}
		recovered += count
	}
	return recovered, nil
}

func (r *usageBillingRepository) recoverBalancePrecharge(ctx context.Context, id string, user int64) (int, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer func() { _ = tx.Rollback() }()
	// A soft-deleted user's money still requires review evidence.
	var locked int64
	if err = tx.QueryRowContext(ctx, `SELECT id FROM users WHERE id=$1 FOR UPDATE`, user).Scan(&locked); err != nil && !errors.Is(err, sql.ErrNoRows) {
		return 0, err
	}
	var eligible bool
	var reason string
	err = tx.QueryRowContext(ctx, `SELECT state='reserved' AND ((lease_owner IS NOT NULL AND lease_expires_at<=NOW())
   OR (lease_owner IS NULL AND recovery_after<=NOW())),
   CASE WHEN lease_owner IS NULL THEN 'legacy_owner_unknown' ELSE 'owner_lease_expired' END
   FROM balance_precharges WHERE id=$1 FOR UPDATE`, id).Scan(&eligible, &reason)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	if !eligible {
		return 0, nil
	}
	result, err := tx.ExecContext(ctx, `INSERT INTO balance_precharge_reviews(precharge_id,reason)
   VALUES($1,$2) ON CONFLICT(precharge_id) DO NOTHING`, id, reason)
	if err != nil {
		return 0, err
	}
	n, err := result.RowsAffected()
	if err != nil {
		return 0, err
	}
	if err = tx.Commit(); err != nil {
		return 0, err
	}
	return int(n), nil
}
