import { mount, flushPromises, enableAutoUnmount } from '@vue/test-utils'
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import ConnectDialog from './ConnectDialog.vue'
import api from '@/api/admin/upstream-governance'
vi.mock('@/api/admin/upstream-governance', () => ({ default: { connect: vi.fn(), loginCredentials: vi.fn() } }))
vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string) => key }) }))

enableAutoUnmount(afterEach)
const savedLogin = { username: 'saved-user', password: '  saved-password  ', version: 7 }
const setup = () => mount(ConnectDialog, { props: { siteId: 7, siteVersion: 7 }, global: { stubs: { BaseDialog: { template: '<div><slot /><button data-test="close-dialog" @click="$emit(\'close\')">Close</button></div>' } } } })
const field = (wrapper: ReturnType<typeof setup>, name: string) => (wrapper.get(`#governance-connect-${name}`).element as HTMLInputElement).value
beforeEach(() => {
  vi.resetAllMocks()
  vi.mocked(api.loginCredentials).mockResolvedValue({ username: '', password: '', version: 7 })
})

describe('saved reauthorization credentials', () => {
  it('loads editable saved credentials without logging in, then submits with their site version', async () => {
    vi.mocked(api.loginCredentials).mockResolvedValue(savedLogin)
    vi.mocked(api.connect).mockResolvedValue({})
    const wrapper = setup()
    await flushPromises()
    expect(api.loginCredentials).toHaveBeenCalledWith(7)
    expect(field(wrapper, 'username')).toBe(savedLogin.username)
    expect(field(wrapper, 'password')).toBe(savedLogin.password)
    expect(wrapper.get('#governance-connect-password').attributes('type')).toBe('text')
    expect(api.connect).not.toHaveBeenCalled()
    await wrapper.get('#governance-connect-username').setValue(' changed-user ')
    await wrapper.get('#governance-connect-password').setValue('  changed-password  ')
    await wrapper.get('form').trigger('submit')
    await flushPromises()
    expect(api.connect).toHaveBeenCalledWith(7, expect.objectContaining({ username: 'changed-user', password: '  changed-password  ', expected_site_version: 7 }))
    expect(wrapper.emitted('connected')).toHaveLength(1)
    expect(field(wrapper, 'password')).toBe('')
  })

  it('disables the form and refuses even programmatic submit until loading finishes', async () => {
    let finish!: (value: typeof savedLogin) => void
    vi.mocked(api.loginCredentials).mockReturnValue(new Promise(resolve => { finish = resolve }))
    const wrapper = setup()
    expect(wrapper.get('fieldset').attributes('disabled')).toBeDefined()
    expect(wrapper.get('#governance-connect-submit').attributes('disabled')).toBeDefined()
    await wrapper.get('form').trigger('submit')
    expect(api.connect).not.toHaveBeenCalled()
    finish(savedLogin)
    await flushPromises()
    expect(wrapper.get('fieldset').attributes('disabled')).toBeUndefined()
  })

  it('leaves old sites without saved credentials ready for manual login', async () => {
    vi.mocked(api.connect).mockResolvedValue({})
    const wrapper = setup()
    await flushPromises()
    expect(field(wrapper, 'username')).toBe('')
    expect(field(wrapper, 'password')).toBe('')
    await wrapper.get('#governance-connect-username').setValue('manual-user')
    await wrapper.get('#governance-connect-password').setValue('manual-password')
    await wrapper.get('form').trigger('submit')
    await flushPromises()
    expect(api.connect).toHaveBeenCalledWith(7, expect.objectContaining({ username: 'manual-user', password: 'manual-password', expected_site_version: 7 }))
  })

  it('allows manual input after a read failure and provides an explicit retry', async () => {
    vi.mocked(api.loginCredentials).mockRejectedValueOnce({ status: 503 })
    const wrapper = setup()
    await flushPromises()
    expect(wrapper.text()).toContain('governance.loginLoadFailed')
    expect(wrapper.get('fieldset').attributes('disabled')).toBeUndefined()
    vi.mocked(api.loginCredentials).mockResolvedValue(savedLogin)
    await wrapper.get('#governance-connect-reload').trigger('click')
    await flushPromises()
    expect(field(wrapper, 'password')).toBe(savedLogin.password)
    expect(api.loginCredentials).toHaveBeenCalledTimes(2)
    expect(api.connect).not.toHaveBeenCalled()
  })

  it('ignores a pending credential response after the dialog closes', async () => {
    let finish!: (value: typeof savedLogin) => void
    vi.mocked(api.loginCredentials).mockReturnValue(new Promise(resolve => { finish = resolve }))
    const wrapper = setup()
    await wrapper.get('[data-test=close-dialog]').trigger('click')
    finish(savedLogin)
    await flushPromises()
    expect(wrapper.emitted('close')).toHaveLength(1)
    expect(field(wrapper, 'username')).toBe('')
    expect(field(wrapper, 'password')).toBe('')
  })

  it('keeps the displayed site version when falling back to manual login after a read failure', async () => {
    vi.mocked(api.loginCredentials).mockRejectedValue({ status: 503 })
    vi.mocked(api.connect).mockResolvedValue({})
    const wrapper = setup()
    await flushPromises()
    await wrapper.get('#governance-connect-username').setValue('manual-user')
    await wrapper.get('#governance-connect-password').setValue('manual-password')
    await wrapper.get('form').trigger('submit')
    await flushPromises()
    expect(api.connect).toHaveBeenCalledWith(7, expect.objectContaining({ expected_site_version: 7, username: 'manual-user', password: 'manual-password' }))
  })

  it('does not copy credentials from an earlier site into the newly selected site', async () => {
    let finish!: (value: typeof savedLogin) => void
    vi.mocked(api.loginCredentials).mockReturnValueOnce(new Promise(resolve => { finish = resolve }))
    const wrapper = setup()
    vi.mocked(api.loginCredentials).mockResolvedValue({ username: 'other-user', password: 'other-password', version: 9 })
    await wrapper.setProps({ siteId: 8 })
    await flushPromises()
    finish(savedLogin)
    await flushPromises()
    expect(api.loginCredentials).toHaveBeenLastCalledWith(8)
    expect(field(wrapper, 'username')).toBe('other-user')
    expect(field(wrapper, 'password')).toBe('other-password')
  })

  it('retains edited credentials after a login failure without silently restoring the saved password', async () => {
    vi.mocked(api.loginCredentials).mockResolvedValue(savedLogin)
    vi.mocked(api.connect).mockRejectedValue({ reason: 'reauth_required' })
    const wrapper = setup()
    await flushPromises()
    await wrapper.get('#governance-connect-password').setValue('corrected-password')
    await wrapper.get('form').trigger('submit')
    await flushPromises()
    expect(field(wrapper, 'password')).toBe('corrected-password')
    expect(wrapper.text()).toContain('governance.reauth')
    expect(api.loginCredentials).toHaveBeenCalledTimes(1)
  })

  it('clears stale credentials and requires reloading before another authorization attempt', async () => {
    vi.mocked(api.loginCredentials).mockResolvedValue(savedLogin)
    vi.mocked(api.connect).mockRejectedValue({ reason: 'stale_preview', status: 409 })
    const wrapper = setup()
    await flushPromises()
    await wrapper.get('form').trigger('submit')
    await flushPromises()
    expect(field(wrapper, 'password')).toBe('')
    expect(wrapper.text()).toContain('governance.loginTargetChanged')
    expect(wrapper.get('#governance-connect-submit').attributes('disabled')).toBeDefined()
    await wrapper.get('form').trigger('submit')
    expect(api.connect).toHaveBeenCalledTimes(1)
    vi.mocked(api.loginCredentials).mockRejectedValueOnce({ status: 503 })
    await wrapper.get('#governance-connect-reload').trigger('click')
    await flushPromises()
    expect(wrapper.get('#governance-connect-submit').attributes('disabled')).toBeDefined()
    vi.mocked(api.loginCredentials).mockResolvedValue({ username: 'new-owner', password: 'new-password', version: 8 })
    await wrapper.get('#governance-connect-reload').trigger('click')
    await flushPromises()
    expect(field(wrapper, 'password')).toBe('new-password')
    expect(wrapper.get('#governance-connect-submit').attributes('disabled')).toBeUndefined()
    expect(api.connect).toHaveBeenCalledTimes(1)
  })

  it('ignores an old login result after switching sites', async () => {
    let finish!: (value: Record<string, never>) => void
    vi.mocked(api.connect).mockReturnValue(new Promise(resolve => { finish = resolve }))
    const wrapper = setup()
    await flushPromises()
    await wrapper.get('form').trigger('submit')
    await wrapper.setProps({ siteId: 8 })
    await flushPromises()
    finish({})
    await flushPromises()
    expect(wrapper.emitted('connected')).toBeUndefined()
  })
})
