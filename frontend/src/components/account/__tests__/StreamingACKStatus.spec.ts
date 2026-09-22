import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { nextTick } from 'vue'
import StreamingACKStatus from '../StreamingACKStatus.vue'

const { getStreamingACKSettingsMock } = vi.hoisted(() => ({
  getStreamingACKSettingsMock: vi.fn(),
}))

vi.mock('@/api/admin/settings', () => ({
  getStreamingACKSettings: getStreamingACKSettingsMock,
}))

vi.mock('vue-i18n', () => ({
  useI18n: () => ({ t: (key: string) => key }),
}))

const mounted: ReturnType<typeof mount>[] = []

function mountStatus(props: { open?: boolean; enabled?: boolean; savedEnabled?: boolean | null } = {}) {
  const wrapper = mount(StreamingACKStatus, {
    props: { open: true, enabled: true, savedEnabled: true, ...props },
    global: { stubs: { Icon: true } },
  })
  mounted.push(wrapper)
  return wrapper
}

describe('StreamingACKStatus', () => {
  beforeEach(() => {
    getStreamingACKSettingsMock.mockReset().mockResolvedValue({ enabled: true })
  })

  afterEach(() => {
    for (const wrapper of mounted.splice(0)) wrapper.unmount()
    vi.restoreAllMocks()
  })

  it('waits for the modal to open and fetches again when it is reopened', async () => {
    const wrapper = mountStatus({ open: false })
    expect(getStreamingACKSettingsMock).not.toHaveBeenCalled()
    expect(wrapper.find('[role="status"]').exists()).toBe(false)

    await wrapper.setProps({ open: true })
    await flushPromises()
    expect(getStreamingACKSettingsMock).toHaveBeenCalledTimes(1)
    expect(wrapper.text()).toContain('syntheticFirstResponseStatusEnabled')

    await wrapper.setProps({ open: false })
    getStreamingACKSettingsMock.mockResolvedValue({ enabled: false })
    await wrapper.setProps({ open: true })
    await flushPromises()
    expect(getStreamingACKSettingsMock).toHaveBeenCalledTimes(2)
    expect(wrapper.text()).toContain('syntheticFirstResponseStatusGlobalOff')
  })

  it('does not show effective before the global state has loaded', async () => {
    let resolve!: (value: { enabled: boolean }) => void
    getStreamingACKSettingsMock.mockReturnValue(new Promise(done => { resolve = done }))
    const wrapper = mountStatus()
    expect(wrapper.text()).toContain('syntheticFirstResponseStatusLoading')
    expect(wrapper.text()).not.toContain('syntheticFirstResponseStatusEnabled')
    resolve({ enabled: true })
    await flushPromises()
    expect(wrapper.text()).toContain('syntheticFirstResponseStatusEnabled')
  })

  it.each([
    { globalEnabled: true, savedEnabled: false, enabled: false, state: 'AccountOff' },
    { globalEnabled: false, savedEnabled: false, enabled: false, state: 'AccountOff' },
    { globalEnabled: false, savedEnabled: true, enabled: true, state: 'GlobalOff' },
    { globalEnabled: true, savedEnabled: true, enabled: true, state: 'Enabled' },
    { globalEnabled: true, savedEnabled: false, enabled: true, state: 'Pending' },
    { globalEnabled: false, savedEnabled: false, enabled: true, state: 'Pending' },
    { globalEnabled: true, savedEnabled: true, enabled: false, state: 'Pending' },
    { globalEnabled: true, savedEnabled: null, enabled: true, state: 'Pending' },
  ])('reports $state for saved=$savedEnabled draft=$enabled global=$globalEnabled', async ({ globalEnabled, savedEnabled, enabled, state }) => {
    getStreamingACKSettingsMock.mockResolvedValue({ enabled: globalEnabled })
    const wrapper = mountStatus({ savedEnabled, enabled })
    await flushPromises()
    expect(wrapper.get('[role="status"]').text()).toContain(`syntheticFirstResponseStatus${state}`)
  })

  it('reports an unavailable state and can retry without pretending the global setting is off', async () => {
    getStreamingACKSettingsMock.mockRejectedValueOnce(new Error('network unavailable'))
    const wrapper = mountStatus()
    await flushPromises()
    expect(wrapper.text()).toContain('syntheticFirstResponseStatusUnavailable')
    expect(wrapper.text()).not.toContain('syntheticFirstResponseStatusGlobalOff')
    expect(wrapper.text()).not.toContain('syntheticFirstResponseStatusEnabled')

    await wrapper.get('[data-testid="streaming-ack-refresh"]').trigger('click')
    await flushPromises()
    expect(getStreamingACKSettingsMock).toHaveBeenCalledTimes(2)
    expect(wrapper.text()).toContain('syntheticFirstResponseStatusEnabled')
  })

  it('treats an invalid response as unavailable', async () => {
    getStreamingACKSettingsMock.mockResolvedValue({})
    const wrapper = mountStatus()
    await flushPromises()
    expect(wrapper.text()).toContain('syntheticFirstResponseStatusUnavailable')
  })

  it('ignores an older result from a closed modal', async () => {
    let resolve!: (value: { enabled: boolean }) => void
    getStreamingACKSettingsMock.mockReturnValueOnce(new Promise(done => { resolve = done }))
    const wrapper = mountStatus()
    await wrapper.setProps({ open: false })
    getStreamingACKSettingsMock.mockResolvedValueOnce({ enabled: false })
    await wrapper.setProps({ open: true })
    await flushPromises()
    resolve({ enabled: true })
    await flushPromises()
    expect(wrapper.text()).toContain('syntheticFirstResponseStatusGlobalOff')
    expect(wrapper.text()).not.toContain('syntheticFirstResponseStatusEnabled')
  })

  it('refreshes on window focus and removes the stale global-off status while loading', async () => {
    getStreamingACKSettingsMock.mockResolvedValueOnce({ enabled: false })
    const wrapper = mountStatus()
    await flushPromises()
    expect(wrapper.text()).toContain('syntheticFirstResponseStatusGlobalOff')

    let resolve!: (value: { enabled: boolean }) => void
    getStreamingACKSettingsMock.mockReturnValueOnce(new Promise(done => { resolve = done }))
    window.dispatchEvent(new Event('focus'))
    await nextTick()
    expect(getStreamingACKSettingsMock).toHaveBeenCalledTimes(2)
    expect(wrapper.text()).toContain('syntheticFirstResponseStatusLoading')
    expect(wrapper.text()).not.toContain('syntheticFirstResponseStatusGlobalOff')

    resolve({ enabled: true })
    await flushPromises()
    expect(wrapper.text()).toContain('syntheticFirstResponseStatusEnabled')
  })

  it('preserves the unsaved account draft when focus refreshes the global setting', async () => {
    getStreamingACKSettingsMock.mockResolvedValueOnce({ enabled: false })
    const wrapper = mountStatus({ enabled: true, savedEnabled: false })
    await flushPromises()
    window.dispatchEvent(new Event('focus'))
    await flushPromises()

    expect(getStreamingACKSettingsMock).toHaveBeenCalledTimes(2)
    expect(wrapper.props('enabled')).toBe(true)
    expect(wrapper.props('savedEnabled')).toBe(false)
    expect(wrapper.text()).toContain('syntheticFirstResponseStatusPending')
    expect(wrapper.text()).not.toContain('syntheticFirstResponseStatusEnabled')
    expect(wrapper.emitted('update:enabled')).toBeUndefined()
  })

  it('ignores focus while loading or while the modal is closed', async () => {
    let resolve!: (value: { enabled: boolean }) => void
    getStreamingACKSettingsMock.mockReturnValueOnce(new Promise(done => { resolve = done }))
    const wrapper = mountStatus()
    window.dispatchEvent(new Event('focus'))
    window.dispatchEvent(new Event('focus'))
    expect(getStreamingACKSettingsMock).toHaveBeenCalledTimes(1)
    resolve({ enabled: true })
    await flushPromises()

    await wrapper.setProps({ open: false })
    window.dispatchEvent(new Event('focus'))
    await flushPromises()
    expect(getStreamingACKSettingsMock).toHaveBeenCalledTimes(1)
  })

  it('removes the focus listener when unmounted', async () => {
    const addListener = vi.spyOn(window, 'addEventListener')
    const removeListener = vi.spyOn(window, 'removeEventListener')
    const wrapper = mountStatus()
    await flushPromises()
    const focusListener = addListener.mock.calls.find(([event]) => event === 'focus')?.[1]
    expect(focusListener).toBeTypeOf('function')

    wrapper.unmount()
    mounted.splice(mounted.indexOf(wrapper), 1)
    expect(removeListener).toHaveBeenCalledWith('focus', focusListener)
    window.dispatchEvent(new Event('focus'))
    await flushPromises()
    expect(getStreamingACKSettingsMock).toHaveBeenCalledTimes(1)
  })
})
