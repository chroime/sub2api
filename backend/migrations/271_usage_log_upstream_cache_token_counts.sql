-- Preserve raw cache buckets for administrator auditing.
-- Customer cache_* columns remain the billed counts and may already include
-- the group unified token multiplier.
ALTER TABLE usage_logs
    ADD COLUMN IF NOT EXISTS upstream_cache_read_tokens INTEGER NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS upstream_cache_creation_tokens INTEGER NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS upstream_cache_creation_5m_tokens INTEGER NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS upstream_cache_creation_1h_tokens INTEGER NOT NULL DEFAULT 0;

COMMENT ON COLUMN usage_logs.upstream_cache_read_tokens IS
    '上游原始缓存读取 Token；管理员审计字段，可能与客户计费用量不同';
COMMENT ON COLUMN usage_logs.upstream_cache_creation_tokens IS
    '上游原始缓存写入 Token；管理员审计字段，可能与客户计费用量不同';
COMMENT ON COLUMN usage_logs.upstream_cache_creation_5m_tokens IS
    '上游原始 5m 缓存写入 Token；管理员审计字段';
COMMENT ON COLUMN usage_logs.upstream_cache_creation_1h_tokens IS
    '上游原始 1h 缓存写入 Token；管理员审计字段';
