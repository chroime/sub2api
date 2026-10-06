ALTER TABLE upstream_governance_sites
 ADD COLUMN IF NOT EXISTS fast_observe_enabled BOOLEAN NOT NULL DEFAULT TRUE;
