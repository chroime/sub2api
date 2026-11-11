import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import AlipayBillRulesEditor from '../AlipayBillRulesEditor.vue'

vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string) => key }) }))
const { decodeImage } = vi.hoisted(() => ({ decodeImage: vi.fn() }))
vi.mock('../alipayBillRules', async importOriginal => ({
  ...await importOriginal<typeof import('../alipayBillRules')>(),
  decodeAlipayQRImage: decodeImage,
}))

const initialRule = { id: 'ten', amount: '10.00', qr_url: '', enabled: true, sort_order: 0 }

function mountEditor() {
  const wrapper = mount(AlipayBillRulesEditor, { props: { modelValue: JSON.stringify([initialRule]) } })
  wrapper.setProps({ 'onUpdate:modelValue': (value: string) => wrapper.setProps({ modelValue: value }) })
  return wrapper
}

describe('Alipay fixed QR editor', () => {
  beforeEach(() => decodeImage.mockReset())

  it('keeps the paste link fallback usable when an uploaded image cannot be decoded', async () => {
    decodeImage.mockResolvedValue(null)
    const wrapper = mountEditor()
    const fileInput = wrapper.get('input[type=file]')
    Object.defineProperty(fileInput.element, 'files', { value: [new File(['bad'], 'code.png', { type: 'image/png' })], configurable: true })
    await fileInput.trigger('change')
    await flushPromises()
    expect(wrapper.text()).toContain('admin.settings.payment.billQRDecodeFailed')
    await wrapper.get('input[type=url]').setValue('https://qr.alipay.com/ten')
    const events = wrapper.emitted('update:modelValue')!
    expect(JSON.parse(events[events.length - 1][0] as string)[0]).toEqual({ ...initialRule, qr_url: 'https://qr.alipay.com/ten' })
    wrapper.unmount()
  })

  it('fills only the decoded URL and leaves the bound amount for explicit verification', async () => {
    decodeImage.mockResolvedValue('https://qr.alipay.com/ten')
    const wrapper = mountEditor()
    const input = wrapper.get('input[type=file]')
    Object.defineProperty(input.element, 'files', { value: [new File(['qr'], 'code.png', { type: 'image/png' })], configurable: true })
    await input.trigger('change')
    await flushPromises()
    expect(JSON.parse(wrapper.emitted('update:modelValue')![0][0] as string)[0]).toEqual({ ...initialRule, qr_url: 'https://qr.alipay.com/ten' })
    expect(wrapper.text()).toContain('admin.settings.payment.billQRDecoded')
    wrapper.unmount()
  })
})
