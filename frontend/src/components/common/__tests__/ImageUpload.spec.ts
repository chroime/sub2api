import { mount, flushPromises } from '@vue/test-utils'
import { describe, expect, it, vi } from 'vitest'
import ImageUpload from '../ImageUpload.vue'

vi.mock('vue-i18n', () => ({
  useI18n: () => ({ t: (key: string, values?: Record<string, unknown>) => values ? `${key}:${values.max}` : key }),
}))

async function upload(size: number, maxSize?: number) {
  const wrapper = mount(ImageUpload, {
    props: { modelValue: '', ...(maxSize ? { maxSize } : {}) },
    global: { stubs: { Icon: true } },
  })
  const content = '<svg xmlns="http://www.w3.org/2000/svg"><animate attributeName="opacity" values="0;1" dur="12s"/></svg>'
  const file = new File([content.padEnd(size, ' ')], 'animated-logo.svg', { type: 'image/svg+xml' })
  const input = wrapper.get('input[type="file"]')
  Object.defineProperty(input.element, 'files', { value: [file] })
  await input.trigger('change')
  await flushPromises()
  return wrapper
}

describe('ImageUpload size boundaries', () => {
  it('accepts the original animated SVG with the site logo limit and preserves its animation', async () => {
    const wrapper = await upload(695030, 1024 * 1024)
    await vi.waitFor(() => expect(wrapper.emitted('update:modelValue')).toHaveLength(1))
    const value = wrapper.emitted('update:modelValue')![0][0] as string
    expect(value).toMatch(/^data:image\/svg\+xml;base64,/)
    expect(atob(value.split(',')[1])).toContain('<animate')
    wrapper.unmount()
  })

  it('accepts exactly 1 MiB but rejects a byte over the site logo limit', async () => {
    const accepted = await upload(1024 * 1024, 1024 * 1024)
    await vi.waitFor(() => expect(accepted.emitted('update:modelValue')).toHaveLength(1))
    accepted.unmount()
    const rejected = await upload(1024 * 1024 + 1, 1024 * 1024)
    expect(rejected.emitted('update:modelValue')).toBeUndefined()
    expect(rejected.text()).toContain('common.fileTooLargeKb:1024')
    rejected.unmount()
  })

  it('retains the shared 300 KiB default for unrelated uploaders', async () => {
    const wrapper = await upload(300 * 1024 + 1)
    expect(wrapper.emitted('update:modelValue')).toBeUndefined()
    expect(wrapper.text()).toContain('common.fileTooLargeKb:300')
    wrapper.unmount()
  })
})

describe('ImageUpload ICO compatibility', () => {
  async function uploadIco(type: string, content: BlobPart, allowIco = true) {
    const wrapper = mount(ImageUpload, {
      props: { modelValue: '', allowIco },
      global: { stubs: { Icon: true } },
    })
    const input = wrapper.get('input[type="file"]')
    Object.defineProperty(input.element, 'files', { value: [new File([content], 'favicon.ico', { type })] })
    await input.trigger('change')
    await flushPromises()
    return wrapper
  }

  const icon = new Uint8Array(26)
  icon.set([0, 0, 1, 0, 1, 0, 1, 1, 0, 0, 1, 0, 32, 0, 4, 0, 0, 0, 22, 0, 0, 0, 0, 0, 0, 0])

  it.each(['', 'application/octet-stream'])('accepts a valid ICO without an image MIME type: %s', async (type) => {
    const wrapper = await uploadIco(type, icon)
    await vi.waitFor(() => expect(wrapper.emitted('update:modelValue')).toHaveLength(1))
    const value = wrapper.emitted('update:modelValue')![0][0] as string
    expect(value).toMatch(/^data:image\/x-icon;base64,/)
    expect(atob(value.split(',')[1]).length).toBe(icon.length)
    expect(wrapper.get('input').attributes('accept')).toContain('.ico')
    wrapper.unmount()
  })

  it('rejects a renamed non-image file rather than trusting the ico extension', async () => {
    const wrapper = await uploadIco('', '<script>alert(1)</script>')
    await vi.waitFor(() => expect(wrapper.text()).toContain('common.selectImageFile'))
    expect(wrapper.emitted('update:modelValue')).toBeUndefined()
    wrapper.unmount()
  })

  it('does not expand acceptance for unrelated uploaders', async () => {
    const wrapper = await uploadIco('', icon, false)
    expect(wrapper.emitted('update:modelValue')).toBeUndefined()
    expect(wrapper.text()).toContain('common.selectImageFile')
    expect(wrapper.get('input').attributes('accept')).toBe('image/*')
    wrapper.unmount()
  })
})
