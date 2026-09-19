import { enableAutoUnmount, flushPromises, mount } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import BalancePrechargeReviews from '../BalancePrechargeReviews.vue'

const { getReviews, resolveReview } = vi.hoisted(() => ({ getReviews: vi.fn(), resolveReview: vi.fn() }))
vi.mock('@/api/admin/balancePrechargeReviews', async (importOriginal) => ({
  ...await importOriginal<typeof import('@/api/admin/balancePrechargeReviews')>(),
  getBalancePrechargeReviews: getReviews, resolveBalancePrechargeReview: resolveReview
}))
vi.mock('vue-i18n', async (importOriginal) => ({
  ...await importOriginal<typeof import('vue-i18n')>(),
  useI18n: () => ({ t: (key: string, values?: Record<string, unknown>) => values ? `${key} ${JSON.stringify(values)}` : key })
}))

const pending = {
  id: 'hold-1', user_id: 1, user_email: 'customer@example.com', api_key_id: 3,
  group_id: 2, group_name: 'OpenAI', amount: 0.02, reason: 'upstream_usage_missing',
  request_id: 'request-1', account_id: 1, model: 'gpt-5.5', status: 'pending',
  created_at: '2026-09-19T00:00:00Z', updated_at: '2026-09-19T00:00:00Z'
}
const resolved = { ...pending, status: 'resolved', resolution: 'charge', actual_cost: 0.002,
  resolved_at: '2026-09-19T01:00:00Z', resolved_by: 7, note: 'Verified upstream invoice' }
const render = () => mount(BalancePrechargeReviews, { global: { stubs: { teleport: true } } })

enableAutoUnmount(afterEach)
describe('BalancePrechargeReviews', () => {
  beforeEach(() => {
    getReviews.mockReset().mockResolvedValue({ items: [pending], total: 1 })
    resolveReview.mockReset().mockResolvedValue(resolved)
  })

  it('loads pending records and opens a confirmation with the correct user, hold and refund', async () => {
    const wrapper = render()
    await flushPromises()
    expect(getReviews).toHaveBeenCalledWith({ status: 'pending', limit: 20, offset: 0 })
    expect(wrapper.text()).toContain('customer@example.com')
    await wrapper.get('[data-testid="review-open"]').trigger('click')
    const dialog = wrapper.get('[role="dialog"]')
    expect(dialog.text()).toContain('hold-1')
    expect(dialog.text()).toContain('request-1')
    expect(dialog.get('[data-testid="review-delta"]').text()).toContain('0.02')
    expect(dialog.get('[data-testid="review-confirm"]').attributes('disabled')).toBeDefined()
    expect(resolveReview).not.toHaveBeenCalled()
  })

  it('requires a note, prevents repeat submission and reloads after a confirmed refund', async () => {
    let finish!: (value: unknown) => void
    resolveReview.mockImplementation(() => new Promise((resolve) => { finish = resolve }))
    const wrapper = render()
    await flushPromises()
    await wrapper.get('[data-testid="review-open"]').trigger('click')
    await wrapper.get('[data-testid="review-note"]').setValue('  Upstream confirmed no charge  ')
    await wrapper.get('[data-testid="review-confirm"]').trigger('click')
    await wrapper.get('[data-testid="review-confirm"]').trigger('click')
    expect(resolveReview).toHaveBeenCalledTimes(1)
    expect(resolveReview).toHaveBeenCalledWith('hold-1', { action: 'release', actual_cost: 0, note: 'Upstream confirmed no charge' })
    expect(wrapper.get('[data-testid="review-confirm"]').attributes('disabled')).toBeDefined()
    expect(wrapper.get('[data-testid="review-cancel"]').attributes('disabled')).toBeDefined()
    getReviews.mockResolvedValue({ items: [], total: 0 })
    finish({ ...resolved, resolution: 'release', actual_cost: 0 })
    await flushPromises()
    expect(wrapper.find('[role="dialog"]').exists()).toBe(false)
    expect(wrapper.get('[data-testid="reviews-empty"]').exists()).toBe(true)
    expect(getReviews).toHaveBeenCalledTimes(2)
  })

  it.each(['0', '-1', 'NaN', '1e-3', '0.000000001', '1000001'])('blocks invalid charge %s', async (amount) => {
    const wrapper = render()
    await flushPromises()
    await wrapper.get('[data-testid="review-open"]').trigger('click')
    await wrapper.get('[data-testid="review-action-charge"]').setValue()
    await wrapper.get('[data-testid="review-cost"]').setValue(amount)
    await wrapper.get('[data-testid="review-note"]').setValue('Verified upstream invoice')
    expect(wrapper.get('[data-testid="review-confirm"]').attributes('disabled')).toBeDefined()
    expect(resolveReview).not.toHaveBeenCalled()
  })

  it.each([
    ['0.002', 'refundDelta', '0.018'],
    ['0.05', 'additionalChargeDelta', '0.03']
  ])('previews and submits the exact %s charge without generating token usage', async (amount, label, delta) => {
    const wrapper = render()
    await flushPromises()
    await wrapper.get('[data-testid="review-open"]').trigger('click')
    await wrapper.get('[data-testid="review-action-charge"]').setValue()
    await wrapper.get('[data-testid="review-cost"]').setValue(amount)
    await wrapper.get('[data-testid="review-note"]').setValue('Verified upstream invoice')
    expect(wrapper.get('[data-testid="review-delta"]').text()).toContain(label)
    expect(wrapper.get('[data-testid="review-delta"]').text()).toContain(delta)
    await wrapper.get('[data-testid="review-confirm"]').trigger('click')
    await flushPromises()
    expect(resolveReview).toHaveBeenCalledWith('hold-1', { action: 'charge', actual_cost: Number(amount), note: 'Verified upstream invoice' })
  })

  it('preserves the selected record and explanation on a failed resolution', async () => {
    resolveReview.mockRejectedValue(new Error('offline'))
    const wrapper = render()
    await flushPromises()
    await wrapper.get('[data-testid="review-open"]').trigger('click')
    await wrapper.get('[data-testid="review-note"]').setValue('Verified bill')
    await wrapper.get('[data-testid="review-confirm"]').trigger('click')
    await flushPromises()
    expect(wrapper.get('[role="dialog"] [role="alert"]').text()).toContain('resolveFailed')
    expect((wrapper.get('[data-testid="review-note"]').element as HTMLTextAreaElement).value).toBe('Verified bill')
    expect(wrapper.get('[data-testid="review-confirm"]').attributes('disabled')).toBeUndefined()
  })

  it('shows resolved records as read-only with operator, cost and explanation', async () => {
    getReviews.mockResolvedValue({ items: [resolved], total: 1 })
    const wrapper = render()
    await flushPromises()
    await wrapper.get('[data-testid="review-open"]').trigger('click')
    const dialog = wrapper.get('[role="dialog"]')
    expect(dialog.text()).toContain('Verified upstream invoice')
    expect(dialog.text()).toContain('0.002')
    expect(dialog.text()).toContain('7')
    expect(dialog.find('input').exists()).toBe(false)
    expect(dialog.find('[data-testid="review-confirm"]').exists()).toBe(false)
  })

  it('keeps records settled by normal billing read-only', async () => {
    getReviews.mockResolvedValue({ items: [{ ...pending, status: 'settled', actual_cost: 0.003 }], total: 1 })
    const wrapper = render()
    await flushPromises()
    await wrapper.get('[data-testid="review-open"]').trigger('click')
    expect(wrapper.get('[role="dialog"]').text()).toContain('settledDescription')
    expect(wrapper.get('[role="dialog"]').text()).not.toContain('resolvedTime')
    expect(wrapper.find('[data-testid="review-confirm"]').exists()).toBe(false)
  })

  it('reloads and prevents another operation when a concurrent settlement wins', async () => {
    resolveReview.mockRejectedValue({ status: 409, code: 'BALANCE_PRECHARGE_REVIEW_CONFLICT' })
    const wrapper = render()
    await flushPromises()
    await wrapper.get('[data-testid="review-open"]').trigger('click')
    await wrapper.get('[data-testid="review-note"]').setValue('No charge')
    getReviews.mockResolvedValue({ items: [], total: 0 })
    await wrapper.get('[data-testid="review-confirm"]').trigger('click')
    await flushPromises()
    expect(wrapper.find('[role="dialog"]').exists()).toBe(false)
    expect(wrapper.text()).toContain('alreadyHandled')
    expect(getReviews).toHaveBeenCalledTimes(2)
  })

  it.each(['  ', 'x'.repeat(2001)])('requires a nonblank explanation of at most 2000 characters', async (note) => {
    const wrapper = render()
    await flushPromises()
    await wrapper.get('[data-testid="review-open"]').trigger('click')
    await wrapper.get('[data-testid="review-note"]').setValue(note)
    expect(wrapper.get('[data-testid="review-confirm"]').attributes('disabled')).toBeDefined()
  })

  it('accepts up to 2000 Unicode characters of evidence and presents a readable reason', async () => {
    getReviews.mockResolvedValue({ items: [{ ...pending, reason: 'responses_stream_usage_missing' }], total: 1 })
    const wrapper = render()
    await flushPromises()
    await wrapper.get('[data-testid="review-open"]').trigger('click')
    expect(wrapper.get('[role="dialog"]').text()).toContain('reasonUsageMissing')
    await wrapper.get('[data-testid="review-note"]').setValue('🧾'.repeat(2000))
    expect(wrapper.get('[data-testid="review-confirm"]').attributes('disabled')).toBeUndefined()
  })

  it('loads the next page and resets pagination when switching status', async () => {
    getReviews.mockResolvedValue({ items: [pending], total: 21 })
    const wrapper = render()
    await flushPromises()
    await wrapper.get('[data-testid="reviews-next"]').trigger('click')
    await flushPromises()
    expect(getReviews).toHaveBeenLastCalledWith({ status: 'pending', limit: 20, offset: 20 })
    await wrapper.get('[data-testid="reviews-filter"]').setValue('resolved')
    await flushPromises()
    expect(getReviews).toHaveBeenLastCalledWith({ status: 'resolved', limit: 20, offset: 0 })
  })

  it('returns to the previous page when the last pending record on a page is resolved', async () => {
    getReviews.mockResolvedValue({ items: [pending], total: 21 })
    const wrapper = render()
    await flushPromises()
    await wrapper.get('[data-testid="reviews-next"]').trigger('click')
    await flushPromises()
    await wrapper.get('[data-testid="review-open"]').trigger('click')
    await wrapper.get('[data-testid="review-note"]').setValue('No charge')
    getReviews.mockResolvedValueOnce({ items: [], total: 20 }).mockResolvedValue({ items: [pending], total: 20 })
    await wrapper.get('[data-testid="review-confirm"]').trigger('click')
    await flushPromises()
    expect(getReviews).toHaveBeenLastCalledWith({ status: 'pending', limit: 20, offset: 0 })
    expect(wrapper.get('[data-testid="reviews-previous"]').attributes('disabled')).toBeDefined()
  })

  it('offers retry on a failed load and then displays an empty list', async () => {
    getReviews.mockRejectedValueOnce(new Error('offline')).mockResolvedValue({ items: [], total: 0 })
    const wrapper = render()
    await flushPromises()
    expect(wrapper.get('[role="alert"]').text()).toContain('loadFailed')
    await wrapper.get('[data-testid="reviews-retry"]').trigger('click')
    await flushPromises()
    expect(wrapper.find('[role="alert"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="reviews-empty"]').exists()).toBe(true)
  })
})
