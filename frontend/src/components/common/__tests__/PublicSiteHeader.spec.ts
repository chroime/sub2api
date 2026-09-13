import { describe, expect, it } from 'vitest'
import { mount, RouterLinkStub } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import PublicSiteHeader from '../PublicSiteHeader.vue'

describe('shared public site header', () => {
  it('renders dynamic branding, navigation and a common account action', async () => {
    const wrapper = mount(PublicSiteHeader, {
      props: { siteName: 'Gateway', siteLogo: '/logo.svg', subtitle: 'Subtitle', destination: '/login', authenticated: false },
      slots: { default: '<a class="public-nav-link" href="/docs">Docs</a>' },
      global: {
        plugins: [createI18n({ legacy: false, locale: 'en', missingWarn: false, fallbackWarn: false, messages: {} })],
        stubs: { RouterLink: RouterLinkStub, LocaleSwitcher: true, Icon: true },
      },
    })
    expect(wrapper.get('header').classes()).toContain('public-site-header')
    expect(wrapper.get('header').classes()).toContain('public-site-header-dark')
    expect(wrapper.get('img').attributes('alt')).toBe('Gateway')
    expect(wrapper.get('.public-nav-link').text()).toBe('Docs')
    expect(wrapper.get('.public-nav-link').find('svg').exists()).toBe(false)
    expect(wrapper.find('locale-switcher-stub').exists()).toBe(true)
    expect(wrapper.findAllComponents(RouterLinkStub).at(-1)?.props('to')).toBe('/login')
    await wrapper.setProps({ siteName: 'Updated' })
    expect(wrapper.get('img').attributes('alt')).toBe('Updated')
    expect(wrapper.find('.public-theme-toggle').exists()).toBe(true)
    await wrapper.setProps({ authenticated: true, destination: '/admin/dashboard' })
    expect(wrapper.get('.public-nav-cta').text()).toBe('home.dashboard')
    expect(wrapper.findAllComponents(RouterLinkStub).at(-1)?.props('to')).toBe('/admin/dashboard')
    wrapper.unmount()
  })
})
