-- Ownership is evidence of a live request, not permission to refund on expiry.
ALTER TABLE balance_precharges ADD COLUMN IF NOT EXISTS lease_owner VARCHAR(128);
ALTER TABLE balance_precharges ADD COLUMN IF NOT EXISTS lease_expires_at TIMESTAMPTZ;
-- Review evidence does not revoke a still-running owner's ability to recover
-- its heartbeat after an outage. Explicit completion does revoke that ability.
ALTER TABLE balance_precharges ADD COLUMN IF NOT EXISTS lease_ended_at TIMESTAMPTZ;
ALTER TABLE balance_precharges ADD COLUMN IF NOT EXISTS recovery_after TIMESTAMPTZ;
-- Give upgraded instances time to drain requests admitted by an older build.
UPDATE balance_precharges SET recovery_after=NOW()+INTERVAL '15 minutes'
WHERE state='reserved' AND lease_owner IS NULL AND recovery_after IS NULL;
CREATE INDEX IF NOT EXISTS idx_balance_precharges_recovery
    ON balance_precharges (lease_expires_at, recovery_after) WHERE state='reserved';
