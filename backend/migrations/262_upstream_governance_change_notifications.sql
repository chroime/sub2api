-- Durable administrator notifications for upstream/group/rate changes.
-- The payload is rendered operational text and must not contain credentials.
CREATE TABLE upstream_governance_change_notifications (
 id BIGSERIAL PRIMARY KEY,
 site_id BIGINT NOT NULL REFERENCES upstream_governance_sites(id) ON DELETE CASCADE,
 dedup_key TEXT NOT NULL CHECK (dedup_key <> ''),
 recipient TEXT NOT NULL CHECK (recipient <> ''),
 kind TEXT NOT NULL CHECK (kind <> ''),
 severity TEXT NOT NULL DEFAULT 'info' CHECK (severity IN ('info','warning','critical')),
 subject TEXT NOT NULL CHECK (subject <> ''),
 body TEXT NOT NULL CHECK (body <> ''),
 initial_baseline BOOLEAN NOT NULL DEFAULT FALSE,
 status TEXT NOT NULL DEFAULT 'pending' CHECK (status IN ('pending','sending','sent')),
 attempts INTEGER NOT NULL DEFAULT 0 CHECK (attempts >= 0),
 next_attempt_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
 sent_at TIMESTAMPTZ,
 last_error TEXT NOT NULL DEFAULT '',
 created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
 UNIQUE(dedup_key,recipient)
);
CREATE INDEX idx_upstream_governance_change_notifications_due
 ON upstream_governance_change_notifications(status,next_attempt_at,id)
 WHERE status='pending';
CREATE INDEX idx_upstream_governance_change_notifications_site
 ON upstream_governance_change_notifications(site_id,created_at DESC);
