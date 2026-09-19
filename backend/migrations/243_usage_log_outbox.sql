-- A successful billing transaction leaves durable, sanitized display evidence.
-- Recovery inserts usage logs only; it must never execute billing a second time.
-- No foreign keys: deleting an operational entity must not discard pending work.
CREATE TABLE IF NOT EXISTS usage_log_outbox (
    id BIGSERIAL PRIMARY KEY,
    request_id VARCHAR(255) NOT NULL,
    api_key_id BIGINT NOT NULL,
    payload JSONB NOT NULL,
    attempts INTEGER NOT NULL DEFAULT 0 CHECK (attempts >= 0),
    available_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    lease_until TIMESTAMPTZ,
    claim_token VARCHAR(64) NOT NULL DEFAULT '',
    last_error_code VARCHAR(64) NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (request_id, api_key_id)
);

CREATE INDEX IF NOT EXISTS idx_usage_log_outbox_available
    ON usage_log_outbox (available_at, id);

COMMENT ON TABLE usage_log_outbox IS
    'Pending usage-log repair only. Contains no request bodies, model outputs or credential relation objects. Delete only after durable usage insertion.';
