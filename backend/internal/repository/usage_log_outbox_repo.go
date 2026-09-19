package repository

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/google/uuid"
)

type usageLogOutboxRepository struct{ db *sql.DB }

func NewUsageLogOutboxRepository(db *sql.DB) service.UsageLogOutboxRepository {
	return &usageLogOutboxRepository{db: db}
}

func enqueueUsageLogOutbox(ctx context.Context, tx *sql.Tx, cmd *service.UsageBillingCommand) error {
	payload, err := service.MarshalUsageLogRecoveryPayload(cmd)
	if err != nil || payload == nil {
		return err
	}
	// The brief delay lets the usual batched write finish before recovery checks
	// the same unique request key, avoiding work on the request's critical path.
	_, err = tx.ExecContext(ctx, `
		INSERT INTO usage_log_outbox (request_id, api_key_id, payload, available_at)
		VALUES ($1, $2, $3::jsonb, NOW() + INTERVAL '30 seconds')
	`, cmd.RequestID, cmd.APIKeyID, string(payload))
	return err
}

func (r *usageLogOutboxRepository) ClaimUsageLogOutbox(ctx context.Context, limit int, lease time.Duration) ([]service.UsageLogOutboxItem, error) {
	if r == nil || r.db == nil {
		return nil, errors.New("usage log outbox database is nil")
	}
	if limit <= 0 {
		limit = 32
	}
	if limit > 256 {
		limit = 256
	}
	if lease < time.Second {
		lease = time.Minute
	}
	if lease > 30*time.Minute {
		lease = 30 * time.Minute
	}
	rows, err := r.db.QueryContext(ctx, `
		WITH ready AS (
			SELECT id FROM usage_log_outbox
			WHERE available_at <= NOW() AND (lease_until IS NULL OR lease_until <= NOW())
			ORDER BY available_at, id
			LIMIT $1 FOR UPDATE SKIP LOCKED
		)
		UPDATE usage_log_outbox o
		SET claim_token = $2, lease_until = NOW() + ($3 * INTERVAL '1 second'),
			attempts = LEAST(o.attempts, 2147483646) + 1, updated_at = NOW()
		FROM ready WHERE o.id = ready.id
		RETURNING o.id, o.request_id, o.api_key_id, o.claim_token, o.attempts, o.payload
	`, limit, uuid.NewString(), lease.Seconds())
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	items := make([]service.UsageLogOutboxItem, 0, limit)
	for rows.Next() {
		var item service.UsageLogOutboxItem
		if err := rows.Scan(&item.ID, &item.RequestID, &item.APIKeyID, &item.ClaimToken, &item.Attempts, &item.Payload); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r *usageLogOutboxRepository) CompleteUsageLogOutbox(ctx context.Context, id int64, claimToken string) (bool, error) {
	if r == nil || r.db == nil {
		return false, errors.New("usage log outbox database is nil")
	}
	if id <= 0 || claimToken == "" {
		return false, errors.New("usage log outbox claim is required")
	}
	res, err := r.db.ExecContext(ctx, `
		DELETE FROM usage_log_outbox o USING usage_logs u
		WHERE o.id = $1 AND o.claim_token = $2 AND `+usageLogOutboxMatchesPersistedSQL, id, claimToken)
	completed, err := usageLogOutboxAffected(res, err)
	if err != nil || completed {
		return completed, err
	}
	// A concurrent successful normal write may already have removed the item.
	// Retain a still-owned item whose actual row is absent or mismatched instead
	// of treating ON CONFLICT DO NOTHING as proof of the correct bill.
	var stillOwned bool
	err = r.db.QueryRowContext(ctx, `SELECT EXISTS (SELECT 1 FROM usage_log_outbox WHERE id=$1 AND claim_token=$2)`, id, claimToken).Scan(&stillOwned)
	if err != nil {
		return false, err
	}
	if stillOwned {
		return false, service.ErrUsageLogOutboxConflict
	}
	return false, nil
}

func (r *usageLogOutboxRepository) RetryUsageLogOutbox(ctx context.Context, id int64, claimToken string, delay time.Duration, reason string) (bool, error) {
	if r == nil || r.db == nil {
		return false, errors.New("usage log outbox database is nil")
	}
	if id <= 0 || claimToken == "" {
		return false, errors.New("usage log outbox claim is required")
	}
	if delay < time.Second {
		delay = time.Second
	}
	if delay > time.Hour {
		delay = time.Hour
	}
	// Only stable error categories may be persisted. Do not store database error
	// text, which can contain interpolated rows or other private data.
	switch reason {
	case "invalid_payload", "usage_write_failed", "usage_write_panicked", "usage_conflict":
	default:
		reason = "usage_write_failed"
	}
	res, err := r.db.ExecContext(ctx, `
		UPDATE usage_log_outbox SET available_at = NOW() + ($3 * INTERVAL '1 second'),
			lease_until = NULL, claim_token = '', last_error_code = $4, updated_at = NOW()
		WHERE id = $1 AND claim_token = $2
	`, id, claimToken, delay.Seconds(), reason)
	return usageLogOutboxAffected(res, err)
}

func usageLogOutboxAffected(result sql.Result, err error) (bool, error) {
	if err != nil {
		return false, err
	}
	count, err := result.RowsAffected()
	return count > 0, err
}
