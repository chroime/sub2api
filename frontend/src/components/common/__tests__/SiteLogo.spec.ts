import { describe, expect, it } from 'vitest'
import { mount } from '@vue/test-utils'
import SiteLogo from '../SiteLogo.vue'

describe('SiteLogo', () => {
  it.each([undefined, '', '  ', '/logo.svg', '/xeno-alien-emotions.svg', '/xeno-alien-spin.svg', '/xeno-alien-spin.svg?rev=20260914', '/xeno-alien-spin.svg?rev=20260914-round-head-2', 'javascript:alert(1)', '//example.com/logo.svg'])(
    'renders the approved mascot for a default or unsafe logo %s',
    (src) => {
      const wrapper = mount(SiteLogo, { props: { src, alt: 'Gateway' } })

      expect(wrapper.element.tagName).toBe('PICTURE')
      expect(wrapper.get('img').attributes('src')).toBe('/xeno-alien-spin.svg?rev=20260914-round-head-2')
      expect(wrapper.get('img').attributes('alt')).toBe('Gateway')
      expect(wrapper.get('source').attributes('media')).toBe('(prefers-reduced-motion: reduce)')
      expect(wrapper.get('source').attributes('srcset')).toBe('/xeno-alien-spin-still.svg')
      expect(wrapper.get('source').attributes('type')).toBe('image/svg+xml')
      wrapper.unmount()
    },
  )

  it.each(['/custom-logo.png', '/custom/xeno-alien-spin.svg?rev=old', 'https://example.com/logo.svg', 'https://example.com/xeno-alien-spin.svg?rev=old', 'data:image/svg+xml;base64,PHN2Zy8+'])(
    'preserves configured logos without substituting the default static image: %s',
    (src) => {
      const wrapper = mount(SiteLogo, { props: { src, alt: 'Custom gateway' } })

      expect(wrapper.get('img').attributes('src')).toBe(src)
      expect(wrapper.find('source').exists()).toBe(false)
      wrapper.unmount()
    },
  )

  it('updates a custom logo and restores reduced-motion fallback after clearing it', async () => {
    const wrapper = mount(SiteLogo, { props: { src: '/custom-logo.png', alt: 'Gateway' } })

    await wrapper.setProps({ src: '' })

    expect(wrapper.get('img').attributes('src')).toBe('/xeno-alien-spin.svg?rev=20260914-round-head-2')
    expect(wrapper.get('source').attributes('srcset')).toBe('/xeno-alien-spin-still.svg')
    wrapper.unmount()
  })

  it('keeps caller dimensions on the picture and intrinsic image dimensions stable', () => {
    const wrapper = mount(SiteLogo, {
      props: { alt: 'Gateway', width: 80, height: 96 },
      attrs: { class: 'h-24 w-20', style: 'margin-top: 4px' },
    })

    expect(wrapper.classes()).toContain('h-24')
    expect(wrapper.classes()).toContain('w-20')
    expect(wrapper.attributes('style')).toContain('margin-top: 4px')
    expect(wrapper.get('img').attributes('width')).toBe('80')
    expect(wrapper.get('img').attributes('height')).toBe('96')
    wrapper.unmount()
  })
})
