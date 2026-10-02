import { enableAutoUnmount, mount } from '@vue/test-utils'
import { afterEach, describe, expect, it, vi } from 'vitest'
import ConnectionFields from './ConnectionFields.vue'
import { continueChallenge, newConnectionForm } from './connection-form'
import Select from '@/components/common/Select.vue'

vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string) => key }) }))
enableAutoUnmount(afterEach)
afterEach(() => { document.body.innerHTML = '' })

async function chooseAuthorizationMode(wrapper: ReturnType<typeof mount>, label: string) {
  await wrapper.get('#fixture-auth-mode').trigger('click')
  const option = [...document.querySelectorAll<HTMLElement>('.select-dropdown-portal [role="option"]')].find(item => item.textContent?.includes(label))
  expect(option).toBeDefined()
  option!.click()
}

describe('provider-specific authorization fields', () => {
  it('labels Turnstile proof and preserves plaintext input', () => {
    const form = newConnectionForm()
    continueChallenge(form, { kind: 'captcha', provider: 'turnstile' })
    const wrapper = mount(ConnectionFields, { props: { modelValue: form, idPrefix: 'fixture', challenge: 'captcha' } })
    expect(wrapper.text()).toContain('governance.captchaProvider_turnstile')
    expect(wrapper.text()).toContain('governance.turnstileToken')
    expect(wrapper.get('#fixture-captcha').attributes('type')).toBe('text')
  })

  it('renders the Tencent ticket and randstr fields instead of a generic token', async () => {
    const form = newConnectionForm()
    continueChallenge(form, { kind: 'captcha', provider: 'tencent' })
    const wrapper = mount(ConnectionFields, { props: { modelValue: form, idPrefix: 'fixture', challenge: 'captcha' } })
    expect(wrapper.find('#fixture-captcha').exists()).toBe(false)
    await wrapper.get('#fixture-tencent-ticket').setValue('fixture-ticket')
    await wrapper.get('#fixture-tencent-randstr').setValue('fixture-random')
    expect(form.tencentTicket).toBe('fixture-ticket')
    expect(form.tencentRandstr).toBe('fixture-random')
  })

  it.each(['aliyun', 'unknown'] as const)('offers session import without an unsupported %s token field', async provider => {
    const form = newConnectionForm()
    continueChallenge(form, { kind: 'captcha', provider })
    const wrapper = mount(ConnectionFields, { props: { modelValue: form, idPrefix: 'fixture', challenge: 'captcha' } })
    expect(wrapper.text()).toContain(`governance.captchaProvider_${provider}`)
    expect(wrapper.text()).toContain('governance.captchaBrowserRequired')
    expect(wrapper.find('#fixture-captcha').exists()).toBe(false)
    await wrapper.get('#fixture-advanced-auth').trigger('click')
    expect(wrapper.findComponent(Select).exists()).toBe(true)
    expect(wrapper.find('select').exists()).toBe(false)
    await chooseAuthorizationMode(wrapper, 'governance.sessionLogin')
    expect(wrapper.find('#fixture-session').exists()).toBe(true)
  })

  it('keeps session metadata optional and clears provider proofs on mode changes', async () => {
    const form = newConnectionForm()
    Object.assign(form, { captchaToken: 'proof', tencentTicket: 'ticket', tencentRandstr: 'random', otp: '123456' })
    const wrapper = mount(ConnectionFields, { props: { modelValue: form, idPrefix: 'fixture' } })
    await wrapper.get('#fixture-advanced-auth').trigger('click')
    await chooseAuthorizationMode(wrapper, 'governance.sessionLogin')
    expect([form.captchaToken, form.tencentTicket, form.tencentRandstr, form.otp]).toEqual(['', '', '', ''])
    await wrapper.get('#fixture-refresh-token').setValue('fixture-refresh')
    await wrapper.get('#fixture-expires-in').setValue(3600)
    await wrapper.get('#fixture-user-agent').setValue('fixture-browser')
    expect(form).toMatchObject({ refreshToken: 'fixture-refresh', expiresIn: 3600, userAgent: 'fixture-browser' })
    expect(wrapper.get('#fixture-refresh-token').attributes('required')).toBeUndefined()
    expect(wrapper.get('#fixture-expires-in').attributes('required')).toBeUndefined()
    await chooseAuthorizationMode(wrapper, 'governance.passwordLogin')
    expect(form.refreshToken).toBe('')
    expect(form.expiresIn).toBeUndefined()
  })

  it('disables the teleported authorization menu with its enclosing form', async () => {
    const form = newConnectionForm()
    const wrapper = mount(ConnectionFields, { props: { modelValue: form, idPrefix: 'fixture', disabled: false } })
    await wrapper.get('#fixture-advanced-auth').trigger('click')
    await wrapper.get('#fixture-auth-mode').trigger('click')
    expect(document.querySelector('.select-dropdown-portal')).not.toBeNull()
    await wrapper.setProps({ disabled: true })
    expect(wrapper.get('#fixture-auth-mode').attributes('aria-expanded')).toBe('false')
    document.querySelectorAll<HTMLElement>('.select-dropdown-portal [role="option"]')[1]?.click()
    expect(form.mode).toBe('password')
    expect(wrapper.get('#fixture-auth-mode').attributes('disabled')).toBeDefined()
  })
})
