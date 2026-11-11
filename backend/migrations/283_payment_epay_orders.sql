-- External EasyPay-compatible orders never grant local account balance.
CREATE TABLE IF NOT EXISTS payment_epay_orders (
    id BIGSERIAL PRIMARY KEY,
    provider_instance_id BIGINT NOT NULL REFERENCES payment_provider_instances(id) ON DELETE RESTRICT,
    out_trade_no VARCHAR(128) NOT NULL,
    trade_no VARCHAR(64) NOT NULL UNIQUE,
    amount_minor BIGINT NOT NULL CHECK (amount_minor > 0),
    receiver_id VARCHAR(32) NOT NULL,
    app_id VARCHAR(64) NOT NULL,
    qr_rule_id VARCHAR(64) NOT NULL,
    qr_url TEXT NOT NULL,
    name VARCHAR(256) NOT NULL,
    param VARCHAR(512) NOT NULL DEFAULT '',
    notify_url TEXT NOT NULL,
    return_url TEXT NOT NULL DEFAULT '',
    callback_key_encrypted TEXT NOT NULL,
    status VARCHAR(16) NOT NULL DEFAULT 'PENDING' CHECK (status IN ('PENDING','PAID','EXPIRED')),
    alipay_order_no VARCHAR(128) NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    expires_at TIMESTAMPTZ NOT NULL,
    paid_at TIMESTAMPTZ,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    notify_status VARCHAR(16) NOT NULL DEFAULT 'none' CHECK (notify_status IN ('none','pending','delivered')),
    notify_attempts INTEGER NOT NULL DEFAULT 0 CHECK (notify_attempts >= 0),
    notify_next_at TIMESTAMPTZ,
    notify_lease_until TIMESTAMPTZ,
    notify_lease_owner VARCHAR(64) NOT NULL DEFAULT '',
    notify_last_error VARCHAR(64) NOT NULL DEFAULT '',
    notify_delivered_at TIMESTAMPTZ,
    UNIQUE (provider_instance_id, out_trade_no)
);
CREATE INDEX IF NOT EXISTS idx_payment_epay_orders_provider_created
    ON payment_epay_orders(provider_instance_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_payment_epay_orders_notify_due
    ON payment_epay_orders(notify_next_at, id) WHERE status='PAID' AND notify_status='pending';
CREATE INDEX IF NOT EXISTS idx_payment_epay_orders_receipt_recovery
    ON payment_epay_orders(provider_instance_id, expires_at) WHERE status IN ('PENDING','EXPIRED');

ALTER TABLE payment_bill_receipts ADD COLUMN epay_order_id BIGINT REFERENCES payment_epay_orders(id) ON DELETE RESTRICT;
ALTER TABLE payment_bill_receipts ADD CONSTRAINT payment_bill_receipts_exclusive_target
    CHECK ((order_id IS NULL OR epay_order_id IS NULL)
        AND (status <> 'matched' OR order_id IS NOT NULL OR epay_order_id IS NOT NULL));

ALTER TABLE payment_bill_claims ALTER COLUMN order_id DROP NOT NULL;
ALTER TABLE payment_bill_claims ADD COLUMN epay_order_id BIGINT REFERENCES payment_epay_orders(id) ON DELETE RESTRICT;
ALTER TABLE payment_bill_claims ADD CONSTRAINT payment_bill_claims_exclusive_target
    CHECK ((order_id IS NOT NULL) <> (epay_order_id IS NOT NULL));
CREATE UNIQUE INDEX IF NOT EXISTS idx_payment_bill_claims_epay_order
    ON payment_bill_claims(epay_order_id) WHERE epay_order_id IS NOT NULL;
