import { describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'

import AmountInput from '../AmountInput.vue'

vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return {
    ...actual,
    useI18n: () => ({
      t: (key: string) => ({
        'payment.quickAmounts': 'Quick Amounts',
      }[key] ?? key),
    }),
  }
})

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
})
