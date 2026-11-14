-- Bound the last-check history read by policy and effort, independent of age.
-- Concurrent creation keeps existing monitoring writes available on rollout.
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_governance_model_runs_iq_history
    ON upstream_governance_model_runs
        (site_id, policy_id, (request->>'effort'), finished_at DESC, id DESC)
    WHERE public_visible
      AND status IN ('succeeded', 'failed', 'indeterminate')
      AND request->'config'->>'target_type' = 'local_group'
      AND request->>'template' IN ('candy', 'pelican');
