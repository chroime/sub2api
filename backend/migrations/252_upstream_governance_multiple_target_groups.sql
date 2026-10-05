-- One remote group/platform retains one key, account and governance binding.
-- local_group_id remains the first target for older clients and its existing FK.
-- NULL permits legacy INSERTs that only supply local_group_id; reads fall back
-- to [local_group_id] for those rows. Upgrade all writers before using multiple
-- targets: legacy UPDATEs cannot keep an existing non-NULL list synchronized.
-- New writes always set the canonical list and its first target together.
ALTER TABLE upstream_governance_bindings ADD COLUMN local_group_ids JSONB;
UPDATE upstream_governance_bindings SET local_group_ids=jsonb_build_array(local_group_id);
ALTER TABLE upstream_governance_bindings ADD CONSTRAINT upstream_governance_bindings_local_groups_check
 CHECK (jsonb_typeof(local_group_ids)='array' AND jsonb_array_length(local_group_ids) BETWEEN 1 AND 100);
