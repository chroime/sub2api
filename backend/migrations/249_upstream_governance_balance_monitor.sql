-- Monitoring policy and runtime delivery reservations are independent from
-- upstream credentials. Runtime writes deliberately do not change site version.
ALTER TABLE upstream_governance_sites
 ADD COLUMN balance_monitor JSONB NOT NULL DEFAULT '{"enabled":false,"threshold":10,"unit":"","recipients":[],"cooldown_minutes":1440}',
 ADD COLUMN balance_monitor_state JSONB NOT NULL DEFAULT '{}';
