-- Keep minute intervals positive without imposing product-level cadence limits.
-- Existing values and due timestamps are preserved. INTEGER provides the
-- storage representation limit shared by the service's input validation.
ALTER TABLE upstream_governance_sites
 DROP CONSTRAINT upstream_governance_sites_interval_minutes_check,
 ADD CONSTRAINT upstream_governance_sites_interval_minutes_check CHECK (interval_minutes > 0);

ALTER TABLE upstream_governance_bindings
 DROP CONSTRAINT upstream_governance_bindings_probe_interval_minutes_check,
 ADD CONSTRAINT upstream_governance_bindings_probe_interval_minutes_check CHECK (probe_interval_minutes > 0);
