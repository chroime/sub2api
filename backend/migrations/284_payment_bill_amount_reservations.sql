-- Immutable amount ownership windows shared by local and EasyPay orders.
-- A fixed QR remains payable after its platform order expires: preserve the
-- window and cooling interval instead of deleting/releasing it on cancellation.
CREATE TABLE IF NOT EXISTS payment_bill_amount_reservations (
    id BIGSERIAL PRIMARY KEY,
    provider_instance_id BIGINT NOT NULL REFERENCES payment_provider_instances(id) ON DELETE RESTRICT,
    receiver_id VARCHAR(32) NOT NULL,
    amount_minor BIGINT NOT NULL CHECK (amount_minor > 0),
    order_id BIGINT UNIQUE REFERENCES payment_orders(id) ON DELETE RESTRICT,
    epay_order_id BIGINT UNIQUE REFERENCES payment_epay_orders(id) ON DELETE RESTRICT,
    created_at TIMESTAMPTZ NOT NULL,
    expires_at TIMESTAMPTZ NOT NULL,
    reuse_after TIMESTAMPTZ NOT NULL,
    CHECK ((order_id IS NOT NULL) <> (epay_order_id IS NOT NULL)),
    CHECK (expires_at > created_at AND reuse_after > expires_at)
);
CREATE INDEX IF NOT EXISTS idx_payment_bill_amount_reservations_window
    ON payment_bill_amount_reservations(receiver_id, amount_minor, created_at, expires_at);
CREATE INDEX IF NOT EXISTS idx_payment_bill_amount_reservations_reuse
    ON payment_bill_amount_reservations(receiver_id, amount_minor, reuse_after DESC);
