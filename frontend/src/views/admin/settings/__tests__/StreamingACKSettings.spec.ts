import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import StreamingACKSettings from '../StreamingACKSettings.vue'

const { getSettings, updateSettings } = vi.hoisted(() => ({
  getSettings: vi.fn(),
  updateSettings: vi.fn(),
}))

vi.mock('@/api/admin/settings', () => ({
  getStreamingACKSettings: getSettings,
  updateStreamingACKSettings: updateSettings,
}))
vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string) => key }) }))

describe('StreamingACKSettings', () => {
  beforeEach(() => {
    getSettings.mockReset().mockResolvedValue({ enabled: false })
    updateSettings.mockReset().mockImplementation(async (settings) => settings)
    window.history.replaceState(null, '', '/')
  })

  it('loads the effective master setting and disables changes while loading', async () => {
    let resolve!: (value: { enabled: boolean }) => void
    getSettings.mockReturnValue(new Promise((done) => { resolve = done }))
    const wrapper = mount(StreamingACKSettings)
    expect(wrapper.get('[role="switch"]').attributes('disabled')).toBeDefined()
    resolve({ enabled: true })
    await flushPromises()
    expect(wrapper.get('[role="switch"]').attributes('aria-checked')).toBe('true')
    expect(wrapper.get('[role="switch"]').attributes('disabled')).toBeUndefined()
    expect(updateSettings).not.toHaveBeenCalled()
    wrapper.unmount()
  })

  it('persists the new value before displaying it as enabled', async () => {
    let resolve!: (value: { enabled: boolean }) => void
    updateSettings.mockReturnValue(new Promise((done) => { resolve = done }))
    const wrapper = mount(StreamingACKSettings)
    await flushPromises()
    await wrapper.get('[role="switch"]').trigger('click')
    expect(updateSettings).toHaveBeenCalledWith({ enabled: true })
    expect(wrapper.get('[role="switch"]').attributes('aria-checked')).toBe('false')
    expect(wrapper.get('[role="switch"]').attributes('disabled')).toBeDefined()
    resolve({ enabled: true })
    await flushPromises()
    expect(wrapper.get('[role="switch"]').attributes('aria-checked')).toBe('true')
    expect(wrapper.text()).toContain('admin.settings.streamingACK.saved')
    wrapper.unmount()
  })

  it('saves an explicit false when the master switch is turned off', async () => {
    getSettings.mockResolvedValue({ enabled: true })
    const wrapper = mount(StreamingACKSettings)
    await flushPromises()
    await wrapper.get('[role="switch"]').trigger('click')
    await flushPromises()
    expect(updateSettings).toHaveBeenCalledWith({ enabled: false })
    expect(wrapper.get('[role="switch"]').attributes('aria-checked')).toBe('false')
    wrapper.unmount()
  })

  it('retains the confirmed state and reports a failed save', async () => {
    getSettings.mockResolvedValue({ enabled: true })
    updateSettings.mockRejectedValue(new Error('offline'))
    const wrapper = mount(StreamingACKSettings)
    await flushPromises()
    await wrapper.get('[role="switch"]').trigger('click')
    await flushPromises()
    expect(wrapper.get('[role="switch"]').attributes('aria-checked')).toBe('true')
    expect(wrapper.get('[role="alert"]').text()).toBe('admin.settings.streamingACK.saveFailed')
    expect(wrapper.text()).not.toContain('admin.settings.streamingACK.saved')
    expect(wrapper.get('[role="switch"]').attributes('disabled')).toBeDefined()
    getSettings.mockResolvedValue({ enabled: false })
    await wrapper.get('[data-testid="streaming-ack-retry"]').trigger('click')
    await flushPromises()
    expect(wrapper.get('[role="switch"]').attributes('aria-checked')).toBe('false')
    expect(wrapper.get('[role="switch"]').attributes('disabled')).toBeUndefined()
    wrapper.unmount()
  })

  it('does not treat an unavailable setting as disabled and supports retry', async () => {
    getSettings.mockRejectedValueOnce(new Error('offline'))
    const wrapper = mount(StreamingACKSettings)
    await flushPromises()
    expect(wrapper.get('[role="alert"]').text()).toBe('admin.settings.streamingACK.loadFailed')
    expect(wrapper.get('[role="switch"]').attributes('disabled')).toBeDefined()
    getSettings.mockResolvedValue({ enabled: true })
    await wrapper.get('[data-testid="streaming-ack-retry"]').trigger('click')
    await flushPromises()
    expect(wrapper.find('[role="alert"]').exists()).toBe(false)
    expect(wrapper.get('[role="switch"]').attributes('aria-checked')).toBe('true')
    wrapper.unmount()
  })
})
