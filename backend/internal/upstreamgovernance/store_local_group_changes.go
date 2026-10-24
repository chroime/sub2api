package upstreamgovernance

import (
	"context"
	"time"
)

const localGroupChangeColumns = `id,group_id,kind,before_value,after_value,status,attempts,next_attempt_at,processed_at,last_error,created_at`

func scanLocalGroupChange(row rowScanner) (LocalGroupChangeEvent, error) {
	var event LocalGroupChangeEvent
	var before, after []byte
	var kind string
	err := row.Scan(&event.ID, &event.GroupID, &kind, &before, &after, &event.Status, &event.Attempts, &event.NextAttemptAt, &event.ProcessedAt, &event.LastError, &event.CreatedAt)
	if err != nil {
		return event, storeError(err)
	}
	event.Kind = LocalGroupChangeKind(kind)
	event.Before, event.After = string(before), string(after)
	return event, nil
}

// EnqueueLocalGroupChange is primarily used by tests and external outbox
// adapters. Production group mutations are captured atomically by the trigger
// in migration 273, so a transaction rollback also rolls the event back.
func (s *sqlStore) EnqueueLocalGroupChange(ctx context.Context, event LocalGroupChangeEvent) error {
	if err := event.Validate(); err != nil {
		return err
	}
	if event.Status == "" {
		event.Status = localGroupChangePending
	}
	if event.Status != localGroupChangePending {
		return ErrInvalid
	}
	if event.CreatedAt.IsZero() {
		event.CreatedAt = time.Now().UTC()
	}
	if event.NextAttemptAt.IsZero() {
		event.NextAttemptAt = event.CreatedAt
	}
	before := event.Before
	if before == "" {
		before = `{}`
	}
	after := event.After
	if after == "" {
		after = `{}`
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO upstream_governance_local_group_changes
	(group_id,kind,before_value,after_value,status,attempts,next_attempt_at,processed_at,last_error,created_at)
	VALUES ($1,$2,$3::jsonb,$4::jsonb,$5,$6,$7,$8,$9,$10)`,
		event.GroupID, string(event.Kind), before, after, event.Status, event.Attempts,
		event.NextAttemptAt, event.ProcessedAt, event.LastError, event.CreatedAt)
	return err
}

func (s *sqlStore) DueLocalGroupChanges(ctx context.Context, now time.Time, limit int) ([]LocalGroupChangeEvent, error) {
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	rows, err := s.db.QueryContext(ctx, `SELECT `+localGroupChangeColumns+`
FROM upstream_governance_local_group_changes
WHERE status IN ('pending','sending') AND next_attempt_at <= $1
ORDER BY next_attempt_at,id LIMIT $2`, now, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]LocalGroupChangeEvent, 0, limit)
	for rows.Next() {
		item, scanErr := scanLocalGroupChange(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *sqlStore) ClaimLocalGroupChange(ctx context.Context, id int64, now, next time.Time) (bool, error) {
	result, err := s.db.ExecContext(ctx, `UPDATE upstream_governance_local_group_changes
SET status='sending',next_attempt_at=$2
WHERE id=$1 AND status IN ('pending','sending') AND next_attempt_at <= $3`, id, next, now)
	if err != nil {
		return false, err
	}
	count, err := result.RowsAffected()
	return count == 1, err
}

func (s *sqlStore) CompleteLocalGroupChange(ctx context.Context, id int64, processedAt time.Time, sendErr error, next time.Time) error {
	if sendErr == nil {
		result, err := s.db.ExecContext(ctx, `UPDATE upstream_governance_local_group_changes
SET status='sent',processed_at=$2,last_error='',next_attempt_at=$2
WHERE id=$1 AND status='sending'`, id, processedAt)
		return affected(result, err, ErrConflict)
	}
	result, err := s.db.ExecContext(ctx, `UPDATE upstream_governance_local_group_changes
SET status='pending',attempts=attempts+1,last_error=$2,next_attempt_at=$3
WHERE id=$1 AND status='sending'`, id, sendErr.Error(), next)
	return affected(result, err, ErrConflict)
}

var _ LocalGroupChangeStore = (*sqlStore)(nil)
