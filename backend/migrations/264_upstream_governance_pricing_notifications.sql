CREATE TABLE IF NOT EXISTS upstream_governance_pricing_notifications (
 site_id BIGINT PRIMARY KEY REFERENCES upstream_governance_sites(id) ON DELETE CASCADE,
 version BIGINT NOT NULL DEFAULT 1 CHECK (version > 0),
 policy JSONB NOT NULL DEFAULT '{"enabled":false,"recipients":[],"group_changes":true,"rate_changes":true,"pricing_changes":true,"protection_changes":true}'::jsonb,
 updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
