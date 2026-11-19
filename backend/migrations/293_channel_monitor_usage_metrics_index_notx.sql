-- Match channel monitor usage aggregation by current group, effective model,
-- and time while excluding failed/unbilled placeholder logs.
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_usage_logs_group_effective_model_created
    ON usage_logs (
        group_id,
        (COALESCE(NULLIF(BTRIM(requested_model), ''), NULLIF(BTRIM(model), ''))),
        created_at
    )
    WHERE group_id IS NOT NULL AND actual_cost > 0;
