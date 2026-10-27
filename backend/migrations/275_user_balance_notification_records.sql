-- Audit only: existing email deduplication and delivery rules remain authoritative.
CREATE TABLE user_balance_notification_records (
 id BIGSERIAL PRIMARY KEY,
 notification_id UUID NOT NULL,
 trigger_type TEXT NOT NULL CHECK (trigger_type IN ('automatic','manual_batch')),
 batch_id TEXT NOT NULL DEFAULT '',
 user_id BIGINT NOT NULL CHECK (user_id > 0),
 user_email TEXT NOT NULL DEFAULT '',
 user_name TEXT NOT NULL DEFAULT '',
 recipient TEXT NOT NULL DEFAULT '',
 balance NUMERIC(20,8),
 threshold NUMERIC(20,8),
 subject TEXT NOT NULL DEFAULT '',
 body TEXT NOT NULL DEFAULT '',
 status TEXT NOT NULL CHECK (status IN ('pending','sending','sent','failed','suppressed')),
 suppression_reason TEXT NOT NULL DEFAULT '',
 attempts INTEGER NOT NULL DEFAULT 0 CHECK (attempts >= 0),
 last_attempt_at TIMESTAMPTZ,
 sent_at TIMESTAMPTZ,
 last_error TEXT NOT NULL DEFAULT '',
 delivery_key TEXT NOT NULL DEFAULT '',
 created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
 updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
 expires_at TIMESTAMPTZ NOT NULL DEFAULT (NOW() + INTERVAL '90 days'),
 UNIQUE(notification_id,recipient)
);
-- Preserve user snapshots for the retention period even after account deletion.
CREATE INDEX idx_user_balance_notifications_created ON user_balance_notification_records(created_at DESC,id DESC);
CREATE INDEX idx_user_balance_notifications_expires ON user_balance_notification_records(expires_at,id);
CREATE INDEX idx_user_balance_notifications_user ON user_balance_notification_records(user_id,created_at DESC,id DESC);
CREATE INDEX idx_user_balance_notifications_status ON user_balance_notification_records(status,created_at DESC,id DESC);
CREATE INDEX idx_user_balance_notifications_trigger ON user_balance_notification_records(trigger_type,created_at DESC,id DESC);
