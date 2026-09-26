package upstreamgovernance

import (
	"context"
	"encoding/json"
)

func (s *sqlStore) SaveBalanceMonitorState(ctx context.Context, siteID int64, state BalanceMonitorState, events []Event) error {
	raw, err := json.Marshal(state)
	if err != nil {
		return err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	result, err := tx.ExecContext(ctx, `UPDATE upstream_governance_sites SET balance_monitor_state=$2::jsonb WHERE id=$1`, siteID, string(raw))
	if err = affected(result, err, ErrNotFound); err != nil {
		return err
	}
	for _, event := range events {
		if _, err = tx.ExecContext(ctx, `INSERT INTO upstream_governance_events (site_id,kind,resource,before_value,after_value,created_at) VALUES ($1,$2,$3,$4,$5,$6)`, siteID, event.Kind, event.Resource, event.Before, event.After, event.CreatedAt); err != nil {
			return err
		}
	}
	if len(events) > 0 {
		if _, err = tx.ExecContext(ctx, pruneEvents, siteID); err != nil {
			return err
		}
	}
	return tx.Commit()
}
