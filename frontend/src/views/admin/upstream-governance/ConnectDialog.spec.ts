import { mount, flushPromises, enableAutoUnmount } from '@vue/test-utils'
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import ConnectDialog from './ConnectDialog.vue'
import ConnectionFields from './ConnectionFields.vue'
import type { ConnectionForm } from './connection-form'
import api, { type AuthorizationStatus } from '@/api/admin/upstream-governance'
vi.mock('@/api/admin/upstream-governance', () => ({
  default: { connect: vi.fn(), loginCredentials: vi.fn(), authStatus: vi.fn() },
}))
vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string) => key }) }))
enableAutoUnmount(afterEach)
afterEach(() => { document.body.innerHTML = '' })
beforeEach(() => {
  vi.resetAllMocks()
  vi.mocked(api.loginCredentials).mockResolvedValue({ username: '', password: '', version: 7 })
})
async function setSession(wrapper: ReturnType<typeof mount>, values: Partial<ConnectionForm> = {}) {
  const fields = wrapper.getComponent(ConnectionFields)
  fields.vm.$emit('update:modelValue', { ...fields.props('modelValue'), mode: 'session' })
  await flushPromises()
  fields.vm.$emit('update:modelValue', { ...fields.props('modelValue'), sessionToken: 'fixture-access', refreshToken: 'fixture-refresh', expiresIn: 3600, ...values })
  await flushPromises()
}

describe('connection challenge', () => {
  it.each([1, 2, 3, 4].flatMap(siteId => (['status_first', 'credentials_first'] as const).map(order => ({ siteId, order }))))('renders one field group in the real dialog for site $siteId with $order', async ({ siteId, order }) => {
    let finishLogin!: (value: Awaited<ReturnType<typeof api.loginCredentials>>) => void
    let finishStatus!: (value: AuthorizationStatus) => void
    vi.mocked(api.loginCredentials).mockReturnValueOnce(new Promise(resolve => { finishLogin = resolve }))
    vi.mocked(api.authStatus).mockReturnValueOnce(new Promise(resolve => { finishStatus = resolve }))
    const wrapper = mount(ConnectDialog, {
      attachTo: document.body,
      props: { siteId, siteUrl: 'https://fixture.example/', platform: 'sub2api' },
      global: { stubs: { BaseDialog: false, ConnectionFields: false, Teleport: false, Transition: false } },
    })
    const credentials = { username: 'fixture-user', password: 'fixture-password', version: 9 }
    const status: AuthorizationStatus = {
      session: { site_id: siteId, site_version: 9, platform: 'sub2api', has_session: true, refresh_supported: true, has_refresh_token: true, auto_refresh_enabled: true, expires_at: null, issued_at: null, refresh_state: 'ready', last_refresh_at: null, reauthorization_required: false },
      browser: { available: false, reason: 'not_configured' },
    }
    await flushPromises()
    expect(document.body.querySelectorAll('fieldset')).toHaveLength(1)
    if (order === 'status_first') finishStatus(status)
    else finishLogin(credentials)
    await flushPromises()
    if (order === 'status_first') finishLogin(credentials)
    else finishStatus(status)
    await flushPromises()
    // A stale DOM fieldset can remain while Vue still reports one component.
    expect(document.body.querySelectorAll('#governance-connect-auth-mode')).toHaveLength(1)
    expect(document.body.querySelectorAll('fieldset')).toHaveLength(1)
    expect(wrapper.findAllComponents(ConnectionFields)).toHaveLength(1)
    const submit = document.body.querySelector('#governance-connect-submit')!
    const fields = document.body.querySelector('fieldset')!
    expect(fields.compareDocumentPosition(submit) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy()
  })
  it.each(['turnstile', 'tencent', 'aliyun', 'unknown'] as const)('retains %s challenge until an explicit session import', async provider => {
    vi.mocked(api.connect).mockResolvedValueOnce({ challenge: { kind: 'captcha', provider } }).mockResolvedValueOnce({})
    const wrapper = mount(ConnectDialog, { props: { siteId: 7, siteUrl: 'https://fixture.example', platform: 'sub2api' }, global: { stubs: { BaseDialog: { template: '<div><slot /></div>' } } } })
    await flushPromises()
    await wrapper.get('#governance-connect-username').setValue('fixture-user')
    await wrapper.get('#governance-connect-password').setValue('fixture-password')
    await wrapper.get('form').trigger('submit')
    await flushPromises()
    const fields = wrapper.getComponent(ConnectionFields)
    expect(fields.props()).toMatchObject({ siteUrl: 'https://fixture.example', platform: 'sub2api', challenge: 'captcha' })
    expect(fields.props('modelValue')).toMatchObject({ mode: 'password', password: 'fixture-password' })
    expect(wrapper.find('#governance-connect-browser').exists()).toBe(false)
    expect(wrapper.emitted('connected')).toBeUndefined()
    await setSession(wrapper)
    expect(wrapper.get('#governance-connect-submit').text()).toBe('governance.importSession')
    expect(api.connect).toHaveBeenCalledTimes(1)
    await wrapper.get('form').trigger('submit')
    await flushPromises()
    expect(api.connect).toHaveBeenLastCalledWith(7, { session_token: 'fixture-access', refresh_token: 'fixture-refresh', expires_in: 3600, user_id: undefined, expected_site_version: 7 })
    expect(wrapper.emitted('connected')).toHaveLength(1)
    expect(fields.props('modelValue')).toMatchObject({ sessionToken: '', refreshToken: '', password: '' })
  })
  it('retains a rejected import for correction and retries with the loaded version', async () => {
    vi.mocked(api.connect).mockRejectedValueOnce({ reason: 'reauth_required' }).mockResolvedValueOnce({})
    const wrapper = mount(ConnectDialog, { props: { siteId: 7 }, global: { stubs: { BaseDialog: { template: '<div><slot /></div>' } } } })
    await flushPromises()
    await setSession(wrapper)
    await wrapper.get('form').trigger('submit')
    await flushPromises()
    expect(wrapper.getComponent(ConnectionFields).props('modelValue')).toMatchObject({ sessionToken: 'fixture-access', refreshToken: 'fixture-refresh', expiresIn: 3600 })
    await setSession(wrapper, { sessionToken: 'corrected-access' })
    await wrapper.get('form').trigger('submit')
    await flushPromises()
    expect(api.connect).toHaveBeenLastCalledWith(7, expect.objectContaining({ session_token: 'corrected-access', expected_site_version: 7 }))
    expect(wrapper.emitted('connected')).toHaveLength(1)
  })
  it('rejects an expired imported session locally and releases the form for correction', async () => {
    const wrapper = mount(ConnectDialog, { props: { siteId: 7 }, global: { stubs: { BaseDialog: { template: '<div><slot /></div>' } } } })
    await flushPromises()
    await setSession(wrapper, { sessionExpiresAt: Date.now() - 1000 })
    await wrapper.get('form').trigger('submit')
    await flushPromises()
    expect(api.connect).not.toHaveBeenCalled()
    expect(wrapper.get('[role=alert]').text()).toBe('governance.sessionImport.errors.expired')
    expect(wrapper.getComponent(ConnectionFields).props('modelValue')).toMatchObject({ sessionToken: 'fixture-access', refreshToken: 'fixture-refresh' })
    expect(wrapper.get('#governance-connect-submit').attributes('disabled')).toBeUndefined()
    expect(wrapper.emitted('connected')).toBeUndefined()
  })
  it('does not reuse an earlier site import when its request completes after a site switch', async () => {
    let finish!: (value: Awaited<ReturnType<typeof api.connect>>) => void
    vi.mocked(api.connect).mockReturnValueOnce(new Promise(resolve => { finish = resolve }))
    const wrapper = mount(ConnectDialog, { props: { siteId: 7, siteVersion: 7 }, global: { stubs: { BaseDialog: { template: '<div><slot /></div>' } } } })
    await flushPromises()
    await setSession(wrapper)
    await wrapper.get('form').trigger('submit')
    expect(wrapper.get('#governance-connect-submit').attributes('disabled')).toBeDefined()
    await wrapper.setProps({ siteId: 8, siteVersion: 9 })
    await flushPromises()
    finish({})
    await flushPromises()
    expect(wrapper.emitted('connected')).toBeUndefined()
    expect(wrapper.getComponent(ConnectionFields).props('modelValue')).toMatchObject({ mode: 'password', sessionToken: '', refreshToken: '' })
    expect(wrapper.get('#governance-connect-submit').attributes('disabled')).toBeUndefined()
  })
  it('submits a parsed login response only after confirmation and clears its raw JSON on success', async () => {
    vi.mocked(api.connect).mockResolvedValueOnce({})
    const wrapper = mount(ConnectDialog, { props: { siteId: 7 }, global: { stubs: { BaseDialog: { template: '<div><slot /></div>' } } } })
    await flushPromises()
    const fields = wrapper.getComponent(ConnectionFields)
    fields.vm.$emit('update:modelValue', { ...fields.props('modelValue'), mode: 'session' })
    await flushPromises()
    const raw = wrapper.get('#governance-connect-session-import-json')
    await raw.setValue(JSON.stringify({ code: 0, data: { access_token: 'parsed-access', refresh_token: 'parsed-refresh', expires_in: 3600 } }))
    await wrapper.get('#governance-connect-session-import-apply').trigger('click')
    expect(api.connect).not.toHaveBeenCalled()
    await wrapper.get('form').trigger('submit')
    await flushPromises()
    expect(api.connect).toHaveBeenCalledWith(7, expect.objectContaining({ session_token: 'parsed-access', refresh_token: 'parsed-refresh', expected_site_version: 7 }))
    expect(wrapper.emitted('connected')).toHaveLength(1)
    expect((raw.element as HTMLTextAreaElement).value).toBe('')
    expect(fields.props('modelValue')).toMatchObject({ sessionToken: '', refreshToken: '' })
  })
  it('starts with only username and password, keeping continuation inputs hidden', () => {
    const wrapper = mount(ConnectDialog, {
      props: { siteId: 7 },
      global: { stubs: { BaseDialog: { template: '<div><slot /></div>' } } },
    })
    expect(wrapper.findAll('input')).toHaveLength(2)
    expect(wrapper.find('select').exists()).toBe(false)
    expect(wrapper.get('#governance-connect-password').attributes('type')).toBe('text')
  })
  it('clears transient password and continues opaque TOTP challenge without exposing session', async () => {
    vi.mocked(api.connect)
      .mockResolvedValueOnce({
        challenge: { kind: 'totp', token: 'fixture-challenge' },
      })
      .mockResolvedValueOnce({})
    const wrapper = mount(ConnectDialog, {
      props: { siteId: 7 },
      global: { stubs: { BaseDialog: { template: '<div><slot /></div>' } } },
    })
    await flushPromises()
    const inputs = wrapper.findAll('input')
    await inputs[0]!.setValue('fixture-user')
    await inputs[1]!.setValue('fixture-password')
    await wrapper.get('form').trigger('submit')
    await flushPromises()
    expect(api.connect).toHaveBeenCalledWith(
      7,
      expect.objectContaining({
        username: 'fixture-user',
        password: 'fixture-password',
      }),
    )
    expect((inputs[1]!.element as HTMLInputElement).value).toBe('')
    expect(wrapper.text()).toContain('governance.totp')
    expect(wrapper.text()).not.toContain('fixture-challenge')
    await wrapper.get('#governance-connect-otp').setValue('123456')
    await wrapper.get('form').trigger('submit')
    await flushPromises()
    expect(api.connect).toHaveBeenLastCalledWith(
      7,
      expect.objectContaining({
        otp: '123456',
        challenge_token: 'fixture-challenge',
      }),
    )
    expect(wrapper.emitted('connected')).toHaveLength(1)
  })
})
