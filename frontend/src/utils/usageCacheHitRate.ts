import type { AdminUsageLog } from '@/types'

type CacheHitRateUsage = Partial<Pick<AdminUsageLog,
  'input_tokens' | 'upstream_input_tokens' | 'cache_read_tokens' | 'cache_creation_tokens' | 'cache_creation_5m_tokens' | 'cache_creation_1h_tokens' | 'upstream_cache_read_tokens' | 'upstream_cache_creation_tokens' | 'upstream_cache_creation_5m_tokens' | 'upstream_cache_creation_1h_tokens' | 'usage_limit_audit'
>>

function hasPersistedUpstreamCache(row: CacheHitRateUsage): boolean {
  return (row.upstream_cache_read_tokens ?? 0) !== 0
    || (row.upstream_cache_creation_tokens ?? 0) !== 0
    || (row.upstream_cache_creation_5m_tokens ?? 0) !== 0
    || (row.upstream_cache_creation_1h_tokens ?? 0) !== 0
}

type CacheHitRateResult =
  | { rate: number; reason: null }
  | { rate: null; reason: 'missing_input' | 'empty_input' | 'invalid_tokens' }

const isTokenCount = (value: number): boolean => Number.isSafeInteger(value) && value >= 0

/** Only recorded upstream input is valid here; billed input may have been adjusted. */
export function getActualCacheHitRate(row: CacheHitRateUsage): CacheHitRateResult {
  if (row.upstream_input_tokens == null
    || (row.upstream_input_tokens === 0 && (row.input_tokens ?? 0) > 0)
    || row.usage_limit_audit?.issues.includes('upstream_input_unavailable')) {
    return { rate: null, reason: 'missing_input' }
  }

  const input = row.upstream_input_tokens
  const useUpstreamCache = hasPersistedUpstreamCache(row)
  const read = useUpstreamCache
    ? (row.upstream_cache_read_tokens ?? 0)
    : (row.cache_read_tokens === undefined ? 0 : row.cache_read_tokens)
  const creation = useUpstreamCache
    ? (row.upstream_cache_creation_tokens ?? 0)
    : (row.cache_creation_tokens === undefined ? 0 : row.cache_creation_tokens)
  const creation5m = useUpstreamCache
    ? (row.upstream_cache_creation_5m_tokens ?? 0)
    : (row.cache_creation_5m_tokens === undefined ? 0 : row.cache_creation_5m_tokens)
  const creation1h = useUpstreamCache
    ? (row.upstream_cache_creation_1h_tokens ?? 0)
    : (row.cache_creation_1h_tokens === undefined ? 0 : row.cache_creation_1h_tokens)
  if (![input, read, creation, creation5m, creation1h].every(isTokenCount)) {
    return { rate: null, reason: 'invalid_tokens' }
  }

  const ttlCreation = creation5m + creation1h
  const totalInput = input + read + Math.max(creation, ttlCreation)
  if (!isTokenCount(ttlCreation) || !isTokenCount(totalInput)) {
    return { rate: null, reason: 'invalid_tokens' }
  }
  if (creation > 0 && ttlCreation > 0 && creation !== ttlCreation) {
    return { rate: null, reason: 'invalid_tokens' }
  }
  if (totalInput === 0) return { rate: null, reason: 'empty_input' }

  return { rate: read / totalInput * 100, reason: null }
}

export function formatActualCacheHitRate(row: CacheHitRateUsage): string {
  const { rate } = getActualCacheHitRate(row)
  return rate === null ? '—' : `${Number(rate.toFixed(2))}%`
}
