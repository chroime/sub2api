import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { mount, flushPromises, type VueWrapper } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import LocaleSwitcher from '../LocaleSwitcher.vue'
import { availableLocales, setLocale } from '@/i18n'

vi.mock('@/i18n', async (importOriginal) => ({
  ...await importOriginal<typeof import('@/i18n')>(),
  setLocale: vi.fn(),
}))

let wrapper: VueWrapper | undefined

function render(locale: 'en' | 'zh' = 'zh') {
  const i18n = createI18n({ legacy: false, locale, messages: { en: {}, zh: {} } })
  wrapper = mount(LocaleSwitcher, { global: { plugins: [i18n] } })
  return { wrapper, i18n }
}

describe('LocaleSwitcher flags', () => {
  beforeEach(() => vi.mocked(setLocale).mockReset())
  afterEach(() => wrapper?.unmount())

  it.each([
    ['en', '/flags/us.png'],
    ['zh', '/flags/cn.png'],
  ] as const)('renders a local flag image for the current %s locale', (locale, src) => {
    const { wrapper } = render(locale)
    const trigger = wrapper.get('button')
    expect(trigger.findAll('img')).toHaveLength(1)
    expect(trigger.get('img').attributes('src')).toBe(src)
    expect(trigger.attributes('aria-label')).toBe(availableLocales.find(item => item.code === locale)?.name)
  })

  it('pairs every menu language with its own fixed-size flag image', async () => {
    const { wrapper } = render()
    await wrapper.get('button').trigger('click')
    for (const [code, src] of [['en', '/flags/us.png'], ['zh', '/flags/cn.png']]) {
      const option = wrapper.get(`button[lang="${code}"]`)
      const flag = option.get('img')
      expect(flag.attributes('src')).toBe(src)
      expect(flag.attributes('width')).toBe('24')
      expect(flag.attributes('height')).toBe('18')
      expect(option.text()).toContain(availableLocales.find(item => item.code === code)?.name)
    }
  })

  it('updates the trigger flag when a different language is selected', async () => {
    const { wrapper, i18n } = render()
    vi.mocked(setLocale).mockImplementation(async (code) => {
      if (code === 'en' || code === 'zh') i18n.global.locale.value = code
    })
    await wrapper.get('button').trigger('click')
    await wrapper.get('button[lang="en"]').trigger('click')
    await flushPromises()
    expect(setLocale).toHaveBeenCalledOnce()
    expect(setLocale).toHaveBeenCalledWith('en')
    expect(wrapper.get('button img').attributes('src')).toBe('/flags/us.png')
    expect(wrapper.get('button').attributes('aria-expanded')).toBe('false')
  })
})
