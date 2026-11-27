import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { enableAutoUnmount, flushPromises, shallowMount } from '@vue/test-utils'
import ChannelStatusV1View from '../ChannelStatusV1View.vue'

const mocks = vi.hoisted(() => ({ list: vi.fn(), analytics: vi.fn(), status: vi.fn() }))
vi.mock('@/api/channelMonitor', () => mocks)
vi.mock('@/stores/app', () => ({ useAppStore: () => ({ cachedPublicSettings: { channel_monitor_enabled: true }, showError: vi.fn() }) }))
vi.mock('vue-i18n', async () => ({
  ...await vi.importActual<typeof import('vue-i18n')>('vue-i18n'),
  useI18n: () => ({ t: (key: string) => key })
}))
enableAutoUnmount(afterEach)
afterEach(() => { vi.useRealTimers(); localStorage.clear() })

beforeEach(() => {
  vi.useFakeTimers()
  vi.resetAllMocks()
  localStorage.clear()
  localStorage.setItem('channel-status-auto-refresh', JSON.stringify({ enabled: true, interval_seconds: 30 }))
  mocks.analytics.mockResolvedValue({ items: [] })
  mocks.list.mockImplementation(async () => ({ items: [{
    id: 1, name: 'Monitor', provider: 'openai', primary_model: 'model', primary_status: 'operational', availability_7d: 99,
    timeline: [{ status: 'operational', checked_at: '2026-11-26T00:00:00Z', latency_ms: null, ping_latency_ms: null }],
  }] }))
})

function mountView() {
  return shallowMount(ChannelStatusV1View, {
    global: { stubs: {
      AppLayout: { template: '<div><slot /></div>' },
      MonitorHero: { props: ['loading'], emits: ['refresh'], template: `<div :data-loading="loading">
        <button class="refresh" @click="$emit('refresh')">Refresh</button>
      </div>` },
      MonitorCardGrid: false,
      MonitorCard: { props: ['availabilityValue'], template: '<output>{{ availabilityValue }}</output>' }
    } }
  })
}

describe('channel monitor fixed recent metrics', () => {
  it('loads and refreshes without requesting date-specific availability details', async () => {
    const wrapper = mountView()
    await flushPromises()
    expect(mocks.list).toHaveBeenCalledTimes(1)
    expect(mocks.status).not.toHaveBeenCalled()
    expect(wrapper.get('output').text()).toBe('100')
    mocks.list.mockResolvedValueOnce({ items: [{
      id: 1, name: 'Monitor', provider: 'openai', primary_model: 'model', primary_status: 'failed', availability_7d: 99,
      timeline: [{ status: 'failed', checked_at: '2026-11-26T00:01:00Z', latency_ms: null, ping_latency_ms: null }],
    }] })
    await wrapper.get('.refresh').trigger('click')
    await flushPromises()
    expect(mocks.list).toHaveBeenCalledTimes(2)
    expect(mocks.status).not.toHaveBeenCalled()
    expect(wrapper.get('output').text()).toBe('0')
    expect(wrapper.get('[data-loading]').attributes('data-loading')).toBe('false')
  })

  it('continues to refresh through the list endpoint on timer ticks', async () => {
    const wrapper = mountView()
    await flushPromises()
    await vi.advanceTimersByTimeAsync(30000)
    await flushPromises()
    expect(mocks.list).toHaveBeenCalledTimes(2)
    expect(mocks.status).not.toHaveBeenCalled()
    expect(wrapper.get('output').text()).toBe('100')
  })
})
