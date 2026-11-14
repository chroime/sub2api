import { mount } from '@vue/test-utils'
import { describe, expect, it, vi } from 'vitest'

import type { UserMonitorView } from '@/api/channelMonitor'
import MonitorCard from '../monitor/MonitorCard.vue'

const { isQuotaVisible } = vi.hoisted(() => ({
  isQuotaVisible: vi.fn(() => false),
}))

vi.mock('@/utils/featureFlags', () => ({
  isChannelMonitorQuotaVisible: () => isQuotaVisible(),
}))

vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return {
    ...actual,
    useI18n: () => ({ t: (key: string) => key, te: () => true }),
  }
})

function makeItem(overrides: Partial<UserMonitorView> = {}): UserMonitorView {
  return {
    id: 1,
    name: 'claude-main',
    provider: 'kimi',
    group_name: '',
    primary_model: 'quota',
    primary_status: 'operational',
    primary_latency_ms: null,
    primary_ping_latency_ms: null,
    availability_7d: 100,
    availability_reset_active: false,
    extra_models: [],
    timeline: [],
    ...overrides,
  }
}

function mountCard(item: UserMonitorView, availabilityValue: number | null = 100) {
  return mount(MonitorCard, {
    props: {
      item,
      window: '7d',
      availabilityValue,
      countdownSeconds: 0,
    },
    global: {
      stubs: {
        MonitorMetricPair: true,
        MonitorAvailabilityRow: true,
        MonitorTimeline: true,
      },
    },
  })
}

describe('MonitorCard quota snapshot visibility', () => {
  it('hides the quota block when the system switch is off even if data exists', () => {
    isQuotaVisible.mockReturnValue(false)
    const wrapper = mountCard(
      makeItem({
        latest_quota: { source: 'cn_quota', success: true, fetched_at: '2026-08-18T00:00:00Z' },
      }),
    )
    expect(wrapper.find('[data-testid="monitor-quota-view"]').exists()).toBe(false)
  })

  it('renders the quota block when the switch is on and a snapshot exists', () => {
    isQuotaVisible.mockReturnValue(true)
    const wrapper = mountCard(
      makeItem({
        latest_quota: {
          source: 'cn_quota',
          success: true,
          plan_level: 'kimi-plus',
          tiers: [{ window: 'daily', label: 'requests', used_percent: 60 }],
          fetched_at: '2026-08-18T00:00:00Z',
        },
      }),
    )
    expect(wrapper.find('[data-testid="monitor-quota-view"]').exists()).toBe(true)
    expect(wrapper.text()).toContain('kimi-plus')
  })

  it('never renders the quota block without a snapshot', () => {
    isQuotaVisible.mockReturnValue(true)
    const wrapper = mountCard(makeItem())
    expect(wrapper.find('[data-testid="monitor-quota-view"]').exists()).toBe(false)
  })

  // 占位符 "quota" 是主模型存储值，不得作为假模型名直接透出到用户端。
  it('shows the localized quota label instead of the raw placeholder model', () => {
    const wrapper = mountCard(makeItem())
    expect(wrapper.attributes('title')).toBe('monitorCommon.checkMode.quota')
  })

  it('keeps the real model name for probe monitors', () => {
    const wrapper = mountCard(makeItem({ primary_model: 'claude-sonnet-4-5' }))
    expect(wrapper.attributes('title')).toBe('claude-sonnet-4-5')
    expect(wrapper.text()).not.toContain('claude-sonnet-4-5')
    expect(wrapper.text()).not.toContain('monitorCommon.checkMode.quota')
  })
})

describe('MonitorCard real metric values', () => {
  it('shows distinct 18-point probe and 60-minute intelligence histories', () => {
    isQuotaVisible.mockReturnValue(false)
    const checkedAt = new Date().toISOString()
    const wrapper = mount(MonitorCard, { props: {
      item: makeItem({
        timeline: Array.from({ length: 60 }, () => ({ status: 'failed', latency_ms: null, ping_latency_ms: null, checked_at: checkedAt })),
        intelligence: { window_end: checkedAt, series: [{ model: 'local-model', effort: 'medium', points: [{ status: 'pass', checked_at: checkedAt }] }] },
      }),
      window: '7d', availabilityValue: 98, countdownSeconds: 58,
    } })
    expect(wrapper.findAll('[data-testid="history-bar"]')).toHaveLength(18)
    expect(wrapper.findAll('[data-testid="intelligence-bar"]')).toHaveLength(60)
    expect(wrapper.findAll('[data-testid="history-bar"][data-status="failed"]')).toHaveLength(18)
    expect(wrapper.findAll('[data-testid="intelligence-bar"][data-status="pass"]')).toHaveLength(1)
  })
  it('keeps missing availability and latency neutral without made-up units', () => {
    const wrapper = mountCard(makeItem(), null)
    expect(wrapper.findAll('dd').map(value => value.text())).toEqual([
      'monitorCommon.latencyEmpty', 'monitorCommon.latencyEmpty', 'monitorCommon.latencyEmpty',
    ])
  })

  it('uses cache rate and actual first-token samples, never total duration or PING', () => {
    const wrapper = mountCard(makeItem({
      primary_latency_ms: 5000, primary_ping_latency_ms: 152,
      usage_metrics: { '7d': { cache_rate_pct: 86.2, first_token_ms: 3000 } },
    }), 98.8)
    expect(wrapper.findAll('dt').map(label => label.text())).toEqual([
      'monitorCommon.cacheRate', 'monitorCommon.availabilityPrefix', 'monitorCommon.firstToken',
    ])
    expect(wrapper.findAll('dd').map(value => value.text())).toEqual(['86.2%', '98.8%', '3.0s'])
  })

  it('does not treat an untested monitor as zero availability', () => {
    const wrapper = mountCard(makeItem({ primary_status: '' as UserMonitorView['primary_status'] }), 0)
    expect(wrapper.findAll('dd')[1].text()).toBe('monitorCommon.latencyEmpty')
  })

  it('switches cache and first-token metrics with the selected period', async () => {
    const wrapper = mountCard(makeItem({ usage_metrics: {
      '7d': { cache_rate_pct: 80, first_token_ms: 2000 },
      '15d': { cache_rate_pct: 62.5, first_token_ms: 500 },
      '30d': { cache_rate_pct: null, first_token_ms: null },
    } }))
    await wrapper.setProps({ window: '15d' })
    expect(wrapper.findAll('dd').map(value => value.text())).toEqual(['62.5%', '100.0%', '500ms'])
    await wrapper.setProps({ window: '30d' })
    expect(wrapper.findAll('dd').map(value => value.text())).toEqual(['monitorCommon.latencyEmpty', '100.0%', 'monitorCommon.latencyEmpty'])
  })

  it('keeps valid zero metrics and free user rates visible', () => {
    const wrapper = mountCard(makeItem({
      user_rate_multiplier: 0,
      usage_metrics: { '7d': { cache_rate_pct: 0, first_token_ms: 0 } },
    }), 0)
    expect(wrapper.findAll('dd').map(value => value.text())).toEqual(['0.0%', '0.0%', '0ms'])
    expect(wrapper.get('[data-testid="monitor-user-rate"]').text()).toContain('0.00x')
  })

  it('does not infer first-token latency from response duration when timing is missing', () => {
    const wrapper = mountCard(makeItem({ primary_latency_ms: 5000, primary_ping_latency_ms: 152 }))
    expect(wrapper.findAll('dd')[2].text()).toBe('monitorCommon.latencyEmpty')
    expect(wrapper.find('[data-testid="monitor-user-rate"]').exists()).toBe(false)
  })
})
