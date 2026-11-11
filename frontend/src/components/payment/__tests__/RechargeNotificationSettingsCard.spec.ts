import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import RechargeNotificationSettingsCard from '../RechargeNotificationSettingsCard.vue'

const { getConfig, updateConfig } = vi.hoisted(() => ({ getConfig: vi.fn(), updateConfig: vi.fn() }))
vi.mock('@/api/admin/payment', () => ({ adminPaymentAPI: { getConfig, updateConfig } }))
vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string) => key, te: () => false }) }))

const mountCard = () => mount(RechargeNotificationSettingsCard)
const save = (wrapper: ReturnType<typeof mountCard>) => wrapper.get('[data-testid="recharge-notify-save"]')
const recipients = (wrapper: ReturnType<typeof mountCard>) => wrapper.get('#recharge-notify-emails')

describe('dedicated administrator recharge notification settings', () => {
  beforeEach(() => {
    vi.resetAllMocks()
    getConfig.mockResolvedValue({ data: { admin_recharge_notify_enabled: false, admin_recharge_notify_emails: [] } })
    updateConfig.mockResolvedValue({ data: {} })
  })

  it('loads the saved switch and dedicated recipients without saving unrelated settings', async () => {
    getConfig.mockResolvedValue({ data: { admin_recharge_notify_enabled: true, admin_recharge_notify_emails: ['admin@example.test', 'billing@example.test'] } })
    const wrapper = mountCard(); await flushPromises()
    expect(wrapper.get('[role="switch"]').attributes('aria-checked')).toBe('true')
    expect((recipients(wrapper).element as HTMLTextAreaElement).value).toBe('admin@example.test\nbilling@example.test')
    expect(updateConfig).not.toHaveBeenCalled()
    wrapper.unmount()
  })

  it('defaults to disabled when an earlier server has no notification fields', async () => {
    getConfig.mockResolvedValue({ data: {} })
    const wrapper = mountCard(); await flushPromises()
    expect(wrapper.get('[role="switch"]').attributes('aria-checked')).toBe('false')
    expect((recipients(wrapper).element as HTMLTextAreaElement).value).toBe('')
    wrapper.unmount()
  })

  it('saves only the notification fields and normalizes comma/newline recipients without duplicates', async () => {
    const wrapper = mountCard(); await flushPromises()
    await wrapper.get('[role="switch"]').trigger('click')
    await recipients(wrapper).setValue(' Admin@Example.test, billing@example.test\nadmin@example.test ')
    await save(wrapper).trigger('click'); await flushPromises()
    expect(updateConfig).toHaveBeenCalledTimes(1)
    expect(updateConfig).toHaveBeenCalledWith({ admin_recharge_notify_enabled: true, admin_recharge_notify_emails: ['admin@example.test', 'billing@example.test'] })
    expect(wrapper.get('[role="status"]').text()).toBe('common.saved')
    expect((recipients(wrapper).element as HTMLTextAreaElement).value).toBe('admin@example.test\nbilling@example.test')
    wrapper.unmount()
  })

  it('requires a recipient before enabling notifications', async () => {
    const wrapper = mountCard(); await flushPromises()
    await wrapper.get('[role="switch"]').trigger('click')
    await save(wrapper).trigger('click')
    expect(updateConfig).not.toHaveBeenCalled()
    expect(wrapper.get('[role="alert"]').text()).toBe('admin.settings.payment.rechargeNotifyRecipientsRequired')
    wrapper.unmount()
  })

  it.each(['not-an-email', 'Admin <admin@example.test>', 'admin@example.test injected', 'https://example.test'])('rejects malformed recipient %s', async value => {
    const wrapper = mountCard(); await flushPromises()
    await recipients(wrapper).setValue(value)
    await save(wrapper).trigger('click')
    expect(updateConfig).not.toHaveBeenCalled()
    expect(wrapper.get('[role="alert"]').text()).toBe('admin.settings.payment.rechargeNotifyRecipientsInvalid')
    wrapper.unmount()
  })

  it('rejects a recipient longer than the server limit of 254 characters', async () => {
    const wrapper = mountCard(); await flushPromises()
    const address = `${'a'.repeat(64)}@${'b'.repeat(63)}.${'c'.repeat(63)}.${'d'.repeat(62)}`
    expect(address).toHaveLength(255)
    await recipients(wrapper).setValue(address)
    await save(wrapper).trigger('click')
    expect(updateConfig).not.toHaveBeenCalled()
    expect(wrapper.get('[role="alert"]').text()).toBe('admin.settings.payment.rechargeNotifyRecipientsInvalid')
    wrapper.unmount()
  })

  it('limits the recipient list to ten distinct addresses', async () => {
    const wrapper = mountCard(); await flushPromises()
    await recipients(wrapper).setValue(Array.from({ length: 11 }, (_, index) => `admin${index}@example.test`).join('\n'))
    await save(wrapper).trigger('click')
    expect(updateConfig).not.toHaveBeenCalled()
    expect(wrapper.get('[role="alert"]').text()).toBe('admin.settings.payment.rechargeNotifyRecipientsLimit')
    wrapper.unmount()
  })

  it('allows disabling notifications and clearing all recipients', async () => {
    getConfig.mockResolvedValue({ data: { admin_recharge_notify_enabled: true, admin_recharge_notify_emails: ['admin@example.test'] } })
    const wrapper = mountCard(); await flushPromises()
    await wrapper.get('[role="switch"]').trigger('click')
    await recipients(wrapper).setValue('')
    await save(wrapper).trigger('click'); await flushPromises()
    expect(updateConfig).toHaveBeenCalledWith({ admin_recharge_notify_enabled: false, admin_recharge_notify_emails: [] })
    wrapper.unmount()
  })

  it('blocks saving after a failed read and supports retrying the saved configuration', async () => {
    getConfig.mockRejectedValueOnce(new Error('unavailable'))
    const wrapper = mountCard(); await flushPromises()
    expect(save(wrapper).attributes('disabled')).toBeDefined()
    expect(wrapper.get('[role="alert"]').text()).toBe('admin.settings.payment.rechargeNotifyLoadFailed')
    await wrapper.get('[data-testid="recharge-notify-retry"]').trigger('click'); await flushPromises()
    expect(getConfig).toHaveBeenCalledTimes(2)
    expect(save(wrapper).attributes('disabled')).toBeUndefined()
    expect(wrapper.find('[role="alert"]').exists()).toBe(false)
    wrapper.unmount()
  })

  it('keeps the edited recipients and reports a failed save without claiming success', async () => {
    updateConfig.mockRejectedValueOnce(new Error('unavailable'))
    const wrapper = mountCard(); await flushPromises()
    await recipients(wrapper).setValue('billing@example.test')
    await save(wrapper).trigger('click'); await flushPromises()
    expect(wrapper.get('[role="alert"]').text()).toBe('admin.settings.payment.rechargeNotifySaveFailed')
    expect((recipients(wrapper).element as HTMLTextAreaElement).value).toBe('billing@example.test')
    expect(wrapper.find('[role="status"]').exists()).toBe(false)
    wrapper.unmount()
  })
})
