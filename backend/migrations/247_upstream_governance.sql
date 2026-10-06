-- Governance metadata is separate from local accounts/routing. Session/key columns
-- contain only application-encrypted ciphertext, never dashboard passwords.
CREATE TABLE upstream_governance_sites (
 id BIGSERIAL PRIMARY KEY,
 name TEXT NOT NULL,
 platform TEXT NOT NULL CHECK (platform IN ('sub2api','newapi')),
 base_url TEXT NOT NULL,
 proxy_id BIGINT REFERENCES proxies(id) ON DELETE RESTRICT,
 enabled BOOLEAN NOT NULL DEFAULT TRUE,
 interval_minutes INTEGER NOT NULL DEFAULT 15 CHECK (interval_minutes BETWEEN 5 AND 1440),
 version BIGINT NOT NULL DEFAULT 1 CHECK (version > 0),
 session_cipher TEXT NOT NULL DEFAULT '',
 status TEXT NOT NULL DEFAULT 'disconnected',
 last_error TEXT NOT NULL DEFAULT '',
 last_sync_at TIMESTAMPTZ,
 next_sync_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
 created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
 updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX idx_upstream_governance_sites_due ON upstream_governance_sites(next_sync_at,id) WHERE enabled;
CREATE TABLE upstream_governance_snapshots (
 id BIGSERIAL PRIMARY KEY,
 site_id BIGINT NOT NULL REFERENCES upstream_governance_sites(id) ON DELETE CASCADE,
 site_version BIGINT NOT NULL,
 catalog JSONB NOT NULL,
 created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX idx_upstream_governance_snapshots_site ON upstream_governance_snapshots(site_id,id DESC);
CREATE TABLE upstream_governance_bindings (
 id BIGSERIAL PRIMARY KEY,
 site_id BIGINT NOT NULL REFERENCES upstream_governance_sites(id) ON DELETE RESTRICT,
 remote_group_id TEXT NOT NULL,
 platform TEXT NOT NULL CHECK (platform IN ('openai','anthropic','gemini')),
 local_group_id BIGINT NOT NULL REFERENCES groups(id) ON DELETE RESTRICT,
 -- Zero is a pending import. Deliberately not an account FK: deleting governance
 -- metadata must never cascade into local accounts, and accounts may be retired.
 account_id BIGINT NOT NULL DEFAULT 0 CHECK (account_id >= 0),
 marker TEXT NOT NULL UNIQUE CHECK (marker <> ''),
 key_cipher TEXT NOT NULL DEFAULT '',
 probe_enabled BOOLEAN NOT NULL DEFAULT FALSE,
 probe_model TEXT NOT NULL DEFAULT '',
 probe_interval_minutes INTEGER NOT NULL DEFAULT 30 CHECK (probe_interval_minutes BETWEEN 15 AND 1440),
 next_probe_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
 UNIQUE(site_id,remote_group_id,platform),
 UNIQUE(site_id,id)
);
CREATE INDEX idx_upstream_governance_bindings_due ON upstream_governance_bindings(next_probe_at) WHERE probe_enabled;
CREATE TABLE upstream_governance_previews (
 id TEXT PRIMARY KEY,
 site_id BIGINT NOT NULL REFERENCES upstream_governance_sites(id) ON DELETE CASCADE,
 payload JSONB NOT NULL,
 result JSONB,
 expires_at TIMESTAMPTZ NOT NULL
);
CREATE INDEX idx_upstream_governance_previews_site ON upstream_governance_previews(site_id,expires_at DESC);
CREATE TABLE upstream_governance_events (
 id BIGSERIAL PRIMARY KEY,
 site_id BIGINT NOT NULL REFERENCES upstream_governance_sites(id) ON DELETE CASCADE,
 kind TEXT NOT NULL,
 resource TEXT NOT NULL DEFAULT '',
 before_value TEXT NOT NULL DEFAULT '',
 after_value TEXT NOT NULL DEFAULT '',
 acknowledged BOOLEAN NOT NULL DEFAULT FALSE,
 created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX idx_upstream_governance_events_site ON upstream_governance_events(site_id,id DESC);
CREATE TABLE upstream_governance_checks (
 id BIGSERIAL PRIMARY KEY,
 site_id BIGINT NOT NULL REFERENCES upstream_governance_sites(id) ON DELETE CASCADE,
 binding_id BIGINT NOT NULL,
 model TEXT NOT NULL,
 success BOOLEAN NOT NULL,
 latency_ms BIGINT NOT NULL CHECK (latency_ms >= 0),
 error_code TEXT NOT NULL DEFAULT '',
 created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
 FOREIGN KEY (site_id,binding_id) REFERENCES upstream_governance_bindings(site_id,id) ON DELETE CASCADE
);
CREATE INDEX idx_upstream_governance_checks_site ON upstream_governance_checks(site_id,id DESC);
CREATE INDEX idx_upstream_governance_checks_binding ON upstream_governance_checks(site_id,binding_id,id DESC);
-- A crash between account creation and result persistence must not duplicate imports.
CREATE UNIQUE INDEX idx_accounts_upstream_governance_marker
 ON accounts ((extra->>'upstream_governance_marker'))
 WHERE deleted_at IS NULL AND COALESCE(extra->>'upstream_governance_marker','') <> '';
