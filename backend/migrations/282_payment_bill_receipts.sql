-- Account-log observations are separate from payment orders: matching an amount
-- is not proof that a receipt belongs to a particular customer.
CREATE TABLE IF NOT EXISTS payment_bill_receipts (
    id BIGSERIAL PRIMARY KEY,
    provider_instance_id BIGINT NOT NULL REFERENCES payment_provider_instances(id) ON DELETE RESTRICT,
    receiver_id VARCHAR(32) NOT NULL,
    account_log_id VARCHAR(128) NOT NULL,
    alipay_order_no VARCHAR(128) NOT NULL DEFAULT '',
    merchant_order_no VARCHAR(128) NOT NULL DEFAULT '',
    amount_minor BIGINT NOT NULL CHECK (amount_minor > 0),
    trans_at TIMESTAMPTZ NOT NULL,
    status VARCHAR(16) NOT NULL DEFAULT 'review' CHECK (status IN ('review','matched','ignored')),
    review_reason VARCHAR(64) NOT NULL DEFAULT 'awaiting_review',
    order_id BIGINT REFERENCES payment_orders(id) ON DELETE RESTRICT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (receiver_id, account_log_id)
);
CREATE INDEX IF NOT EXISTS idx_payment_bill_receipts_review ON payment_bill_receipts(receiver_id, status, trans_at DESC);

-- One official transaction may produce several account-log rows. Claim the
-- underlying transaction as well as the individual log to prevent double credit.
CREATE TABLE IF NOT EXISTS payment_bill_claims (
    receipt_id BIGINT PRIMARY KEY REFERENCES payment_bill_receipts(id) ON DELETE RESTRICT,
    receiver_id VARCHAR(32) NOT NULL,
    alipay_order_no VARCHAR(128) NOT NULL CHECK (alipay_order_no <> ''),
    order_id BIGINT NOT NULL UNIQUE REFERENCES payment_orders(id) ON DELETE RESTRICT,
    actor VARCHAR(100) NOT NULL,
    reason VARCHAR(500) NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (receiver_id, alipay_order_no)
);

CREATE TABLE IF NOT EXISTS payment_bill_sync_state (
    receiver_id VARCHAR(32) PRIMARY KEY,
    cursor_at TIMESTAMPTZ,
    rescan_cursor_at TIMESTAMPTZ,
    lease_until TIMESTAMPTZ NOT NULL,
    lease_owner VARCHAR(64) NOT NULL,
    error_code VARCHAR(64) NOT NULL DEFAULT '',
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
