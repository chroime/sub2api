-- NULL preserves legacy marker-based names and in-flight creation requests.
-- New keys reserve their display name and pre-creation matching IDs before POST.
ALTER TABLE upstream_governance_keys
 ADD COLUMN creation_plan JSONB,
 ADD CONSTRAINT upstream_governance_keys_creation_plan_check CHECK (
  creation_plan IS NULL OR COALESCE((
   jsonb_typeof(creation_plan)='object'
   AND jsonb_typeof(creation_plan->'name')='string'
   AND length(btrim(creation_plan->>'name'))>0
   AND jsonb_typeof(creation_plan->'existing_ids') IN ('null','array')
  ),FALSE)
 );
