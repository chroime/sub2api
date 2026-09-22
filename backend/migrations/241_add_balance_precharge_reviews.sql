-- Only terminal requests whose billable result is unknown enter this queue.
-- Hold age alone never proves that refunding an upstream request is correct.
CREATE TABLE IF NOT EXISTS balance_precharge_reviews (
    precharge_id VARCHAR(128) PRIMARY KEY,
    reason VARCHAR(512) NOT NULL,
    request_id VARCHAR(255) NOT NULL DEFAULT '',
    account_id BIGINT NOT NULL DEFAULT 0 CHECK (account_id >= 0),
    model VARCHAR(255) NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    resolved_at TIMESTAMPTZ,
    resolved_by BIGINT,
    resolution VARCHAR(16),
    actual_cost NUMERIC(20,8),
    note VARCHAR(2000),
    new_balance NUMERIC(20,8),
    CHECK (
        (resolved_at IS NULL AND resolved_by IS NULL AND resolution IS NULL
            AND actual_cost IS NULL AND note IS NULL AND new_balance IS NULL)
        OR
        (resolved_at IS NOT NULL AND resolved_by IS NOT NULL AND resolved_by > 0
            AND resolution IS NOT NULL AND resolution IN ('release', 'charge')
            AND actual_cost IS NOT NULL AND actual_cost >= 0 AND actual_cost <= 1000000
            AND ((resolution = 'charge' AND actual_cost > 0) OR (resolution = 'release' AND actual_cost = 0))
            AND note IS NOT NULL AND LENGTH(BTRIM(note)) > 0 AND new_balance IS NOT NULL)
    )
);

-- Keep evidence even when users, keys, or groups are removed; ledger ownership
-- is immutable and audit records must not cascade with operational entities.
CREATE INDEX IF NOT EXISTS idx_balance_precharge_reviews_pending
    ON balance_precharge_reviews (created_at, precharge_id) WHERE resolved_at IS NULL;
CREATE INDEX IF NOT EXISTS idx_balance_precharge_reviews_resolved
    ON balance_precharge_reviews (resolved_at DESC, precharge_id) WHERE resolved_at IS NOT NULL;
