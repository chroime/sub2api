package upstreamgovernance

import (
	"context"
	"time"
)

const changeNotificationColumns = `id,site_id,dedup_key,recipient,kind,severity,subject,body,initial_baseline,status,attempts,next_attempt_at,sent_at,last_error,created_at`

func scanChangeNotification(row rowScanner) (ChangeNotification, error) {
	var item ChangeNotification
	err := row.Scan(&item.ID, &item.SiteID, &item.DedupKey, &item.Recipient, &item.Kind, &item.Severity, &item.Subject, &item.Body, &item.InitialBaseline, &item.Status, &item.Attempts, &item.NextAttemptAt, &item.SentAt, &item.LastError, &item.CreatedAt)
	return item, storeError(err)
}

func (s *sqlStore) EnqueueChangeNotification(ctx context.Context, item ChangeNotification) error {
	if item.SiteID <= 0 || item.DedupKey == "" || item.Recipient == "" || item.Kind == "" || item.Subject == "" || item.Body == "" || item.Status != "" && item.Status != ChangeNotificationPending {
		return ErrInvalid
	}
	if item.Status == "" {
		item.Status = ChangeNotificationPending
	}
	if item.Severity == "" {
		item.Severity = "info"
	}
	if item.CreatedAt.IsZero() {
		item.CreatedAt = time.Now().UTC()
	}
	if item.NextAttemptAt.IsZero() {
		item.NextAttemptAt = item.CreatedAt
	}
	var err error
	if item.ID > 0 {
		_, err = s.db.ExecContext(ctx, `INSERT INTO upstream_governance_change_notifications (`+changeNotificationColumns+`)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15)
ON CONFLICT(dedup_key,recipient) DO NOTHING`, item.ID, item.SiteID, item.DedupKey, item.Recipient, item.Kind, item.Severity, item.Subject, item.Body, item.InitialBaseline, item.Status, item.Attempts, item.NextAttemptAt, item.SentAt, item.LastError, item.CreatedAt)
	} else {
		_, err = s.db.ExecContext(ctx, `INSERT INTO upstream_governance_change_notifications (site_id,dedup_key,recipient,kind,severity,subject,body,initial_baseline,status,attempts,next_attempt_at,sent_at,last_error,created_at)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14)
ON CONFLICT(dedup_key,recipient) DO NOTHING`, item.SiteID, item.DedupKey, item.Recipient, item.Kind, item.Severity, item.Subject, item.Body, item.InitialBaseline, item.Status, item.Attempts, item.NextAttemptAt, item.SentAt, item.LastError, item.CreatedAt)
	}
	return err
}

func (s *sqlStore) DueChangeNotifications(ctx context.Context, now time.Time, limit int) ([]ChangeNotification, error) {
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	rows, err := s.db.QueryContext(ctx, `SELECT `+changeNotificationColumns+` FROM upstream_governance_change_notifications
WHERE status IN ('pending','sending') AND next_attempt_at <= $1 ORDER BY next_attempt_at,id LIMIT $2`, now, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []ChangeNotification{}
	for rows.Next() {
		item, scanErr := scanChangeNotification(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *sqlStore) ClaimChangeNotification(ctx context.Context, id int64, now, next time.Time) (bool, error) {
	result, err := s.db.ExecContext(ctx, `UPDATE upstream_governance_change_notifications SET status='sending',next_attempt_at=$2
WHERE id=$1 AND status IN ('pending','sending') AND next_attempt_at <= $3`, id, next, now)
	if err != nil {
		return false, err
	}
	count, err := result.RowsAffected()
	return count == 1, err
}

func (s *sqlStore) CompleteChangeNotification(ctx context.Context, id int64, sentAt time.Time, sendErr error, next time.Time) error {
	if sendErr == nil {
		result, err := s.db.ExecContext(ctx, `UPDATE upstream_governance_change_notifications SET status='sent',sent_at=$2,last_error='',next_attempt_at=$2 WHERE id=$1 AND status='sending'`, id, sentAt)
		return affected(result, err, ErrConflict)
	}
	result, err := s.db.ExecContext(ctx, `UPDATE upstream_governance_change_notifications SET status='pending',attempts=attempts+1,last_error=$2,next_attempt_at=$3 WHERE id=$1 AND status='sending'`, id, sendErr.Error(), next)
	return affected(result, err, ErrConflict)
}

func (s *sqlStore) GetChangeNotification(ctx context.Context, id int64) (ChangeNotification, error) {
	return scanChangeNotification(s.db.QueryRowContext(ctx, `SELECT `+changeNotificationColumns+` FROM upstream_governance_change_notifications WHERE id=$1`, id))
}

var _ ChangeNotificationStore = (*sqlStore)(nil)
