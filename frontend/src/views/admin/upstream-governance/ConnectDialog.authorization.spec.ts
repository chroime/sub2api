import { enableAutoUnmount, flushPromises, mount } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import ConnectDialog from './ConnectDialog.vue'
import api, { type AuthorizationStatus } from '@/api/admin/upstream-governance'
import { formatGovernanceTime } from './format'

vi.mock('@/api/admin/upstream-governance', () => ({ default: { connect: vi.fn(), loginCredentials: vi.fn(), authStatus: vi.fn(), startBrowserAuth: vi.fn() } }))
vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string) => key }) }))
enableAutoUnmount(afterEach)
const status = (): AuthorizationStatus => ({
  session: { site_id: 7, site_version: 9, platform: 'sub2api', has_session: true, refresh_supported: true, has_refresh_token: true, auto_refresh_enabled: true, expires_at: '2026-09-28T04:00:00Z', issued_at: null, refresh_state: 'ready', last_refresh_at: null, reauthorization_required: false },
  browser: { available: true, reason: '' },
})
const setup = () => mount(ConnectDialog, {
  props: { siteId: 7, siteVersion: 9 },
  global: { stubs: {
    BaseDialog: { template: '<div><slot /></div>' },
    BrowserAuthorizationDialog: { name: 'BrowserAuthorizationDialog', props: ['siteId', 'siteVersion', 'username', 'password'], template: '<div data-test="browser-dialog"><button data-test="complete-browser" @click="$emit(\'connected\')">Complete</button><button data-test="stale-browser" @click="$emit(\'invalidated\')">Stale</button></div>' },
  } },
})
beforeEach(() => {
  vi.resetAllMocks()
  vi.mocked(api.loginCredentials).mockResolvedValue({ username: 'saved-user', password: 'saved-password', version: 9 })
  vi.mocked(api.authStatus).mockResolvedValue(status())
  vi.mocked(api.connect).mockResolvedValue({})
})

describe('authorization status and explicit browser entry', () => {
  it.each([
    ['ready', 'sessionAutoReauthorizationReady'],
    ['collection_disabled', 'sessionAutoReauthorizationCollectionDisabled'],
    ['missing_session', 'sessionAutoReauthorizationMissingSession'],
    ['missing_credentials', 'sessionAutoReauthorizationMissingCredentials'],
    ['identity_mismatch', 'sessionAutoReauthorizationIdentityMismatch'],
    ['verification_required', 'sessionAutoReauthorizationVerificationRequired'],
    ['credentials_rejected', 'sessionAutoReauthorizationCredentialsRejected'],
    ['retry_wait', 'sessionAutoReauthorizationRetryWait'],
  ])('explains auto reauthorization state %s separately from token refresh', async (state, label) => {
    const value = status()
    value.session.auto_reauthorization_enabled = state === 'ready' || state === 'retry_wait'
    value.session.auto_reauthorization_state = state
    vi.mocked(api.authStatus).mockResolvedValue(value)
    const wrapper = setup()
    await flushPromises()
    expect(wrapper.text()).toContain('governance.sessionAutoReauthorization')
    expect(wrapper.text()).toContain(`governance.${label}`)
    expect(wrapper.text()).toContain('governance.sessionRefreshEnabled')
  })

  it('shows the next automatic collection attempt for an expired session with automatic reauthorization enabled', async () => {
    const value = status()
    value.session.reauthorization_required = true
    value.session.auto_reauthorization_enabled = true
    value.session.auto_reauthorization_state = 'retry_wait'
    value.session.last_auto_reauthorization_at = '2026-09-28T06:07:08Z'
    vi.mocked(api.authStatus).mockResolvedValue(value)
    const wrapper = setup()
    await flushPromises()
    expect(wrapper.text()).toContain('governance.sessionReauthorizationScheduled')
    expect(wrapper.text()).not.toContain('governance.sessionReauthorizationRequired')
    expect(wrapper.text()).toContain(formatGovernanceTime(value.session.last_auto_reauthorization_at))
  })

  it('keeps manual reauthorization guidance when automatic reauthorization is disabled', async () => {
    const value = status()
    value.session.reauthorization_required = true
    value.session.auto_reauthorization_enabled = false
    value.session.auto_reauthorization_state = 'collection_disabled'
    vi.mocked(api.authStatus).mockResolvedValue(value)
    const wrapper = setup()
    await flushPromises()
    expect(wrapper.text()).toContain('governance.sessionReauthorizationRequired')
    expect(wrapper.text()).not.toContain('governance.sessionReauthorizationScheduled')
  })

  it('does not infer automatic reauthorization from a legacy authorization response', async () => {
    const value = status()
    value.session.reauthorization_required = true
    vi.mocked(api.authStatus).mockResolvedValue(value)
    const wrapper = setup()
    await flushPromises()
    expect(wrapper.text()).toContain('governance.sessionAutoReauthorizationUnknown')
    expect(wrapper.text()).toContain('governance.sessionReauthorizationRequired')
  })

  it('does not present an incomplete refresh pair as scheduled renewal', async () => {
    const value = status()
    value.session.expires_at = null
    value.session.auto_refresh_enabled = false
    vi.mocked(api.authStatus).mockResolvedValue(value)
    const wrapper = setup()
    await flushPromises()
    expect(wrapper.text()).toContain('governance.sessionRefreshTimeUnknown')
    expect(wrapper.text()).not.toContain('governance.sessionRefreshPaused')
  })
  it('displays actual expiry and ready renewal capability without logging in', async () => {
    const wrapper = setup()
    await flushPromises()
    expect(api.authStatus).toHaveBeenCalledWith(7)
    expect(wrapper.text()).toContain(formatGovernanceTime(status().session.expires_at))
    expect(wrapper.text()).toContain('governance.sessionRefreshEnabled')
    expect(api.connect).not.toHaveBeenCalled()
    expect(api.startBrowserAuth).not.toHaveBeenCalled()
    expect(wrapper.find('[data-test=browser-dialog]').exists()).toBe(false)
  })

  it.each(['identity_pending', 'pending', 'unexpected'])('does not claim renewal is ready for refresh state %s', async refreshState => {
    const value = status(); value.session.refresh_state = refreshState
    vi.mocked(api.authStatus).mockResolvedValue(value)
    const wrapper = setup()
    await flushPromises()
    expect(wrapper.text()).not.toContain('governance.sessionRefreshEnabled')
    expect(wrapper.text()).toContain(refreshState === 'identity_pending' ? 'governance.sessionIdentityPending' : refreshState === 'pending' ? 'governance.sessionRefreshUncertain' : 'governance.sessionRefreshUnknown')
  })

  it('keeps manual auth usable when the optional capability request fails', async () => {
    vi.mocked(api.authStatus).mockRejectedValue({ status: 503 })
    const wrapper = setup()
    await flushPromises()
    expect(wrapper.get('#governance-connect-submit').attributes('disabled')).toBeUndefined()
    expect(wrapper.get('#governance-connect-browser').attributes('disabled')).toBeDefined()
    expect(wrapper.text()).toContain('governance.authorizationStatusUnavailable')
    await wrapper.get('form').trigger('submit')
    await flushPromises()
    expect(api.connect).toHaveBeenCalledTimes(1)
  })

  it('shows unavailable helper explicitly and does not open a browser job', async () => {
    const value = status(); value.browser = { available: false, reason: 'not_configured' }
    vi.mocked(api.authStatus).mockResolvedValue(value)
    const wrapper = setup()
    await flushPromises()
    expect(wrapper.text()).toContain('governance.browserUnavailable')
    await wrapper.get('#governance-connect-browser').trigger('click')
    expect(wrapper.find('[data-test=browser-dialog]').exists()).toBe(false)
    expect(wrapper.get('#governance-connect-submit').attributes('disabled')).toBeUndefined()
  })

  it('passes current edited credentials and version only after an explicit browser click', async () => {
    const wrapper = setup()
    await flushPromises()
    await wrapper.get('#governance-connect-username').setValue('edited-user')
    await wrapper.get('#governance-connect-password').setValue('edited-password')
    await wrapper.get('#governance-connect-browser').trigger('click')
    const dialog = wrapper.getComponent({ name: 'BrowserAuthorizationDialog' })
    expect(dialog.props()).toMatchObject({ siteId: 7, siteVersion: 9, username: 'edited-user', password: 'edited-password' })
    expect(wrapper.emitted('connected')).toBeUndefined()
    await wrapper.get('[data-test=complete-browser]').trigger('click')
    expect(wrapper.emitted('connected')).toHaveLength(1)
    expect((wrapper.get('#governance-connect-password').element as HTMLInputElement).value).toBe('')
  })

  it('locks stale browser authorization and requires reloading credentials', async () => {
    const wrapper = setup()
    await flushPromises()
    await wrapper.get('#governance-connect-browser').trigger('click')
    await wrapper.get('[data-test=stale-browser]').trigger('click')
    expect(wrapper.find('[data-test=browser-dialog]').exists()).toBe(false)
    expect(wrapper.get('#governance-connect-submit').attributes('disabled')).toBeDefined()
    expect(wrapper.text()).toContain('governance.loginTargetChanged')
  })

  it('invalidates the open browser when the site configuration version changes', async () => {
    const wrapper = setup()
    await flushPromises()
    await wrapper.get('#governance-connect-browser').trigger('click')
    await wrapper.setProps({ siteVersion: 10 })
    await flushPromises()
    expect(wrapper.find('[data-test=browser-dialog]').exists()).toBe(false)
  })

  it('discards stale authorization status after switching sites', async () => {
    let finish!: (value: AuthorizationStatus) => void
    vi.mocked(api.authStatus).mockReturnValueOnce(new Promise(resolve => { finish = resolve }))
    const wrapper = setup()
    vi.mocked(api.authStatus).mockRejectedValue({ status: 503 })
    await wrapper.setProps({ siteId: 8 })
    await flushPromises()
    finish(status())
    await flushPromises()
    expect(wrapper.text()).not.toContain('governance.sessionRefreshEnabled')
    expect(wrapper.get('#governance-connect-browser').attributes('disabled')).toBeDefined()
  })
})
