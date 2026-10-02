-- Local-group pricing policies and trusted upstream cost facts.
--
-- A cost fact is deliberately separate from the group's applied sale rate. A
-- credible upstream increase can therefore put the source into protection
-- even when the sale-rate write is waiting for review or a cache invalidation.
CREATE TABLE IF NOT EXISTS upstream_governance_pricing_policies (
    local_group_id BIGINT PRIMARY KEY REFERENCES groups(id) ON DELETE CASCADE,
    enabled BOOLEAN NOT NULL DEFAULT FALSE,
    mode TEXT NOT NULL DEFAULT 'keep_margin' CHECK (mode IN ('keep_margin','target_margin')),
    baseline_cost NUMERIC(20,8) NOT NULL DEFAULT 0 CHECK (baseline_cost >= 0),
    baseline_sale NUMERIC(20,8) NOT NULL DEFAULT 0 CHECK (baseline_sale >= 0),
    ratio NUMERIC(20,8) NOT NULL DEFAULT 0 CHECK (ratio >= 0),
    min_margin NUMERIC(10,8) NOT NULL DEFAULT 0 CHECK (min_margin >= 0 AND min_margin < 1),
    safety_buffer NUMERIC(10,8) NOT NULL DEFAULT 0 CHECK (safety_buffer >= 0 AND safety_buffer < 1),
    decrease_stability_seconds BIGINT NOT NULL DEFAULT 60 CHECK (decrease_stability_seconds >= 0),
    max_increase_percent NUMERIC(20,8) NOT NULL DEFAULT 20 CHECK (max_increase_percent >= 0),
    version BIGINT NOT NULL DEFAULT 1 CHECK (version > 0),
    manual_owner BOOLEAN NOT NULL DEFAULT FALSE,
    manual_version BIGINT NOT NULL DEFAULT 0 CHECK (manual_version >= 0),
    last_automatic_sale NUMERIC(20,8) NOT NULL DEFAULT 0 CHECK (last_automatic_sale >= 0),
    last_automatic_cost NUMERIC(20,8) NOT NULL DEFAULT 0 CHECK (last_automatic_cost >= 0),
    active_cost NUMERIC(20,8) NOT NULL DEFAULT 0 CHECK (active_cost >= 0),
    active_cost_source TEXT NOT NULL DEFAULT '',
    protected BOOLEAN NOT NULL DEFAULT FALSE,
    protection_reason TEXT NOT NULL DEFAULT '',
    cost_fact_revision BIGINT NOT NULL DEFAULT 0 CHECK (cost_fact_revision >= 0),
    decrease_observed_at TIMESTAMPTZ,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CHECK (NOT enabled OR (baseline_cost > 0 AND baseline_sale > 0 AND min_margin + safety_buffer < 1))
);

CREATE TABLE IF NOT EXISTS upstream_governance_pricing_cost_facts (
    id BIGSERIAL PRIMARY KEY,
    local_group_id BIGINT NOT NULL REFERENCES groups(id) ON DELETE CASCADE,
    source_id TEXT NOT NULL CHECK (source_id <> ''),
    site_id BIGINT REFERENCES upstream_governance_sites(id) ON DELETE CASCADE,
    binding_id BIGINT,
    cost NUMERIC(20,8) NOT NULL DEFAULT 0 CHECK (cost >= 0),
    unit TEXT NOT NULL DEFAULT '',
    currency TEXT NOT NULL DEFAULT '',
    comparable BOOLEAN NOT NULL DEFAULT FALSE,
    eligible BOOLEAN NOT NULL DEFAULT FALSE,
    unknown BOOLEAN NOT NULL DEFAULT TRUE,
    protected BOOLEAN NOT NULL DEFAULT FALSE,
    error_code TEXT NOT NULL DEFAULT '',
    observed_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    revision BIGINT NOT NULL DEFAULT 1 CHECK (revision > 0),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (local_group_id, source_id)
);

CREATE INDEX IF NOT EXISTS idx_upstream_governance_pricing_cost_facts_group
    ON upstream_governance_pricing_cost_facts(local_group_id, eligible, comparable, observed_at DESC);
CREATE INDEX IF NOT EXISTS idx_upstream_governance_pricing_cost_facts_site
    ON upstream_governance_pricing_cost_facts(site_id, observed_at DESC);

CREATE TABLE IF NOT EXISTS upstream_governance_pricing_operations (
    operation_id TEXT PRIMARY KEY CHECK (operation_id <> ''),
    local_group_id BIGINT NOT NULL REFERENCES groups(id) ON DELETE CASCADE,
    policy_version BIGINT NOT NULL CHECK (policy_version > 0),
    cost_fact_revision BIGINT NOT NULL CHECK (cost_fact_revision >= 0),
    source_id TEXT NOT NULL DEFAULT '',
    before_cost NUMERIC(20,8) NOT NULL DEFAULT 0 CHECK (before_cost >= 0),
    after_cost NUMERIC(20,8) NOT NULL DEFAULT 0 CHECK (after_cost >= 0),
    before_sale NUMERIC(20,8) NOT NULL DEFAULT 0 CHECK (before_sale >= 0),
    target_sale NUMERIC(20,8) NOT NULL DEFAULT 0 CHECK (target_sale >= 0),
    status TEXT NOT NULL CHECK (status IN ('prepared','applied','protected','rejected','conflict','failed')),
    protected BOOLEAN NOT NULL DEFAULT FALSE,
    reason TEXT NOT NULL DEFAULT '',
    error_code TEXT NOT NULL DEFAULT '',
    idempotency_key TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    applied_at TIMESTAMPTZ
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_upstream_governance_pricing_operations_idempotency
    ON upstream_governance_pricing_operations(local_group_id, idempotency_key)
    WHERE idempotency_key <> '';
CREATE INDEX IF NOT EXISTS idx_upstream_governance_pricing_operations_group
    ON upstream_governance_pricing_operations(local_group_id, created_at DESC);
