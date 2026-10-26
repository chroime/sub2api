const ipGeoMocks = vi.hoisted(() => ({
  getEntry: vi.fn(() => ({ status: 'idle' as const })),
  fetchOne: vi.fn(),
  fetchBatch: vi.fn(),
}))

const appStoreMocks = vi.hoisted(() => ({
  showSuccess: vi.fn(),
  showError: vi.fn(),
}))

vi.mock('@/utils/ipGeoLookup', () => ipGeoMocks)
vi.mock('@/stores/app', () => ({ useAppStore: () => appStoreMocks }))

import { describe, expect, it, vi, beforeEach } from 'vitest'
import { mount } from '@vue/test-utils'
import { nextTick } from 'vue'

import en from '@/i18n/locales/en/admin/resources'
import zh from '@/i18n/locales/zh/admin/resources'
import UsageTable from '../UsageTable.vue'

let locale: 'en' | 'zh' = 'en'
const localizedMessages: Record<'en' | 'zh', Record<string, string>> = {
  en: {
    'admin.usage.longContext': en.usage.longContext,
    'admin.usage.longContextPricingTooltip': en.usage.longContextPricingTooltip,
  },
  zh: {
    'admin.usage.longContext': zh.usage.longContext,
    'admin.usage.longContextPricingTooltip': zh.usage.longContextPricingTooltip,
  },
}

const messages: Record<string, string> = {
  'usage.outputTps': 'Output TPS',
  'admin.usage.userDeletedBadge': 'Deleted',
  'usage.costDetails': 'Cost Breakdown',
  'admin.usage.inputCost': 'Input Cost',
  'admin.usage.outputCost': 'Output Cost',
  'admin.usage.cacheCreationCost': 'Cache Creation Cost',
  'admin.usage.cacheReadCost': 'Cache Read Cost',
  'usage.inputTokenPrice': 'Input price',
  'usage.outputTokenPrice': 'Output price',
  'usage.perMillionTokens': '/ 1M tokens',
  'usage.serviceTier': 'Service tier',
  'usage.serviceTierPriority': 'Fast',
  'usage.serviceTierUltrafast': 'Ultrafast',
  'usage.serviceTierFlex': 'Flex',
  'usage.serviceTierStandard': 'Standard',
  'usage.rate': 'Rate',
  'usage.accountMultiplier': 'Account rate',
  'usage.original': 'Original',
  'usage.userBilled': 'User billed',
  'usage.accountBilled': 'Account billed',
  'usage.imageUnit': ' images',
  'usage.imageCount': 'Image count',
  'usage.imageBillingSize': 'Billing size',
  'usage.imageInputSize': 'Input size',
  'usage.imageOutputSize': 'Output size',
  'usage.imageSizeSource': 'Size source',
  'usage.imageSizeBreakdown': 'Size breakdown',
  'usage.imageSizeSourceOutput': 'Upstream output',
  'usage.imageSizeSourceInput': 'Request input',
  'usage.imageSizeSourceDefault': 'Default billing tier',
  'usage.imageSizeSourceLegacy': 'Legacy record',
  'usage.imageSizeSourceMissing': 'Not recorded',
  'usage.imageSizeNotRecorded': 'not recorded',
  'usage.imageSizeLegacyUnstandardized': 'legacy unstandardized',
  'usage.imageSizeUnknown': 'unknown',
  'usage.imageUnitPrice': 'Per-image price',
  'usage.imageTotalPrice': 'Image total price',
  'usage.stream': 'Stream',
  'usage.sync': 'Sync',
  'usage.nativeCompactionV2': 'Compaction',
  'usage.latencyFirstToken': 'First',
  'usage.latencyDuration': 'Total',
  'usage.streamingACK': 'ACK',
  'admin.usage.billingModeToken': 'Token',
  'admin.usage.billingModePerRequest': 'Per request',
  'admin.usage.billingModeImage': 'Image',
	'admin.usage.requestIdCopied': 'Request ID copied',
	'admin.usage.upstreamRequestIdCopied': 'Upstream ID copied',
	'keys.copied': 'Copied',
	'keys.copyToClipboard': 'Copy to clipboard',
	'common.copyFailed': 'Copy failed',
	'usage.requestedModel': 'Requested',
	'usage.sentUpstreamModel': 'Sent upstream',
	'usage.upstreamResponseModel': 'Upstream response',
	'usage.modelVariant': 'Possible version variant',
	'usage.modelMismatch': 'Different model',
  'admin.usage.actualTokens': 'Actual Tokens',
  'admin.usage.upstreamInputTokens': 'Actual input tokens',
  'admin.usage.upstreamOutputTokens': 'Actual output tokens',
  'admin.usage.actualTotalTokens': 'Total',
  'admin.usage.cacheHitRateTooltip': en.usage.cacheHitRateTooltip,
  'admin.usage.cacheHitRateMissingInput': 'Actual upstream input was not recorded.',
  'admin.usage.cacheHitRateEmptyInput': 'Actual total input is zero.',
  'admin.usage.cacheHitRateInvalidTokens': 'Invalid token counts; a cache hit rate cannot be calculated.',
  'admin.usage.rawCostCalculated': 'Calculated from actual tokens × rate',
  'admin.usage.rawCostDifference': 'Difference',
  'admin.usage.rawCostCalculatedTooltip': 'Calculated from actual upstream tokens, current effective pricing, and this record’s rate. Difference = billed cost minus actual-token × rate cost.',
  'admin.usage.rawCostRecordedTooltip': 'The original recorded charge for unadjusted usage.',
  'admin.usage.rawCostUnavailable': 'The raw usage cost is unavailable.',
  'admin.usage.rawCostMissingUpstreamUsage': 'Actual upstream usage was not recorded.',
  'admin.usage.rawCostMissingUpstreamCacheUsage': 'Actual upstream cache usage was not recorded.',
  'admin.usage.rawCostInvalidUsage': 'The upstream usage is invalid.',
  'admin.usage.usageAuditInvalid': 'Anomaly',
  'admin.usage.usageAuditUnknown': 'Unverified',
  'admin.usage.usageAuditModel': 'Model',
  'admin.usage.usageAuditInput': 'Actual input / model limit',
  'admin.usage.usageAuditOutput': 'Actual output / model limit',
  'admin.usage.usageAuditContext': 'Actual context / model limit',
  'admin.usage.usageAuditInputExceeded': 'Actual input exceeds the model limit.',
  'admin.usage.usageAuditMissingInputLimit': 'The model input limit is unavailable.',
  'admin.usage.usageAuditUpstreamInputUnavailable': 'Actual upstream input was not recorded.',
  'admin.usage.billedCacheReadTokens': 'Billed cache read tokens',
  'admin.usage.billedCacheCreationTokens': 'Billed cache creation tokens',
  'admin.usage.cacheReadShort': 'R',
  'admin.usage.cacheCreationShort': 'W',
  'admin.usage.cacheReadTokens': 'Cache Read Tokens',
  'admin.usage.cacheCreationTokens': 'Cache Creation Tokens',
  'admin.usage.cacheCreation5mTokens': 'Cache Write',
  'admin.usage.cacheCreation1hTokens': 'Cache Write',
  'usage.cacheTtlOverriddenHint': 'TTL overridden',
  'usage.cacheTtlOverriddenLabel': 'TTL overridden',
  'usage.cacheTtlOverridden1h': '1h',
  'usage.cacheTtlOverridden5m': '5m',
  'usage.tokenDetails': 'Token details',
  'usage.totalTokens': 'Total tokens',
  'usage.tokens': 'Tokens',
}

vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return {
    ...actual,
    useI18n: () => ({
      t: (key: string) => localizedMessages[locale][key] ?? messages[key] ?? key,
    }),
  }
})

describe('admin UsageTable actual cache hit rate', () => {
  const baseRow = {
    request_id: 'req-cache-hit-rate',
    billing_mode: 'token',
    input_tokens: 99999,
    output_tokens: 50000,
    upstream_input_tokens: 100,
    upstream_output_tokens: 20000,
    cache_read_tokens: 600,
    cache_creation_tokens: 200,
    cache_creation_5m_tokens: 60,
    cache_creation_1h_tokens: 140,
  }

  const mountCacheHitRate = (overrides: Record<string, unknown> = {}) => mount(UsageTable, {
    props: {
      data: [{ ...baseRow, ...overrides }],
      columns: [{ key: 'cache_hit_rate', label: 'Cache hit rate', sortable: false }],
    },
    global: { stubs: {
      DataTable: { props: ['data'], template: '<div><slot v-for="row in data" name="cell-cache_hit_rate" :row="row" /></div>' },
      EmptyState: true, Icon: true, Teleport: true,
    } },
  })

  it.each([
    ['aggregate cache writes', {}, '66.67%'],
    ['TTL write buckets without an aggregate', { cache_creation_tokens: 0 }, '66.67%'],
    ['zero reads', { cache_read_tokens: 0 }, '0%'],
    ['a rate above ninety percent', { upstream_input_tokens: 5, cache_read_tokens: 95, cache_creation_tokens: 0, cache_creation_5m_tokens: 0, cache_creation_1h_tokens: 0 }, '95%'],
    ['unified multiplier billed cache', { cache_read_tokens: 1200, cache_creation_tokens: 400, cache_creation_5m_tokens: 120, cache_creation_1h_tokens: 280, upstream_cache_read_tokens: 600, upstream_cache_creation_tokens: 200, upstream_cache_creation_5m_tokens: 60, upstream_cache_creation_1h_tokens: 140 }, '66.67%'],
    ['entirely cached input', { input_tokens: 0, upstream_input_tokens: 0, cache_creation_tokens: 0, cache_creation_5m_tokens: 0, cache_creation_1h_tokens: 0 }, '100%'],
    ['omitted zero write buckets', { upstream_input_tokens: 600, cache_creation_tokens: undefined, cache_creation_5m_tokens: undefined, cache_creation_1h_tokens: undefined }, '50%'],
  ])('uses actual upstream input for %s', (_name, overrides, expected) => {
    const wrapper = mountCacheHitRate(overrides)
    const cell = wrapper.get('[data-testid="cache-hit-rate-cell"]')
    expect(cell.text()).toBe(expected)
    expect(cell.attributes('title')).toBe(messages['admin.usage.cacheHitRateTooltip'])
    wrapper.unmount()
  })

  it.each([undefined, null])('does not substitute billed input when actual input is %s', (upstreamInput) => {
    const wrapper = mountCacheHitRate({ upstream_input_tokens: upstreamInput })
    const cell = wrapper.get('[data-testid="cache-hit-rate-cell"]')
    expect(cell.text()).toBe('—')
    expect(cell.attributes('title')).toContain(messages['admin.usage.cacheHitRateMissingInput'])
    wrapper.unmount()
  })

  it('shows an unavailable rate when total actual input is zero', () => {
    const wrapper = mountCacheHitRate({ input_tokens: 0, upstream_input_tokens: 0, cache_read_tokens: 0, cache_creation_tokens: 0, cache_creation_5m_tokens: 0, cache_creation_1h_tokens: 0 })
    const cell = wrapper.get('[data-testid="cache-hit-rate-cell"]')
    expect(cell.text()).toBe('—')
    expect(cell.attributes('title')).toContain(messages['admin.usage.cacheHitRateEmptyInput'])
    wrapper.unmount()
  })

  it.each([50, 300])('rejects conflicting cache-write totals instead of silently choosing the larger count: %s', (creation) => {
    const wrapper = mountCacheHitRate({ cache_creation_tokens: creation })
    const cell = wrapper.get('[data-testid="cache-hit-rate-cell"]')
    expect(cell.text()).toBe('—')
    expect(cell.attributes('title')).toContain(messages['admin.usage.cacheHitRateInvalidTokens'])
    wrapper.unmount()
  })

  it.each([
    { upstream_input_tokens: 0 },
    { usage_limit_audit: { status: 'unknown', issues: ['upstream_input_unavailable'] } },
  ])('does not mistake unrecorded historical input for a fully cached request', (overrides) => {
    const wrapper = mountCacheHitRate(overrides)
    const cell = wrapper.get('[data-testid="cache-hit-rate-cell"]')
    expect(cell.text()).toBe('—')
    expect(cell.attributes('title')).toContain(messages['admin.usage.cacheHitRateMissingInput'])
    wrapper.unmount()
  })

  it.each([
    'upstream_input_tokens', 'cache_read_tokens', 'cache_creation_tokens', 'cache_creation_5m_tokens', 'cache_creation_1h_tokens',
  ])('rejects invalid values in %s without clamping', (field) => {
    for (const value of [-1, 0.5, NaN, Infinity, Number.MAX_SAFE_INTEGER + 1, '10']) {
      const wrapper = mountCacheHitRate({ [field]: value })
      const cell = wrapper.get('[data-testid="cache-hit-rate-cell"]')
      expect(cell.text(), `${field}: ${value}`).toBe('—')
      expect(cell.attributes('title')).toContain(messages['admin.usage.cacheHitRateInvalidTokens'])
      wrapper.unmount()
    }
  })

  it.each([
    { upstream_input_tokens: Number.MAX_SAFE_INTEGER },
    { cache_creation_5m_tokens: Number.MAX_SAFE_INTEGER },
  ])('rejects unsafe token sums even when every bucket is a safe integer', (overrides) => {
    const wrapper = mountCacheHitRate(overrides)
    const cell = wrapper.get('[data-testid="cache-hit-rate-cell"]')
    expect(cell.text()).toBe('—')
    expect(cell.attributes('title')).toContain(messages['admin.usage.cacheHitRateInvalidTokens'])
    wrapper.unmount()
  })
})

describe('admin UsageTable actual token audit', () => {
  const baseAudit = {
    status: 'invalid', model: 'gpt-5.5', input_limit: 1000, output_limit: 500, context_limit: 1500,
    total_input_tokens: 1200, total_output_tokens: 100, total_context_tokens: 1300, issues: ['input_limit_exceeded'],
  }
  const mountActualTokens = (overrides: Record<string, unknown> = {}) => mount(UsageTable, {
    props: {
      data: [{
        request_id: 'req-audit', input_tokens: 99999, output_tokens: 999,
        upstream_input_tokens: 200, upstream_output_tokens: 100, cache_read_tokens: 1000,
        cache_creation_tokens: 0, cache_creation_5m_tokens: 0, cache_creation_1h_tokens: 0,
        usage_limit_audit: baseAudit, ...overrides,
      }],
      columns: [{ key: 'upstream_tokens', label: 'Actual Tokens', sortable: false }],
    },
    global: { stubs: {
      DataTable: { props: ['data'], template: '<div><slot v-for="row in data" name="cell-upstream_tokens" :row="row" /></div>' },
      EmptyState: true, Icon: true, Teleport: true,
    } },
  })

  it('marks model-limit violations while retaining actual upstream token counts', () => {
    const wrapper = mountActualTokens()
    expect(wrapper.get('[data-testid="upstream-input-tokens"]').text()).toBe('200')
    expect(wrapper.get('[data-testid="upstream-output-tokens"]').text()).toBe('100')
    expect(wrapper.get('[data-testid="upstream-cache-read-tokens"]').text()).toBe('1.0K')
    const details = wrapper.get('[data-testid="upstream-token-details"]').attributes('title')
    expect(details).toContain('Actual input tokens: 200')
    expect(details).toContain('Actual output tokens: 100')
    expect(details).toContain('Total: 1,300')
    const marker = wrapper.get('[data-testid="usage-limit-audit-marker"]')
    expect(marker.text()).toBe('')
    expect(marker.attributes('aria-label')).toBe('Anomaly')
    expect(marker.attributes('title')).toContain('gpt-5.5')
    expect(marker.attributes('title')).toContain('1,200 / 1,000')
    expect(marker.attributes('title')).toContain('Actual input exceeds the model limit.')
    wrapper.unmount()
  })

  it('marks partial model-limit checks as unverified with an unavailable limit', () => {
    const wrapper = mountActualTokens({ usage_limit_audit: { ...baseAudit, status: 'unknown', input_limit: null, issues: ['missing_input_limit'] } })
    const marker = wrapper.get('[data-testid="usage-limit-audit-marker"]')
    expect(marker.text()).toBe('')
    expect(marker.attributes('aria-label')).toBe('Unverified')
    expect(marker.attributes('title')).toContain('1,200 / —')
    expect(marker.attributes('title')).toContain('The model input limit is unavailable.')
    wrapper.unmount()
  })

  it('does not add an anomaly marker to valid token counts', () => {
    const wrapper = mountActualTokens({ usage_limit_audit: { ...baseAudit, status: 'valid', input_limit: 1500, issues: [] } })
    expect(wrapper.find('[data-testid="usage-limit-audit-marker"]').exists()).toBe(false)
    expect(wrapper.get('[data-testid="upstream-input-tokens"]').text()).toBe('200')
    wrapper.unmount()
  })

  it('keeps the displayed total consistent with the audited aggregate for contradictory cache-write records', () => {
    const wrapper = mountActualTokens({
      cache_creation_tokens: 50, cache_creation_5m_tokens: 60, cache_creation_1h_tokens: 140,
      usage_limit_audit: { ...baseAudit, total_input_tokens: 1250, total_context_tokens: 1350, issues: ['cache_creation_total_mismatch'] },
    })
    const details = wrapper.get('[data-testid="upstream-token-details"]').attributes('title')
    expect(details).toContain('Total: 1,350')
    expect(wrapper.get('[data-testid="upstream-cache-creation-tokens"]').text()).toBe('50')
    expect(details).toContain('Cache Write 5m: 60')
    expect(details).toContain('Cache Write 1h: 140')
    expect(wrapper.get('[data-testid="usage-limit-audit-marker"]').attributes('aria-label')).toBe('Anomaly')
    wrapper.unmount()
  })

  it('keeps an unavailable audited context total unavailable', () => {
    const wrapper = mountActualTokens({ usage_limit_audit: { ...baseAudit, total_context_tokens: null } })
    expect(wrapper.get('[data-testid="upstream-token-details"]').attributes('title')).toContain('Total: —')
    wrapper.unmount()
  })

  it('keeps missing actual input and total unavailable instead of substituting billed tokens', () => {
    const wrapper = mountActualTokens({
      upstream_input_tokens: undefined,
      usage_limit_audit: { ...baseAudit, status: 'unknown', total_input_tokens: null, total_context_tokens: null, issues: ['upstream_input_unavailable'] },
    })
    expect(wrapper.get('[data-testid="upstream-input-tokens"]').text()).toBe('—')
    expect(wrapper.get('[data-testid="upstream-output-tokens"]').text()).toBe('100')
    const details = wrapper.get('[data-testid="upstream-token-details"]').attributes('title')
    expect(details).toContain('Actual input tokens: —')
    expect(details).toContain('Total: —')
    expect(wrapper.get('[data-testid="usage-limit-audit-marker"]').attributes('title')).toContain('Actual upstream input was not recorded.')
    wrapper.unmount()
  })

  it.each([-1, 0.5, NaN, Infinity, Number.MAX_SAFE_INTEGER + 1])('does not display invalid raw measurements as usable token counts: %s', (input) => {
    const wrapper = mountActualTokens({ upstream_input_tokens: input })
    expect(wrapper.get('[data-testid="upstream-input-tokens"]').text()).toBe('—')
    const details = wrapper.get('[data-testid="upstream-token-details"]').attributes('title')
    expect(details).toContain('Actual input tokens: —')
    expect(details).toContain('Total: —')
    expect(wrapper.get('[data-testid="usage-limit-audit-marker"]').attributes('aria-label')).toBe('Anomaly')
    wrapper.unmount()
  })

  it.each([
    [{ upstream_input_tokens: 0 }, 'upstream-input-tokens'],
    [{ upstream_output_tokens: 0 }, 'upstream-output-tokens'],
    [{ usage_limit_audit: { ...baseAudit, status: 'unknown', issues: ['upstream_input_unavailable'] } }, 'upstream-input-tokens'],
    [{ usage_limit_audit: { ...baseAudit, status: 'unknown', issues: ['upstream_output_unavailable'] } }, 'upstream-output-tokens'],
  ])('keeps historical or explicitly unavailable raw measurements unknown', (overrides, selector) => {
    const wrapper = mountActualTokens(overrides)
    expect(wrapper.get(`[data-testid="${selector}"]`).text()).toBe('—')
    expect(wrapper.get('[data-testid="upstream-token-details"]').attributes('title')).toContain('Total: —')
    wrapper.unmount()
  })
})

describe('admin UsageTable raw usage cost', () => {
  const mountRawCost = (audit?: Record<string, unknown>) => mount(UsageTable, {
    props: {
      data: [{ request_id: 'req-raw-cost', actual_cost: 0.012, raw_cost_audit: audit }],
      columns: [{ key: 'raw_cost', label: 'Raw usage cost', sortable: false }],
    },
    global: { stubs: {
      DataTable: { props: ['data'], template: '<div><slot v-for="row in data" name="cell-raw_cost" :row="row" /></div>' },
      EmptyState: true, Icon: true, Teleport: true,
    } },
  })

  it('shows the real-token cost after the current multiplier and compares it with the billed cost', () => {
    const wrapper = mountRawCost({ status: 'calculated', amount: 0.01038454, total_cost: 0.00519227, billed_amount: 0.012, difference: 999, reason: 'current_pricing_calculation', model: 'gpt-5.5' })
    const cell = wrapper.get('[data-testid="raw-cost-cell"]')
    expect(cell.get('[data-testid="raw-cost-amount"]').text()).toBe('$0.01038454')
    expect(cell.get('[data-testid="raw-cost-difference"]').text()).toBe('Difference +$0.00161546')
    expect(cell.text()).not.toContain('Estimated')
    expect(cell.attributes('title')).toBe(messages['admin.usage.rawCostCalculatedTooltip'])
    expect(cell.text()).not.toContain('0.012')
    wrapper.unmount()
  })

  it.each([
    [0.000727075, '$0.00072708'],
    [0.000234265, '$0.00023427'],
  ])('rounds the calculated actual-token cost %s at the decimal half boundary', (amount, expected) => {
    const wrapper = mountRawCost({ status: 'calculated', amount, total_cost: amount / 2, reason: 'current_pricing_calculation' })
    expect(wrapper.get('[data-testid="raw-cost-amount"]').text()).toBe(expected)
    wrapper.unmount()
  })

  it.each([0, 0.12345678])('preserves the recorded unadjusted amount %s without an estimate badge', (amount) => {
    const wrapper = mountRawCost({ status: 'recorded', amount, total_cost: amount, billed_amount: amount, difference: 0, reason: 'original_usage_unadjusted' })
    const cell = wrapper.get('[data-testid="raw-cost-cell"]')
    expect(cell.get('[data-testid="raw-cost-amount"]').text()).toBe(`$${amount.toFixed(8)}`)
    expect(cell.text()).not.toContain('Estimated')
    expect(cell.attributes('title')).toBe(messages['admin.usage.rawCostRecordedTooltip'])
    wrapper.unmount()
  })

  it.each([
    [undefined, 'admin.usage.rawCostUnavailable'],
    [{ status: 'unavailable', amount: null, total_cost: null, reason: 'missing_upstream_usage' }, 'admin.usage.rawCostMissingUpstreamUsage'],
    [{ status: 'unavailable', amount: null, total_cost: null, reason: 'missing_upstream_cache_usage' }, 'admin.usage.rawCostMissingUpstreamCacheUsage'],
    [{ status: 'unavailable', amount: 99, reason: 'invalid_usage' }, 'admin.usage.rawCostInvalidUsage'],
    [{ status: 'calculated', amount: NaN, total_cost: NaN }, 'admin.usage.rawCostUnavailable'],
    [{ status: 'calculated', amount: Infinity, total_cost: Infinity }, 'admin.usage.rawCostUnavailable'],
    [{ status: 'recorded', amount: -1 }, 'admin.usage.rawCostUnavailable'],
  ])('shows unavailable raw cost without falling back to the billed amount', (audit, reasonKey) => {
    const wrapper = mountRawCost(audit)
    const cell = wrapper.get('[data-testid="raw-cost-cell"]')
    expect(cell.text()).toBe('—')
    expect(cell.attributes('title')).toBe(messages[reasonKey])
    wrapper.unmount()
  })
})

const DataTableStub = {
  props: ['data'],
  template: `
    <div>
      <div v-for="row in data" :key="row.request_id" :data-request-id="row.request_id">
        <slot name="cell-model" :row="row" :value="row.model" />
        <slot name="cell-reasoning_effort" :row="row" :value="row.reasoning_effort" />
        <slot name="cell-billing_mode" :row="row" />
        <slot name="cell-tokens" :row="row" />
        <slot name="cell-upstream_tokens" :row="row" />
        <slot name="cell-latency" :row="row" />
        <slot name="cell-cost" :row="row" />
        <slot name="cell-request_id" :row="row" />
        <slot name="cell-upstream_request_id" :row="row" />
      </div>
    </div>
  `,
}

const baseImageRow = {
  request_id: 'req-admin-image',
  model: 'gpt-image-2',
  actual_cost: 0.4,
  total_cost: 0.4,
  account_rate_multiplier: 1,
  rate_multiplier: 1,
  service_tier: null,
  input_cost: 0,
  output_cost: 0,
  cache_creation_cost: 0,
  cache_read_cost: 0,
  input_tokens: 0,
  output_tokens: 0,
  cache_creation_tokens: 0,
  cache_read_tokens: 0,
  cache_creation_5m_tokens: 0,
  cache_creation_1h_tokens: 0,
  cache_ttl_overridden: false,
  billing_mode: 'image',
  image_count: 2,
  image_size: '2K',
  image_input_size: null,
  image_output_size: null,
  image_size_source: null,
  image_size_breakdown: null,
}

describe('UsageTable streaming ACK timing', () => {
  const latencyStub = {
    props: ['data'],
    template: '<div><div v-for="row in data" :key="row.request_id" data-testid="latency-row"><slot name="cell-latency" :row="row" /></div></div>',
  }

  function mountLatency(rows: Record<string, unknown>[], firstTokenMode?: 'model' | 'response') {
    return mount(UsageTable, {
      props: { data: rows, columns: [], loading: false, ...(firstTokenMode ? { firstTokenMode } : {}) },
      global: { stubs: { DataTable: latencyStub, EmptyState: true, Icon: true, Teleport: true } },
    })
  }

  it('shows ACK separately without changing first-token timing or severity', () => {
    const wrapper = mountLatency([{
      request_id: 'slow-text-fast-ack',
      first_token_ms: 60_000,
      duration_ms: 80_000,
      streaming_ack_ms: 720,
    }])

    expect(wrapper.get('[data-testid="streaming-ack-value"]').text()).toBe('720ms')
    expect(wrapper.text()).toContain('ACK')
    const firstToken = wrapper.findAll('span').find(span => span.text() === '1m 0s')!
    expect(firstToken.classes()).toContain('text-red-600')
    expect(wrapper.get('[aria-hidden="true"]').classes()).toContain('from-red-500')
    expect(wrapper.get('[data-testid="streaming-ack-value"]').classes()).not.toContain('text-emerald-600')
  })

  it('renders a recorded zero ACK and omits null or absent ACK values', () => {
    const wrapper = mountLatency([
      { request_id: 'zero', first_token_ms: 100, duration_ms: 500, streaming_ack_ms: 0 },
      { request_id: 'null', first_token_ms: 100, duration_ms: 500, streaming_ack_ms: null },
      { request_id: 'legacy', first_token_ms: 100, duration_ms: 500 },
    ])

    expect(wrapper.findAll('[data-testid="streaming-ack-value"]')).toHaveLength(1)
    expect(wrapper.get('[data-testid="streaming-ack-value"]').text()).toBe('0ms')
  })

  it('does not fill in missing first-token timing from the ACK', () => {
    const wrapper = mountLatency([{
      request_id: 'ack-without-text',
      first_token_ms: null,
      duration_ms: 2000,
      streaming_ack_ms: 650,
    }])

    expect(wrapper.get('.grid').findAll('span').map(span => span.text())).toEqual([
      'First', '-', 'Total', '2.00s', 'ACK', '650ms', 'Output TPS', '—',
    ])
    expect(wrapper.get('[aria-hidden="true"]').classes()).not.toContain('from-emerald-500')
  })

  it('shows ACK as the first response without modifying the real token timing', () => {
    const row = Object.freeze({
      request_id: 'different-views-same-request',
      first_token_ms: 60_000,
      duration_ms: 80_000,
      streaming_ack_ms: 720,
    })
    const user = mountLatency([row], 'response')
    const admin = mountLatency([row])

    expect(user.get('[data-testid="first-token-value"]').text()).toBe('720ms')
    expect(user.get('[data-testid="first-token-value"]').classes()).toContain('text-emerald-600')
    expect(user.get('[aria-hidden="true"]').classes()).toContain('from-emerald-500')
    expect(user.find('[data-testid="streaming-ack-value"]').exists()).toBe(false)
    expect(user.get('.grid').findAll('span').map(span => span.text())).toEqual([
      'First', '720ms', 'Total', '1m 20s', 'Output TPS', '—',
    ])
    expect(user.find('[title="usage.outputTpsHint"]').exists()).toBe(true)
    expect(admin.get('[data-testid="first-token-value"]').text()).toBe('1m 0s')
    expect(admin.get('[aria-hidden="true"]').classes()).toContain('from-red-500')
    expect(admin.get('[data-testid="streaming-ack-value"]').text()).toBe('720ms')
    expect(row.first_token_ms).toBe(60_000)
    expect(row.streaming_ack_ms).toBe(720)
  })

  it('shows response and real latency as independent columns for administrators', () => {
    const wrapper = mount(UsageTable, {
      props: {
        data: [
          { request_id: 'ack-and-text', first_token_ms: 60_000, duration_ms: 80_000, streaming_ack_ms: 720 },
          { request_id: 'ack-only', first_token_ms: null, duration_ms: 2000, streaming_ack_ms: 650 },
          { request_id: 'fast', first_token_ms: 180, duration_ms: 500, streaming_ack_ms: null },
        ],
        columns: [{ key: 'latency', label: 'Latency' }, { key: 'real_latency', label: 'Real latency' }],
        firstTokenMode: 'response',
      },
      global: { stubs: {
        DataTable: {
          props: ['data', 'columns'],
          template: '<div><div v-for="row in data" :key="row.request_id"><div v-for="col in columns" :key="col.key"><slot :name="`cell-${col.key}`" :row="row" /></div></div></div>',
        },
        EmptyState: true, Icon: true, Teleport: true,
      } },
    })

    expect(wrapper.findAll('[data-testid="first-token-value"]').map(node => node.text())).toEqual([
      '720ms', '650ms', '180ms',
    ])
    expect(wrapper.findAll('[data-testid="real-first-token-value"]').map(node => node.text())).toEqual([
      '1m 0s', '-', '180ms',
    ])
    const realLatency = wrapper.findAll('[data-testid="real-latency"]')[0]
    expect(realLatency.get('[aria-hidden="true"]').classes()).toContain('from-red-500')
    expect(realLatency.get('[data-testid="real-first-token-value"]').classes()).toContain('text-red-600')
    expect(realLatency.text()).toContain('1m 20s')
    expect(wrapper.find('[data-testid="streaming-ack-value"]').exists()).toBe(false)
  })

  it.each([null, undefined])('falls back to the real token for user rows without ACK (%s)', (ack) => {
    const wrapper = mountLatency([
      { request_id: 'fast', first_token_ms: 180, duration_ms: 500, streaming_ack_ms: ack },
      { request_id: 'slow', first_token_ms: 35_000, duration_ms: 40_000, streaming_ack_ms: ack },
      { request_id: 'zero', first_token_ms: 0, duration_ms: 500, streaming_ack_ms: ack },
    ], 'response')

    expect(wrapper.findAll('[data-testid="first-token-value"]').map(node => node.text())).toEqual([
      '180ms', '35.00s', '0ms',
    ])
    expect(wrapper.findAll('[data-testid="first-token-value"]')[1].classes()).toContain('text-orange-600')
  })

  it('preserves zero ACK, ACK-only and missing metrics in the user view', () => {
    const wrapper = mountLatency([
      { request_id: 'zero-ack', first_token_ms: 100, duration_ms: 500, streaming_ack_ms: 0 },
      { request_id: 'ack-only', first_token_ms: null, duration_ms: 2000, streaming_ack_ms: 650 },
      { request_id: 'missing', first_token_ms: null, duration_ms: 500, streaming_ack_ms: null },
    ], 'response')

    expect(wrapper.findAll('[data-testid="first-token-value"]').map(node => node.text())).toEqual([
      '0ms', '650ms', '-',
    ])
    expect(wrapper.findAll('[data-testid="streaming-ack-value"]')).toHaveLength(0)
  })

  it('keeps a visible health bar for every latency row', () => {
    const wrapper = mountLatency([
      { request_id: 'missing-first-token', first_token_ms: null, duration_ms: 3_440 },
      { request_id: 'normal-first-token', first_token_ms: 1_420, duration_ms: 5_900 },
      { request_id: 'slow-total', first_token_ms: 3_480, duration_ms: 42_910 },
    ], 'response')

    expect(wrapper.findAll('[data-testid="latency-health-bar"]')).toHaveLength(3)
  })

  it('keeps gradient health bars visible without relying on generated utility colors', () => {
    const wrapper = mountLatency([
      { request_id: 'gradient-bar', first_token_ms: 1_420, duration_ms: 5_900 },
    ])

    const bar = wrapper.get('[data-testid="latency-health-bar"]')
    const inlineStyle = bar.attributes('style') || ''
    expect(inlineStyle).toContain('background:')
    expect(inlineStyle).toContain('linear-gradient')
    expect(inlineStyle).toContain('width: 4px')
  })

  it('updates the latency and health indicator when switching display modes', async () => {
    const wrapper = mountLatency([{
      request_id: 'switch-mode', first_token_ms: 60_000, duration_ms: 80_000, streaming_ack_ms: 720,
    }], 'response')

    await wrapper.setProps({ firstTokenMode: 'model' })
    expect(wrapper.get('[data-testid="first-token-value"]').text()).toBe('1m 0s')
    expect(wrapper.get('[aria-hidden="true"]').classes()).toContain('from-red-500')
    expect(wrapper.get('[data-testid="streaming-ack-value"]').text()).toBe('720ms')
  })
})

describe('admin UsageTable tooltip', () => {
  it('shows output TPS in the latency cell using total duration', () => {
    const row = { ...baseImageRow, image_count: 0, billing_mode: 'token', output_tokens: 1000, duration_ms: 20_000, first_token_ms: 10_000 }
    const wrapper = mount(UsageTable, {
      props: {
        data: [row, { ...row, request_id: 'no-duration', duration_ms: null }, { ...row, request_id: 'image', image_count: 1 }],
        loading: false,
        columns: [{ key: 'latency', label: 'Latency' }],
      },
      global: { stubs: { DataTable: DataTableStub, EmptyState: true, Icon: true, Teleport: true } },
    })
    expect(wrapper.findAll('[data-testid="output-tps"]').map(cell => cell.text())).toEqual(['50.0 tok/s', '—', '—'])
    expect(wrapper.text()).toContain('Output TPS')
  })

  beforeEach(() => {
    locale = 'en'
    vi.spyOn(HTMLElement.prototype, 'getBoundingClientRect').mockReturnValue({
      x: 0,
      y: 0,
      top: 20,
      left: 20,
      right: 120,
      bottom: 40,
      width: 100,
      height: 20,
      toJSON: () => ({}),
    } as DOMRect)
  })

  it.each([
    ['en', 'Long context', 'Long-context pricing was applied. Input and output rates depend on the pricing tier, not a uniform multiplier.'],
    ['zh', '长上下文', '已应用长上下文计费。输入和输出费率取决于定价档位，并非统一倍率。'],
  ] as const)('marks only applied long-context billing with localized text in %s', (language, label, tooltip) => {
    locale = language
    const wrapper = mount(UsageTable, {
      props: {
        data: [
          {
            ...baseImageRow,
            request_id: 'req-long-context-enabled',
            long_context_billing_applied: true,
          },
          {
            ...baseImageRow,
            request_id: 'req-long-context-disabled',
            long_context_billing_applied: false,
          },
          {
            ...baseImageRow,
            request_id: 'req-long-context-absent',
          },
        ],
        loading: false,
        columns: [],
      },
      global: {
        stubs: {
          DataTable: DataTableStub,
          EmptyState: true,
          Icon: true,
          Teleport: true,
        },
      },
    })

    expect(wrapper.findAll('[data-testid="long-context-billing-marker"]')).toHaveLength(1)
    const marker = wrapper.get('[data-request-id="req-long-context-enabled"] [data-testid="long-context-billing-marker"]')
    expect(marker.text()).toBe(label)
    expect(marker.attributes('title')).toBe(tooltip)
    expect(wrapper.find('[data-request-id="req-long-context-disabled"] [data-testid="long-context-billing-marker"]').exists()).toBe(false)
    expect(wrapper.find('[data-request-id="req-long-context-absent"] [data-testid="long-context-billing-marker"]').exists()).toBe(false)
    expect(wrapper.text()).not.toContain('x2')
  })

  it('keeps the request type badge and adds a separate badge only for native compaction rows', () => {
    const DataTableStreamStub = {
      props: ['data'],
      template: `
        <div>
          <div v-for="row in data" :key="row.request_id">
            <slot name="cell-stream" :row="row" />
          </div>
        </div>
      `,
    }
    const wrapper = mount(UsageTable, {
      props: {
        data: [
          {
            ...baseImageRow,
            request_id: 'req-compaction-stream',
            request_type: 'stream',
            stream: true,
            native_compaction_v2: true,
          },
          {
            ...baseImageRow,
            request_id: 'req-historical-sync',
            request_type: 'sync',
            stream: false,
            native_compaction_v2: false,
          },
        ],
        loading: false,
        columns: [],
      },
      global: {
        stubs: {
          DataTable: DataTableStreamStub,
          EmptyState: true,
          Icon: true,
          Teleport: true,
        },
      },
    })

    const requestBadges = wrapper.findAll('[data-testid="request-type-badge"]')
    expect(requestBadges).toHaveLength(2)
    expect(requestBadges[0].text()).toBe('Stream')
    expect(requestBadges[1].text()).toBe('Sync')
    expect(wrapper.findAll('[data-testid="native-compaction-badge"]')).toHaveLength(1)
    expect(wrapper.get('[data-testid="native-compaction-badge"]').text()).toBe('Compaction')
  })

  it.each([
    [0, '0.0x'],
    [0.5, '0.50x'],
    [undefined, '1.00x'],
  ])('shows the stored user rate %s in cost details', async (rate, expected) => {
    const wrapper = mount(UsageTable, {
      props: { data: [{ ...baseImageRow, rate_multiplier: rate }], loading: false, columns: [] },
      global: { stubs: { DataTable: DataTableStub, EmptyState: true, Icon: true, Teleport: true } },
    })
    const triggers = wrapper.findAll('.group.relative')
    await triggers[triggers.length - 1].trigger('mouseenter')
    const rateLabel = wrapper.get('.fixed').findAll('span').find(span => span.text() === 'Rate')!
    expect(rateLabel.element.parentElement?.textContent).toContain(expected)
    wrapper.unmount()
  })

  it('shows service tier and billing breakdown in cost tooltip', async () => {
    const row = {
      request_id: 'req-admin-1',
      actual_cost: 0.092883,
      total_cost: 0.092883,
      account_rate_multiplier: 1,
      rate_multiplier: 1,
      service_tier: 'priority',
      input_cost: 0.020285,
      output_cost: 0.00303,
      cache_creation_cost: 0,
      cache_read_cost: 0.069568,
      input_tokens: 4057,
      output_tokens: 101,
    }

    const wrapper = mount(UsageTable, {
      props: {
        data: [row],
        loading: false,
        columns: [],
      },
      global: {
        stubs: {
          DataTable: DataTableStub,
          EmptyState: true,
          Icon: true,
          Teleport: true,
        },
      },
    })

    const tooltipTriggers = wrapper.findAll('.group.relative')
    await tooltipTriggers[tooltipTriggers.length - 1].trigger('mouseenter')
    await nextTick()

    const text = wrapper.text()
    expect(text).toContain('Service tier')
    expect(text).toContain('Fast')
    expect(text).toContain('Rate')
    expect(text).toContain('1.00x')
    expect(text).toContain('Account rate')
    expect(text).toContain('User billed')
    expect(text).toContain('Account billed')
    expect(text).toContain('$0.092883')
    expect(text).toContain('$5.0000 / 1M tokens')
    expect(text).toContain('$30.0000 / 1M tokens')
    expect(text).toContain('$0.069568')
  })

  it('uses the official billed input token denominator for input price', async () => {
    const row = {
      request_id: 'req-cache-neutral-price',
      model: 'gpt-5.5',
      billing_mode: 'token',
      actual_cost: 0.005285,
      total_cost: 0.1057,
      account_rate_multiplier: 0.045,
      rate_multiplier: 0.05,
      input_cost: 0.10486,
      output_cost: 0.00084,
      cache_creation_cost: 0,
      cache_read_cost: 0,
      input_tokens: 3778,
      output_tokens: 28,
      upstream_input_tokens: 1889,
      upstream_output_tokens: 14,
      cache_read_tokens: 8597,
      cache_creation_tokens: 0,
    }
    const wrapper = mount(UsageTable, {
      props: { data: [row], loading: false, columns: [] },
      global: { stubs: { DataTable: DataTableStub, EmptyState: true, Icon: true, Teleport: true } },
    })

    const triggers = wrapper.findAll('.group.relative')
    await triggers[triggers.length - 1].trigger('mouseenter')

    const text = wrapper.text()
    // Sub2API's official usage detail derives the displayed token price from
    // the recorded input cost and billed text input token count.
    expect(text).toContain('$27.7554 / 1M tokens')
    expect(text).not.toContain('$5.0000 / 1M tokens')
    expect(text).not.toContain('$10.0000 / 1M tokens')
    wrapper.unmount()
  })

  it('shows raw upstream tokens beside billed tokens without changing billed values', () => {
    const row = {
      request_id: 'req-real-token-column',
      model: 'gpt-5.5',
      billing_mode: 'token',
      input_tokens: 3778,
      output_tokens: 28,
      upstream_input_tokens: 1889,
      upstream_output_tokens: 14,
      cache_read_tokens: 17194,
      cache_creation_tokens: 1800,
      cache_creation_5m_tokens: 1200,
      cache_creation_1h_tokens: 600,
      upstream_cache_read_tokens: 8597,
      upstream_cache_creation_tokens: 900,
      upstream_cache_creation_5m_tokens: 600,
      upstream_cache_creation_1h_tokens: 300,
    }
    const wrapper = mount(UsageTable, {
      props: {
        data: [row],
        loading: false,
        columns: [
          { key: 'tokens', label: 'Tokens', sortable: false },
          { key: 'upstream_tokens', label: 'Actual Tokens', sortable: false },
        ],
      },
      global: { stubs: { DataTable: DataTableStub, EmptyState: true, Icon: true, Teleport: true } },
    })

    const billed = wrapper.get('[data-testid="billed-token-cell"]')
    const upstream = wrapper.get('[data-testid="upstream-token-cell"]')
    expect(billed.text()).toContain('3,778')
    expect(billed.text()).toContain('28')
    expect(billed.text()).toContain('17.2K')
    expect(billed.text()).toContain('1.8K')
    expect(billed.text()).not.toContain('8.6K')
    expect(billed.text()).not.toContain('900')
    expect(upstream.text()).toContain('1,889')
    expect(upstream.text()).toContain('14')
    expect(upstream.text()).toContain('8.6K')
    expect(upstream.get('[data-testid="upstream-cache-read-tokens"]').text()).toContain('8.6K')
    expect(upstream.get('[data-testid="upstream-cache-creation-tokens"]').text()).toContain('900')
    const details = upstream.get('[data-testid="upstream-token-details"]').attributes('title')
    expect(details).toContain('Cache Write 5m: 600')
    expect(details).toContain('Cache Write 1h: 300')
    expect(details).toContain('Total: 11,400')
    wrapper.unmount()
  })

  it('shows adjusted cache buckets and total in the billed token tooltip', async () => {
    const row = {
      request_id: 'req-billed-cache-tooltip',
      model: 'gpt-5.5',
      billing_mode: 'token',
      input_tokens: 3778,
      output_tokens: 28,
      upstream_input_tokens: 1889,
      upstream_output_tokens: 14,
      cache_read_tokens: 17194,
      cache_creation_tokens: 1800,
      upstream_cache_read_tokens: 8597,
      upstream_cache_creation_tokens: 900,
    }
    const wrapper = mount(UsageTable, {
      props: { data: [row], loading: false, columns: [] },
      global: { stubs: { DataTable: DataTableStub, EmptyState: true, Icon: true, Teleport: true } },
    })

    await wrapper.get('[data-testid="billed-token-cell"] .group.relative').trigger('mouseenter')
    const tooltip = wrapper.get('[data-testid="billed-token-cell"]')
    // The tooltip is teleported to body, so assert against the full wrapper text.
    const text = wrapper.text()
    expect(text).toContain('17.2K')
    expect(text).toContain('1.8K')
    expect(text).toContain('22,800')
    expect(tooltip.text()).toContain('17.2K')
    wrapper.unmount()
  })

  it('keeps raw cache buckets in the billed view when no input multiplier is inferred', () => {
    const row = {
      request_id: 'req-unadjusted-cache',
      model: 'gpt-5.5',
      billing_mode: 'token',
      input_tokens: 100,
      output_tokens: 10,
      upstream_input_tokens: 100,
      upstream_output_tokens: 10,
      cache_read_tokens: 20,
      cache_creation_tokens: 5,
    }
    const wrapper = mount(UsageTable, {
      props: { data: [row], loading: false, columns: [] },
      global: { stubs: { DataTable: DataTableStub, EmptyState: true, Icon: true, Teleport: true } },
    })

    const billed = wrapper.get('[data-testid="billed-token-cell"]')
    expect(billed.get('[data-testid="billed-cache-read-tokens"]').text()).toContain('20')
    expect(billed.get('[data-testid="billed-cache-creation-tokens"]').text()).toContain('5')
    wrapper.unmount()
  })

  it('keeps the original cache icon and labels for the user-facing view', async () => {
    const row = {
      request_id: 'req-user-cache-display',
      model: 'gpt-5.5',
      billing_mode: 'token',
      input_tokens: 3778,
      output_tokens: 28,
      cache_read_tokens: 8597,
      cache_creation_tokens: 0,
      cache_creation_5m_tokens: 0,
      cache_creation_1h_tokens: 0,
    }
    const wrapper = mount(UsageTable, {
      props: { data: [row], loading: false, columns: [], showAdjustedCacheDetails: false },
      global: { stubs: { DataTable: DataTableStub, EmptyState: true, Icon: true, Teleport: true } },
    })

    const billed = wrapper.get('[data-testid="billed-token-cell"]')
    expect(billed.text()).toContain('8.6K')
    expect(billed.text()).not.toContain('R 8.6K')
    expect(billed.findAll('svg')).toHaveLength(1)

    await billed.find('.group.relative').trigger('mouseenter')
    const text = wrapper.text()
    expect(text).toContain('Cache Read Tokens')
    expect(text).not.toContain('Billed cache read tokens')
    expect(text).not.toContain('Adjusted cache read tokens')
    expect(text).toContain('12,403')
    wrapper.unmount()
  })

  it.each(['token', 'image', 'per_request'])('keeps eight decimal places in %s cost details', async (billingMode) => {
    const row = {
      ...baseImageRow,
      billing_mode: billingMode,
      image_count: billingMode === 'image' ? 2 : 0,
      input_cost: 0.00000001,
      image_input_cost: 0.00000002,
      output_cost: 0.00000003,
      image_output_cost: 0.00000004,
      cache_creation_cost: 0.00000005,
      cache_read_cost: 0.00000006,
      total_cost: 0.00000022,
      actual_cost: 0.00000042,
      account_stats_cost: 0.00000012,
      account_rate_multiplier: 1.5,
    }
    const wrapper = mount(UsageTable, {
      props: { data: [row], loading: false, columns: [] },
      global: { stubs: { DataTable: DataTableStub, EmptyState: true, Icon: true, Teleport: true } },
    })
    const triggers = wrapper.findAll('.group.relative')
    await triggers[triggers.length - 1].trigger('mouseenter')
    const amounts = wrapper.get('.fixed').findAll('span').map(span => span.text())
    expect(amounts).toEqual(expect.arrayContaining([
      '$0.00000001', '$0.00000002', '$0.00000003', '$0.00000004',
      '$0.00000005', '$0.00000006', '$0.00000022', '$0.00000042', '$0.00000018',
    ]))
    if (billingMode === 'image') expect(amounts).toContain('$0.00000011')
    wrapper.unmount()
  })

  it('uses eight decimal places for missing cost values', async () => {
    const wrapper = mount(UsageTable, {
      props: {
        data: [{ ...baseImageRow, billing_mode: 'per_request', image_count: 0, total_cost: undefined, actual_cost: undefined }],
        loading: false,
        columns: [],
      },
      global: { stubs: { DataTable: DataTableStub, EmptyState: true, Icon: true, Teleport: true } },
    })
    const triggers = wrapper.findAll('.group.relative')
    await triggers[triggers.length - 1].trigger('mouseenter')
    const amounts = wrapper.get('.fixed').findAll('span').map(span => span.text()).filter(text => text.startsWith('$'))
    expect(amounts).toEqual(['$0.00000000', '$0.00000000', '$0.00000000', '$0.00000000'])
    wrapper.unmount()
  })

  it('shows requested and upstream models separately for admin rows', () => {
    const row = {
      request_id: 'req-admin-model-1',
      model: 'claude-sonnet-4',
      upstream_model: 'claude-sonnet-4-20250514',
      actual_cost: 0,
      total_cost: 0,
      account_rate_multiplier: 1,
      rate_multiplier: 1,
      input_cost: 0,
      output_cost: 0,
      cache_creation_cost: 0,
      cache_read_cost: 0,
      input_tokens: 0,
      output_tokens: 0,
    }

    const wrapper = mount(UsageTable, {
      props: {
        data: [row],
        loading: false,
        columns: [],
      },
      global: {
        stubs: {
          DataTable: DataTableStub,
          EmptyState: true,
          Icon: true,
          Teleport: true,
        },
      },
    })

    const text = wrapper.text()
    expect(text).toContain('claude-sonnet-4')
    expect(text).toContain('claude-sonnet-4-20250514')
  })

  it('shows requested and forwarded reasoning effort separately when they differ', () => {
    const wrapper = mount(UsageTable, {
      props: {
        data: [{
          request_id: 'req-admin-effort-1',
          model: 'gpt-5.4',
          reasoning_effort: 'max',
          upstream_reasoning_effort: 'xhigh',
        }],
        loading: false,
        columns: [],
      },
      global: {
        stubs: {
          DataTable: DataTableStub,
          EmptyState: true,
          Icon: true,
          Teleport: true,
        },
      },
    })

    const text = wrapper.text()
    expect(text).toContain('Max')
    expect(text).toContain('XHigh')
    expect(text).toContain('↳')
  })

  it('shows a single reasoning effort when requested matches forwarded', () => {
    const wrapper = mount(UsageTable, {
      props: {
        data: [{
          request_id: 'req-admin-effort-2',
          model: 'gpt-5.6-sol',
          reasoning_effort: 'max',
        }],
        loading: false,
        columns: [],
      },
      global: {
        stubs: {
          DataTable: DataTableStub,
          EmptyState: true,
          Icon: true,
          Teleport: true,
        },
      },
    })

    const text = wrapper.text()
    expect(text).toContain('Max')
    expect(text).not.toContain('↳')
  })

  it('hides mapped reasoning effort for user rows that only have the requested value', () => {
    const wrapper = mount(UsageTable, {
      props: {
        data: [{
          request_id: 'req-user-effort-1',
          model: 'gpt-5.4',
          reasoning_effort: 'max',
        }],
        loading: false,
        columns: [],
      },
      global: {
        stubs: {
          DataTable: DataTableStub,
          EmptyState: true,
          Icon: true,
          Teleport: true,
        },
      },
    })

    expect(wrapper.text()).toContain('Max')
    expect(wrapper.text()).not.toContain('XHigh')
    expect(wrapper.text()).not.toContain('↳')
  })

	it.each([
		{
			name: 'possible version variant',
			responseModel: 'gpt-5.5-2026-08-01',
			expectedBadge: 'Possible version variant',
		},
		{
			name: 'different upstream model',
			responseModel: 'gpt-5.4',
			expectedBadge: 'Different model',
		},
	])('shows a compact upstream response audit marker for $name', ({ responseModel, expectedBadge }) => {
		const wrapper = mount(UsageTable, {
			props: {
				data: [{
					request_id: `req-${responseModel}`,
					model: 'gpt-5.6-sol',
					upstream_model: 'gpt-5.5',
					model_mapping_chain: 'gpt-5.6-sol→gpt-5.5',
					upstream_response_model: responseModel,
					upstream_model_mismatch: true,
				}],
				loading: false,
				columns: [],
			},
			global: {
				stubs: {
					DataTable: DataTableStub,
					EmptyState: true,
					Icon: true,
					Teleport: true,
				},
			},
		})

		const text = wrapper.text()
		expect(text).toContain('gpt-5.6-sol')
		expect(text).toContain('gpt-5.5')
		expect(text).toContain(responseModel)
		expect(text).toContain(expectedBadge)
	})

  it.each([
    {
      name: 'defaulted row',
      row: {
        ...baseImageRow,
        request_id: 'req-admin-default-image',
        image_size: '2K',
        image_input_size: 'auto',
        image_output_size: null,
        image_size_source: 'default',
      },
      expected: ['2K', 'Default billing tier', 'auto', 'unknown'],
    },
    {
      name: 'output-sourced row',
      row: {
        ...baseImageRow,
        request_id: 'req-admin-output-image',
        image_size: '4K',
        image_input_size: '1024x1024',
        image_output_size: '3840x2160',
        image_size_source: 'output',
        image_size_breakdown: { '4K': 1 },
      },
      expected: ['4K', 'Upstream output', '1024x1024', '3840x2160', '4K x 1'],
    },
    {
      name: 'input-sourced row',
      row: {
        ...baseImageRow,
        request_id: 'req-admin-input-image',
        image_size: '1K',
        image_input_size: '1024x1024',
        image_output_size: null,
        image_size_source: 'input',
      },
      expected: ['1K', 'Request input', '1024x1024', 'unknown'],
    },
    {
      name: 'legacy unstandardized row',
      row: {
        ...baseImageRow,
        request_id: 'req-admin-legacy-unstandardized-image',
        image_size: '512x512',
        image_input_size: null,
        image_output_size: null,
        image_size_source: null,
      },
      expected: ['legacy unstandardized: 512x512', 'Legacy record', 'unknown'],
    },
  ])('shows image usage metadata for $name', async ({ row, expected }) => {
    const wrapper = mount(UsageTable, {
      props: {
        data: [row],
        loading: false,
        columns: [],
      },
      global: {
        stubs: {
          DataTable: DataTableStub,
          EmptyState: true,
          Icon: true,
          Teleport: true,
        },
      },
    })

    await wrapper.find('.group.relative').trigger('mouseenter')
    await nextTick()

    const text = wrapper.text()
    expect(text).toContain('Image count')
    expect(text).toContain('Billing size')
    expect(text).toContain('Size source')
    expect(text).toContain('Input size')
    expect(text).toContain('Output size')
    expect(text).toContain('Per-image price')
    expect(text).toContain('Image total price')
    for (const value of expected) {
      expect(text).toContain(value)
    }
  })

  it('displays historical image rows with missing billing_mode as image usage without a 2K fallback', async () => {
    const wrapper = mount(UsageTable, {
      props: {
        data: [
          {
            ...baseImageRow,
            request_id: 'req-admin-legacy-missing-image',
            billing_mode: null,
            image_size: null,
            image_input_size: null,
            image_output_size: null,
            image_size_source: null,
            image_size_breakdown: null,
          },
        ],
        loading: false,
        columns: [],
      },
      global: {
        stubs: {
          DataTable: DataTableStub,
          EmptyState: true,
          Icon: true,
          Teleport: true,
        },
      },
    })

    await wrapper.find('.group.relative').trigger('mouseenter')
    await nextTick()

    const text = wrapper.text()
    expect(text).toContain('Image')
    expect(text).toContain('Image count')
    expect(text).toContain('Per-image price')
    expect(text).toContain('not recorded')
    expect(text).not.toContain('(2K)')
  })
})

describe('admin UsageTable request ID column', () => {
  beforeEach(() => {
    appStoreMocks.showSuccess.mockReset()
    appStoreMocks.showError.mockReset()
  })

  it('renders and copies the request ID', async () => {
    const writeText = vi.fn().mockResolvedValue(undefined)
    vi.stubGlobal('navigator', { clipboard: { writeText } })

    const wrapper = mount(UsageTable, {
      props: {
        data: [{ ...baseImageRow, request_id: 'req-admin-visible-id' }],
        loading: false,
        columns: [{ key: 'request_id', label: 'Request ID' }],
      },
      global: {
        stubs: {
          DataTable: DataTableStub,
          EmptyState: true,
          Icon: true,
          Teleport: true,
        },
      },
    })

    expect(wrapper.text()).toContain('req-admin-visible-id')
    await wrapper.get('button[title="Copy to clipboard"]').trigger('click')

    expect(writeText).toHaveBeenCalledWith('req-admin-visible-id')
    expect(appStoreMocks.showSuccess).toHaveBeenCalledWith('Request ID copied')
  })

  it('renders and copies the upstream ID', async () => {
    const writeText = vi.fn().mockResolvedValue(undefined)
    vi.stubGlobal('navigator', { clipboard: { writeText } })

    const wrapper = mount(UsageTable, {
      props: {
        data: [{ ...baseImageRow, request_id: '', upstream_request_id: '20260903082826779695' }],
        loading: false,
        columns: [{ key: 'upstream_request_id', label: 'Upstream ID' }],
      },
      global: {
        stubs: {
          DataTable: DataTableStub,
          EmptyState: true,
          Icon: true,
          Teleport: true,
        },
      },
    })

    expect(wrapper.text()).toContain('20260903082826779695')
    const copyButtons = wrapper.findAll('button[title="Copy to clipboard"]')
    expect(copyButtons).toHaveLength(1)
    await copyButtons[0].trigger('click')

    expect(writeText).toHaveBeenCalledWith('20260903082826779695')
    expect(appStoreMocks.showSuccess).toHaveBeenCalledWith('Upstream ID copied')
  })
})

describe('admin UsageTable IP geolocation batch toolbar', () => {
  const DataTableStubWithIp = {
    props: ['data'],
    template: `
      <div>
        <div v-for="row in data" :key="row.request_id">
          <slot name="cell-ip_address" :row="row" />
        </div>
      </div>
    `,
  }

  beforeEach(() => {
    ipGeoMocks.getEntry.mockReset()
    ipGeoMocks.fetchOne.mockReset()
    ipGeoMocks.fetchBatch.mockReset()
    ipGeoMocks.getEntry.mockReturnValue({ status: 'idle' })
  })

  it('does not render the batch toolbar when the ip_address column is not visible', () => {
    const wrapper = mount(UsageTable, {
      props: {
        data: [{ request_id: 'r1', ip_address: '8.8.8.8' }],
        loading: false,
        columns: [],
      },
      global: { stubs: { DataTable: DataTableStubWithIp, EmptyState: true, Teleport: true } },
    })
    expect(wrapper.text()).not.toContain('usage.ipGeo.batchFetch')
  })

  it('renders the batch toolbar with a pending count when the ip_address column is visible', () => {
    const wrapper = mount(UsageTable, {
      props: {
        data: [
          { request_id: 'r1', ip_address: '8.8.8.8' },
          { request_id: 'r2', ip_address: '8.8.8.8' },
          { request_id: 'r3', ip_address: '1.1.1.1' },
        ],
        loading: false,
        columns: [{ key: 'ip_address', label: 'IP' }],
      },
      global: { stubs: { DataTable: DataTableStubWithIp, EmptyState: true, Teleport: true } },
    })
    expect(wrapper.text()).toContain('usage.ipGeo.pending')
    const button = wrapper.find('button')
    expect(button.exists()).toBe(true)
    expect((button.element as HTMLButtonElement).disabled).toBe(false)
  })

  it('fetches deduplicated IPs from the current page when the batch button is clicked', async () => {
    ipGeoMocks.fetchBatch.mockResolvedValue(true)
    const wrapper = mount(UsageTable, {
      props: {
        data: [
          { request_id: 'r1', ip_address: '8.8.8.8' },
          { request_id: 'r2', ip_address: '8.8.8.8' },
          { request_id: 'r3', ip_address: '1.1.1.1' },
        ],
        loading: false,
        columns: [{ key: 'ip_address', label: 'IP' }],
      },
      global: { stubs: { DataTable: DataTableStubWithIp, EmptyState: true, Teleport: true } },
    })
    await wrapper.find('button').trigger('click')
    expect(ipGeoMocks.fetchBatch).toHaveBeenCalledWith(['8.8.8.8', '1.1.1.1'])
    expect(wrapper.emitted('ipGeoBatchFailed')).toBeUndefined()
  })

  it('emits ipGeoBatchFailed when the batch request reports a network-level failure', async () => {
    ipGeoMocks.fetchBatch.mockResolvedValue(false)
    const wrapper = mount(UsageTable, {
      props: {
        data: [{ request_id: 'r1', ip_address: '8.8.8.8' }],
        loading: false,
        columns: [{ key: 'ip_address', label: 'IP' }],
      },
      global: { stubs: { DataTable: DataTableStubWithIp, EmptyState: true, Teleport: true } },
    })
    await wrapper.find('button').trigger('click')
    expect(wrapper.emitted('ipGeoBatchFailed')).toHaveLength(1)
  })

  it('renders IpGeoCell content for ip_address cells', () => {
    ipGeoMocks.getEntry.mockReturnValue({ status: 'success', label: 'CN · Guangdong · Shenzhen', detail: {} })
    const wrapper = mount(UsageTable, {
      props: {
        data: [{ request_id: 'r1', ip_address: '121.35.47.43' }],
        loading: false,
        columns: [{ key: 'ip_address', label: 'IP' }],
      },
      global: { stubs: { DataTable: DataTableStubWithIp, EmptyState: true, Teleport: true } },
    })
    expect(wrapper.text()).toContain('121.35.47.43')
    expect(wrapper.text()).toContain('CN · Guangdong · Shenzhen')
  })
})

// A DataTable stub that also renders cell-user, so the deleted badge can be asserted.
const DataTableStubWithUser = {
  props: ['data'],
  template: `
    <div>
      <div v-for="row in data" :key="row.request_id">
        <slot name="cell-user" :row="row" />
        <slot name="cell-model" :row="row" :value="row.model" />
        <slot name="cell-reasoning_effort" :row="row" :value="row.reasoning_effort" />
        <slot name="cell-billing_mode" :row="row" />
        <slot name="cell-tokens" :row="row" />
        <slot name="cell-cost" :row="row" />
      </div>
    </div>
  `,
}

describe('admin UsageTable deleted-user badge', () => {
  it('renders deleted badge for a soft-deleted user row', () => {
    const row = {
      request_id: 'req-deleted-user-1',
      model: 'claude-3',
      user_id: 2,
      user: { id: 2, email: 'd@test.com', deleted_at: '2026-05-28T00:00:00Z' },
      actual_cost: 0,
      total_cost: 0,
      input_cost: 0,
      output_cost: 0,
      rate_multiplier: 1,
      input_tokens: 1,
      output_tokens: 1,
    }

    const wrapper = mount(UsageTable, {
      props: {
        data: [row],
        loading: false,
        columns: [{ key: 'user', label: 'User' }],
      },
      global: {
        stubs: {
          DataTable: DataTableStubWithUser,
          EmptyState: true,
          Icon: true,
          Teleport: true,
        },
      },
    })

    expect(wrapper.text()).toContain('Deleted')
    expect(wrapper.text()).toContain('d@test.com')
  })

  it('does NOT render deleted badge for an active user row', () => {
    const row = {
      request_id: 'req-active-user-1',
      model: 'claude-3',
      user_id: 3,
      user: { id: 3, email: 'active@test.com', deleted_at: null },
      actual_cost: 0,
      total_cost: 0,
      input_cost: 0,
      output_cost: 0,
      rate_multiplier: 1,
      input_tokens: 1,
      output_tokens: 1,
    }

    const wrapper = mount(UsageTable, {
      props: {
        data: [row],
        loading: false,
        columns: [{ key: 'user', label: 'User' }],
      },
      global: {
        stubs: {
          DataTable: DataTableStubWithUser,
          EmptyState: true,
          Icon: true,
          Teleport: true,
        },
      },
    })

    expect(wrapper.text()).not.toContain('Deleted')
    expect(wrapper.text()).toContain('active@test.com')
  })
})
