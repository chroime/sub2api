import { afterEach, describe, expect, it, vi } from 'vitest'
import { enableAutoUnmount, mount } from '@vue/test-utils'

import AmountInput from '../AmountInput.vue'

vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return {
    ...actual,
    useI18n: () => ({
      t: (key: string) => ({
        'payment.quickAmounts': 'Quick Amounts',
        'payment.customAmount': 'Custom Amount',
      }[key] ?? key),
    }),
  }
})

enableAutoUnmount(afterEach)

describe('AmountInput', () => {
  it('renders only fixed amount choices and no custom amount entry', () => {
    const wrapper = mount(AmountInput, {
      props: {
        modelValue: null,
        amounts: [10, 20, 50],
      },
    })

    expect(wrapper.text()).toContain('Quick Amounts')
    expect(wrapper.text()).toContain('10')
    expect(wrapper.text()).toContain('20')
    expect(wrapper.text()).toContain('50')
    expect(wrapper.text()).not.toContain('Custom Amount')
    expect(wrapper.find('input').exists()).toBe(false)
  })

  it('still emits selected fixed amount values', async () => {
    const wrapper = mount(AmountInput, {
      props: {
        modelValue: null,
        amounts: [10, 20, 50],
      },
    })

    await wrapper.findAll('button')[1].trigger('click')

    expect(wrapper.emitted('update:modelValue')).toEqual([[20]])
  })

  it.each([
    { min: 20, max: 100, expected: ['20', '50', '100'] },
    { min: 0, max: 20, expected: ['10', '20'] },
    { min: 100, max: 0, expected: ['100', '200'] },
  ])('filters fixed amounts with min=$min and max=$max', ({ min, max, expected }) => {
    const wrapper = mount(AmountInput, {
      props: { modelValue: null, amounts: [10, 20, 50, 100, 200], min, max },
    })

    expect(wrapper.findAll('button').map((button) => button.text())).toEqual(expected)
    expect(wrapper.find('input').exists()).toBe(false)
  })
})

function mountInput(value: number | null = null) {
  return mount(AmountInput, { props: { modelValue: value, allowCustomAmount: true } })
}

describe('recharge amount input', () => {
  it.each(['10abc', '10.555', '-10', '1e2'])('restores the accepted amount after rejecting %s', async (value) => {
    const wrapper = mountInput(10)
    const input = wrapper.get('input')
    await input.setValue(value)
    expect((input.element as HTMLInputElement).value).toBe('10')
    expect(wrapper.emitted('update:modelValue')).toBeUndefined()
  })

  it('restores the last typed amount rather than a stale prop', async () => {
    const wrapper = mountInput()
    const input = wrapper.get('input')
    await input.setValue('12.50')
    await input.setValue('12.500')
    expect((input.element as HTMLInputElement).value).toBe('12.50')
    expect(wrapper.emitted('update:modelValue')).toEqual([[12.5]])
  })

  it('restores the most recently selected fixed amount after invalid custom input', async () => {
    const wrapper = mountInput(10)
    await wrapper.findAll('button')[1].trigger('click')
    const input = wrapper.get('input')
    await input.setValue('20.999')

    expect((input.element as HTMLInputElement).value).toBe('20')
    expect(wrapper.emitted('update:modelValue')).toEqual([[20]])
  })

  it('preserves decimal editing and allows clearing the amount', async () => {
    const wrapper = mountInput()
    const input = wrapper.get('input')
    for (const value of ['0', '0.', '0.5', '0.50', '']) await input.setValue(value)
    expect(wrapper.emitted('update:modelValue')).toEqual([[null], [null], [0.5], [0.5], [null]])
    expect((input.element as HTMLInputElement).value).toBe('')
  })
})
