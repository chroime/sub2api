import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { reactive } from 'vue'
import { mount, RouterLinkStub } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import PlazaNavBar from '../PlazaNavBar.vue'

const { app, auth } = vi.hoisted(() => ({ app: vi.fn(), auth: vi.fn() }))
vi.mock('@/stores/app', () => ({ useAppStore: app }))
vi.mock('@/stores/auth', () => ({ useAuthStore: auth }))

function render() {
  return mount(PlazaNavBar, {
    global: {
      plugins: [createI18n({ legacy: false, locale: 'en', missingWarn: false, fallbackWarn: false, messages: {} })],
      stubs: { RouterLink: RouterLinkStub, LocaleSwitcher: true, Icon: true },
    },
  })
}

describe('model plaza public navigation', () => {
  beforeEach(() => {
    app.mockReturnValue({ cachedPublicSettings: { site_name: 'Test gateway', site_logo: '/logo.svg' } })
    auth.mockReturnValue({ isAuthenticated: false, isAdmin: false })
    document.documentElement.classList.remove('dark')
    localStorage.clear()
  })

  afterEach(() => {
    document.documentElement.classList.remove('dark')
    localStorage.clear()
  })

  it('uses the docs header styling and text-only navigation with shared language control', () => {
    const wrapper = render()
    expect(wrapper.get('header').classes()).toContain('public-site-header')
    const links = wrapper.findAllComponents(RouterLinkStub)
    expect(links.filter(link => link.props('to') === '/home')).toHaveLength(2)
    const docs = links.find(link => link.props('to') === '/docs')!
    expect(docs.exists()).toBe(true)
    expect(docs.find('icon-stub').exists()).toBe(false)
    expect(docs.classes()).toContain('public-nav-link')
    expect(wrapper.find('locale-switcher-stub').exists()).toBe(true)
    expect(links.at(-1)?.props('to')).toEqual({ path: '/login', query: { redirect: '/model-plaza' } })
    wrapper.unmount()
  })

  it.each([[false, '/dashboard'], [true, '/admin/dashboard']])('preserves the authenticated destination (admin: %s)', (isAdmin, target) => {
    auth.mockReturnValue({ isAuthenticated: true, isAdmin })
    const wrapper = render()
    expect(wrapper.findAllComponents(RouterLinkStub).at(-1)?.props('to')).toBe(target)
    wrapper.unmount()
  })

  it('updates branding when public settings arrive', async () => {
    const store = reactive<{ cachedPublicSettings: { site_name: string; site_logo: string } | null }>({ cachedPublicSettings: null })
    app.mockReturnValue(store)
    const wrapper = render()
    store.cachedPublicSettings = { site_name: 'Updated gateway', site_logo: '/custom-logo.png' }
    await wrapper.vm.$nextTick()
    expect(wrapper.get('img').attributes('alt')).toBe('Updated gateway')
    expect(wrapper.get('img').attributes('src')).toBe('/custom-logo.png')
    expect(wrapper.text()).toContain('Updated gateway')
    wrapper.unmount()
  })

})
