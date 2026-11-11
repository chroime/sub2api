import { describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import { ref } from 'vue'
import PaymentProviderList from '../PaymentProviderList.vue'
import ProviderCard from '../ProviderCard.vue'
import type { ProviderInstance } from '@/types/payment'

vi.mock('vue-i18n', async importOriginal => ({ ...await importOriginal<typeof import('vue-i18n')>(), useI18n: () => ({ t: (key: string) => key, locale: ref('en') }) }))

const providers: ProviderInstance[] = ['alipay', 'easypay', 'alipay_bill'].map((key, index) => ({ id: index + 1, name: key, provider_key: key, config: {}, supported_types: ['alipay'], enabled: true, payment_mode: 'qrcode', refund_enabled: false, allow_user_refund: false, limits: '', sort_order: index }))

describe('Alipay source selector', () => {
  it('allows enabled providers to coexist and emits one explicit source without toggling provider enablement', async () => {
    const wrapper = mount(PaymentProviderList, {
      props: { providers, loading: false, canCreate: true, enabledPaymentTypes: ['alipay', 'easypay', 'alipay_bill'], allPaymentTypes: [{ value: 'alipay', label: 'Alipay' }], redirectLabel: 'Redirect', alipaySource: 'official_alipay' },
      global: { stubs: { VueDraggable: { template: '<div><slot /></div>' }, ProviderCard: true, teleport: true, transition: false } },
    })
    const trigger = wrapper.get('button[aria-haspopup=true]')
    expect(trigger.text()).toContain('Official Alipay')
    await trigger.trigger('click')
    const options = wrapper.findAll('[role=option]')
    expect(options.filter(option => option.attributes('aria-selected') === 'true')).toHaveLength(1)
    const bill = options.find(option => option.text().includes('Built-in EasyPay'))!
    expect(bill.attributes('aria-disabled')).toBe('false')
    await bill.trigger('click')
    expect(wrapper.emitted('selectAlipaySource')).toEqual([['alipay_bill']])
    expect(wrapper.emitted('toggleField')).toBeUndefined()
    await wrapper.setProps({ alipaySource: 'alipay_bill' })
    expect(trigger.text()).toContain('Built-in EasyPay')
    expect(wrapper.findAllComponents(ProviderCard).map(card => card.props('provider').provider_key)).toEqual(['alipay', 'easypay'])
    wrapper.unmount()
  })
})
