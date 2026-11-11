-- Retain existing low-balance history while recording administrator-triggered
-- system status emails in the same delivery audit. This does not change mail
-- sending, subscription, deduplication or retention rules.
ALTER TABLE user_balance_notification_records
    ADD COLUMN event_type TEXT NOT NULL DEFAULT 'balance.low'
    CHECK (event_type IN ('balance.low', 'system.health_snapshot'));

CREATE INDEX idx_user_notifications_event_created
    ON user_balance_notification_records(event_type, created_at DESC, id DESC);
