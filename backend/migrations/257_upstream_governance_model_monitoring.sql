-- Synthetic model evaluations are private governance data, never user usage.
CREATE TABLE upstream_governance_model_policies (
 id BIGSERIAL PRIMARY KEY,
 site_id BIGINT NOT NULL REFERENCES upstream_governance_sites(id) ON DELETE CASCADE,
 name TEXT NOT NULL,
 config JSONB NOT NULL,
 identity JSONB NOT NULL,
 enabled BOOLEAN NOT NULL DEFAULT FALSE,
 interval_minutes INTEGER NOT NULL CHECK (interval_minutes > 0),
 daily_request_limit INTEGER NOT NULL CHECK (daily_request_limit > 0),
 notify_enabled BOOLEAN NOT NULL DEFAULT FALSE,
 recipients JSONB NOT NULL DEFAULT '[]',
 failure_threshold INTEGER NOT NULL DEFAULT 2 CHECK (failure_threshold > 0),
 version BIGINT NOT NULL DEFAULT 1,
 next_run_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
 last_error TEXT NOT NULL DEFAULT '',
 notify_error TEXT NOT NULL DEFAULT '',
 notify_at TIMESTAMPTZ,
 failure_count INTEGER NOT NULL DEFAULT 0,
 incident_state TEXT NOT NULL DEFAULT 'healthy',
 incident_number BIGINT NOT NULL DEFAULT 0,
 quality_state TEXT NOT NULL DEFAULT 'unknown',
 quality_incident_number BIGINT NOT NULL DEFAULT 0,
 created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
 updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
 UNIQUE(site_id,id)
);
CREATE INDEX idx_governance_model_policies_due ON upstream_governance_model_policies(next_run_at,id) WHERE enabled;
CREATE TABLE upstream_governance_model_budgets (
 policy_id BIGINT NOT NULL REFERENCES upstream_governance_model_policies(id) ON DELETE CASCADE,
 budget_day DATE NOT NULL,
 reserved_requests INTEGER NOT NULL CHECK (reserved_requests >= 0),
 PRIMARY KEY(policy_id,budget_day)
);
CREATE TABLE upstream_governance_model_batches (
 id TEXT PRIMARY KEY,
 site_id BIGINT NOT NULL REFERENCES upstream_governance_sites(id) ON DELETE CASCADE,
 policy_id BIGINT REFERENCES upstream_governance_model_policies(id) ON DELETE SET NULL,
 policy_version BIGINT NOT NULL DEFAULT 0,
 request_id TEXT NOT NULL,
 request_hash TEXT NOT NULL,
 target_hash TEXT NOT NULL,
 identity JSONB NOT NULL,
 config JSONB NOT NULL,
 total INTEGER NOT NULL CHECK (total BETWEEN 1 AND 300),
 concurrency INTEGER NOT NULL CHECK (concurrency BETWEEN 1 AND 32),
 status TEXT NOT NULL DEFAULT 'queued' CHECK (status IN ('queued','running','completed','cancelled')),
 cancel_requested BOOLEAN NOT NULL DEFAULT FALSE,
 notification_processed BOOLEAN NOT NULL DEFAULT FALSE,
 scheduled BOOLEAN NOT NULL DEFAULT FALSE,
 created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
 finished_at TIMESTAMPTZ,
 UNIQUE(site_id,request_id),
 UNIQUE(site_id,id)
);
-- Only the selected concurrency inside one batch can execute for a target.
CREATE UNIQUE INDEX idx_governance_model_active_target ON upstream_governance_model_batches(target_hash) WHERE status='running';
CREATE INDEX idx_governance_model_batches_pending ON upstream_governance_model_batches(created_at,id) WHERE status IN ('queued','running');
CREATE TABLE upstream_governance_model_runs (
 id TEXT PRIMARY KEY,
 batch_id TEXT NOT NULL,
 site_id BIGINT NOT NULL,
 policy_id BIGINT REFERENCES upstream_governance_model_policies(id) ON DELETE SET NULL,
 sequence INTEGER NOT NULL CHECK (sequence > 0),
 request JSONB NOT NULL,
 budget_day DATE,
 status TEXT NOT NULL DEFAULT 'queued' CHECK (status IN ('queued','running','succeeded','failed','cancelled','indeterminate','skipped')),
 result JSONB,
 review TEXT NOT NULL DEFAULT 'pending' CHECK (review IN ('pending','pass','fail')),
 review_note TEXT NOT NULL DEFAULT '',
 reviewer_id BIGINT,
 reviewed_at TIMESTAMPTZ,
 review_version BIGINT NOT NULL DEFAULT 0,
 lease_owner TEXT NOT NULL DEFAULT '',
 lease_until TIMESTAMPTZ,
 created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
 started_at TIMESTAMPTZ,
 finished_at TIMESTAMPTZ,
 FOREIGN KEY(site_id,batch_id) REFERENCES upstream_governance_model_batches(site_id,id) ON DELETE CASCADE,
 UNIQUE(batch_id,sequence)
);
CREATE INDEX idx_governance_model_runs_site ON upstream_governance_model_runs(site_id,created_at DESC,id);
CREATE INDEX idx_governance_model_runs_batch ON upstream_governance_model_runs(batch_id,sequence);
CREATE INDEX idx_governance_model_runs_lease ON upstream_governance_model_runs(lease_until) WHERE status='running';
CREATE TABLE upstream_governance_model_notices (
 id BIGSERIAL PRIMARY KEY,
 policy_id BIGINT NOT NULL REFERENCES upstream_governance_model_policies(id) ON DELETE CASCADE,
 incident_number BIGINT NOT NULL,
 policy_version BIGINT NOT NULL,
 kind TEXT NOT NULL,
 notice JSONB NOT NULL,
 recipients JSONB NOT NULL,
 delivered JSONB NOT NULL DEFAULT '[]',
 status TEXT NOT NULL DEFAULT 'pending' CHECK (status IN ('pending','sending','sent')),
 lease_until TIMESTAMPTZ,
 next_attempt_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
 last_error TEXT NOT NULL DEFAULT '',
 created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
 sent_at TIMESTAMPTZ,
 UNIQUE(policy_id,incident_number,kind)
);
