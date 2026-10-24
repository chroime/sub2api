import { afterEach, describe, expect, it, vi } from 'vitest'
import { enableAutoUnmount, mount } from '@vue/test-utils'
import AmountInput from '../AmountInput.vue'

vi.mock('vue-i18n', () => ({
  useI18n: () => ({
    t: (key: string, params?: Record<string, unknown>) =>
      params && 'amount' in params ? `${key} ${String(params.amount)}` : key,
  }),
}))
enableAutoUnmount(afterEach)

describe('recharge amount selection', () => {
  it('offers only the supplied preset buttons without a custom amount field', async () => {
    const wrapper = mount(AmountInput, { props: { modelValue: null, amounts: [20, 100] } })

    expect(wrapper.find('input').exists()).toBe(false)
    expect(wrapper.findAll('button').map(button => button.text())).toEqual(['20', '100'])
    await wrapper.get('[data-testid="quick-amount-20"]').trigger('click')
    await wrapper.get('[data-testid="quick-amount-100"]').trigger('click')
    expect(wrapper.emitted('update:modelValue')).toEqual([[20], [100]])
  })

  it('has no fallback amounts when presets are missing or explicitly empty', () => {
    const missing = mount(AmountInput, { props: { modelValue: null } })
    const empty = mount(AmountInput, { props: { modelValue: null, amounts: [] } })

    expect(missing.findAll('button')).toHaveLength(0)
    expect(empty.findAll('button')).toHaveLength(0)
    expect(missing.find('input').exists()).toBe(false)
    expect(empty.find('input').exists()).toBe(false)
  })

  it('only displays finite positive presets within the supplied range', () => {
    const wrapper = mount(AmountInput, {
      props: { modelValue: null, amounts: [0, -10, NaN, Infinity, 10, 20, 50, 100], min: 20, max: 50 },
    })

    expect(wrapper.findAll('button').map(button => button.text())).toEqual(['20', '50'])
    const unlimited = mount(AmountInput, { props: { modelValue: null, amounts: [0, -10, NaN, Infinity, 10] } })
    expect(unlimited.findAll('button').map(button => button.text())).toEqual(['10'])
  })

  it('does not select or re-emit a programmatic amount outside the visible presets', async () => {
    const wrapper = mount(AmountInput, { props: { modelValue: 15, amounts: [20, 100] } })

    expect(wrapper.find('.border-primary-500').exists()).toBe(false)
    expect(wrapper.find('input').exists()).toBe(false)
    await wrapper.setProps({ modelValue: 20 })
    expect(wrapper.get('[data-testid="quick-amount-20"]').classes()).toContain('border-primary-500')
    await wrapper.setProps({ min: 50 })
    expect(wrapper.find('[data-testid="quick-amount-20"]').exists()).toBe(false)
    expect(wrapper.find('.border-primary-500').exists()).toBe(false)
    expect(wrapper.emitted('update:modelValue')).toBeUndefined()
  })
})

describe('recharge bonus hints on quick amounts', () => {
  const tiers = [
    { min_amount: 100, bonus_percent: 20 },
    { min_amount: 500, bonus_percent: 30 },
  ]

  it('renders no badge or second line when no tiers are configured', () => {
    const wrapper = mount(AmountInput, { props: { modelValue: null, amounts: [50, 100, 500] } })
    expect(wrapper.find('[data-testid="quick-amount-bonus-badge"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="quick-amount-credited"]').exists()).toBe(false)
  })

  it('bonus mode: price tag only on amounts that hit a tier, credited totals on every button', () => {
    const wrapper = mount(AmountInput, { props: { modelValue: null, amounts: [50, 100, 500], bonusTiers: tiers } })
    const below = wrapper.get('[data-testid="quick-amount-50"]')
    const first = wrapper.get('[data-testid="quick-amount-100"]')
    const second = wrapper.get('[data-testid="quick-amount-500"]')

    expect(below.find('[data-testid="quick-amount-bonus-badge"]').exists()).toBe(false)
    expect(below.get('[data-testid="quick-amount-credited"]').text()).toContain('$50.00')
    expect(first.get('[data-testid="quick-amount-bonus-badge"]').text()).toBe('+20%')
    expect(first.get('[data-testid="quick-amount-credited"]').text()).toContain('$120.00')
    expect(second.get('[data-testid="quick-amount-bonus-badge"]').text()).toBe('+30%')
    expect(second.get('[data-testid="quick-amount-credited"]').text()).toContain('$650.00')
  })

  it('bonus mode: matches tiers by the entered amount but credits by the multiplier', () => {
    const wrapper = mount(AmountInput, {
      props: { modelValue: null, amounts: [1000], bonusTiers: tiers, multiplier: 0.14 },
    })
    const button = wrapper.get('[data-testid="quick-amount-1000"]')
    expect(button.get('[data-testid="quick-amount-bonus-badge"]').text()).toBe('+30%')
    // 1000 × 0.14 = 140 base, +30% = 182
    expect(button.get('[data-testid="quick-amount-credited"]').text()).toContain('$182.00')
  })

  it('discount mode: tag reads N% OFF and the second line shows the discounted payment', () => {
    const wrapper = mount(AmountInput, {
      props: { modelValue: null, amounts: [50, 500], bonusTiers: tiers, bonusMode: 'discount', currency: 'USD' },
    })
    const below = wrapper.get('[data-testid="quick-amount-50"]')
    const hit = wrapper.get('[data-testid="quick-amount-500"]')
    expect(below.find('[data-testid="quick-amount-bonus-badge"]').exists()).toBe(false)
    expect(below.get('[data-testid="quick-amount-credited"]').text()).toContain('50.00')
    expect(hit.get('[data-testid="quick-amount-bonus-badge"]').text()).toBe('30% OFF')
    // 500 × (1 − 30%) = 350
    expect(hit.get('[data-testid="quick-amount-credited"]').text()).toContain('350.00')
  })

  it('shows the server-rounded discount price at a half-cent boundary', () => {
    const wrapper = mount(AmountInput, {
      props: {
        modelValue: null,
        amounts: [10],
        bonusTiers: [{ min_amount: 10, bonus_percent: 0.85 }],
        bonusMode: 'discount',
        currency: 'USD',
      },
    })

    expect(wrapper.get('[data-testid="quick-amount-credited"]').text()).toContain('9.92')
  })
})
