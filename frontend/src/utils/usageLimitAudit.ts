import type { AdminUsageLog } from '@/types'

type Translate = (key: string) => string

const issueKeys: Record<string, string> = {
  missing_usage: 'admin.usage.usageAuditMissingUsage',
  missing_model: 'admin.usage.usageAuditMissingModel',
  missing_input_limit: 'admin.usage.usageAuditMissingInputLimit',
  missing_output_limit: 'admin.usage.usageAuditMissingOutputLimit',
  missing_context_limit: 'admin.usage.usageAuditMissingContextLimit',
  upstream_input_unavailable: 'admin.usage.usageAuditUpstreamInputUnavailable',
  upstream_output_unavailable: 'admin.usage.usageAuditUpstreamOutputUnavailable',
  cache_creation_total_mismatch: 'admin.usage.usageAuditCacheCreationMismatch',
  cache_creation_sum_overflow: 'admin.usage.usageAuditUnsafeTotal',
  input_sum_overflow: 'admin.usage.usageAuditUnsafeTotal',
  context_sum_overflow: 'admin.usage.usageAuditUnsafeTotal',
  token_count_unsafe_integer: 'admin.usage.usageAuditUnsafeTokens',
  cache_creation_sum_unsafe_integer: 'admin.usage.usageAuditUnsafeTotal',
  input_sum_unsafe_integer: 'admin.usage.usageAuditUnsafeTotal',
  context_sum_unsafe_integer: 'admin.usage.usageAuditUnsafeTotal',
  input_limit_unsafe_integer: 'admin.usage.usageAuditUnsafeLimit',
  output_limit_unsafe_integer: 'admin.usage.usageAuditUnsafeLimit',
  context_limit_unsafe_integer: 'admin.usage.usageAuditUnsafeLimit',
  input_limit_exceeded: 'admin.usage.usageAuditInputExceeded',
  output_limit_exceeded: 'admin.usage.usageAuditOutputExceeded',
  context_limit_exceeded: 'admin.usage.usageAuditContextExceeded',
}

const negativeTokenKeys: Record<string, string> = {
  negative_upstream_input_tokens: 'admin.usage.upstreamInputTokens',
  negative_upstream_output_tokens: 'admin.usage.upstreamOutputTokens',
  negative_cache_read_tokens: 'admin.usage.cacheReadTokens',
  negative_cache_creation_tokens: 'admin.usage.cacheCreationTokens',
  negative_cache_creation_5m_tokens: 'admin.usage.cacheCreation5mTokens',
  negative_cache_creation_1h_tokens: 'admin.usage.cacheCreation1hTokens',
}

const formatCount = (value: number | null | undefined): string =>
  typeof value === 'number' && Number.isSafeInteger(value) && value >= 0 ? value.toLocaleString() : '—'

export function getUsageLimitAuditTooltip(row: Pick<AdminUsageLog, 'usage_limit_audit'>, t: Translate): string {
  const audit = row.usage_limit_audit
  if (!audit) return t('admin.usage.usageAuditMissingUsage')
  const details = [
    t('admin.usage.usageAuditCurrentLimits'),
    `${t('admin.usage.usageAuditModel')}: ${audit.model || '—'}`,
    `${t('admin.usage.usageAuditInput')}: ${formatCount(audit.total_input_tokens)} / ${formatCount(audit.input_limit)}`,
    `${t('admin.usage.usageAuditOutput')}: ${formatCount(audit.total_output_tokens)} / ${formatCount(audit.output_limit)}`,
    `${t('admin.usage.usageAuditContext')}: ${formatCount(audit.total_context_tokens)} / ${formatCount(audit.context_limit)}`,
  ]
  for (const issue of audit.issues) {
    const negativeTokenKey = negativeTokenKeys[issue]
    details.push(negativeTokenKey
      ? `${t('admin.usage.usageAuditNegativeTokens')}: ${t(negativeTokenKey)}`
      : t(issueKeys[issue] || 'admin.usage.usageAuditUnknownIssue'))
  }
  return details.join('\n')
}
