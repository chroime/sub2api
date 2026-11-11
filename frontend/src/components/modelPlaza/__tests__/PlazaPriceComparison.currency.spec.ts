import { describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import PlazaPriceComparisonCell from '../PlazaPriceComparison.vue'
import type { PlazaPriceComparison } from '../plazaPriceComparison'

vi.mock('vue-i18n', () => ({
  useI18n: () => ({ t: (key: string, params?: Record<string, unknown>) => params ? `${key}:${JSON.stringify(params)}` : key })
}))

function price(overrides: Partial<PlazaPriceComparison> = {}): PlazaPriceComparison {
  return { key: 'input', label: '', reference: 3, site: 2.5, currency: 'USD', referenceKind: 'official', comparable: true, billingMode: 'token', ...overrides }
}

describe('plaza prices compared in the customer payment currency', () => {
  it('compares official USD at 6.7 CNY with site credits funded at one CNY', () => {
    const wrapper = mount(PlazaPriceComparisonCell, { props: { price: price() } })
    expect(wrapper.get('[data-reference-price]').text()).toBe('$3.00')
    expect(wrapper.get('[data-reference-cny]').text()).toContain('¥20.10')
    expect(wrapper.get('[data-site-price]').text()).toBe('¥2.50')
    expect(wrapper.get('[data-site-credit]').text()).toContain('$2.50')
    expect(wrapper.get('[data-price-difference]').text()).toContain('¥17.60')
    expect(wrapper.get('[data-price-difference]').text()).toContain('87.56%')
    expect(wrapper.get('[data-price-difference]').text()).not.toContain('16.66%')
  })

  it('does not convert CNY model prices for a second time', () => {
    const wrapper = mount(PlazaPriceComparisonCell, { props: { price: price({ currency: 'CNY', referenceKind: 'configured', site: 1.5 }) } })
    expect(wrapper.get('[data-reference-price]').text()).toBe('¥3.00')
    expect(wrapper.get('[data-site-price]').text()).toBe('¥1.50')
    expect(wrapper.get('[data-price-difference]').text()).toContain('¥1.50')
    expect(wrapper.get('[data-price-difference]').text()).toContain('50%')
    expect(wrapper.find('[data-reference-cny]').exists()).toBe(false)
    expect(wrapper.find('[data-site-credit]').exists()).toBe(false)
  })

  it('recognizes equal and higher cash prices after converting the official price', async () => {
    const wrapper = mount(PlazaPriceComparisonCell, { props: { price: price({ reference: 1, site: 6.7 }) } })
    expect(wrapper.get('[data-comparison]').attributes('data-comparison')).toBe('equal')
    await wrapper.setProps({ price: price({ reference: 1, site: 7 }) })
    expect(wrapper.get('[data-comparison]').attributes('data-comparison')).toBe('higher')
    expect(wrapper.get('[data-price-difference]').text()).toContain('¥0.30')
    expect(wrapper.get('[data-price-difference]').text()).toContain('4.47%')
  })

  it('does not invent discounts when the reference or billing basis is unavailable', () => {
    const missing = mount(PlazaPriceComparisonCell, { props: { price: price({ reference: null, comparable: false }) } })
    expect(missing.get('[data-site-price]').text()).toBe('¥2.50')
    expect(missing.find('[data-price-difference]').exists()).toBe(false)
    const different = mount(PlazaPriceComparisonCell, { props: { price: price({ comparable: false }) } })
    expect(different.get('[data-reference-cny]').text()).toContain('¥20.10')
    expect(different.find('[data-price-difference]').exists()).toBe(false)
  })

  it('uses the same per-second unit on both sides of a media comparison', () => {
    const wrapper = mount(PlazaPriceComparisonCell, { props: { price: price({ reference: 0.1, site: 0.04, billingMode: 'video', label: '480P' }), unit: '/秒' } })
    expect(wrapper.get('[data-reference-price]').text()).toBe('$0.10/秒')
    expect(wrapper.get('[data-reference-cny]').text()).toContain('¥0.67/秒')
    expect(wrapper.get('[data-site-price]').text()).toBe('¥0.04/秒')
    expect(wrapper.get('[data-price-difference]').text()).toContain('¥0.63')
    expect(wrapper.get('[data-price-difference]').text()).toContain('94.02%')
  })
})
