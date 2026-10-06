-- Explicit repairs retain the old encrypted key until a candidate is safely
-- promoted with the native account and binding in one transaction.
CREATE TABLE upstream_governance_key_repairs (
 id TEXT PRIMARY KEY CHECK (id <> ''),
 site_id BIGINT NOT NULL REFERENCES upstream_governance_sites(id) ON DELETE RESTRICT,
 managed_key_id BIGINT NOT NULL REFERENCES upstream_governance_keys(id) ON DELETE RESTRICT,
 binding_id BIGINT NOT NULL,
 account_id BIGINT NOT NULL CHECK (account_id > 0),
 account_name TEXT NOT NULL,
 site_version BIGINT NOT NULL CHECK (site_version > 0),
 owner_user_id BIGINT NOT NULL CHECK (owner_user_id > 0),
 marker TEXT NOT NULL CHECK (marker <> ''),
 remote_group_id TEXT NOT NULL CHECK (remote_group_id <> ''),
 platform TEXT NOT NULL CHECK (platform <> ''),
 base_url TEXT NOT NULL CHECK (base_url <> ''),
 old_remote_key_id TEXT NOT NULL CHECK (old_remote_key_id <> ''),
 old_key_cipher TEXT NOT NULL CHECK (old_key_cipher <> ''),
 old_creation_plan JSONB,
 expected_account_fingerprint TEXT NOT NULL CHECK (expected_account_fingerprint <> ''),
 expected_account_identity TEXT NOT NULL CHECK (expected_account_identity <> ''),
 plan JSONB NOT NULL CHECK (jsonb_typeof(plan)='object' AND jsonb_typeof(plan->'name')='string' AND jsonb_typeof(plan->'existing_ids')='array'),
 idempotency_key TEXT NOT NULL CHECK (idempotency_key <> ''),
 candidate_remote_key_id TEXT NOT NULL DEFAULT '',
 candidate_key_cipher TEXT NOT NULL DEFAULT '',
 stage TEXT NOT NULL CHECK (stage IN ('prepared','post_intent','awaiting_visibility','candidate_ready','committed','conflict','abandoned')),
 error_code TEXT NOT NULL DEFAULT '',
 post_intent_at TIMESTAMPTZ,
 created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
 updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
 FOREIGN KEY (site_id,binding_id) REFERENCES upstream_governance_bindings(site_id,id) ON DELETE RESTRICT,
 CHECK ((candidate_remote_key_id='')=(candidate_key_cipher='')),
 CHECK (stage NOT IN ('candidate_ready','committed') OR candidate_key_cipher<>''),
 CHECK (stage IN ('prepared','conflict') OR post_intent_at IS NOT NULL)
);
CREATE UNIQUE INDEX idx_upstream_governance_key_repairs_active
 ON upstream_governance_key_repairs(managed_key_id)
 WHERE stage NOT IN ('committed','abandoned') AND NOT (stage='conflict' AND post_intent_at IS NULL);
CREATE INDEX idx_upstream_governance_key_repairs_key
 ON upstream_governance_key_repairs(site_id,managed_key_id,created_at DESC);
