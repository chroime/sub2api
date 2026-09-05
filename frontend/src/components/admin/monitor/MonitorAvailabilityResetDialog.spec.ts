import { defineComponent } from 'vue'
import { mount } from '@vue/test-utils'
import { describe, expect, it, vi } from 'vitest'

import type { ChannelMonitor } from '@/api/admin/channelMonitor'
import MonitorAvailabilityResetDialog from '@/components/admin/monitor/MonitorAvailabilityResetDialog.vue'

vi.mock('vue-i18n', () => ({
  useI18n: () => ({ t: (key: string) => key }),
}))

const BaseDialogStub = defineComponent({
  props: { show: { type: Boolean, default: false } },
  template: '<div v-if="show"><slot /></div>',
})

function makeMonitor(overrides: Partial<ChannelMonitor> = {}): ChannelMonitor {
  return {
    id: 42,
    name: 'primary',
    provider: 'openai',
    api_mode: 'chat_completions',
    endpoint: 'https://api.example.com',
    api_key_masked: 'sk-t***',
    primary_model: 'gpt-4o-mini',
    extra_models: [],
    group_name: '',
    enabled: true,
    interval_seconds: 60,
    jitter_seconds: 0,
    last_checked_at: null,
    created_by: 1,
    created_at: '2026-07-16T00:00:00Z',
    updated_at: '2026-07-16T00:00:00Z',
    primary_status: '',
    primary_latency_ms: null,
    availability_7d: 73.25,
    availability_reset_active: false,
    extra_models_status: [],
    template_id: null,
    extra_headers: {},
    body_override_mode: 'off',
    body_override: null,
    check_mode: 'probe',
    account_id: null,
    ...overrides,
  }
}

function mountDialog() {
  return mount(MonitorAvailabilityResetDialog, {
    props: { show: true, monitor: makeMonitor(), submitting: false },
    global: { stubs: { BaseDialog: BaseDialogStub } },
  })
}

describe('MonitorAvailabilityResetDialog', () => {
  it('initializes from the current availability and offers 0 through 8 yellow bars', () => {
    const wrapper = mountDialog()

    expect((wrapper.get('#availability-reset-pct').element as HTMLInputElement).value).toBe('73.25')
    expect(wrapper.findAll('fieldset button')).toHaveLength(9)
    expect(wrapper.findAll('fieldset button')[0].attributes('aria-pressed')).toBe('true')
  })

  it('validates percentage precision and range', async () => {
    const wrapper = mountDialog()
    const input = wrapper.get('#availability-reset-pct')

    await input.setValue('12.345')
    await wrapper.get('form').trigger('submit')
    expect(wrapper.text()).toContain('admin.channelMonitor.availabilityReset.percentageError')
    expect(wrapper.emitted('submit')).toBeUndefined()

    await input.setValue('100')
    await wrapper.get('form').trigger('submit')
    expect(wrapper.emitted('submit')).toEqual([[{ availability_pct: 100, degraded_bars: 0, degraded_bar_layout: 'even' }]])
  })

  it('emits the selected yellow bar count', async () => {
    const wrapper = mountDialog()
    await wrapper.findAll('fieldset button')[8].trigger('click')
    await wrapper.get('#availability-reset-pct').setValue('0')
    await wrapper.get('form').trigger('submit')

    expect(wrapper.emitted('submit')).toEqual([[{ availability_pct: 0, degraded_bars: 8, degraded_bar_layout: 'even' }]])
  })

  it('offers four layouts and submits the selected layout', async () => {
    const wrapper = mountDialog()
    const select = wrapper.get('#availability-reset-layout')
    expect(select.findAll('option')).toHaveLength(4)
    expect((select.element as HTMLSelectElement).value).toBe('even')

    await select.setValue('block_newest')
    await wrapper.get('form').trigger('submit')
    expect(wrapper.emitted('submit')).toEqual([[
      { availability_pct: 73.25, degraded_bars: 0, degraded_bar_layout: 'block_newest' },
    ]])
  })
})
