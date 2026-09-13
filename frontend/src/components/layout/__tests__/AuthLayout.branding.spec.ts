import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { enableAutoUnmount, mount } from '@vue/test-utils'
import { nextTick, reactive } from 'vue'

import AuthLayout from '../AuthLayout.vue'

const appStore = reactive({
  siteName: 'Test gateway',
  siteLogo: '',
  cachedPublicSettings: { site_subtitle: 'Unified model access' },
  publicSettingsLoaded: true,
  fetchPublicSettings: vi.fn().mockResolvedValue(null),
})

vi.mock('@/stores', () => ({
  useAppStore: () => appStore,
}))

enableAutoUnmount(afterEach)

describe('AuthLayout branding', () => {
  beforeEach(() => {
    appStore.siteName = 'Test gateway'
    appStore.siteLogo = ''
    appStore.cachedPublicSettings = { site_subtitle: 'Unified model access' }
    appStore.publicSettingsLoaded = true
    appStore.fetchPublicSettings.mockClear()
  })

  it('keeps the logo unframed while preserving its size and the auth form surface', () => {
    const wrapper = mount(AuthLayout, {
      slots: { default: '<form aria-label="Sign in"></form>' },
    })
    const mark = wrapper.get('picture').element.parentElement!
    const classes = [...mark.classList]

    expect(classes).toEqual(expect.arrayContaining(['mb-4', 'inline-flex', 'h-16', 'w-16', 'items-center', 'justify-center']))
    expect(classes.filter(name => /(?:^|:)(?:shadow(?:-|$)|rounded(?:-|$)|bg-)/.test(name))).toEqual([])
    expect(wrapper.get('picture img').attributes('src')).toBe('/xeno-alien-spin.svg?rev=20260914-round-head-2')
    expect(wrapper.get('.card-glass').classes()).toEqual(expect.arrayContaining(['rounded-2xl', 'shadow-glass']))
    expect(wrapper.find('form[aria-label="Sign in"]').exists()).toBe(true)
  })

  it.each(['', '   ', '/logo.svg', '/xeno-alien-emotions.svg', '/xeno-alien-spin.svg?rev=20260914'])(
    'uses the spin logo and reduced-motion still for a default or legacy logo: %j',
    (siteLogo) => {
      appStore.siteLogo = siteLogo
      const wrapper = mount(AuthLayout)

      expect(wrapper.get('picture img').attributes('src')).toBe('/xeno-alien-spin.svg?rev=20260914-round-head-2')
      expect(wrapper.get('picture source').attributes()).toMatchObject({
        media: '(prefers-reduced-motion: reduce)',
        srcset: '/xeno-alien-spin-still.svg',
      })
      expect(wrapper.get('h1').text()).toBe('Test gateway')
      expect(appStore.fetchPublicSettings).toHaveBeenCalledOnce()
    },
  )

  it.each([
    '/custom-brand.svg',
    'https://cdn.example.test/custom-brand.png',
    'data:image/svg+xml;base64,PHN2Zy8+',
  ])('preserves a configured custom logo without the built-in still source: %s', (siteLogo) => {
    appStore.siteLogo = siteLogo
    const wrapper = mount(AuthLayout)

    expect(wrapper.get('img').attributes('src')).toBe(siteLogo)
    expect(wrapper.find('source').exists()).toBe(false)
  })

  it.each([
    'javascript:alert(1)',
    'data:text/html;base64,PHNjcmlwdD4=',
    '//untrusted.example.test/logo.svg',
  ])('falls back to the site logo for an unsafe configured URL: %s', (siteLogo) => {
    appStore.siteLogo = siteLogo
    const wrapper = mount(AuthLayout)

    expect(wrapper.get('img').attributes('src')).toBe('/xeno-alien-spin.svg?rev=20260914-round-head-2')
    expect(wrapper.get('source').attributes('srcset')).toBe('/xeno-alien-spin-still.svg')
  })

  it('updates the rendered logo when a custom logo is set and cleared', async () => {
    const wrapper = mount(AuthLayout)
    expect(wrapper.get('img').attributes('src')).toBe('/xeno-alien-spin.svg?rev=20260914-round-head-2')

    appStore.siteLogo = '/custom-brand.svg'
    await nextTick()
    expect(wrapper.get('img').attributes('src')).toBe('/custom-brand.svg')
    expect(wrapper.find('source').exists()).toBe(false)

    appStore.siteLogo = ''
    await nextTick()
    expect(wrapper.get('img').attributes('src')).toBe('/xeno-alien-spin.svg?rev=20260914-round-head-2')
    expect(wrapper.get('source').attributes('srcset')).toBe('/xeno-alien-spin-still.svg')
  })

  it('waits for public settings before showing branding without hiding auth content', async () => {
    appStore.publicSettingsLoaded = false
    const wrapper = mount(AuthLayout, {
      slots: {
        default: '<form aria-label="Sign in"></form>',
        footer: '<a href="/register">Register</a>',
      },
    })

    expect(wrapper.find('img').exists()).toBe(false)
    expect(wrapper.find('picture').exists()).toBe(false)
    expect(wrapper.find('h1').exists()).toBe(false)
    expect(wrapper.find('form[aria-label="Sign in"]').exists()).toBe(true)
    expect(wrapper.get('a[href="/register"]').text()).toBe('Register')
    expect(appStore.fetchPublicSettings).toHaveBeenCalledOnce()

    appStore.siteName = 'Configured gateway'
    appStore.siteLogo = '/configured-brand.svg'
    appStore.publicSettingsLoaded = true
    await nextTick()

    expect(wrapper.get('img').attributes('src')).toBe('/configured-brand.svg')
    expect(wrapper.find('source').exists()).toBe(false)
    expect(wrapper.get('h1').text()).toBe('Configured gateway')
  })
})
