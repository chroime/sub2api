import { enableAutoUnmount, flushPromises, mount } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import BrowserAuthorizationDialog from './BrowserAuthorizationDialog.vue'
import api, { type BrowserAuthJob } from '@/api/admin/upstream-governance'

vi.mock('@/api/admin/upstream-governance', () => ({ default: {
  startBrowserAuth: vi.fn(), browserAuth: vi.fn(), browserAuthAction: vi.fn(), completeBrowserAuth: vi.fn(), cancelBrowserAuth: vi.fn(),
} }))
vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string) => key }) }))
enableAutoUnmount(afterEach)

const makeJob = (status: BrowserAuthJob['status'] = 'waiting'): BrowserAuthJob => ({
  id: 'fixture-job', site_id: 7, status, expires_at: new Date(Date.now() + 600000).toISOString(),
  frame: { image: '/9j/Zml4dHVyZQ==', width: 1024, height: 720 },
})
const setup = (show = true) => mount(BrowserAuthorizationDialog, {
  props: { show, siteId: 7, siteVersion: 9, username: 'edited-user', password: ' edited-password ' },
  global: { stubs: { BaseDialog: { props: ['show'], template: '<div v-if="show"><slot /><slot name="footer" /><button data-test="close-dialog" @click="$emit(\'close\')">Close</button></div>' } } },
})
function frame(wrapper: ReturnType<typeof setup>) {
  const target = wrapper.get('[data-test=browser-frame]')
  vi.spyOn(target.element, 'getBoundingClientRect').mockReturnValue({ left: 10, top: 20, width: 512, height: 360, right: 522, bottom: 380, x: 10, y: 20, toJSON: () => ({}) })
  Object.assign(target.element, { setPointerCapture: vi.fn(), releasePointerCapture: vi.fn(), hasPointerCapture: () => true })
  return target
}

beforeEach(() => {
  vi.useFakeTimers()
  vi.resetAllMocks()
  vi.mocked(api.startBrowserAuth).mockResolvedValue(makeJob())
  vi.mocked(api.browserAuth).mockResolvedValue(makeJob())
  vi.mocked(api.browserAuthAction).mockResolvedValue({ accepted: true })
  vi.mocked(api.cancelBrowserAuth).mockResolvedValue({ cancelled: true })
  vi.mocked(api.completeBrowserAuth).mockResolvedValue({})
})
afterEach(() => vi.useRealTimers())

describe('browser authorization lifecycle', () => {
  it('starts with edited credentials only when visible and never submits Login automatically', async () => {
    const wrapper = setup(false)
    await flushPromises()
    expect(api.startBrowserAuth).not.toHaveBeenCalled()
    await wrapper.setProps({ show: true })
    await flushPromises()
    expect(api.startBrowserAuth).toHaveBeenCalledWith(7, { expected_site_version: 9, username: 'edited-user', password: ' edited-password ' })
    expect(api.browserAuthAction).not.toHaveBeenCalled()
    expect(api.completeBrowserAuth).not.toHaveBeenCalled()
    expect(wrapper.get('[data-test=browser-frame] img').attributes('src')).toBe('data:image/jpeg;base64,/9j/Zml4dHVyZQ==')
  })

  it('stops frame polling at ready and persists only on explicit completion', async () => {
    vi.mocked(api.browserAuth).mockResolvedValue(makeJob('ready'))
    const wrapper = setup()
    await flushPromises()
    await vi.advanceTimersByTimeAsync(1000)
    await flushPromises()
    expect(api.browserAuth).toHaveBeenCalledTimes(1)
    await vi.advanceTimersByTimeAsync(5000)
    expect(api.browserAuth).toHaveBeenCalledTimes(1)
    expect(wrapper.emitted('connected')).toBeUndefined()
    expect(api.completeBrowserAuth).not.toHaveBeenCalled()
    await wrapper.get('[data-test=browser-complete]').trigger('click')
    await flushPromises()
    expect(api.completeBrowserAuth).toHaveBeenCalledWith(7, 'fixture-job')
    expect(wrapper.emitted('connected')).toHaveLength(1)
    wrapper.unmount()
    await flushPromises()
    expect(api.cancelBrowserAuth).not.toHaveBeenCalled()
  })

  it('cancels once on close, discards an in-flight frame and removes polling timers', async () => {
    let finish!: (job: BrowserAuthJob) => void
    vi.mocked(api.browserAuth).mockReturnValue(new Promise(resolve => { finish = resolve }))
    const wrapper = setup()
    await flushPromises()
    await vi.advanceTimersByTimeAsync(1000)
    await wrapper.get('[data-test=close-dialog]').trigger('click')
    await flushPromises()
    expect(api.cancelBrowserAuth).toHaveBeenCalledWith(7, 'fixture-job')
    finish(makeJob('ready'))
    await flushPromises()
    expect(wrapper.emitted('close')).toHaveLength(1)
    expect(wrapper.emitted('connected')).toBeUndefined()
    expect(api.cancelBrowserAuth).toHaveBeenCalledWith(7, 'fixture-job')
    wrapper.unmount()
    await vi.advanceTimersByTimeAsync(10000)
    expect(api.cancelBrowserAuth).toHaveBeenCalledTimes(1)
    expect(api.browserAuth).toHaveBeenCalledTimes(1)
  })

  it('cancels a late-created job after unmount rather than reviving it', async () => {
    let finish!: (job: BrowserAuthJob) => void
    vi.mocked(api.startBrowserAuth).mockReturnValue(new Promise(resolve => { finish = resolve }))
    const wrapper = setup()
    await flushPromises()
    wrapper.unmount()
    finish(makeJob())
    await flushPromises()
    expect(api.cancelBrowserAuth).toHaveBeenCalledWith(7, 'fixture-job')
    await vi.advanceTimersByTimeAsync(10000)
    expect(api.browserAuth).not.toHaveBeenCalled()
  })

  it.each([{ siteId: 8 }, { siteVersion: 10 }, { show: false }])('cancels on target/visibility change without starting a replacement: %j', async props => {
    const wrapper = setup()
    await flushPromises()
    await wrapper.setProps(props)
    await flushPromises()
    expect(api.cancelBrowserAuth).toHaveBeenCalledWith(7, 'fixture-job')
    expect(api.startBrowserAuth).toHaveBeenCalledTimes(1)
    expect(wrapper.emitted('connected')).toBeUndefined()
  })

  it.each(['failed', 'cancelled', 'expired'] as const)('stops polling when the helper reports %s', async status => {
    vi.mocked(api.browserAuth).mockResolvedValue(makeJob(status))
    const wrapper = setup()
    await flushPromises()
    await vi.advanceTimersByTimeAsync(1000)
    await flushPromises()
    await vi.advanceTimersByTimeAsync(5000)
    expect(api.browserAuth).toHaveBeenCalledTimes(1)
    expect(wrapper.get('[data-test=browser-complete]').attributes('disabled')).toBeDefined()
    expect(wrapper.text()).toContain(`governance.browserState_${status}`)
    expect(wrapper.text()).toContain('governance.browserRestartHint')
    expect(api.startBrowserAuth).toHaveBeenCalledTimes(1)
    expect(api.browserAuthAction).not.toHaveBeenCalled()
    expect(api.completeBrowserAuth).not.toHaveBeenCalled()
  })

  it('shows the sanitized helper error when a browser job fails', async () => {
    vi.mocked(api.browserAuth).mockResolvedValue({ ...makeJob('failed'), error_code: 'browser_navigation_failed' })
    const wrapper = setup()
    await flushPromises()
    await vi.advanceTimersByTimeAsync(1000)
    await flushPromises()
    expect(wrapper.text()).toContain('governance.browserErrorNavigationFailed')
  })

  it.each([
    ['browser_launch_failed', 'browserErrorLaunchFailed'],
    ['browser_dependency_missing', 'browserErrorDependencyMissing'],
    ['browser_unavailable', 'browserErrorUnavailable'],
    ['browser_unsupported_route', 'browserErrorUnsupportedRoute'],
    ['browser_node_missing', 'browserErrorNodeMissing'],
    ['browser_script_missing', 'browserErrorScriptMissing'],
    ['browser_executable_missing', 'browserErrorExecutableMissing'],
  ])('maps a failed helper job to actionable diagnostics: %s', async (error_code, label) => {
    vi.mocked(api.startBrowserAuth).mockResolvedValue({ ...makeJob('failed'), frame: undefined, error_code })
    const wrapper = setup()
    await flushPromises()
    expect(wrapper.get('[role=alert]').text()).toBe(`governance.${label}`)
    expect(wrapper.text()).not.toContain(error_code)
    expect(wrapper.text()).toContain('governance.browserRestartHint')
    await vi.advanceTimersByTimeAsync(5000)
    expect(api.startBrowserAuth).toHaveBeenCalledTimes(1)
    expect(api.browserAuth).not.toHaveBeenCalled()
  })

  it.each([
    ['browser_launch_failed', 'browserErrorLaunchFailed'],
    ['browser_dependency_missing', 'browserErrorDependencyMissing'],
    ['browser_unavailable', 'browserErrorUnavailable'],
    ['browser_unsupported_route', 'browserErrorUnsupportedRoute'],
    ['browser_node_missing', 'browserErrorNodeMissing'],
    ['browser_script_missing', 'browserErrorScriptMissing'],
    ['browser_executable_missing', 'browserErrorExecutableMissing'],
  ])('maps rejected startup reasons without exposing server messages: %s', async (reason, label) => {
    vi.mocked(api.startBrowserAuth).mockRejectedValue({ reason, message: 'private launch command /srv/helper secret=fixture' })
    const wrapper = setup()
    await flushPromises()
    expect(wrapper.get('[role=alert]').text()).toBe(`governance.${label}`)
    expect(wrapper.text()).not.toContain('private launch command')
    expect(wrapper.text()).not.toContain(reason)
    expect(wrapper.text()).toContain('governance.browserState_failed')
    expect(wrapper.text()).toContain('governance.browserRestartHint')
    await vi.advanceTimersByTimeAsync(5000)
    expect(api.startBrowserAuth).toHaveBeenCalledTimes(1)
    expect(api.browserAuth).not.toHaveBeenCalled()
  })

  it.each(['untrusted secret=fixture <script>bad</script>', 'constructor', 'toString', '__proto__'])('uses only the allowlisted helper diagnostic labels: %s', async error_code => {
    vi.mocked(api.startBrowserAuth).mockResolvedValue({ ...makeJob('failed'), frame: undefined, error_code })
    const wrapper = setup()
    await flushPromises()
    expect(wrapper.get('[role=alert]').text()).toBe('governance.browserErrorGeneric')
    expect(wrapper.find('script').exists()).toBe(false)
    expect(wrapper.text()).not.toContain(error_code)
  })

  it('shows safe generic text for an unrecognized startup error instead of its message or reason', async () => {
    vi.mocked(api.startBrowserAuth).mockRejectedValue({ reason: 'constructor', message: 'private command and credential fixture' })
    const wrapper = setup()
    await flushPromises()
    expect(wrapper.get('[role=alert]').text()).toBe('governance.browserAuthFailed')
    expect(wrapper.text()).not.toContain('constructor')
    expect(wrapper.text()).not.toContain('private command')
  })

  it('explains startup and missing frames while keeping manual instructions and inputs safe', async () => {
    let finish!: (job: BrowserAuthJob) => void
    vi.mocked(api.startBrowserAuth).mockReturnValue(new Promise(resolve => { finish = resolve }))
    const wrapper = setup()
    await flushPromises()
    expect(wrapper.get('[data-test=browser-frame]').text()).toContain('governance.browserFrameStarting')
    const instructions = wrapper.get('[data-test=browser-instructions]')
    expect(instructions.text()).toContain('governance.browserManualVerification')
    expect(instructions.text()).toContain('governance.browserManualLogin')
    expect(instructions.text()).toContain('governance.browserManualComplete')
    expect(wrapper.get('[data-test=browser-complete]').attributes('disabled')).toBeDefined()

    finish({ ...makeJob(), frame: undefined })
    await flushPromises()
    const target = frame(wrapper)
    expect(target.text()).toContain('governance.browserFrameWaiting')
    expect(target.attributes('aria-disabled')).toBe('true')
    expect(wrapper.get('[data-test=browser-text]').attributes('disabled')).toBeDefined()
    await target.trigger('pointerdown', { pointerId: 1, button: 0, clientX: 100, clientY: 100 })
    await target.trigger('keydown', { key: 'Enter' })
    await flushPromises()
    expect(api.browserAuthAction).not.toHaveBeenCalled()
    expect(api.startBrowserAuth).toHaveBeenCalledTimes(1)
    expect(api.completeBrowserAuth).not.toHaveBeenCalled()
  })

  it('recovers a transient render error by polling the same job without replaying input or leaving stale diagnostics', async () => {
    vi.mocked(api.browserAuth)
      .mockResolvedValueOnce({ ...makeJob(), frame: undefined, error_code: 'browser_render_failed' })
      .mockResolvedValueOnce(makeJob())
    const wrapper = setup()
    await flushPromises()
    await vi.advanceTimersByTimeAsync(1000)
    await flushPromises()
    expect(wrapper.get('[data-test=browser-diagnostic]').text()).toBe('governance.browserRenderRecovering')
    expect(wrapper.get('[data-test=browser-diagnostic]').attributes('role')).toBe('status')
    expect(wrapper.text()).toContain('governance.browserState_waiting')
    expect(wrapper.text()).not.toContain('governance.browserRestartHint')
    expect(wrapper.get('[data-test=browser-frame]').attributes('aria-disabled')).toBe('true')
    expect(wrapper.get('[data-test=browser-complete]').attributes('disabled')).toBeDefined()

    await vi.advanceTimersByTimeAsync(1000)
    await flushPromises()
    expect(wrapper.find('[data-test=browser-diagnostic]').exists()).toBe(false)
    expect(wrapper.find('[role=alert]').exists()).toBe(false)
    expect(wrapper.get('[data-test=browser-frame]').attributes('aria-disabled')).toBe('false')
    expect(wrapper.find('[data-test=browser-frame] img').exists()).toBe(true)
    expect(api.browserAuth).toHaveBeenCalledTimes(2)
    expect(api.startBrowserAuth).toHaveBeenCalledTimes(1)
    expect(api.browserAuthAction).not.toHaveBeenCalled()
    expect(api.completeBrowserAuth).not.toHaveBeenCalled()
  })

  it('pauses input to a stale frame until transient rendering recovers', async () => {
    vi.mocked(api.startBrowserAuth).mockResolvedValue({ ...makeJob(), error_code: 'browser_render_failed' })
    const wrapper = setup()
    await flushPromises()
    const target = frame(wrapper)
    expect(target.attributes('aria-disabled')).toBe('true')
    await target.trigger('pointerdown', { pointerId: 1, button: 0, clientX: 100, clientY: 100 })
    await target.trigger('keydown', { key: 'Enter' })
    await flushPromises()
    expect(api.browserAuthAction).not.toHaveBeenCalled()
    await vi.advanceTimersByTimeAsync(1000)
    await flushPromises()
    expect(target.attributes('aria-disabled')).toBe('false')
    expect(wrapper.find('[data-test=browser-diagnostic]').exists()).toBe(false)
  })

  it('replaces a transient render warning with the later terminal request diagnostic', async () => {
    vi.mocked(api.startBrowserAuth).mockResolvedValue({ ...makeJob(), frame: undefined, error_code: 'browser_render_failed' })
    vi.mocked(api.browserAuth).mockRejectedValue({ reason: 'browser_unavailable', message: 'private helper endpoint' })
    const wrapper = setup()
    await flushPromises()
    await vi.advanceTimersByTimeAsync(1000)
    await flushPromises()
    expect(wrapper.findAll('[role=alert]')).toHaveLength(1)
    expect(wrapper.get('[role=alert]').text()).toBe('governance.browserErrorUnavailable')
    expect(wrapper.text()).not.toContain('governance.browserRenderRecovering')
    expect(wrapper.text()).not.toContain('private helper endpoint')
    expect(wrapper.text()).toContain('governance.browserState_failed')
  })

  it('expires locally at the actual job deadline even while a ready job awaits confirmation', async () => {
    vi.mocked(api.startBrowserAuth).mockResolvedValue({ ...makeJob('ready'), expires_at: new Date(Date.now() + 2000).toISOString() })
    const wrapper = setup()
    await flushPromises()
    await vi.advanceTimersByTimeAsync(2001)
    await flushPromises()
    expect(wrapper.text()).toContain('governance.browserState_expired')
    expect(wrapper.get('[data-test=browser-complete]').attributes('disabled')).toBeDefined()
    expect(api.cancelBrowserAuth).toHaveBeenCalledWith(7, 'fixture-job')
  })

  it('does not revive an expired job from a late frame response', async () => {
    const initial = { ...makeJob(), expires_at: new Date(Date.now() + 1500).toISOString() }
    let finish!: (value: BrowserAuthJob) => void
    vi.mocked(api.startBrowserAuth).mockResolvedValue(initial)
    vi.mocked(api.browserAuth).mockReturnValue(new Promise(resolve => { finish = resolve }))
    const wrapper = setup()
    await flushPromises()
    await vi.advanceTimersByTimeAsync(1501)
    finish({ ...initial, status: 'ready' })
    await flushPromises()
    expect(wrapper.text()).toContain('governance.browserState_expired')
    expect(wrapper.get('[data-test=browser-complete]').attributes('disabled')).toBeDefined()
    expect(api.cancelBrowserAuth).toHaveBeenCalledWith(7, 'fixture-job')
  })

  it('shows a terminal failure when startup fails instead of remaining in starting state', async () => {
    vi.mocked(api.startBrowserAuth).mockRejectedValue({ reason: 'browser_unavailable' })
    const wrapper = setup()
    await flushPromises()
    expect(wrapper.text()).toContain('governance.browserState_failed')
    expect(wrapper.text()).not.toContain('governance.browserState_starting')
    await vi.advanceTimersByTimeAsync(5000)
    expect(api.browserAuth).not.toHaveBeenCalled()
  })

  it('ignores completion after the viewer is unmounted by navigation', async () => {
    let finish!: (value: Record<string, never>) => void
    vi.mocked(api.startBrowserAuth).mockResolvedValue(makeJob('ready'))
    vi.mocked(api.completeBrowserAuth).mockReturnValue(new Promise(resolve => { finish = resolve }))
    const wrapper = setup()
    await flushPromises()
    await wrapper.get('[data-test=browser-complete]').trigger('click')
    wrapper.unmount()
    finish({})
    await flushPromises()
    expect(wrapper.emitted('connected')).toBeUndefined()
    expect(api.cancelBrowserAuth).toHaveBeenCalledWith(7, 'fixture-job')
  })
  it('does not allow close while a completion request may persist the session', async () => {
    let finish!: (value: Record<string, never>) => void
    vi.mocked(api.startBrowserAuth).mockResolvedValue(makeJob('ready'))
    vi.mocked(api.completeBrowserAuth).mockReturnValue(new Promise(resolve => { finish=resolve }))
    const wrapper=setup()
    await flushPromises()
    await wrapper.get('[data-test=browser-complete]').trigger('click')
    expect(wrapper.get('[data-test=browser-cancel]').attributes('disabled')).toBeDefined()
    await wrapper.get('[data-test=close-dialog]').trigger('click')
    expect(wrapper.emitted('close')).toBeUndefined()
    expect(api.cancelBrowserAuth).not.toHaveBeenCalled()
    finish({})
    await flushPromises()
    expect(wrapper.emitted('connected')).toHaveLength(1)
  })

  it('retains ready state after a lost completion response so the idempotent result can be retrieved', async () => {
    vi.mocked(api.startBrowserAuth).mockResolvedValue(makeJob('ready'))
    vi.mocked(api.completeBrowserAuth).mockRejectedValueOnce({status:0,code:'ERR_NETWORK'}).mockResolvedValueOnce({})
    const wrapper=setup()
    await flushPromises()
    await wrapper.get('[data-test=browser-complete]').trigger('click')
    await flushPromises()
    expect(wrapper.text()).toContain('governance.browserState_ready')
    expect(wrapper.get('[data-test=browser-complete]').attributes('disabled')).toBeUndefined()
    await wrapper.get('[data-test=browser-complete]').trigger('click')
    await flushPromises()
    expect(api.completeBrowserAuth).toHaveBeenCalledTimes(2)
    expect(wrapper.emitted('connected')).toHaveLength(1)
  })

  it('keeps a captured session ready when completion encounters a busy site and permits explicit retry', async () => {
    vi.mocked(api.startBrowserAuth).mockResolvedValue(makeJob('ready'))
    vi.mocked(api.completeBrowserAuth).mockRejectedValueOnce({ reason: 'site_busy', status: 409 }).mockResolvedValueOnce({})
    const wrapper = setup()
    await flushPromises()
    await wrapper.get('[data-test=browser-complete]').trigger('click')
    await flushPromises()
    expect(wrapper.text()).toContain('governance.browserState_ready')
    expect(wrapper.text()).toContain('governance.siteBusy')
    expect(wrapper.get('[data-test=browser-complete]').attributes('disabled')).toBeUndefined()
    expect(wrapper.emitted('connected')).toBeUndefined()
    expect(api.cancelBrowserAuth).not.toHaveBeenCalled()
    await wrapper.get('[data-test=browser-complete]').trigger('click')
    await flushPromises()
    expect(api.completeBrowserAuth).toHaveBeenCalledTimes(2)
    expect(wrapper.emitted('connected')).toHaveLength(1)
  })

  it('rejects a mismatched site/job frame and never renders upstream markup', async () => {
    vi.mocked(api.browserAuth).mockResolvedValue({ ...makeJob(), id: 'wrong-job', frame: { width: 1024, height: 720, image: '<script>fixture</script>' } })
    const wrapper = setup()
    await flushPromises()
    await vi.advanceTimersByTimeAsync(1000)
    await flushPromises()
    expect(wrapper.text()).toContain('governance.browserState_failed')
    expect(wrapper.find('img').exists()).toBe(false)
    expect(wrapper.find('script').exists()).toBe(false)
  })
})

describe('bounded browser input', () => {
  it.each(['key', 'pointer_move'] as const)('reads one terminal job diagnostic after a rejected %s without replaying input', async type => {
    vi.mocked(api.browserAuthAction).mockRejectedValue({ reason: 'unsupported', message: 'private helper response' })
    vi.mocked(api.browserAuth).mockResolvedValue({ ...makeJob('failed'), frame: undefined, error_code: 'browser_unsupported_route' })
    const wrapper = setup()
    await flushPromises()
    const target = frame(wrapper)
    if (type === 'key') await target.trigger('keydown', { key: 'Enter' })
    else {
      await target.trigger('pointermove', { clientX: 100, clientY: 100, pointerId: 1 })
      await vi.advanceTimersByTimeAsync(50)
    }
    await flushPromises()
    expect(wrapper.get('[role=alert]').text()).toBe('governance.browserErrorUnsupportedRoute')
    expect(wrapper.text()).toContain('governance.browserState_failed')
    expect(wrapper.text()).not.toContain('private helper response')
    await vi.advanceTimersByTimeAsync(5000)
    expect(api.browserAuth).toHaveBeenCalledTimes(1)
    expect(api.browserAuth).toHaveBeenCalledWith(7, 'fixture-job')
    expect(api.browserAuthAction).toHaveBeenCalledTimes(1)
    expect(api.startBrowserAuth).toHaveBeenCalledTimes(1)
    expect(api.completeBrowserAuth).not.toHaveBeenCalled()
  })

  it.each(['expired', 'cancelled'] as const)('respects a %s diagnostic readback after a rejected action', async status => {
    vi.mocked(api.browserAuthAction).mockRejectedValue({ reason: 'unsupported' })
    vi.mocked(api.browserAuth).mockResolvedValue({ ...makeJob(status), frame: undefined })
    const wrapper = setup()
    await flushPromises()
    await frame(wrapper).trigger('keydown', { key: 'Enter' })
    await flushPromises()
    expect(wrapper.text()).toContain(`governance.browserState_${status}`)
    expect(wrapper.text()).toContain('governance.browserRestartHint')
    expect(wrapper.get('[data-test=browser-complete]').attributes('disabled')).toBeDefined()
    await vi.advanceTimersByTimeAsync(5000)
    expect(api.browserAuth).toHaveBeenCalledTimes(1)
    expect(api.browserAuthAction).toHaveBeenCalledTimes(1)
  })

  it.each(['readback_rejected', 'still_waiting', 'wrong_job', 'wrong_site'] as const)('falls back safely when action diagnostic readback is %s', async result => {
    vi.mocked(api.browserAuthAction).mockRejectedValue({ reason: 'unsupported', message: 'private action error' })
    if (result === 'readback_rejected') vi.mocked(api.browserAuth).mockRejectedValue({ reason: 'private reason', message: 'private readback error' })
    else vi.mocked(api.browserAuth).mockResolvedValue({
      ...makeJob(result === 'still_waiting' ? 'waiting' : 'failed'),
      id: result === 'wrong_job' ? 'different-job' : 'fixture-job',
      site_id: result === 'wrong_site' ? 8 : 7,
      error_code: 'browser_unsupported_route',
    })
    const wrapper = setup()
    await flushPromises()
    await frame(wrapper).trigger('keydown', { key: 'Enter' })
    await flushPromises()
    expect(wrapper.get('[role=alert]').text()).toBe('governance.browserAuthFailed')
    expect(wrapper.text()).toContain('governance.browserState_failed')
    expect(wrapper.text()).not.toContain('private')
    expect(wrapper.text()).not.toContain('governance.browserErrorUnsupportedRoute')
    await vi.advanceTimersByTimeAsync(5000)
    expect(api.browserAuth).toHaveBeenCalledTimes(1)
    expect(api.browserAuthAction).toHaveBeenCalledTimes(1)
  })

  it('ignores a terminal action readback arriving after the dialog is closed', async () => {
    let finish!: (job: BrowserAuthJob) => void
    vi.mocked(api.browserAuthAction).mockRejectedValue({ reason: 'unsupported' })
    vi.mocked(api.browserAuth).mockReturnValue(new Promise(resolve => { finish = resolve }))
    const wrapper = setup()
    await flushPromises()
    await frame(wrapper).trigger('keydown', { key: 'Enter' })
    await flushPromises()
    expect(api.browserAuth).toHaveBeenCalledTimes(1)
    await wrapper.get('[data-test=browser-cancel]').trigger('click')
    finish({ ...makeJob('failed'), error_code: 'browser_unsupported_route' })
    await flushPromises()
    expect(wrapper.find('[role=alert]').exists()).toBe(false)
    expect(wrapper.emitted('close')).toHaveLength(1)
    expect(wrapper.emitted('connected')).toBeUndefined()
    expect(api.cancelBrowserAuth).toHaveBeenCalledTimes(1)
    expect(api.browserAuthAction).toHaveBeenCalledTimes(1)
    await vi.advanceTimersByTimeAsync(5000)
    expect(api.browserAuth).toHaveBeenCalledTimes(1)
  })

  it('does not start diagnostic readback when a pending action fails after close', async () => {
    let reject!: (cause: unknown) => void
    vi.mocked(api.browserAuthAction).mockReturnValue(new Promise((_resolve, rejectAction) => { reject = rejectAction }))
    const wrapper = setup()
    await flushPromises()
    await frame(wrapper).trigger('keydown', { key: 'Enter' })
    await flushPromises()
    await wrapper.get('[data-test=browser-cancel]').trigger('click')
    reject({ reason: 'unsupported' })
    await flushPromises()
    expect(api.browserAuth).not.toHaveBeenCalled()
    expect(api.browserAuthAction).toHaveBeenCalledTimes(1)
    expect(api.cancelBrowserAuth).toHaveBeenCalledTimes(1)
  })

  it('keeps a stationary click aligned when focusing the viewport could scroll the dialog', async () => {
    const wrapper = setup()
    await flushPromises()
    const target = frame(wrapper)
    vi.spyOn(target.element as HTMLElement, 'focus').mockImplementation(options => {
      if (!options?.preventScroll) {
        vi.mocked(target.element.getBoundingClientRect).mockReturnValue({ left: 10, top: -80, width: 512, height: 360, right: 522, bottom: 280, x: 10, y: -80, toJSON: () => ({}) })
      }
    })
    await target.trigger('pointerdown', { clientX: 266, clientY: 200, pointerId: 1, button: 0 })
    await target.trigger('pointerup', { clientX: 266, clientY: 200, pointerId: 1 })
    await flushPromises()
    expect(vi.mocked(api.browserAuthAction).mock.calls.map(call => call[2])).toEqual([
      { type: 'pointer_down', x: 512, y: 360 },
      { type: 'pointer_up', x: 512, y: 360 },
    ])
  })

  it('does not click blank margins of the actual-size browser image', async () => {
    const wrapper=setup()
    await flushPromises()
    frame(wrapper)
    await wrapper.get('[data-test=browser-zoom-actual]').trigger('click')
    const image=wrapper.get('[data-test=browser-frame] img')
    vi.spyOn(image.element,'getBoundingClientRect').mockReturnValue({left:10,top:20,width:1024,height:720,right:1034,bottom:740,x:10,y:20,toJSON:()=>({})})
    await wrapper.get('[data-test=browser-frame]').trigger('pointerdown',{pointerId:8,button:0,clientX:1100,clientY:120})
    await flushPromises()
    expect(api.browserAuthAction).not.toHaveBeenCalled()
    await wrapper.get('[data-test=browser-frame]').trigger('pointerdown',{pointerId:9,button:0,clientX:100,clientY:120})
    await flushPromises()
    expect(api.browserAuthAction).toHaveBeenCalledWith(7,'fixture-job',expect.objectContaining({type:'pointer_down',x:90,y:100}))
  })
  it('supports actual-size panning on narrow screens without sending pan gestures upstream', async () => {
    const wrapper = setup()
    await flushPromises()
    const target=frame(wrapper)
    await wrapper.get('[data-test=browser-zoom-actual]').trigger('click')
    expect(target.classes()).toContain('overflow-auto')
    const image=wrapper.get('[data-test=browser-frame] img')
    expect(image.attributes('style')).toContain('width: 1024px')
    const imageRect=vi.spyOn(image.element,'getBoundingClientRect').mockReturnValue({left:10,top:20,width:1024,height:720,right:1034,bottom:740,x:10,y:20,toJSON:()=>({})})
    await wrapper.get('[data-test=browser-pan]').trigger('click')
    await target.trigger('pointermove',{pointerId:6,clientX:100,clientY:100})
    await vi.advanceTimersByTimeAsync(60)
    expect(api.browserAuthAction).not.toHaveBeenCalled()
    await target.trigger('pointerdown',{pointerId:6,button:0,clientX:100,clientY:100})
    await target.trigger('pointermove',{pointerId:6,clientX:60,clientY:80})
    await target.trigger('pointerup',{pointerId:6,button:0,clientX:60,clientY:80})
    await flushPromises()
    expect(api.browserAuthAction).not.toHaveBeenCalled()
    await wrapper.get('[data-test=browser-pan]').trigger('click')
    imageRect.mockReturnValue({left:-152,top:-20,width:1024,height:720,right:872,bottom:700,x:-152,y:-20,toJSON:()=>({})})
    await target.trigger('pointerdown',{pointerId:7,button:0,clientX:100,clientY:100})
    await flushPromises()
    expect(api.browserAuthAction).toHaveBeenCalledWith(7,'fixture-job',expect.objectContaining({type:'pointer_down',x:252,y:120}))
  })
  it('scales, bounds and serializes dragging with coalesced pointer moves', async () => {
    let finish!: (value: { accepted: boolean }) => void
    vi.mocked(api.browserAuthAction).mockReturnValueOnce(new Promise(resolve => { finish = resolve }))
    const wrapper = setup()
    await flushPromises()
    const target = frame(wrapper)
    await target.trigger('pointerdown', { clientX: 266, clientY: 200, pointerId: 1, button: 0 })
    await target.trigger('pointermove', { clientX: 300, clientY: 220, pointerId: 1 })
    await target.trigger('pointermove', { clientX: 600, clientY: -10, pointerId: 1 })
    await target.trigger('pointerup', { clientX: 600, clientY: -10, pointerId: 1 })
    await flushPromises()
    expect(api.browserAuthAction).toHaveBeenCalledTimes(1)
    expect(api.browserAuthAction).toHaveBeenNthCalledWith(1, 7, 'fixture-job', { type: 'pointer_down', x: 512, y: 360 })
    finish({ accepted: true })
    await flushPromises()
    expect(vi.mocked(api.browserAuthAction).mock.calls.map(call => call[2])).toEqual([
      { type: 'pointer_down', x: 512, y: 360 },
      { type: 'pointer_move', x: 1023, y: 0 },
      { type: 'pointer_up', x: 1023, y: 0 },
    ])
    expect((target.element as HTMLElement).setPointerCapture).toHaveBeenCalledWith(1)
  })

  it('releases an existing drag during a render interruption while rejecting new gestures', async () => {
    vi.mocked(api.browserAuth).mockResolvedValueOnce({ ...makeJob(), frame: undefined, error_code: 'browser_render_failed' })
    const wrapper = setup()
    await flushPromises()
    const target = frame(wrapper)
    await target.trigger('pointerdown', { clientX: 266, clientY: 200, pointerId: 1, button: 0 })
    await flushPromises()
    await vi.advanceTimersByTimeAsync(1000)
    await flushPromises()
    expect(target.attributes('aria-disabled')).toBe('true')
    await target.trigger('pointerup', { clientX: 300, clientY: 220, pointerId: 1 })
    await target.trigger('pointerdown', { clientX: 100, clientY: 100, pointerId: 2, button: 0 })
    await target.trigger('pointermove', { clientX: 110, clientY: 110, pointerId: 2 })
    await flushPromises()
    expect(vi.mocked(api.browserAuthAction).mock.calls.map(call => call[2])).toEqual([
      { type: 'pointer_down', x: 512, y: 360 },
      { type: 'pointer_up', x: 580, y: 400 },
    ])
    await vi.advanceTimersByTimeAsync(1000)
    await flushPromises()
    expect(target.attributes('aria-disabled')).toBe('false')
    expect(api.browserAuthAction).toHaveBeenCalledTimes(2)
  })

  it('discards pending hover moves during render recovery and resumes coalescing new moves', async () => {
    let finish!: (job: BrowserAuthJob) => void
    vi.mocked(api.browserAuth).mockReturnValueOnce(new Promise(resolve => { finish = resolve }))
    const wrapper = setup()
    await flushPromises()
    const target = frame(wrapper)
    await vi.advanceTimersByTimeAsync(1000)
    await target.trigger('pointermove', { clientX: 100, clientY: 100, pointerId: 1 })
    finish({ ...makeJob(), frame: undefined, error_code: 'browser_render_failed' })
    await flushPromises()
    await vi.advanceTimersByTimeAsync(1000)
    await flushPromises()
    expect(target.attributes('aria-disabled')).toBe('false')
    expect(api.browserAuthAction).not.toHaveBeenCalled()

    await target.trigger('pointermove', { clientX: 266, clientY: 200, pointerId: 1 })
    await vi.advanceTimersByTimeAsync(50)
    await flushPromises()
    expect(api.browserAuthAction).toHaveBeenCalledTimes(1)
    expect(api.browserAuthAction).toHaveBeenCalledWith(7, 'fixture-job', { type: 'pointer_move', x: 512, y: 360 })
  })

  it('bounds wheel events and sends explicit keys, printable text and pasted OTP', async () => {
    const wrapper = setup()
    await flushPromises()
    const target = frame(wrapper)
    target.element.dispatchEvent(new WheelEvent('wheel', { clientX: 20, clientY: 30, deltaX: -9000, deltaY: 9000, bubbles: true, cancelable: true }))
    await target.trigger('keydown', { key: 'Enter' })
    await target.trigger('keydown', { key: 'a' })
    await target.trigger('keydown', { key: 'F12' })
    await target.trigger('paste', { clipboardData: { getData: () => '123456' } })
    await flushPromises()
    expect(vi.mocked(api.browserAuthAction).mock.calls.map(call => call[2])).toEqual([
      { type: 'wheel', x: 20, y: 20, delta_x: -2000, delta_y: 2000 },
      { type: 'key', key: 'Enter' }, { type: 'text', text: 'a' }, { type: 'text', text: '123456' },
    ])
  })

  it('rejects oversized text and sends the explicit text input only once', async () => {
    const wrapper = setup()
    await flushPromises()
    await wrapper.get('[data-test=browser-text]').setValue('x'.repeat(2049))
    await wrapper.get('[data-test=browser-send-text]').trigger('click')
    await flushPromises()
    expect(api.browserAuthAction).not.toHaveBeenCalled()
    await wrapper.get('[data-test=browser-text]').setValue('fixture-otp')
    await wrapper.get('[data-test=browser-send-text]').trigger('click')
    await flushPromises()
    expect(api.browserAuthAction).toHaveBeenCalledWith(7, 'fixture-job', { type: 'text', text: 'fixture-otp' })
    expect((wrapper.get('[data-test=browser-text]').element as HTMLInputElement).value).toBe('')
  })

  it('validates UTF-8 bytes rather than code units and leaves the job usable after a bad paste', async () => {
    const wrapper = setup()
    await flushPromises()
    const target = frame(wrapper)
    await target.trigger('paste', { clipboardData: { getData: () => '\u4e2d'.repeat(683) } })
    await flushPromises()
    expect(api.browserAuthAction).not.toHaveBeenCalled()
    expect(wrapper.text()).toContain('governance.browserTextTooLong')
    expect(wrapper.text()).toContain('governance.browserState_waiting')
    await target.trigger('paste', { clipboardData: { getData: () => '\u4e2d'.repeat(682) } })
    await flushPromises()
    expect(api.browserAuthAction).toHaveBeenCalledWith(7, 'fixture-job', { type: 'text', text: '\u4e2d'.repeat(682) })
  })

  it.each(['\n', '\r', '\t', '\u0000', '\u001f', '\u007f'])('rejects control characters in pasted text without ending authorization: %j', async control => {
    const wrapper = setup()
    await flushPromises()
    const target = frame(wrapper)
    await target.trigger('paste', { clipboardData: { getData: () => `fixture${control}text` } })
    await flushPromises()
    expect(api.browserAuthAction).not.toHaveBeenCalled()
    expect(wrapper.text()).toContain('governance.browserTextInvalid')
    expect(wrapper.text()).toContain('governance.browserState_waiting')
    await target.trigger('paste', { clipboardData: { getData: () => '123456' } })
    await flushPromises()
    expect(api.browserAuthAction).toHaveBeenCalledWith(7, 'fixture-job', { type: 'text', text: '123456' })
  })

  it('waits for an active frame request before issuing queued input', async () => {
    let finish!: (job: BrowserAuthJob) => void
    vi.mocked(api.browserAuth).mockReturnValueOnce(new Promise(resolve => { finish = resolve }))
    const wrapper = setup()
    await flushPromises()
    await vi.advanceTimersByTimeAsync(1000)
    await frame(wrapper).trigger('keydown', { key: 'Tab' })
    await flushPromises()
    expect(api.browserAuthAction).not.toHaveBeenCalled()
    finish(makeJob())
    await flushPromises()
    expect(api.browserAuthAction).toHaveBeenCalledWith(7, 'fixture-job', { type: 'key', key: 'Tab' })
  })

  it('releases a captured pointer only once when capture loss follows pointer-up', async () => {
    const wrapper = setup()
    await flushPromises()
    const target = frame(wrapper)
    vi.mocked((target.element as HTMLElement).releasePointerCapture).mockImplementationOnce(() => {
      void target.trigger('lostpointercapture', { pointerId: 1, clientX: 266, clientY: 200 })
    })
    await target.trigger('pointerdown', { pointerId: 1, button: 0, clientX: 266, clientY: 200 })
    await target.trigger('pointerup', { pointerId: 1, clientX: 266, clientY: 200 })
    await flushPromises()
    expect(vi.mocked(api.browserAuthAction).mock.calls.filter(call => call[2].type === 'pointer_up')).toHaveLength(1)
  })
})
