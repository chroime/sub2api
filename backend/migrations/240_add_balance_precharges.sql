-- Request-local cash holds. IDs come from the server, independently of usage
-- request IDs. Keep terminal records for idempotency and uncertain holds for
-- reconciliation; elapsed time alone is not evidence that upstream cost is zero.
CREATE TABLE IF NOT EXISTS balance_precharges (
    id VARCHAR(128) PRIMARY KEY,
    user_id BIGINT NOT NULL,
    api_key_id BIGINT NOT NULL,
    group_id BIGINT NOT NULL,
    threshold NUMERIC(20,8) NOT NULL CHECK (threshold > 0),
    amount NUMERIC(20,8) NOT NULL CHECK (amount > 0 AND amount <= threshold),
    state VARCHAR(16) NOT NULL CHECK (state IN ('reserved', 'captured', 'released', 'skipped')),
    captured_request_id VARCHAR(255),
    captured_cost NUMERIC(20,8) CHECK (captured_cost >= 0),
    duplicate_request_id VARCHAR(255),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CHECK ((state = 'captured') = (captured_request_id IS NOT NULL AND captured_cost IS NOT NULL)),
    CHECK (duplicate_request_id IS NULL OR state = 'released')
);

-- Like usage_billing_dedup, owner IDs are retained without cascading foreign
-- keys so deleting an API key or group cannot erase financial evidence.
CREATE INDEX IF NOT EXISTS idx_balance_precharges_reserved_user
    ON balance_precharges (user_id, created_at) WHERE state = 'reserved';
