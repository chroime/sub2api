-- Keep the originally issued deadline for audit. A confirmed receipt can close
-- its half-open amount ownership window early, atomically with the claim.
ALTER TABLE payment_bill_amount_reservations
    ADD COLUMN closed_at TIMESTAMPTZ
    CHECK (closed_at IS NULL OR (closed_at > created_at AND closed_at <= expires_at));
