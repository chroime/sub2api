-- Remote inventory observations are independent of saved key material.
ALTER TABLE upstream_governance_keys
 ADD COLUMN key_health JSONB NOT NULL DEFAULT '{"status":"unknown","missing_count":0}'::jsonb,
 ADD CONSTRAINT upstream_governance_keys_health_check CHECK (
  jsonb_typeof(key_health)='object'
  AND key_health->>'status' IN ('unknown','present','suspected_missing','confirmed_missing','group_changed')
  AND jsonb_typeof(key_health->'missing_count')='number'
  AND (key_health->>'missing_count')::integer >= 0
 );
