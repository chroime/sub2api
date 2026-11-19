import { flushPromises, shallowMount } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import type { UserMonitorView } from '@/api/channelMonitor'
import MonitorCard from '@/components/user/monitor/MonitorCard.vue'
import ChannelStatusV1View from '../ChannelStatusV1View.vue'

const { list, analytics } = vi.hoisted(() => ({ list: vi.fn(), analytics: vi.fn() }))
vi.mock('@/api/channelMonitor', () => ({ list, analytics, status: vi.fn() }))
vi.mock('@/stores/app', () => ({ useAppStore: () => ({ cachedPublicSettings: { channel_monitor_enabled: true }, showError: vi.fn() }) }))
vi.mock('vue-i18n', async () => ({
  ...await vi.importActual<typeof import('vue-i18n')>('vue-i18n'),
  useI18n: () => ({ t: (key: string) => key }),
}))
const mountView = () => shallowMount(ChannelStatusV1View, {
  global: { stubs: {
    AppLayout: { template: '<div><slot /></div>' },
    MonitorCardGrid: false,
    MonitorCard: false,
    MonitorTimeline: false,
    MonitorIntelligence: false,
    MonitorHero: { props: ['autoRefresh'], emits: ['refresh'], template: `<div>
      <button class="interval" @click="autoRefresh.setInterval(120)">120 seconds</button>
      <button class="refresh" @click="$emit('refresh')">Refresh</button>
      <button class="disable" @click="autoRefresh.setEnabled(false)">Disable</button>
    </div>` },
  } },
})
let wrapper: ReturnType<typeof mountView>
beforeEach(() => {
  vi.useFakeTimers()
  localStorage.clear()
  list.mockReset().mockResolvedValue({ items: [] })
  analytics.mockReset().mockResolvedValue({ items: [] })
})
afterEach(() => { wrapper?.unmount(); vi.useRealTimers(); localStorage.clear() })

describe('channel monitor refresh interval', () => {
  it('renders availability before the asynchronous analytics response', async () => {
    const item: UserMonitorView = {
      id: 4, name: 'Fast gateway', provider: 'openai', group_name: '', primary_model: 'gpt-test',
      primary_status: 'operational', primary_latency_ms: 100, primary_ping_latency_ms: null,
      availability_7d: 100, extra_models: [],
      timeline: [{ status: 'operational', checked_at: '2026-11-18T02:00:00Z', latency_ms: 100, ping_latency_ms: null }],
    }
    let resolveAnalytics!: (response: { items: Array<{
      id: number
      intelligence: NonNullable<UserMonitorView['intelligence']>
      usage_metrics: NonNullable<UserMonitorView['usage_metrics']>
    }> }) => void
    list.mockResolvedValue({ items: [item] })
    analytics.mockReturnValue(new Promise(resolve => { resolveAnalytics = resolve }))

    wrapper = mountView()
    await flushPromises()

    const card = wrapper.findComponent(MonitorCard)
    expect(card.exists()).toBe(true)
    expect(card.find('[data-status="operational"]').exists()).toBe(true)
    expect(analytics).toHaveBeenCalledOnce()
    expect(card.get('[data-metric="cache-rate"]').text()).toContain('monitorCommon.latencyEmpty')

    resolveAnalytics({ items: [{
      id: 4,
      intelligence: { window_end: '2026-11-18T02:01:00Z', series: [{ model: 'gpt-test', effort: 'high', points: [
        { status: 'pass', checked_at: '2026-11-18T02:00:30Z' },
      ] }] },
      usage_metrics: { '7d': { cache_rate_pct: 86.2, first_token_ms: 3000 } },
    }] })
    await flushPromises()

    expect(card.get('[data-metric="cache-rate"]').text()).toContain('86.2')
    expect(card.get('[data-metric="first-token"]').text()).toContain('3.0')
    expect(card.find('[data-testid="monitor-intelligence"]').exists()).toBe(true)
  })

  it('retains list analytics when the separate analytics endpoint is unavailable', async () => {
    const item: UserMonitorView = {
      id: 5, name: 'Legacy gateway', provider: 'openai', group_name: '', primary_model: 'gpt-test',
      primary_status: 'operational', primary_latency_ms: 100, primary_ping_latency_ms: null,
      availability_7d: 100, extra_models: [], timeline: [],
      intelligence: { window_end: '2026-11-18T02:01:00Z', series: [{ model: 'gpt-test', effort: 'high', points: [
        { status: 'pass', checked_at: '2026-11-18T02:00:30Z' },
      ] }] },
      usage_metrics: { '7d': { cache_rate_pct: 72.5, first_token_ms: 1400 } },
    }
    list.mockResolvedValue({ items: [item] })
    analytics.mockRejectedValue(new Error('endpoint unavailable'))

    wrapper = mountView()
    await flushPromises()

    const card = wrapper.findComponent(MonitorCard)
    expect(card.get('[data-metric="cache-rate"]').text()).toContain('72.5')
    expect(card.get('[data-metric="first-token"]').text()).toContain('1.4')
    expect(card.find('[data-testid="monitor-intelligence"]').exists()).toBe(true)
  })

  it.each(['manual', 'automatic'] as const)('renders the server monitor order after a %s refresh', async (refreshMode) => {
    const monitor = (id: number, name: string, status: UserMonitorView['primary_status'], latency: number): UserMonitorView => ({
      id,
      name,
      provider: 'openai',
      group_name: '',
      primary_model: 'gpt-test',
      primary_status: status,
      primary_latency_ms: latency,
      primary_ping_latency_ms: null,
      availability_7d: 100,
      extra_models: [],
      timeline: [],
    })
    // Deliberately independent of ID, name, status, and latency order.
    const items = [
      monitor(40, 'Zulu gateway', 'degraded', 850),
      monitor(7, 'Alpha gateway', 'operational', 150),
      monitor(65, 'Omega gateway', 'failed', 400),
      monitor(22, 'Beta gateway', 'error', 1000),
    ]
    list
      .mockResolvedValueOnce({ items })
      .mockResolvedValueOnce({ items: [items[2], items[0], items[3], items[1]] })

    wrapper = mountView()
    await flushPromises()
    const renderedNames = () => wrapper.findAllComponents(MonitorCard).map(card => card.get('[data-testid="monitor-name"]').text())
    expect(renderedNames()).toEqual(['Zulu gateway', 'Alpha gateway', 'Omega gateway', 'Beta gateway'])

    if (refreshMode === 'manual') {
      await wrapper.get('.refresh').trigger('click')
    } else {
      await wrapper.get('.interval').trigger('click')
      await vi.advanceTimersByTimeAsync(120000)
    }
    await flushPromises()

    expect(list).toHaveBeenCalledTimes(2)
    expect(renderedNames()).toEqual(['Omega gateway', 'Zulu gateway', 'Beta gateway', 'Alpha gateway'])
  })

  it('keeps automatic refresh disabled after reopening the page', async () => {
    wrapper = mountView(); await flushPromises()
    await wrapper.get('.disable').trigger('click')
    wrapper.unmount()
    wrapper = mountView(); await flushPromises()
    const calls = list.mock.calls.length
    await vi.advanceTimersByTimeAsync(240000)
    expect(list).toHaveBeenCalledTimes(calls)
    expect(JSON.parse(localStorage.getItem('channel-status-auto-refresh')!).enabled).toBe(false)
    await wrapper.get('.refresh').trigger('click'); await flushPromises()
    expect(list).toHaveBeenCalledTimes(calls + 1)
  })

  it('starts automatic refresh for a first visit', async () => {
    wrapper = mountView(); await flushPromises()
    await vi.advanceTimersByTimeAsync(120000)
    expect(list.mock.calls.length).toBeGreaterThan(1)
  })

  it('refreshes at the selected interval on successive automatic refreshes', async () => {
    wrapper = mountView(); await flushPromises()
    await wrapper.get('.interval').trigger('click')
    await vi.advanceTimersByTimeAsync(119000)
    expect(list).toHaveBeenCalledTimes(1)
    await vi.advanceTimersByTimeAsync(1000)
    expect(list).toHaveBeenCalledTimes(2)
    await vi.advanceTimersByTimeAsync(119000)
    expect(list).toHaveBeenCalledTimes(2)
    await vi.advanceTimersByTimeAsync(1000)
    expect(list).toHaveBeenCalledTimes(3)
  })

  it('preserves the selected interval after a manual refresh', async () => {
    wrapper = mountView(); await flushPromises()
    await wrapper.get('.interval').trigger('click')
    await wrapper.get('.refresh').trigger('click'); await flushPromises()
    expect(list).toHaveBeenCalledTimes(2)
    await vi.advanceTimersByTimeAsync(119000)
    expect(list).toHaveBeenCalledTimes(2)
    await vi.advanceTimersByTimeAsync(1000)
    expect(list).toHaveBeenCalledTimes(3)
  })

  it('honors a persisted interval after the initial load', async () => {
    localStorage.setItem('channel-status-auto-refresh', JSON.stringify({ enabled: true, interval_seconds: 120 }))
    wrapper = mountView(); await flushPromises()
    await vi.advanceTimersByTimeAsync(119000)
    expect(list).toHaveBeenCalledTimes(1)
    await vi.advanceTimersByTimeAsync(1000)
    expect(list).toHaveBeenCalledTimes(2)
    await wrapper.get('.disable').trigger('click')
    await vi.advanceTimersByTimeAsync(120000)
    expect(list).toHaveBeenCalledTimes(2)
  })
})
