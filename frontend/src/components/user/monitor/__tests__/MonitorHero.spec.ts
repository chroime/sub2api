import { mount } from '@vue/test-utils'
import { describe, expect, it, vi } from 'vitest'
import MonitorHero from '../MonitorHero.vue'

vi.mock('vue-i18n', async () => ({
  ...await vi.importActual<typeof import('vue-i18n')>('vue-i18n'),
  useI18n: () => ({ t: (key: string) => key }),
}))

describe('MonitorHero', () => {
  it('keeps refresh controls without a date-window selector', () => {
    const wrapper = mount(MonitorHero, {
      props: {
        loading: false,
        autoRefresh: {
          enabled: { value: true },
          intervalSeconds: { value: 30 },
          countdown: { value: 10 },
          intervals: [30, 60],
          setEnabled: vi.fn(),
          setInterval: vi.fn(),
        },
      },
      global: { stubs: {
        Icon: true,
        AutoRefreshButton: { template: '<button data-testid="auto-refresh" />' },
      } },
    })

    expect(wrapper.find('[role="tablist"]').exists()).toBe(false)
    expect(wrapper.find('[title="common.refresh"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="auto-refresh"]').exists()).toBe(true)
  })
})
