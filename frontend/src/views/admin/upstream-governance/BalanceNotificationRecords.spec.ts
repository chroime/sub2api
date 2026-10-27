import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { createI18n } from 'vue-i18n'
import zh from '@/i18n/locales/zh/governance'
import api, { type BalanceNotification, type BalanceNotificationPage } from '@/api/admin/balance-notifications'
import BalanceNotificationRecords from './BalanceNotificationRecords.vue'

vi.mock('@/api/admin/balance-notifications', () => ({ default: { list: vi.fn(), detail: vi.fn() } }))
const row = (overrides: Partial<BalanceNotification> = {}): BalanceNotification => ({ id: 1, notification_id: 'fixture', trigger_type: 'automatic', batch_id: '', user_id: 7, user_email: 'user@example.test', user_name: '测试用户', recipient: 'notify@example.test', balance: 0.065, threshold: 1, subject: '余额不足提醒', status: 'sent', suppression_reason: '', attempts: 1, last_attempt_at: '2026-10-27T01:00:00Z', sent_at: '2026-10-27T01:00:01Z', created_at: '2026-10-27T01:00:00Z', updated_at: '2026-10-27T01:00:01Z', expires_at: '2027-01-25T01:00:00Z', last_error: '', ...overrides })
const result = (items = [row()]): BalanceNotificationPage => ({ items, total: items.length, page: 1, page_size: 20, pages: 1, summary: { total: items.length, pending: 0, sent: 1, failed: 0, suppressed: 0 }, evaluated_at: '2026-10-27T01:00:02Z' })
function render() { return mount(BalanceNotificationRecords, { global: { plugins: [createI18n({ legacy: false, locale: 'zh', messages: { zh: { governance: zh } } })], stubs: { BaseDialog: { props: ['show'], template: '<div v-if="show"><slot /></div>' } } } }) }

describe('user balance notification records', () => {
  beforeEach(() => { vi.resetAllMocks(); vi.mocked(api.list).mockResolvedValue(result()) })
  it('shows exact user/recipient snapshots and balance without fetching message bodies', async () => {
    const w = render(); await flushPromises()
    expect(w.text()).toContain('用户余额通知'); expect(w.text()).toContain('user@example.test'); expect(w.text()).toContain('notify@example.test'); expect(w.text()).toContain('$0.065')
    expect(w.text()).toContain('2026-10-27 09:00:00'); expect(api.detail).not.toHaveBeenCalled()
    w.unmount()
  })
  it('applies filters and returns to the first page', async () => {
    const w = render(); await flushPromises()
    await w.get('[data-test="notice-user-email"]').setValue('account@')
    await w.get('[data-test="notice-recipient"]').setValue('notify@')
    await w.get('form').trigger('submit'); await flushPromises()
    expect(api.list).toHaveBeenLastCalledWith(expect.objectContaining({ page: 1, user_email: 'account@', recipient: 'notify@' }))
    w.unmount()
  })
  it('fetches the real body on demand and isolates preview scripts and network', async () => {
    vi.mocked(api.detail).mockResolvedValue({ ...row(), body: '<h2>实际邮件</h2><script>parent.alert(1)</script><img src="https://tracker.example/pixel"><form action="https://tracker.example/send"></form>' })
    const w = render(); await flushPromises(); await w.get('[data-test="notice-detail"]').trigger('click'); await flushPromises()
    expect(api.detail).toHaveBeenCalledWith(1)
    const frame = w.get('iframe'); expect(frame.attributes('sandbox')).toBe('')
    const body = frame.attributes('srcdoc'); expect(body).toContain('实际邮件'); expect(body).toContain("default-src 'none'"); expect(body).not.toContain('<script'); expect(body).not.toContain('https://tracker.example'); expect(body).not.toContain('<form')
    w.unmount()
  })
  it('shows explicit skip/failure reasons and does not claim retrying', async () => {
    vi.mocked(api.list).mockResolvedValue(result([row({ id: 2, status: 'suppressed', suppression_reason: 'duplicate', attempts: 0 }), row({ id: 3, status: 'failed', last_error: 'smtp rcpt: 550 recipient rejected' })]))
    const w = render(); await flushPromises()
    expect(w.text()).toContain('重复发送保护'); expect(w.text()).toContain('smtp rcpt: 550 recipient rejected'); expect(w.text()).not.toContain('等待自动重试')
    w.unmount()
  })
  it('does not round a tiny effective percentage threshold to zero', async () => {
    vi.mocked(api.list).mockResolvedValue(result([row({ threshold: 0.0000000001 })]))
    const w = render(); await flushPromises()
    expect(w.text()).toContain('$0.0000000001')
    w.unmount()
  })
  it('retains the visible page and provides retry after a failed refresh', async () => {
    const w = render(); await flushPromises(); vi.mocked(api.list).mockRejectedValue(new Error('network'))
    await w.get('[data-test="notice-refresh"]').trigger('click'); await flushPromises()
    expect(w.text()).toContain('user@example.test'); expect(w.get('[role="alert"]').text()).toContain('加载失败')
    w.unmount()
  })
})
