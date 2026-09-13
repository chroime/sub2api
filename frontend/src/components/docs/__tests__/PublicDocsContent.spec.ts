import { afterAll, describe, expect, it, vi } from 'vitest'
import { mount, RouterLinkStub } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import zh from '@/i18n/locales/zh/publicDocs'
import en from '@/i18n/locales/en/publicDocs'
import PublicDocsContent from '../PublicDocsContent.vue'

vi.hoisted(() => { vi.stubGlobal('__INTLIFY_JIT_COMPILATION__', true) })
afterAll(() => vi.unstubAllGlobals())

describe('public documentation empty state', () => {
  it.each([
    ['zh', '模型广场'],
    ['en', 'Model Plaza'],
  ])('links to the Model Plaza in %s', (locale, label) => {
    const wrapper = mount(PublicDocsContent, {
      props: { content: '' },
      global: {
        plugins: [createI18n({ legacy: false, locale, messages: { zh, en } })],
        stubs: { Icon: true, RouterLink: RouterLinkStub },
      },
    })
    const link = wrapper.getComponent(RouterLinkStub)
    expect(link.props('to')).toBe('/model-plaza')
    expect(link.text()).toBe(label)
    wrapper.unmount()
  })
})
