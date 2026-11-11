import { describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import ProviderCard from '../ProviderCard.vue'

vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string) => key }) }))

describe('disabled provider type card', () => {
  it('keeps configuration and deletion available while disabling collection controls', async () => {
    const wrapper = mount(ProviderCard, {
      props: { provider: { id: 9, provider_key: 'easypay', name: 'Saved provider', config: {}, supported_types: ['alipay'], enabled: false, payment_mode: 'qrcode', refund_enabled: false, allow_user_refund: false, limits: '', sort_order: 0 }, enabled: false, availableTypes: [{ value: 'alipay', label: 'Alipay' }] },
    })
    const enableSwitch = wrapper.get('button[role="switch"]')
    expect(enableSwitch.attributes('disabled')).toBeDefined()
    await enableSwitch.trigger('click')
    expect(wrapper.emitted('toggleField')).toBeUndefined()
    for (const action of ['edit', 'delete'] as const) {
      const button = wrapper.findAll('button').find(node => node.text() === `common.${action}`)!
      expect(button.element.closest('.pointer-events-none')).toBeNull()
      await button.trigger('click')
      expect(wrapper.emitted(action)).toHaveLength(1)
    }
    expect(wrapper.text()).not.toContain('admin.settings.payment.billTestConnection')
    wrapper.unmount()
  })
})
