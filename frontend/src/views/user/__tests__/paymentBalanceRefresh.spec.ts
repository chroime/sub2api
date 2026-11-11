import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'

const { pollOrderStatus, getCurrentUser } = vi.hoisted(() => ({ pollOrderStatus: vi.fn(), getCurrentUser: vi.fn() }))
vi.mock('vue-i18n', async importOriginal => ({
  ...await importOriginal<typeof import('vue-i18n')>(),
  useI18n: () => ({ t: (key: string) => key }),
}))
vi.mock('vue-router', () => ({
  useRoute: () => ({ path: '/payment/result', query: { order_id: '42' }, params: {}, meta: {}, matched: [] }),
  useRouter: () => ({ push: vi.fn() }),
}))
vi.mock('@/stores/auth', async () => {
  const { reactive } = await import('vue')
  const store = reactive({
    user: { id: 17, username: 'payment-test', email: 'payment-test@local.test', role: 'user', balance: 0 },
    isAdmin: false, isSimpleMode: false,
    async refreshUser() {
      const response = await getCurrentUser()
      store.user = response.data
      return store.user
    },
  })
  return { useAuthStore: () => store }
})
vi.mock('@/stores', async () => {
  const { useAuthStore } = await import('@/stores/auth')
  return {
    useAuthStore,
    useAppStore: () => ({ contactInfo: '', docUrl: '', cachedPublicSettings: {} }),
    useOnboardingStore: () => ({}),
  }
})
vi.mock('@/stores/adminSettings', () => ({ useAdminSettingsStore: () => ({ customMenuItems: [] }) }))
vi.mock('@/stores/payment', () => ({ usePaymentStore: () => ({ pollOrderStatus }) }))
vi.mock('@/api/payment', () => ({ paymentAPI: {} }))
vi.mock('@/utils/featureFlags', async importOriginal => ({
  ...await importOriginal<typeof import('@/utils/featureFlags')>(), isFeatureFlagEnabled: () => false,
}))

import AppHeader from '@/components/layout/AppHeader.vue'
import PaymentResultView from '../PaymentResultView.vue'

beforeEach(() => {
  vi.useFakeTimers()
  window.localStorage.clear()
  pollOrderStatus.mockReset()
  getCurrentUser.mockReset()
})
afterEach(() => vi.useRealTimers())

it('updates the mounted header from the refreshed account when paid recharge finishes', async () => {
  const order = { id: 42, user_id: 17, order_type: 'balance', amount: 0.1, pay_amount: 0.1, fee_rate: 0, payment_type: 'alipay', status: 'PAID' }
  pollOrderStatus.mockResolvedValueOnce(order).mockResolvedValueOnce({ ...order, status: 'COMPLETED' })
  getCurrentUser.mockResolvedValue({ data: { id: 17, username: 'payment-test', email: 'payment-test@local.test', role: 'user', balance: 0.1 } })
  const wrapper = mount({ components: { AppHeader, PaymentResultView }, template: '<AppHeader /><PaymentResultView />' }, {
    global: { stubs: { Icon: true, LocaleSwitcher: true, SubscriptionProgressMini: true, AnnouncementBell: true, RouterLink: true, OrderStatusBadge: true } },
  })
  await flushPromises()
  expect(wrapper.get('header').text()).toContain('$0.00')
  expect(getCurrentUser).not.toHaveBeenCalled()
  await vi.advanceTimersByTimeAsync(2000)
  await flushPromises()
  expect(getCurrentUser).toHaveBeenCalledTimes(1)
  expect(wrapper.get('header').text()).toContain('$0.10')
  expect(wrapper.text()).toContain('payment.result.success')
  wrapper.unmount()
})
