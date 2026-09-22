import { flushPromises, shallowMount } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import type { UserMonitorView } from '@/api/channelMonitor'
import MonitorCard from '@/components/user/monitor/MonitorCard.vue'
import ChannelStatusV1View from '../ChannelStatusV1View.vue'

const { list } = vi.hoisted(() => ({ list: vi.fn() }))
vi.mock('@/api/channelMonitor', () => ({ list, status: vi.fn() }))
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
    MonitorHero: { props: ['autoRefresh'], emits: ['refresh'], template: `<div>
      <button class="interval" @click="autoRefresh.setInterval(120)">120 seconds</button>
      <button class="refresh" @click="$emit('refresh')">Refresh</button>
      <button class="disable" @click="autoRefresh.setEnabled(false)">Disable</button>
    </div>` },
  } },
})
let wrapper: ReturnType<typeof mountView>
beforeEach(() => { vi.useFakeTimers(); localStorage.clear(); list.mockReset().mockResolvedValue({ items: [] }) })
afterEach(() => { wrapper?.unmount(); vi.useRealTimers(); localStorage.clear() })

describe('channel monitor refresh interval', () => {
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
    const renderedNames = () => wrapper.findAllComponents(MonitorCard).map(card => card.get('.text-base').text())
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
