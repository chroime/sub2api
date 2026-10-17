-- Administrators may remove a model-monitoring result from the public IQ
-- projection without destroying the diagnostic evidence kept in the admin
-- workbench.
ALTER TABLE upstream_governance_model_runs
    ADD COLUMN IF NOT EXISTS public_visible BOOLEAN NOT NULL DEFAULT TRUE;

CREATE INDEX IF NOT EXISTS idx_governance_model_runs_public_visibility
    ON upstream_governance_model_runs(site_id, created_at DESC, id)
    WHERE public_visible;
