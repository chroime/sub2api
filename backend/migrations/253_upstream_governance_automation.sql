-- Automation metadata only: existing accounts remain untouched on upgrade.
CREATE TABLE upstream_governance_automation (
 site_id BIGINT PRIMARY KEY REFERENCES upstream_governance_sites(id) ON DELETE CASCADE,
 version BIGINT NOT NULL DEFAULT 1 CHECK(version > 0),
 policy JSONB NOT NULL,
 updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE TABLE upstream_governance_reconciliation_state (
 site_id BIGINT NOT NULL,
 binding_id BIGINT PRIMARY KEY,
 payload JSONB NOT NULL,
 updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
 FOREIGN KEY(site_id,binding_id) REFERENCES upstream_governance_bindings(site_id,id) ON DELETE CASCADE
);
CREATE TABLE upstream_governance_reconcile_previews (
 id TEXT PRIMARY KEY,
 site_id BIGINT NOT NULL REFERENCES upstream_governance_sites(id) ON DELETE CASCADE,
 payload JSONB NOT NULL,
 result JSONB,
 expires_at TIMESTAMPTZ NOT NULL
);
CREATE INDEX idx_upstream_governance_reconcile_previews_site ON upstream_governance_reconcile_previews(site_id,expires_at DESC);
