import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { enableAutoUnmount, flushPromises, mount } from '@vue/test-utils'
import PaymentQRDialog from '../PaymentQRDialog.vue'
import PaymentStatusPanel from '../PaymentStatusPanel.vue'
import PaymentQRCodeView from '@/views/user/PaymentQRCodeView.vue'
import { decidePaymentLaunch, readPaymentRecoverySnapshot } from '../paymentFlow'

const { pollOrderStatus, verifyOrder, push, query } = vi.hoisted(() => ({
  pollOrderStatus: vi.fn(), verifyOrder: vi.fn(), push: vi.fn(), query: {} as Record<string, string>,
}))
vi.mock('vue-i18n', async (importOriginal) => ({
  ...await importOriginal<typeof import('vue-i18n')>(),
  useI18n: () => ({ t: (key: string) => key }),
}))
vi.mock('@/stores/payment', () => ({ usePaymentStore: () => ({ pollOrderStatus }) }))
vi.mock('@/stores', () => ({ useAppStore: () => ({ showError: vi.fn() }) }))
vi.mock('@/api/payment', () => ({ paymentAPI: { verifyOrder } }))
vi.mock('qrcode', () => ({ default: { toCanvas: vi.fn().mockResolvedValue(undefined) } }))
vi.mock('vue-router', () => ({ useRoute: () => ({ query }), useRouter: () => ({ push }) }))

enableAutoUnmount(afterEach)
beforeEach(() => {
  vi.useFakeTimers()
  vi.setSystemTime(new Date('2026-11-11T04:00:00Z'))
  vi.spyOn(HTMLCanvasElement.prototype, 'getContext').mockReturnValue(null)
  pollOrderStatus.mockReset().mockImplementation(() => Promise.resolve({
    id: 42, amount: 10, pay_amount: 10, bill_amount_matching: query.bill_amount_matching === '1',
    out_trade_no: 'bill-order-42', status: Date.now() >= Date.parse(query.expires_at) ? 'EXPIRED' : 'PENDING',
  }))
  verifyOrder.mockReset()
  push.mockReset()
  Object.assign(query, {
    order_id: '42', qr: 'https://qr.alipay.com/fixed-ten', pay_url: 'https://qr.alipay.com/fixed-ten',
    payment_type: 'alipay', bill_amount_matching: '1', expires_at: new Date(Date.now() + 60_000).toISOString(),
  })
})
afterEach(() => vi.useRealTimers())

async function open(kind: 'dialog' | 'panel' | 'page', billAmountMatching = true) {
  query.bill_amount_matching = billAmountMatching ? '1' : '0'
  const props = {
    orderId: 42, qrCode: query.qr, payUrl: query.pay_url, expiresAt: query.expires_at,
    paymentType: 'alipay', billAmountMatching,
  }
  const global = { stubs: {
    Icon: true, AppLayout: { template: '<div><slot /></div>' },
    BaseDialog: { props: ['show', 'title'], template: '<div v-if="show"><h2>{{ title }}</h2><slot /><slot name="footer" /></div>' },
  } }
  if (kind === 'page') return mount(PaymentQRCodeView, { global })
  if (kind === 'panel') return mount(PaymentStatusPanel, { props, global })
  const wrapper = mount(PaymentQRDialog, { props: { ...props, show: false }, global })
  await wrapper.setProps({ show: true })
  return wrapper
}

describe.each(['dialog', 'panel', 'page'] as const)('fixed-code payment window: %s', (kind) => {
  it('waits for fulfillment after payment confirmation and keeps polling past expiry', async () => {
    pollOrderStatus.mockResolvedValue({ id: 42, amount: 10, pay_amount: 10, status: 'RECHARGING' })
    const wrapper = await open(kind)
    await vi.advanceTimersByTimeAsync(3000)
    expect(wrapper.find('canvas').exists()).toBe(false)
    expect(wrapper.text()).toContain('payment.result.processing')
    expect(wrapper.emitted('success')).toBeUndefined()
    expect(push).not.toHaveBeenCalled()
    await vi.advanceTimersByTimeAsync(60_000)
    expect(wrapper.text()).not.toContain('payment.qr.expired')
    pollOrderStatus.mockResolvedValue({ id: 42, amount: 10, pay_amount: 10, status: 'COMPLETED' })
    await vi.advanceTimersByTimeAsync(3000)
    if (kind === 'page') expect(push).toHaveBeenCalledWith(expect.objectContaining({ path: '/payment/result' }))
    else expect(wrapper.emitted('success')).toHaveLength(1)
  })

  it('bounds fulfillment polling without showing an unpaid or expired result', async () => {
    pollOrderStatus.mockResolvedValue({ id: 42, amount: 10, pay_amount: 10, status: 'PAID' })
    const wrapper = await open(kind)
    await vi.advanceTimersByTimeAsync(124_000)
    expect(wrapper.text()).toContain('payment.result.fulfillmentDelayed')
    expect(wrapper.text()).not.toContain('payment.qr.expired')
    expect(wrapper.emitted('success')).toBeUndefined()
    expect(push).not.toHaveBeenCalled()
    const calls = pollOrderStatus.mock.calls.length
    await vi.advanceTimersByTimeAsync(30_000)
    expect(pollOrderStatus).toHaveBeenCalledTimes(calls)
    expect(vi.getTimerCount()).toBe(0)
  })

  it('keeps a confirmed payment recoverable when fulfillment temporarily fails', async () => {
    pollOrderStatus.mockResolvedValue({ id: 42, amount: 10, pay_amount: 10, status: 'PAID' })
    const wrapper = await open(kind)
    await vi.advanceTimersByTimeAsync(3000)
    pollOrderStatus.mockResolvedValue({ id: 42, amount: 10, pay_amount: 10, status: 'FAILED' })
    await vi.advanceTimersByTimeAsync(3000)
    expect(wrapper.text()).toContain('payment.result.fulfillmentPending')
    const calls = pollOrderStatus.mock.calls.length
    pollOrderStatus.mockResolvedValue({ id: 42, amount: 10, pay_amount: 10, status: 'COMPLETED' })
    await vi.advanceTimersByTimeAsync(3000)
    expect(pollOrderStatus.mock.calls.length).toBeGreaterThan(calls)
    if (kind === 'page') expect(push).toHaveBeenCalledWith(expect.objectContaining({ path: '/payment/result' }))
    else expect(wrapper.emitted('success')).toHaveLength(1)
  })

  it('hides payment controls at the server deadline but recovers a delayed bill', async () => {
    const wrapper = await open(kind)
    await flushPromises()
    expect(wrapper.text()).toContain('01:00')
    expect(wrapper.find('canvas').exists()).toBe(true)
    await vi.advanceTimersByTimeAsync(63_000)
    expect(wrapper.text()).toContain('payment.qr.billReconcilingTitle')
    expect(wrapper.text()).toContain('payment.qr.billReconciling')
    expect(wrapper.text()).not.toContain('payment.qr.expired')
    expect(wrapper.text()).not.toContain('payment.qr.scanAlipay')
    expect(wrapper.find('.animate-spin').exists()).toBe(true)
    expect(wrapper.find('canvas').exists()).toBe(false)
    expect(wrapper.text()).not.toContain('payment.qr.openPayWindow')
    expect(verifyOrder).not.toHaveBeenCalled()
    const before = pollOrderStatus.mock.calls.length
    pollOrderStatus.mockResolvedValue({ id: 42, amount: 10, pay_amount: 10, status: 'COMPLETED' })
    await vi.advanceTimersByTimeAsync(3000)
    expect(pollOrderStatus.mock.calls.length).toBeGreaterThan(before)
    if (kind === 'page') expect(push).toHaveBeenCalledWith(expect.objectContaining({ path: '/payment/result' }))
    else expect(wrapper.emitted('success')).toHaveLength(1)
  })

  it('bounds bill waiting to two minutes after expiry and points to order history', async () => {
    const wrapper = await open(kind)
    await vi.advanceTimersByTimeAsync(181_000)
    expect(wrapper.text()).toContain('payment.qr.billUnconfirmedTitle')
    expect(wrapper.text()).toContain('payment.qr.billExpiredDesc')
    expect(wrapper.text()).not.toContain('payment.qr.billReconciling')
    expect(wrapper.text()).not.toContain('payment.qr.expired')
    expect(wrapper.find('.animate-spin').exists()).toBe(false)
    expect(wrapper.find('canvas').exists()).toBe(false)
    expect(wrapper.find('a[href="/orders"]').exists()).toBe(true)
    const calls = pollOrderStatus.mock.calls.length
    await vi.advanceTimersByTimeAsync(30_000)
    expect(pollOrderStatus).toHaveBeenCalledTimes(calls)
    expect(vi.getTimerCount()).toBe(0)
  })

  it('restores an expired checkout without showing or extending its payment code', async () => {
    query.expires_at = new Date(Date.now() - 90_000).toISOString()
    const wrapper = await open(kind)
    await flushPromises()
    expect(wrapper.find('canvas').exists()).toBe(false)
    expect(wrapper.text()).toContain('payment.qr.billReconcilingTitle')
    expect(wrapper.text()).toContain('payment.qr.billReconciling')
    expect(wrapper.text()).not.toContain('payment.qr.expired')
    await vi.advanceTimersByTimeAsync(31_000)
    expect(wrapper.text()).toContain('payment.qr.billUnconfirmedTitle')
    expect(wrapper.text()).toContain('payment.qr.billExpiredDesc')
    expect(vi.getTimerCount()).toBe(0)
  })

  it('keeps ordinary provider expiry final without entering bill reconciliation', async () => {
    const wrapper = await open(kind, false)
    await vi.advanceTimersByTimeAsync(63_000)
    expect(wrapper.text()).toContain('payment.qr.expired')
    expect(wrapper.text()).not.toContain('payment.qr.billReconciling')
    expect(wrapper.text()).not.toContain('payment.qr.billUnconfirmedTitle')
    expect(wrapper.find('canvas').exists()).toBe(false)
    expect(wrapper.find('.animate-spin').exists()).toBe(false)
    const calls = pollOrderStatus.mock.calls.length
    await vi.advanceTimersByTimeAsync(30_000)
    expect(pollOrderStatus).toHaveBeenCalledTimes(calls)
    expect(vi.getTimerCount()).toBe(0)
  })

  it('ignores a delayed result after leaving the checkout', async () => {
    let complete!: (order: unknown) => void
    pollOrderStatus.mockImplementation(() => new Promise(resolve => { complete = resolve }))
    const wrapper = await open(kind)
    await vi.advanceTimersByTimeAsync(3000)
    wrapper.unmount()
    complete({ id: 42, amount: 10, pay_amount: 10, status: 'COMPLETED' })
    await flushPromises()
    expect(wrapper.emitted('success')).toBeUndefined()
    expect(push).not.toHaveBeenCalled()
  })
})

it('carries fixed-code semantics into recovery and restores only the bill waiting window', () => {
  const decision = decidePaymentLaunch({
    order_id: 42, amount: 10, pay_amount: 10, fee_rate: 0, expires_at: query.expires_at,
    bill_amount_matching: true, qr_code: query.qr, pay_url: query.pay_url, payment_mode: 'qrcode',
  }, { visibleMethod: 'alipay', orderType: 'balance', isMobile: true })
  expect(decision.kind).toBe('qr_waiting')
  expect(decision.paymentState.billAmountMatching).toBe(true)
  const snapshot = JSON.stringify(decision.recovery)
  expect(readPaymentRecoverySnapshot(snapshot, { now: Date.now() + 75_000 })?.billAmountMatching).toBe(true)
  expect(readPaymentRecoverySnapshot(snapshot, { now: Date.now() + 181_000 })).toBeNull()
})

it('does not settle after the user exits bill waiting while a poll is in flight', async () => {
  query.expires_at = new Date(Date.now() - 10_000).toISOString()
  let complete!: (order: unknown) => void
  pollOrderStatus.mockImplementation(() => new Promise(resolve => { complete = resolve }))
  const wrapper = await open('panel')
  await vi.advanceTimersByTimeAsync(3000)
  await wrapper.get('button.btn-primary').trigger('click')
  expect(wrapper.emitted('done')).toHaveLength(1)
  complete({ id: 42, amount: 10, pay_amount: 10, status: 'COMPLETED' })
  await flushPromises()
  expect(wrapper.emitted('success')).toBeUndefined()
})
