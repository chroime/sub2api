import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import AlipayBillReviewPanel from '../AlipayBillReviewPanel.vue'
import type { AlipayBillReceipt, ProviderInstance } from '@/types/payment'

vi.mock('vue-i18n', () => ({ useI18n: () => ({
  t: (key: string, params?: Record<string, unknown>) => params ? `${key} ${JSON.stringify(params)}` : key,
  te: (key: string) => key.endsWith('missing_order_reference'),
}) }))
const { get, post } = vi.hoisted(() => ({ get: vi.fn(), post: vi.fn() }))
vi.mock('@/api/client', () => ({ apiClient: { get, post } }))

const provider: ProviderInstance = { id: 9, provider_key: 'alipay_bill', name: 'Fixed QR', config: { receiverId: '2088123456789012' }, supported_types: ['alipay'], enabled: true, payment_mode: 'qrcode', refund_enabled: false, allow_user_refund: false, limits: '', sort_order: 0 }
const receipt: AlipayBillReceipt = { id: 4, account_log_id: 'log-4', alipay_order_no: 'ali-4', merchant_order_no: '', amount: '10.00', trans_at: '2026-11-10T12:00:00Z', status: 'review', review_reason: 'missing_order_reference' }
const order = { id: 12, user_id: 7, amount: 1.5, pay_amount: 10, payment_type: 'alipay', out_trade_no: 'order-12', status: 'PENDING', order_type: 'balance', created_at: '2026-11-10T11:59:00Z', expires_at: '2026-11-10T12:30:00Z', refund_amount: 0, fee_rate: 0, provider_instance_id: '9' }
const gatewayOrder = { id: 42, out_trade_no: 'merchant-42', trade_no: 'EPAY42', amount: '10.00', status: 'PENDING', notify_status: 'PENDING', created_at: '2026-11-10T11:59:00Z' }

function mountPanel() {
  return mount(AlipayBillReviewPanel, { props: { show: true, provider }, global: { stubs: { BaseDialog: { template: '<div><slot /></div>' } } } })
}

function button(wrapper: ReturnType<typeof mountPanel>, key: string) {
  return wrapper.findAll('button').find(node => node.text() === key)!
}

describe('Alipay receipt review', () => {
  beforeEach(() => {
    post.mockReset()
    get.mockReset()
    get.mockImplementation(async (url: string) => ({ data: url.endsWith('/orders') ? { items: [order], total: 1, page: 1, page_size: 20, pages: 1 } : { items: [receipt], total: 1, page: 1, page_size: 20 } }))
  })

  it('disables statement sync without a saved receiving UID and explains how to enable it', async () => {
    const wrapper = mountPanel()
    await wrapper.setProps({ provider: { ...provider, enabled: false, config: {} } })
    await flushPromises()
    expect(button(wrapper, 'admin.settings.payment.billSync').attributes('disabled')).toBeDefined()
    expect(wrapper.text()).toContain('admin.settings.payment.billSyncReceiverRequired')
    await button(wrapper, 'admin.settings.payment.billSync').trigger('click')
    expect(post).not.toHaveBeenCalled()
    wrapper.unmount()
  })

  it('keeps amount-only receipts in review and requires a selected order, evidence and verification before claiming', async () => {
    const wrapper = mountPanel()
    await flushPromises()
    expect(get).toHaveBeenCalledWith('/admin/payment/providers/9/bill-receipts', { params: { page: 1, page_size: 20, status: 'review' } })
    expect(wrapper.text()).toContain('admin.settings.payment.billStatusReview')
    expect(wrapper.text()).toContain('admin.settings.payment.billReviewReasons.missing_order_reference')
    await button(wrapper, 'admin.settings.payment.billClaim').trigger('click')
    const confirm = button(wrapper, 'admin.settings.payment.billConfirmClaim')
    expect(confirm.attributes('disabled')).toBeDefined()
    await wrapper.get('input[type=text]').setValue('order-12')
    await button(wrapper, 'common.search').trigger('click')
    await flushPromises()
    expect(get).toHaveBeenCalledWith('/admin/payment/orders', { params: { page: 1, page_size: 20, keyword: 'order-12', payment_type: 'alipay' } })
    expect((wrapper.get('input[type=radio]').element as HTMLInputElement).checked).toBe(false)
    await wrapper.get('input[type=radio]').setValue(true)
    await wrapper.get('textarea').setValue(' verified payment evidence ')
    expect(confirm.attributes('disabled')).toBeDefined()
    await wrapper.get('input[type=checkbox]').setValue(true)
    expect(confirm.attributes('disabled')).toBeUndefined()
    post.mockResolvedValue({ data: { success: true } })
    get.mockResolvedValue({ data: { items: [], total: 0, page: 1, page_size: 20 } })
    await confirm.trigger('click')
    await flushPromises()
    expect(post).toHaveBeenCalledWith('/admin/payment/providers/9/bill-receipts/4/claim', { order_id: 12, reason: 'verified payment evidence' })
    expect(wrapper.text()).toContain('admin.settings.payment.billClaimSuccess')
    expect(wrapper.find('input[type=radio]').exists()).toBe(false)
    wrapper.unmount()
  })

  it('shows matched and review counts separately after a sync', async () => {
    post.mockResolvedValue({ data: { fetched: 5, inserted: 4, matched: 1, review: 3, checked_at: '2026-11-10T12:05:00Z' } })
    const wrapper = mountPanel()
    await flushPromises()
    await button(wrapper, 'admin.settings.payment.billSync').trigger('click')
    await flushPromises()
    expect(post).toHaveBeenCalledWith('/admin/payment/providers/9/bill-sync', undefined, { timeout: 60000 })
    expect(wrapper.text()).toContain('"matched":1,"review":3')
    expect(wrapper.text()).toContain('admin.settings.payment.billStatusReview')
    wrapper.unmount()
  })

  it('searches external orders for the saved gateway and claims only the external target with evidence', async () => {
    const wrapper = mountPanel()
    await flushPromises()
    await button(wrapper, 'admin.settings.payment.billClaim').trigger('click')
    wrapper.getComponent('[data-testid="bill-claim-target"]').vm.$emit('update:modelValue', 'gateway')
    await flushPromises()
    await wrapper.get('input[type=text]').setValue('merchant-42')
    get.mockResolvedValueOnce({ data: { items: [gatewayOrder], total: 1, page: 1, page_size: 20 } })
    await button(wrapper, 'common.search').trigger('click')
    await flushPromises()
    expect(get).toHaveBeenLastCalledWith('/admin/payment/providers/9/epay-orders', { params: { page: 1, page_size: 20, keyword: 'merchant-42' } })
    expect(wrapper.text()).toContain('EPAY42')
    expect(wrapper.text()).toContain('merchant-42')
    expect((wrapper.get('input[type=radio]').element as HTMLInputElement).checked).toBe(false)
    await wrapper.get('input[type=radio]').setValue(true)
    await wrapper.get('textarea').setValue('External merchant receipt verified')
    await wrapper.get('input[type=checkbox]').setValue(true)
    post.mockResolvedValue({ data: { success: true } })
    await button(wrapper, 'admin.settings.payment.billConfirmClaim').trigger('click')
    await flushPromises()
    expect(post).toHaveBeenCalledWith('/admin/payment/providers/9/bill-receipts/4/claim', { epay_order_id: 42, reason: 'External merchant receipt verified' })
    wrapper.unmount()
  })

  it('clears a local selection and evidence when switching to external orders', async () => {
    const wrapper = mountPanel()
    await flushPromises()
    await button(wrapper, 'admin.settings.payment.billClaim').trigger('click')
    await wrapper.get('input[type=text]').setValue('order-12')
    await button(wrapper, 'common.search').trigger('click')
    await flushPromises()
    await wrapper.get('input[type=radio]').setValue(true)
    await wrapper.get('textarea').setValue('Local evidence')
    await wrapper.get('input[type=checkbox]').setValue(true)
    wrapper.getComponent('[data-testid="bill-claim-target"]').vm.$emit('update:modelValue', 'gateway')
    await flushPromises()
    expect(wrapper.find('input[type=radio]').exists()).toBe(false)
    expect((wrapper.get('input[type=text]').element as HTMLInputElement).value).toBe('')
    expect((wrapper.get('textarea').element as HTMLTextAreaElement).value).toBe('')
    expect((wrapper.get('input[type=checkbox]').element as HTMLInputElement).checked).toBe(false)
    expect(button(wrapper, 'admin.settings.payment.billConfirmClaim').attributes('disabled')).toBeDefined()
    wrapper.unmount()
  })

  it('labels a claimed external gateway receipt separately from a local order', async () => {
    get.mockResolvedValueOnce({ data: { items: [{ ...receipt, status: 'matched', epay_order_id: 42 }], total: 1, page: 1, page_size: 20 } })
    const wrapper = mountPanel()
    await flushPromises()
    expect(wrapper.text()).toContain('admin.settings.payment.billClaimedGatewayOrder #42')
    expect(wrapper.text()).not.toContain('admin.settings.payment.billClaimedOrder #')
    wrapper.unmount()
  })

  it('keeps the review form and shows an API error if another administrator already claimed the receipt', async () => {
    post.mockRejectedValue({ message: 'Receipt already claimed' })
    const wrapper = mountPanel()
    await flushPromises()
    await button(wrapper, 'admin.settings.payment.billClaim').trigger('click')
    await wrapper.get('input[type=text]').setValue('order-12')
    await button(wrapper, 'common.search').trigger('click')
    await flushPromises()
    await wrapper.get('input[type=radio]').setValue(true)
    await wrapper.get('textarea').setValue('Evidence verified')
    await wrapper.get('input[type=checkbox]').setValue(true)
    await button(wrapper, 'admin.settings.payment.billConfirmClaim').trigger('click')
    await flushPromises()
    expect(wrapper.get('[role=alert]').text()).toBe('Receipt already claimed')
    expect(wrapper.find('textarea').exists()).toBe(true)
    expect(wrapper.text()).not.toContain('admin.settings.payment.billClaimSuccess')
    wrapper.unmount()
  })
})
