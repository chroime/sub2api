-- A repair can replace a managed remote key after its local account was
-- deleted, or before its first import. Existing repairs retain account CAS.
ALTER TABLE upstream_governance_key_repairs
 ADD COLUMN mode TEXT NOT NULL DEFAULT 'account',
 ALTER COLUMN binding_id DROP NOT NULL;

-- Remove the old per-column requirements; the mode-specific check below
-- preserves them for account repairs. Discover names to avoid PostgreSQL's
-- automatic constraint-name truncation.
DO $$
DECLARE constraint_name TEXT;
BEGIN
 FOR constraint_name IN
  SELECT c.conname
  FROM pg_constraint c
  JOIN pg_attribute a ON a.attrelid=c.conrelid AND c.conkey=ARRAY[a.attnum]
  WHERE c.conrelid='upstream_governance_key_repairs'::regclass
   AND c.contype='c'
   AND a.attname IN ('account_id','expected_account_fingerprint','expected_account_identity')
 LOOP
  EXECUTE format('ALTER TABLE upstream_governance_key_repairs DROP CONSTRAINT %I', constraint_name);
 END LOOP;
END $$;

ALTER TABLE upstream_governance_key_repairs
 ADD CONSTRAINT upstream_governance_key_repairs_mode_check CHECK (
  (mode='account' AND binding_id IS NOT NULL AND binding_id>0 AND account_id>0
   AND expected_account_fingerprint<>'' AND expected_account_identity<>'')
  OR
  (mode='key_only' AND account_id>=0
   AND ((binding_id IS NULL AND account_id=0) OR (binding_id IS NOT NULL AND binding_id>0))
   AND expected_account_fingerprint='' AND expected_account_identity='')
 );
