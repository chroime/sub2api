import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import api from '@/api/admin/upstream-governance'
import DeliveryRecords from './DeliveryRecords.vue'

vi.mock('@/api/admin/upstream-governance', () => ({ default: { deliveryRecords: vi.fn() } }))
vi.mock('./BalanceNotificationRecords.vue', () => ({ default: { template: '<div data-test="balance-notification-records" />' } }))
vi.mock('vue-i18n', async (importOriginal) => ({
  ...await importOriginal<typeof import('vue-i18n')>(),
  useI18n: () => ({ t: (key: string, params?: Record<string, unknown>) => params ? `${key}:${JSON.stringify(params)}` : key }),
}))

const page = <T>(items: T[], total = items.length) => ({ items, total, page: 1, page_size: 20, pages: Math.ceil(total / 20) })

describe('DeliveryRecords', () => {
  beforeEach(() => {
    vi.mocked(api.deliveryRecords).mockReset()
  })

  it('lists upstream change mails first and switches to AstrBot messages and user mails', async () => {
    vi.mocked(api.deliveryRecords).mockImplementation(async channel => channel === 'email'
      ? page([{ id: 9, channel: 'email', site_id: 3, site_name: 'DD批发站', kind: 'group_changes', subject: '分组变化', recipient: 'ops@example.com', status: 'sent', attempts: 0, created_at: '2026-11-05T15:00:00Z', sent_at: '2026-11-05T15:00:01Z' }])
      : page([{ id: 4, channel: 'astrbot', group_id: 52, group_name: 'Grok Heavy', kind: 'updated', status: 'pending', attempts: 2, last_error: 'timeout', created_at: '2026-11-05T15:00:00Z' }]))
    const wrapper = mount(DeliveryRecords)
    await flushPromises()
    expect(api.deliveryRecords).toHaveBeenCalledWith('email', 1)
    const mail = wrapper.get('[data-test=delivery-record]')
    expect(mail.text()).toContain('DD批发站')
    expect(mail.text()).toContain('分组变化')
    expect(mail.text()).toContain('ops@example.com')
    expect(mail.text()).toContain('governance.deliveryRecords.status.sent')

    await wrapper.get('[data-test=delivery-channel-astrbot]').trigger('click')
    await flushPromises()
    expect(api.deliveryRecords).toHaveBeenLastCalledWith('astrbot', 1)
    const message = wrapper.get('[data-test=delivery-record]')
    expect(message.text()).toContain('Grok Heavy')
    expect(message.text()).toContain('governance.deliveryRecords.astrbotKinds.updated')
    expect(message.text()).toContain('governance.deliveryRecords.status.pending')
    expect(message.text()).toContain('timeout')

    await wrapper.get('[data-test=delivery-channel-users]').trigger('click')
    await flushPromises()
    expect(wrapper.find('[data-test=balance-notification-records]').exists()).toBe(true)
    expect(api.deliveryRecords).toHaveBeenCalledTimes(2)
  })

  it('can open on user balance mails and pages through long lists', async () => {
    const users = mount(DeliveryRecords, { props: { initialChannel: 'users' } })
    await flushPromises()
    expect(users.find('[data-test=balance-notification-records]').exists()).toBe(true)
    expect(api.deliveryRecords).not.toHaveBeenCalled()

    vi.mocked(api.deliveryRecords).mockResolvedValue(page([], 45))
    const wrapper = mount(DeliveryRecords)
    await flushPromises()
    const next = wrapper.findAll('button').find(button => button.text() === 'governance.next')!
    await next.trigger('click')
    await flushPromises()
    expect(api.deliveryRecords).toHaveBeenLastCalledWith('email', 2)
  })

  it('shows a read failure instead of an empty list', async () => {
    vi.mocked(api.deliveryRecords).mockRejectedValue(new Error('unavailable'))
    const wrapper = mount(DeliveryRecords)
    await flushPromises()
    expect(wrapper.text()).toContain('governance.deliveryRecords.readFailed')
    expect(wrapper.find('[data-test=delivery-record]').exists()).toBe(false)
  })
})
