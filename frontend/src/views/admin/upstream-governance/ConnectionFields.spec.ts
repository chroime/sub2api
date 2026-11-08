import { enableAutoUnmount, mount } from '@vue/test-utils'
import { reactive } from 'vue'
import { afterEach, describe, expect, it, vi } from 'vitest'
import ConnectionFields from './ConnectionFields.vue'
import { clearConnectionSecrets, continueChallenge, newConnectionForm } from './connection-form'
import Select from '@/components/common/Select.vue'

vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string) => key }) }))

enableAutoUnmount(afterEach)
afterEach(() => { document.body.innerHTML = ''; vi.restoreAllMocks() })

async function chooseAuthorizationMode(wrapper: ReturnType<typeof mount>, label: string) {
  await wrapper.get('#fixture-auth-mode').trigger('click')
  const option = [...document.querySelectorAll<HTMLElement>('.select-dropdown-portal [role="option"]')].find(item => item.textContent?.includes(label))
  expect(option).toBeDefined()
  option!.click()
  await wrapper.vm.$nextTick()
}

function sessionForm() {
  return reactive(Object.assign(newConnectionForm(), { mode: 'session' as const }))
}

const loginResponse = JSON.stringify({ code: 0, data: { access_token: 'fixture-session-access', refresh_token: 'fixture-session-refresh', expires_in: 3600, user_agent: 'Fixture Browser', user: { id: 999, role: 'admin', password: 'must-not-import' } } })

describe('provider-specific authorization fields', () => {
  it('shows authorization modes before expanding advanced fields', () => {
    const wrapper = mount(ConnectionFields, { props: { modelValue: newConnectionForm(), idPrefix: 'fixture' } })
    expect(wrapper.findComponent(Select).exists()).toBe(true)
    expect(wrapper.get('#fixture-auth-mode').attributes('aria-label')).toBe('governance.authMode')
    expect(wrapper.find('#fixture-username').exists()).toBe(true)
  })

  it('keeps Turnstile proof in advanced authorization and preserves plaintext input', async () => {
    const form = reactive(newConnectionForm())
    continueChallenge(form, { kind: 'captcha', provider: 'turnstile' })
    const wrapper = mount(ConnectionFields, { props: { modelValue: form, idPrefix: 'fixture', challenge: 'captcha' } })
    expect(wrapper.text()).toContain('governance.captchaProvider_turnstile')
    expect(wrapper.find('#fixture-captcha').exists()).toBe(false)
    await wrapper.get('#fixture-advanced-auth').trigger('click')
    expect(wrapper.text()).toContain('governance.turnstileToken')
    expect(wrapper.get('#fixture-captcha').attributes('type')).toBe('text')
  })

  it('renders the Tencent ticket and randstr fields instead of a generic token', async () => {
    const form = reactive(newConnectionForm())
    continueChallenge(form, { kind: 'captcha', provider: 'tencent' })
    const wrapper = mount(ConnectionFields, { props: { modelValue: form, idPrefix: 'fixture', challenge: 'captcha' } })
    expect(wrapper.find('#fixture-captcha').exists()).toBe(false)
    expect(wrapper.find('#fixture-tencent-ticket').exists()).toBe(false)
    await wrapper.get('#fixture-advanced-auth').trigger('click')
    await wrapper.get('#fixture-tencent-ticket').setValue('fixture-ticket')
    await wrapper.get('#fixture-tencent-randstr').setValue('fixture-random')
    expect(form.tencentTicket).toBe('fixture-ticket')
    expect(form.tencentRandstr).toBe('fixture-random')
  })

  it.each(['turnstile', 'tencent', 'aliyun', 'unknown'] as const)('offers an explicit session-import switch for %s without changing modes automatically', async provider => {
    const form = reactive(newConnectionForm())
    Object.assign(form, { username: 'fixture-user', password: 'fixture-password' })
    continueChallenge(form, { kind: 'captcha', provider })
    const wrapper = mount(ConnectionFields, { props: { modelValue: form, idPrefix: 'fixture', challenge: 'captcha' } })
    expect(form.mode).toBe('password')
    expect(form.password).toBe('fixture-password')
    expect(wrapper.text()).toContain(`governance.captchaProvider_${provider}`)
    expect(wrapper.get('#fixture-session-import-switch').attributes('type')).toBe('button')
    expect(wrapper.find('#fixture-session-import-json').exists()).toBe(false)
    await wrapper.get('#fixture-session-import-switch').trigger('click')
    expect(form.mode).toBe('session')
    expect(form.password).toBe('')
    expect(wrapper.find('#fixture-session-import-json').exists()).toBe(true)
    expect(wrapper.find('#fixture-tencent-ticket').exists()).toBe(false)
    expect(wrapper.find('iframe').exists()).toBe(false)
  })

  it('keeps TOTP continuation available without opening or changing authorization mode', async () => {
    const form = reactive(newConnectionForm())
    continueChallenge(form, { kind: 'totp', token: 'fixture-continuation' })
    const wrapper = mount(ConnectionFields, { props: { modelValue: form, idPrefix: 'fixture', challenge: 'totp' } })
    expect(wrapper.text()).toContain('governance.totp')
    expect(wrapper.find('#fixture-session-import-switch').exists()).toBe(false)
    await wrapper.get('#fixture-otp').setValue('123456')
    await wrapper.get('#fixture-advanced-auth').trigger('click')
    expect(form).toMatchObject({ mode: 'password', otp: '123456', challengeToken: 'fixture-continuation' })
  })

  it('keeps raw session metadata in advanced fields and clears proofs on mode changes', async () => {
    const form = reactive(newConnectionForm())
    Object.assign(form, { captchaToken: 'proof', tencentTicket: 'ticket', tencentRandstr: 'random', otp: '123456' })
    const wrapper = mount(ConnectionFields, { props: { modelValue: form, idPrefix: 'fixture' } })
    await chooseAuthorizationMode(wrapper, 'governance.sessionLogin')
    expect([form.captchaToken, form.tencentTicket, form.tencentRandstr, form.otp]).toEqual(['', '', '', ''])
    expect(wrapper.find('#fixture-session').exists()).toBe(false)
    await wrapper.get('#fixture-advanced-auth').trigger('click')
    await wrapper.get('#fixture-session').setValue('manual-session')
    await wrapper.get('#fixture-refresh-token').setValue('fixture-refresh')
    await wrapper.get('#fixture-expires-in').setValue(3600)
    await wrapper.get('#fixture-user-agent').setValue('fixture-browser')
    expect(form).toMatchObject({ sessionToken: 'manual-session', refreshToken: 'fixture-refresh', expiresIn: 3600, userAgent: 'fixture-browser' })
    expect(wrapper.get('#fixture-refresh-token').attributes('required')).toBeUndefined()
    expect(wrapper.get('#fixture-expires-in').attributes('required')).toBeUndefined()
    await chooseAuthorizationMode(wrapper, 'governance.passwordLogin')
    expect(form.refreshToken).toBe('')
    expect(form.expiresIn).toBeUndefined()
  })

  it('disables the teleported authorization menu with its enclosing form', async () => {
    const form = reactive(newConnectionForm())
    const wrapper = mount(ConnectionFields, { props: { modelValue: form, idPrefix: 'fixture', disabled: false } })
    await wrapper.get('#fixture-auth-mode').trigger('click')
    expect(document.querySelector('.select-dropdown-portal')).not.toBeNull()
    await wrapper.setProps({ disabled: true })
    expect(wrapper.get('#fixture-auth-mode').attributes('aria-expanded')).toBe('false')
    document.querySelectorAll<HTMLElement>('.select-dropdown-portal [role="option"]')[1]?.click()
    expect(form.mode).toBe('password')
    expect(wrapper.get('#fixture-auth-mode').attributes('disabled')).toBeDefined()
  })
})

describe('one-time session response import', () => {
  it('shows three steps and an explicit upstream link with query and fragment removed', () => {
    const wrapper = mount(ConnectionFields, { props: { modelValue: sessionForm(), idPrefix: 'fixture', siteUrl: 'https://upstream.example/dashboard?access_token=never-link#secret', platform: 'sub2api' } })
    expect(wrapper.findAll('ol li')).toHaveLength(3)
    expect(wrapper.get('#fixture-session-import-site').attributes('href')).toBe('https://upstream.example/dashboard')
    expect(wrapper.get('#fixture-session-import-site').attributes('target')).toBe('_blank')
    expect(wrapper.get('#fixture-session-import-site').attributes('rel')).toContain('noopener')
    expect(wrapper.find('#fixture-session').exists()).toBe(false)
  })

  it.each(['javascript:alert(1)', 'https://user:password@upstream.example/'])('does not offer an unsafe upstream link for %s', siteUrl => {
    const wrapper = mount(ConnectionFields, { props: { modelValue: sessionForm(), idPrefix: 'fixture', siteUrl } })
    expect(wrapper.find('#fixture-session-import-site').exists()).toBe(false)
  })

  it('imports only session fields and atomically clears stale identity and metadata', async () => {
    vi.spyOn(Date, 'now').mockReturnValue(2_000_000_000_000)
    const form = sessionForm()
    const wrapper = mount(ConnectionFields, { props: { modelValue: form, idPrefix: 'fixture' } })
    Object.assign(form, { username: 'kept-user', refreshToken: 'old-refresh', userAgent: 'old-agent', userId: 77, expiresIn: 30, sessionExpiresAt: 2_000_000_030_000 })
    await wrapper.get('#fixture-session-import-json').setValue(loginResponse)
    await wrapper.get('#fixture-session-import-apply').trigger('click')
    expect(form).toMatchObject({ sessionToken: 'fixture-session-access', refreshToken: 'fixture-session-refresh', userAgent: 'Fixture Browser', expiresIn: 3600, sessionExpiresAt: 2_000_003_600_000, username: 'kept-user', password: '' })
    expect(form.userId).toBeUndefined()
    expect(wrapper.get('[role="status"]').text()).toBe('governance.sessionImport.ready')
    await wrapper.get('#fixture-session-import-json').setValue(JSON.stringify({ access_token: 'second-session-access' }))
    await wrapper.get('#fixture-session-import-apply').trigger('click')
    expect(form).toMatchObject({ sessionToken: 'second-session-access', refreshToken: '', userAgent: '' })
    expect(form.expiresIn).toBeUndefined()
    expect(form.sessionExpiresAt).toBeUndefined()
    expect(form.userId).toBeUndefined()
    expect(wrapper.text()).not.toContain('must-not-import')
  })

  it('invalidates a parsed session immediately when the response JSON is edited', async () => {
    const form = sessionForm()
    const wrapper = mount(ConnectionFields, { props: { modelValue: form, idPrefix: 'fixture' } })
    await wrapper.get('#fixture-session-import-json').setValue(loginResponse)
    await wrapper.get('#fixture-session-import-apply').trigger('click')
    expect(form.sessionToken).toBe('fixture-session-access')
    await wrapper.get('#fixture-session-import-json').setValue('{"access_token":"different-session"}')
    expect(form.sessionToken).toBe('')
    expect(form.refreshToken).toBe('')
    expect(form.expiresIn).toBeUndefined()
    expect(form.sessionExpiresAt).toBeUndefined()
    expect(form.userAgent).toBe('')
    expect(form.userId).toBeUndefined()
    expect(wrapper.find('[role="status"]').exists()).toBe(false)
  })

  it('reports a fixed error without echoing malformed JSON or retaining session values', async () => {
    const form = sessionForm()
    const wrapper = mount(ConnectionFields, { props: { modelValue: form, idPrefix: 'fixture' } })
    Object.assign(form, { sessionToken: 'stale-access', refreshToken: 'stale-refresh', userId: 77 })
    await wrapper.get('#fixture-session-import-json').setValue('secret-canary {not json')
    await wrapper.get('#fixture-session-import-apply').trigger('click')
    expect(wrapper.get('[role="alert"]').text()).toBe('governance.sessionImport.errors.invalid_json')
    expect(wrapper.get('[role="alert"]').text()).not.toContain('secret-canary')
    expect(form.sessionToken).toBe('')
    expect(form.refreshToken).toBe('')
    expect(form.userId).toBeUndefined()
    await wrapper.get('#fixture-session-import-json').setValue('')
    expect(wrapper.find('[role="alert"]').exists()).toBe(false)
  })

  it('clears both the pasted JSON and every parsed session field', async () => {
    const form = sessionForm()
    const wrapper = mount(ConnectionFields, { props: { modelValue: form, idPrefix: 'fixture' } })
    await wrapper.get('#fixture-session-import-json').setValue(loginResponse)
    await wrapper.get('#fixture-session-import-apply').trigger('click')
    await wrapper.get('#fixture-session-import-clear').trigger('click')
    expect((wrapper.get('#fixture-session-import-json').element as HTMLTextAreaElement).value).toBe('')
    expect(form).toMatchObject({ sessionToken: '', refreshToken: '', userAgent: '' })
    expect(form.expiresIn).toBeUndefined()
    expect(form.sessionExpiresAt).toBeUndefined()
    expect(form.userId).toBeUndefined()
    expect(wrapper.find('[role="status"]').exists()).toBe(false)
  })

  it('discards pasted contents and import status across mode changes and model replacement', async () => {
    const form = sessionForm()
    const wrapper = mount(ConnectionFields, { props: { modelValue: form, idPrefix: 'fixture' } })
    await wrapper.get('#fixture-session-import-json').setValue(loginResponse)
    await wrapper.get('#fixture-session-import-apply').trigger('click')
    await chooseAuthorizationMode(wrapper, 'governance.passwordLogin')
    await chooseAuthorizationMode(wrapper, 'governance.sessionLogin')
    expect((wrapper.get('#fixture-session-import-json').element as HTMLTextAreaElement).value).toBe('')
    expect(form.sessionToken).toBe('')
    expect(wrapper.find('[role="status"]').exists()).toBe(false)
    await wrapper.get('#fixture-session-import-json').setValue('unfinished-response')
    await wrapper.setProps({ modelValue: sessionForm() })
    expect((wrapper.get('#fixture-session-import-json').element as HTMLTextAreaElement).value).toBe('')
  })

  it('clears local pasted credentials when the parent clears its existing form after connecting', async () => {
    const form = sessionForm()
    const wrapper = mount(ConnectionFields, { props: { modelValue: form, idPrefix: 'fixture' } })
    await wrapper.get('#fixture-session-import-json').setValue(loginResponse)
    await wrapper.get('#fixture-session-import-apply').trigger('click')
    clearConnectionSecrets(form)
    await wrapper.vm.$nextTick()
    expect((wrapper.get('#fixture-session-import-json').element as HTMLTextAreaElement).value).toBe('')
    expect(wrapper.find('[role="status"]').exists()).toBe(false)
  })

  it('retains parsed fields when detection normalizes target props before submission', async () => {
    const form = sessionForm()
    const wrapper = mount(ConnectionFields, { props: { modelValue: form, idPrefix: 'fixture', siteUrl: 'https://upstream.example' } })
    await wrapper.get('#fixture-session-import-json').setValue(loginResponse)
    await wrapper.get('#fixture-session-import-apply').trigger('click')
    await wrapper.setProps({ siteUrl: 'https://upstream.example/', platform: 'sub2api' })
    expect(form.sessionToken).toBe('fixture-session-access')
    expect(form.refreshToken).toBe('fixture-session-refresh')
    expect(form.sessionExpiresAt).toEqual(expect.any(Number))
    expect((wrapper.get('#fixture-session-import-json').element as HTMLTextAreaElement).value).toBe('')
  })

  it.each([
    ['host', 'https://other.example/'],
    ['port', 'https://upstream.example:8443/'],
    ['base path', 'https://upstream.example/other/'],
  ])('clears imported session fields when the target %s changes', async (_part, siteUrl) => {
    const form = sessionForm()
    const wrapper = mount(ConnectionFields, { props: { modelValue: form, idPrefix: 'fixture', siteUrl: 'https://upstream.example/' } })
    await wrapper.get('#fixture-session-import-json').setValue(loginResponse)
    await wrapper.get('#fixture-session-import-apply').trigger('click')
    await wrapper.setProps({ siteUrl })
    expect(form).toMatchObject({ sessionToken: '', refreshToken: '', userAgent: '' })
    expect(form.expiresIn).toBeUndefined()
    expect(form.sessionExpiresAt).toBeUndefined()
    expect(wrapper.find('[role="status"]').exists()).toBe(false)
  })

  it('clears imported session fields when an established platform changes', async () => {
    const form = sessionForm()
    const wrapper = mount(ConnectionFields, { props: { modelValue: form, idPrefix: 'fixture', platform: 'sub2api' } })
    await wrapper.get('#fixture-session-import-json').setValue(loginResponse)
    await wrapper.get('#fixture-session-import-apply').trigger('click')
    await wrapper.setProps({ platform: 'newapi' })
    expect(form.sessionToken).toBe('')
    expect(form.refreshToken).toBe('')
    expect(form.sessionExpiresAt).toBeUndefined()
  })

  it('keeps the original deadline when the same response is applied again', async () => {
    const now = vi.spyOn(Date, 'now').mockReturnValue(2_000_000_000_000)
    const form = sessionForm()
    const wrapper = mount(ConnectionFields, { props: { modelValue: form, idPrefix: 'fixture' } })
    await wrapper.get('#fixture-session-import-json').setValue(loginResponse)
    await wrapper.get('#fixture-session-import-apply').trigger('click')
    now.mockReturnValue(2_000_000_060_000)
    await wrapper.get('#fixture-session-import-apply').trigger('click')
    expect(form.sessionExpiresAt).toBe(2_000_003_600_000)
    expect(wrapper.get('#fixture-session-import-apply').attributes('disabled')).toBeDefined()
  })

  it.each(['session', 'expires-in'])('clears imported absolute expiry when the raw %s is edited', async field => {
    const form = sessionForm()
    const wrapper = mount(ConnectionFields, { props: { modelValue: form, idPrefix: 'fixture' } })
    await wrapper.get('#fixture-session-import-json').setValue(loginResponse)
    await wrapper.get('#fixture-session-import-apply').trigger('click')
    expect(form.sessionExpiresAt).toEqual(expect.any(Number))
    await wrapper.get('#fixture-advanced-auth').trigger('click')
    await wrapper.get(`#fixture-${field}`).setValue(field === 'session' ? 'new-manual-access' : 7200)
    expect(form.sessionExpiresAt).toBeUndefined()
    expect((wrapper.get('#fixture-session-import-json').element as HTMLTextAreaElement).value).toBe('')
    expect(wrapper.find('[role="status"]').exists()).toBe(false)
  })

  it('disables import actions while the connection is busy', async () => {
    const form = sessionForm()
    const wrapper = mount(ConnectionFields, { props: { modelValue: form, idPrefix: 'fixture' } })
    await wrapper.get('#fixture-session-import-json').setValue(loginResponse)
    await wrapper.setProps({ disabled: true })
    expect(wrapper.get('#fixture-session-import-apply').attributes('disabled')).toBeDefined()
    await wrapper.get('#fixture-session-import-apply').trigger('click')
    expect(form.sessionToken).toBe('')
  })
})
