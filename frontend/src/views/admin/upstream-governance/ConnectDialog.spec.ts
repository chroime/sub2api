import { mount, flushPromises } from '@vue/test-utils'
import { describe, it, expect, vi } from 'vitest'
import ConnectDialog from './ConnectDialog.vue'
import api from '@/api/admin/upstream-governance'
vi.mock('@/api/admin/upstream-governance', () => ({
  default: { connect: vi.fn() },
}))
vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string) => key }) }))
describe('connection challenge', () => {
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
    await inputs[2]!.setValue('123456')
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
