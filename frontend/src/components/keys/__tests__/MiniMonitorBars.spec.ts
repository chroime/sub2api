import { defineComponent } from 'vue'
import { enableAutoUnmount, flushPromises, mount } from '@vue/test-utils'
import { afterEach, describe, expect, it, vi } from 'vitest'
import MiniMonitorBars from '../MiniMonitorBars.vue'
import { availabilityBars } from '@/utils/monitorBars'

vi.mock('vue-i18n', async () => ({
  ...await vi.importActual<typeof import('vue-i18n')>('vue-i18n'),
  useI18n: () => ({ t: (key: string) => key }),
}))
enableAutoUnmount(afterEach)
afterEach(() => { document.body.innerHTML = '' })

const column = {
  state: 'ready' as const,
  bars: availabilityBars([{ status: 'operational', latency_ms: 1234, ping_latency_ms: null, checked_at: '2026-11-14T09:00:00Z' }], 20),
  details: [{ model: 'gpt-6.1-sol', state: 'ready' as const, status: 'operational', latencyMs: 1234, checkedAt: '2026-11-14T09:00:00Z' }],
}

describe('miniature channel monitoring bars', () => {
  it('describes IQ history as actual checks rather than a minute grid', async () => {
    const wrapper = mount(MiniMonitorBars, { props: { column: { ...column, details: [{ ...column.details[0], effort: 'high' }] }, kind: 'intelligence', groupName: 'OpenAI' } })
    await wrapper.get('button').trigger('click')
    const tooltip = document.body.querySelector('[role=tooltip]')!
    expect(tooltip.textContent).toContain('keys.groupMonitoring.checkWindow')
    expect(tooltip.textContent).not.toContain('keys.groupMonitoring.minuteWindow')
    expect(tooltip.querySelector('time')?.getAttribute('datetime')).toBe(column.details[0].checkedAt)
    wrapper.unmount()
  })
  it('retains actual bar colors and neutral padding, exposing measured detail', async () => {
    const wrapper = mount(MiniMonitorBars, { props: { column, kind: 'availability', groupName: 'OpenAI' } })
    const bars = wrapper.findAll('[data-monitor-bar]')
    expect(bars).toHaveLength(20)
    expect(bars[0].attributes('data-status')).toBe('empty')
    expect(bars[19].attributes('data-status')).toBe('operational')
    await wrapper.get('button').trigger('click')
    await flushPromises()
    expect(document.body.querySelector('[role=tooltip]')?.textContent).toContain('gpt-6.1-sol')
    expect(document.body.querySelector('[role=tooltip]')?.textContent).toContain('1234')
    wrapper.unmount()
  })

  it('never lets tapping or keyboard detail activation select the parent group', async () => {
    const select = vi.fn()
    const host = defineComponent({ components: { MiniMonitorBars }, setup: () => ({ column, select }),
      template: '<div @click="select" @keydown.enter="select"><MiniMonitorBars :column="column" kind="availability" group-name="OpenAI" /></div>' })
    const wrapper = mount(host)
    await wrapper.get('button').trigger('click')
    await wrapper.get('button').trigger('keydown', { key: 'Enter' })
    expect(select).not.toHaveBeenCalled()
    wrapper.unmount()
  })

  it('closes details on Escape and scroll', async () => {
    const wrapper = mount(MiniMonitorBars, { props: { column, kind: 'availability', groupName: 'OpenAI' } })
    await wrapper.get('button').trigger('click')
    expect(document.body.querySelector('[role=tooltip]')).not.toBeNull()
    await wrapper.get('button').trigger('keydown', { key: 'Escape' })
    expect(document.body.querySelector('[role=tooltip]')).toBeNull()
    await wrapper.get('button').trigger('click')
    window.dispatchEvent(new Event('scroll'))
    await flushPromises()
    expect(document.body.querySelector('[role=tooltip]')).toBeNull()
    wrapper.unmount()
  })
})
