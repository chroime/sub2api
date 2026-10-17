-- Preserve raw non-cache upstream token counts for administrator auditing.
-- Existing input_tokens/output_tokens remain the customer-facing billed counts.
ALTER TABLE usage_logs
    ADD COLUMN IF NOT EXISTS upstream_input_tokens INTEGER NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS upstream_output_tokens INTEGER NOT NULL DEFAULT 0;

COMMENT ON COLUMN usage_logs.upstream_input_tokens IS
    '上游原始非缓存输入 Token；管理员审计字段，可能与客户计费用量不同';
COMMENT ON COLUMN usage_logs.upstream_output_tokens IS
    '上游原始输出 Token；管理员审计字段，可能与客户计费用量不同';
