-- Latest streaming samples must remain bounded even when a group or account
-- has no recent streaming usage. Keep applied migration 293 unchanged.
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_usage_logs_monitor_stream_recent
    ON usage_logs (
        group_id,
        (COALESCE(NULLIF(BTRIM(requested_model), ''), NULLIF(BTRIM(model), ''))),
        created_at DESC,
        id DESC
    )
    WHERE group_id IS NOT NULL AND stream = TRUE AND actual_cost > 0;

CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_usage_logs_monitor_account_stream_recent
    ON usage_logs (
        group_id,
        (COALESCE(NULLIF(BTRIM(requested_model), ''), NULLIF(BTRIM(model), ''))),
        account_id,
        created_at DESC,
        id DESC
    )
    WHERE group_id IS NOT NULL AND stream = TRUE AND actual_cost > 0;
