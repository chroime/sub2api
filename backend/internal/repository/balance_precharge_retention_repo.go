package repository

import (
	"context"
	"errors"
	"time"
)

// ArchiveBalancePrecharges moves completed financial evidence atomically. It
// never deletes the only copy, pending holds, or live ownership. Archive rows
// remain available to admission, billing dedup and administrator review.
func (r *usageBillingRepository) ArchiveBalancePrecharges(ctx context.Context, cutoff time.Time, limit int) (int, error) {
	if limit < 1 || limit > 1000 {
		return 0, errors.New("invalid precharge archival batch size")
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	rows, err := tx.QueryContext(ctx, `SELECT p.id FROM balance_precharges p
 WHERE p.state<>'reserved' AND p.updated_at<$1
 AND (p.lease_expires_at IS NULL OR p.lease_expires_at<=NOW())
 AND NOT EXISTS(SELECT 1 FROM balance_precharge_reviews r WHERE r.precharge_id=p.id AND
  ((r.resolved_at IS NULL AND p.state='reserved') OR r.updated_at>=$1))
 ORDER BY p.updated_at,p.id LIMIT $2 FOR UPDATE OF p SKIP LOCKED`, cutoff, limit)
	if err != nil {
		return 0, err
	}
	ids := make([]string, 0, limit)
	for rows.Next() {
		var id string
		if err = rows.Scan(&id); err != nil {
			rows.Close()
			return 0, err
		}
		ids = append(ids, id)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return 0, err
	}
	for _, id := range ids {
		// No ON CONFLICT: an unexpected duplicate must rollback instead of silently
		// replacing or discarding financial evidence.
		if _, err = tx.ExecContext(ctx, `INSERT INTO balance_precharges_archive SELECT * FROM balance_precharges WHERE id=$1`, id); err != nil {
			return 0, err
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO balance_precharge_reviews_archive SELECT * FROM balance_precharge_reviews WHERE precharge_id=$1`, id); err != nil {
			return 0, err
		}
		if _, err = tx.ExecContext(ctx, `DELETE FROM balance_precharge_reviews WHERE precharge_id=$1`, id); err != nil {
			return 0, err
		}
		if _, err = tx.ExecContext(ctx, `DELETE FROM balance_precharges WHERE id=$1 AND state<>'reserved'`, id); err != nil {
			return 0, err
		}
	}
	if err = tx.Commit(); err != nil {
		return 0, err
	}
	return len(ids), nil
}
