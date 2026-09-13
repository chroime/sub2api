import { afterEach, describe, expect, it, vi } from 'vitest'
import { mount, type VueWrapper } from '@vue/test-utils'
import { parse } from 'vue/compiler-sfc'
import PublicHomeFooter from '../PublicHomeFooter.vue'
import source from '../PublicHomeFooter.vue?raw'

vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string) => key }) }))

let wrapper: VueWrapper | undefined
let host: HTMLDivElement | undefined
let style: HTMLStyleElement | undefined

afterEach(() => {
  wrapper?.unmount()
  host?.remove()
  style?.remove()
})

describe('PublicHomeFooter logo surface', () => {
  it.each(['dark', 'light'])('keeps the %s logo transparent without changing its size or alignment', (theme) => {
    const { descriptor } = parse(source)
    style = document.createElement('style')
    style.textContent = descriptor.styles.map(block => block.content).join('\n')
    document.head.append(style)
    host = document.createElement('div')
    host.className = theme === 'light' ? 'public-home-light' : ''
    document.body.append(host)
    wrapper = mount(PublicHomeFooter, {
      attachTo: host,
      props: { siteName: 'XenoAI', siteLogo: '/xeno-alien-spin.svg' },
      global: { stubs: { Icon: true } },
    })

    const mark = getComputedStyle(wrapper.get('.public-footer-brand-mark').element)
    const icon = getComputedStyle(wrapper.get('.public-footer-brand-icon').element)
    expect(mark.backgroundColor).toBe('rgba(0, 0, 0, 0)')
    expect(mark.backgroundImage || 'none').toBe('none')
    expect(mark.width).toBe('40px')
    expect(mark.height).toBe('40px')
    expect(mark.placeItems).toBe('center')
    expect(icon.width).toBe('32px')
    expect(icon.height).toBe('32px')
    expect(wrapper.get('.public-footer-brand-icon img').attributes('src')).toBe('/xeno-alien-spin.svg?rev=20260914-round-head-2')
  })
})
