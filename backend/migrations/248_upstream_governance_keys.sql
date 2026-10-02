-- Remote inference keys exist independently of local import bindings. Pending
-- rows keep ownership fixed across uncertain upstream responses and retries.
CREATE TABLE upstream_governance_keys (
 id BIGSERIAL PRIMARY KEY,
 site_id BIGINT NOT NULL REFERENCES upstream_governance_sites(id) ON DELETE RESTRICT,
 remote_group_id TEXT NOT NULL CHECK (remote_group_id <> ''),
 platform TEXT NOT NULL CHECK (platform IN ('openai','anthropic','gemini')),
 remote_key_id TEXT NOT NULL DEFAULT '',
 marker TEXT NOT NULL UNIQUE CHECK (marker <> ''),
 owner_user_id BIGINT NOT NULL CHECK (owner_user_id > 0),
 key_cipher TEXT NOT NULL DEFAULT '',
 created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
 updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
 UNIQUE(site_id,remote_group_id,platform),
 CHECK (key_cipher <> '' OR remote_key_id = '')
);
CREATE INDEX idx_upstream_governance_keys_site ON upstream_governance_keys(site_id,id);
