package balancenotice

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
)

type SQLStore struct{ db *sql.DB }

func NewSQLStore(db *sql.DB) *SQLStore { return &SQLStore{db: db} }

const columns = `id,notification_id,trigger_type,batch_id,user_id,user_email,user_name,recipient,balance,threshold,subject,status,suppression_reason,attempts,last_attempt_at,sent_at,last_error,created_at,updated_at,expires_at`

func (s *SQLStore) Save(ctx context.Context, r *Record) error {
	return s.db.QueryRowContext(ctx, `INSERT INTO user_balance_notification_records
(notification_id,trigger_type,batch_id,user_id,user_email,user_name,recipient,balance,threshold,subject,body,status,suppression_reason,attempts,last_attempt_at,sent_at,last_error,delivery_key,created_at,updated_at,expires_at)
VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20,$21)
ON CONFLICT(notification_id,recipient) DO UPDATE SET subject=EXCLUDED.subject,body=EXCLUDED.body,status=EXCLUDED.status,
suppression_reason=EXCLUDED.suppression_reason,attempts=EXCLUDED.attempts,last_attempt_at=EXCLUDED.last_attempt_at,
sent_at=EXCLUDED.sent_at,last_error=EXCLUDED.last_error,balance=EXCLUDED.balance,threshold=EXCLUDED.threshold,updated_at=EXCLUDED.updated_at
RETURNING id`, r.NotificationID, r.TriggerType, r.BatchID, r.UserID, r.UserEmail, r.UserName, r.Recipient, r.Balance, r.Threshold, r.Subject, r.Body, r.Status, r.SuppressionReason, r.Attempts, r.LastAttemptAt, r.SentAt, r.LastError, r.DeliveryKey, r.CreatedAt, r.UpdatedAt, r.ExpiresAt).Scan(&r.ID)
}

type scanner interface{ Scan(...any) error }

func scan(row scanner, detail bool) (*Record, error) {
	r := new(Record)
	args := []any{&r.ID, &r.NotificationID, &r.TriggerType, &r.BatchID, &r.UserID, &r.UserEmail, &r.UserName, &r.Recipient, &r.Balance, &r.Threshold, &r.Subject, &r.Status, &r.SuppressionReason, &r.Attempts, &r.LastAttemptAt, &r.SentAt, &r.LastError, &r.CreatedAt, &r.UpdatedAt, &r.ExpiresAt}
	if detail {
		args = append(args, &r.Body, &r.DeliveryKey)
	}
	if err := row.Scan(args...); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return r, nil
}

func (s *SQLStore) Get(ctx context.Context, id int64, now time.Time) (*Record, error) {
	return scan(s.db.QueryRowContext(ctx, `SELECT `+columns+`,body,delivery_key FROM user_balance_notification_records WHERE id=$1 AND expires_at>$2`, id, now), true)
}

func filters(q Query, now time.Time) (string, []any) {
	args := []any{now, q.From, q.To}
	parts := []string{"expires_at>$1", "created_at>=$2", "created_at<$3"}
	add := func(column string, value any) {
		args = append(args, value)
		parts = append(parts, fmt.Sprintf("%s=$%d", column, len(args)))
	}
	if q.UserID > 0 {
		add("user_id", q.UserID)
	}
	if q.Status != "" {
		add("status", q.Status)
	}
	if q.TriggerType != "" {
		add("trigger_type", q.TriggerType)
	}
	for _, f := range []struct{ column, value string }{{"user_email", q.UserEmail}, {"recipient", q.Recipient}} {
		if f.value != "" {
			args = append(args, f.value)
			parts = append(parts, fmt.Sprintf("strpos(lower(%s),lower($%d))>0", f.column, len(args)))
		}
	}
	return strings.Join(parts, " AND "), args
}

func (s *SQLStore) List(ctx context.Context, q Query, now time.Time) (*Page, error) {
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()
	where, args := filters(q, now)
	p := &Page{Items: []Record{}, Page: q.Page, PageSize: q.PageSize, EvaluatedAt: now}
	err = tx.QueryRowContext(ctx, `SELECT COUNT(*),COUNT(*) FILTER(WHERE status IN ('pending','sending')),COUNT(*) FILTER(WHERE status='sent'),COUNT(*) FILTER(WHERE status='failed'),COUNT(*) FILTER(WHERE status='suppressed') FROM user_balance_notification_records WHERE `+where, args...).Scan(&p.Summary.Total, &p.Summary.Pending, &p.Summary.Sent, &p.Summary.Failed, &p.Summary.Suppressed)
	if err != nil {
		return nil, err
	}
	p.Total = p.Summary.Total
	p.Pages = max(1, int((p.Total+int64(q.PageSize)-1)/int64(q.PageSize)))
	args = append(args, q.PageSize, (q.Page-1)*q.PageSize)
	rows, err := tx.QueryContext(ctx, `SELECT `+columns+` FROM user_balance_notification_records WHERE `+where+fmt.Sprintf(` ORDER BY created_at DESC,id DESC LIMIT $%d OFFSET $%d`, len(args)-1, len(args)), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		r, e := scan(rows, false)
		if e != nil {
			return nil, e
		}
		p.Items = append(p.Items, *r)
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}
	if err = rows.Close(); err != nil {
		return nil, err
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	return p, nil
}

func (s *SQLStore) Prune(ctx context.Context, now time.Time, limit int) (int64, error) {
	r, err := s.db.ExecContext(ctx, `DELETE FROM user_balance_notification_records WHERE id IN (SELECT id FROM user_balance_notification_records WHERE expires_at<=$1 ORDER BY expires_at,id LIMIT $2)`, now, limit)
	if err != nil {
		return 0, err
	}
	return r.RowsAffected()
}
