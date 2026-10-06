import { flushPromises, shallowMount } from '@vue/test-utils'
import { describe, expect, it, vi } from 'vitest'
import type { UserMonitorView } from '@/api/channelMonitor'
import MonitorCard from '@/components/user/monitor/MonitorCard.vue'
import { usePublicPlatformHome } from '@/composables/usePublicPlatformHome'
import PublicHomeChannelStatus from '../PublicHomeChannelStatus.vue'

const { getMonitors } = vi.hoisted(() => ({ getMonitors: vi.fn() }))

vi.mock('@/api/publicHome', () => ({
  getPublicChannels: vi.fn().mockResolvedValue([]),
  getPublicPricing: vi.fn().mockResolvedValue({ groups: [] }),
  getPublicChannelMonitors: getMonitors,
}))
vi.mock('@/utils/featureFlags', () => ({ isChannelMonitorQuotaVisible: () => false }))
vi.mock('vue-i18n', async () => ({
  ...await vi.importActual<typeof import('vue-i18n')>('vue-i18n'),
  useI18n: () => ({ t: (key: string) => key }),
}))

describe('PublicHomeChannelStatus monitor order', () => {
  it('renders the public endpoint order and applies its new order on reload', async () => {
    const monitor = (id: number, name: string): UserMonitorView => ({
      id,
      name,
      provider: 'openai',
      group_name: '',
      primary_model: 'gpt-test',
      primary_status: 'operational',
      primary_latency_ms: null,
      primary_ping_latency_ms: null,
      availability_7d: 100,
      extra_models: [],
      timeline: [],
    })
    const items = [
      monitor(40, 'Zulu gateway'),
      monitor(7, 'Alpha gateway'),
      monitor(65, 'Omega gateway'),
    ]
    getMonitors
      .mockResolvedValueOnce({ items })
      .mockResolvedValueOnce({ items: [items[2], items[0], items[1]] })

    const home = usePublicPlatformHome()
    const wrapper = shallowMount(PublicHomeChannelStatus, {
      props: {
        monitors: () => home.monitors.value,
        status: () => home.channelStatus.value,
      },
      global: { stubs: { MonitorCard: false } },
    })
    const renderedNames = () => wrapper.findAllComponents(MonitorCard).map(card => card.get('.text-base').text())

    try {
      await home.load()
      await flushPromises()
      expect(renderedNames()).toEqual(['Zulu gateway', 'Alpha gateway', 'Omega gateway'])

      await home.load()
      await flushPromises()
      expect(getMonitors).toHaveBeenCalledTimes(2)
      expect(renderedNames()).toEqual(['Omega gateway', 'Zulu gateway', 'Alpha gateway'])
    } finally {
      wrapper.unmount()
      home.abort()
    }
  })
})
