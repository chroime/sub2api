-- Account diagnostics are administrator-owned and never use customer keys.
CREATE TABLE IF NOT EXISTS account_quality_policies (
 id BIGSERIAL PRIMARY KEY, version BIGINT NOT NULL DEFAULT 1,
 enabled BOOLEAN NOT NULL DEFAULT FALSE, config JSONB NOT NULL,
 deleted_at TIMESTAMPTZ, created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(), updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE TABLE IF NOT EXISTS account_quality_bindings (
 id BIGSERIAL PRIMARY KEY, account_id BIGINT NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
 policy_id BIGINT NOT NULL REFERENCES account_quality_policies(id), version BIGINT NOT NULL DEFAULT 1,
 state JSONB NOT NULL, next_run_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
 UNIQUE(account_id,policy_id)
);
CREATE INDEX IF NOT EXISTS account_quality_bindings_due ON account_quality_bindings(next_run_at,id);
CREATE TABLE IF NOT EXISTS account_quality_runs (
 id TEXT PRIMARY KEY, request_id TEXT NOT NULL UNIQUE,
 account_id BIGINT NOT NULL, binding_id BIGINT NOT NULL, policy_id BIGINT NOT NULL,
 execution_status TEXT NOT NULL, evidence JSONB NOT NULL,
 lease_token TEXT NOT NULL DEFAULT '', lease_until TIMESTAMPTZ,
 created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(), finished_at TIMESTAMPTZ
);
CREATE UNIQUE INDEX IF NOT EXISTS account_quality_one_active_account ON account_quality_runs(account_id)
 WHERE execution_status IN ('queued','running');
CREATE INDEX IF NOT EXISTS account_quality_runs_history ON account_quality_runs(account_id,created_at DESC,id);
CREATE INDEX IF NOT EXISTS account_quality_runs_binding ON account_quality_runs(binding_id,created_at DESC,id);
CREATE INDEX IF NOT EXISTS account_quality_runs_queue ON account_quality_runs(execution_status,created_at,id);
CREATE TABLE IF NOT EXISTS account_quality_actions (
 id TEXT PRIMARY KEY, account_id BIGINT NOT NULL DEFAULT 0, binding_id BIGINT NOT NULL DEFAULT 0,
 action TEXT NOT NULL, reason TEXT NOT NULL, actor_id BIGINT NOT NULL DEFAULT 0,
 created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS account_quality_actions_history ON account_quality_actions(account_id,created_at DESC);
CREATE TABLE IF NOT EXISTS account_quality_notifications (
 id BIGSERIAL PRIMARY KEY, dedup_key TEXT NOT NULL UNIQUE, event TEXT NOT NULL,
	 binding_id BIGINT NOT NULL,
 recipient TEXT NOT NULL, body TEXT NOT NULL, attempts INTEGER NOT NULL DEFAULT 0,
 status TEXT NOT NULL DEFAULT 'pending', next_attempt_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
 last_error TEXT NOT NULL DEFAULT '', created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS account_quality_notifications_due ON account_quality_notifications(status,next_attempt_at,id);
CREATE INDEX IF NOT EXISTS account_quality_notifications_reminders ON account_quality_notifications(binding_id,event,recipient,created_at DESC);
