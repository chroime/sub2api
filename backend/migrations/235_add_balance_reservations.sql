-- Durable request-level balance holds. PostgreSQL remains the money ledger;
-- this table stores the lifecycle and idempotency state for each hold.
CREATE TABLE IF NOT EXISTS balance_reservations (
    id BIGSERIAL PRIMARY KEY,
    request_id VARCHAR(255) NOT NULL,
    request_fingerprint VARCHAR(128) NOT NULL DEFAULT '',
    api_key_id BIGINT NOT NULL,
    user_id BIGINT NOT NULL,
    hold_amount NUMERIC(20,8) NOT NULL CHECK (hold_amount > 0),
    actual_amount NUMERIC(20,8),
    status VARCHAR(16) NOT NULL DEFAULT 'held'
        CHECK (status IN ('held', 'settled', 'released')),
    expires_at TIMESTAMPTZ NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT balance_reservations_actual_amount_check
        CHECK (actual_amount IS NULL OR (actual_amount >= 0 AND actual_amount <= hold_amount))
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_balance_reservations_request_api_key
    ON balance_reservations (request_id, api_key_id);

CREATE INDEX IF NOT EXISTS idx_balance_reservations_expiry
    ON balance_reservations (status, expires_at);

CREATE INDEX IF NOT EXISTS idx_balance_reservations_user_status
    ON balance_reservations (user_id, api_key_id, status);
