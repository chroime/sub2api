import { mount, flushPromises } from '@vue/test-utils'
import { describe, it, expect, vi } from 'vitest'
import ConnectDialog from './ConnectDialog.vue'
import api from '@/api/admin/upstream-governance'
vi.mock('@/api/admin/upstream-governance', () => ({
  default: { connect: vi.fn() },
}))
vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string) => key }) }))
describe('connection challenge', () => {
  it('keeps the transient password when a CAPTCHA response requires the same login to continue', async () => {
    vi.mocked(api.connect).mockResolvedValueOnce({ challenge: { kind: 'captcha' } })
    const wrapper = mount(ConnectDialog, { props: { siteId: 7 }, global: { stubs: { BaseDialog: { template: '<div><slot /></div>' } } } })
    await wrapper.get('#governance-connect-username').setValue('fixture-user')
    await wrapper.get('#governance-connect-password').setValue('fixture-password')
    await wrapper.get('form').trigger('submit')
    await flushPromises()
    expect(wrapper.emitted('connected')).toBeUndefined()
    expect((wrapper.get('#governance-connect-password').element as HTMLInputElement).value).toBe('fixture-password')
    expect(wrapper.find('#governance-connect-captcha').exists()).toBe(true)
    expect(wrapper.get('#governance-connect-captcha').attributes('type')).toBe('text')
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
  it('displays an entered dashboard session token in plaintext in advanced authorization', async () => {
    const wrapper = mount(ConnectDialog, { props: { siteId: 7 }, global: { stubs: { BaseDialog: { template: '<div><slot /></div>' } } } })
    await wrapper.get('#governance-connect-advanced-auth').trigger('click')
    await wrapper.get('#governance-connect-auth-mode').setValue('session')
    const token = wrapper.get('#governance-connect-session')
    await token.setValue('fixture-dashboard-session')
    expect(token.attributes('type')).toBe('text')
    expect((token.element as HTMLInputElement).value).toBe('fixture-dashboard-session')
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
