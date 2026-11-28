import { mount } from '@vue/test-utils'
import { createPinia } from 'pinia'
import { describe, expect, it, vi } from 'vitest'
import type { UserMonitorView } from '@/api/channelMonitor'
import MonitorCardGrid from '../monitor/MonitorCardGrid.vue'
import MonitorCard from '../monitor/MonitorCard.vue'
import zhDashboard from '@/i18n/locales/zh/dashboard'
import enDashboard from '@/i18n/locales/en/dashboard'

vi.mock('vue-i18n', async () => ({
  ...await vi.importActual<typeof import('vue-i18n')>('vue-i18n'),
  useI18n: () => ({ t: (key: string) => key }),
}))

function item(id: number, provider: UserMonitorView['provider'], name: string): UserMonitorView {
  return { id, name, provider, group_name: '', primary_model: 'test-model', primary_status: 'operational', primary_latency_ms: 500, primary_ping_latency_ms: 50, availability_7d: 99, extra_models: [], timeline: [] }
}
const items = [item(9, 'grok', 'Grok first'), item(5, 'openai', 'OpenAI first'), item(3, 'grok', 'Grok second'), item(1, 'anthropic', 'Claude first')]

function open(rows: UserMonitorView[] = items) {
  return mount(MonitorCardGrid, { props: { items: rows, countdownSeconds: 58, loading: false }, global: { stubs: { MonitorCard: { props: ['item', 'availabilityValue'], emits: ['click'], template: '<button @click="$emit(\'click\')">{{ item.name }}</button>' }, ProviderIcon: true } } })
}

describe('channel status platform sections', () => {
  it('groups platforms in first-seen order and keeps channel order within each platform', () => {
    const wrapper = open()
    const sections = wrapper.findAll('section[data-provider]')
    expect(sections.map(section => section.attributes('data-provider'))).toEqual(['grok', 'openai', 'anthropic'])
    expect(sections[0].findAllComponents(MonitorCard).map(card => card.props('item').id)).toEqual([9, 3])
    expect(sections[0].get('h2').text()).toContain('monitorCommon.providers.grok')
    expect(sections[0].get('[data-testid="provider-count"]').text()).toBe('2')
  })

  it('does not forward card clicks to a details dialog', async () => {
    const wrapper = open()
    await wrapper.findAllComponents(MonitorCard)[1].trigger('click')
    expect(wrapper.emitted('cardClick')).toBeUndefined()
  })

  it('uses a neutral value when there are no recent probe records', () => {
    const wrapper = open([items[0]!])
    expect(wrapper.findComponent(MonitorCard).props('availabilityValue')).toBeNull()
  })

  it('calculates availability from only the newest 24 timeline points', () => {
    const statuses: UserMonitorView['timeline'][number]['status'][] = [
      ...Array.from({ length: 12 }, () => 'operational' as const),
      ...Array.from({ length: 6 }, () => 'degraded' as const),
      ...Array.from({ length: 6 }, () => 'failed' as const),
      ...Array.from({ length: 6 }, () => 'failed' as const),
    ]
    const checkedAt = Date.now()
    const monitor = {
      ...items[0]!,
      availability_7d: 99,
      timeline: statuses.map((status, index) => ({
        status,
        latency_ms: null,
        ping_latency_ms: null,
        checked_at: new Date(checkedAt - index * 1000).toISOString(),
      })),
    }

    const wrapper = open([monitor])

    expect(wrapper.findComponent(MonitorCard).props('availabilityValue')).toBe(75)
  })

  it('does not invent availability when the timeline is empty', () => {
    const wrapper = open([{ ...items[0]!, timeline: [] }])
    expect(wrapper.findComponent(MonitorCard).props('availabilityValue')).toBeNull()
  })

  it('counts probe errors as unavailable', () => {
    const wrapper = open([{
      ...items[0]!,
      timeline: [
        { status: 'operational', latency_ms: null, ping_latency_ms: null, checked_at: '2026-11-26T00:00:00Z' },
        { status: 'error', latency_ms: null, ping_latency_ms: null, checked_at: '2026-11-25T23:59:00Z' },
      ],
    }])
    expect(wrapper.findComponent(MonitorCard).props('availabilityValue')).toBe(50)
  })
})

describe('monitor metric sample description', () => {
  it('keeps an accessible sample label without hover hints', () => {
    const wrapper = mount(MonitorCard, {
      props: { item: items[0], availabilityValue: 99, countdownSeconds: 58 },
      global: { plugins: [createPinia()], stubs: { ProviderIcon: true, MonitorTimeline: true, MonitorIntelligence: true, MonitorQuotaView: true } },
    })
    expect(wrapper.get('.monitor-card-metrics').attributes('aria-label')).toBe('monitorCommon.recent24Metrics')
    expect(wrapper.get('.monitor-card-metrics').attributes('title')).toBe('')
    expect(wrapper.findAll('[data-metric][title]')).toHaveLength(0)
  })

  it('explains actual-token weighting and zero-hit input in the denominator', () => {
    expect(zhDashboard.monitorCommon.recentStreamCacheHint).toContain('真实 Token 加权')
    expect(zhDashboard.monitorCommon.recentStreamCacheHint).toContain('零命中请求计入分母')
    expect(enDashboard.monitorCommon.recentStreamCacheHint).toContain('Actual-token-weighted')
    expect(enDashboard.monitorCommon.recentStreamCacheHint).toContain('Zero-hit input is included')
  })
})
