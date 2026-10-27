import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount, RouterLinkStub, type VueWrapper } from '@vue/test-utils'
import HomepageRenderer from '../HomepageRenderer.vue'
import type { HomepageTemplate } from '@/utils/homepageTemplate'

const { load, auth } = vi.hoisted(() => ({ load: vi.fn(), auth: { isAuthenticated: false, isAdmin: false } }))
vi.mock('@/stores', () => ({ useAuthStore: () => auth }))
vi.mock('@/composables/usePublicPlatformHome', () => ({ usePublicPlatformHome: () => ({ load, pricingRows: { value: [] } }) }))

const wrappers: VueWrapper[] = []
describe('runtime homepage templates', () => {
  beforeEach(() => { load.mockClear(); auth.isAuthenticated = false; auth.isAdmin = false })
  afterEach(() => { wrappers.splice(0).forEach(wrapper => wrapper.unmount()) })

  it.each([
    ['classic', 'home-experience'], ['brand', 'brand-home'], ['compact', 'compact-home'], ['custom', 'custom-home'],
  ] as const)('renders %s despite retained legacy flags, loading pricing only for brand', async (template, marker) => {
    const wrapper = mount(HomepageRenderer, {
      props: { settings: { homepage_template: template, home_content: '<h1>Custom</h1>', compact_home_enabled: true, site_name: 'Preview brand' } },
      global: { stubs: { RouterLink: RouterLinkStub, LocaleSwitcher: true, PublicHomeStoryB2: true, PublicHomeB2Integration: true } },
    })
    wrappers.push(wrapper)
    await flushPromises()
    await vi.waitFor(() => expect(wrapper.find(`[data-testid="${marker}"]`).exists()).toBe(true))
    expect(load).toHaveBeenCalledTimes(template === 'brand' ? 1 : 0)
  })

  it('switches modes without remounting the page and preserves shared permission rules', async () => {
    const settings = { homepage_template: 'compact' as HomepageTemplate, site_name: 'Draft', model_plaza_enabled: true, model_plaza_require_auth: true }
    const wrapper = mount(HomepageRenderer, { props: { settings }, global: { stubs: { RouterLink: RouterLinkStub, LocaleSwitcher: true } } })
    wrappers.push(wrapper)
    expect(wrapper.findAllComponents(RouterLinkStub).some(link => link.props('to') === '/model-plaza')).toBe(false)
    await wrapper.setProps({ settings: { ...settings, homepage_template: 'classic', model_plaza_require_auth: false } })
    await flushPromises()
    await vi.waitFor(() => expect(wrapper.find('[data-testid="home-experience"]').exists()).toBe(true))
    expect(wrapper.get('h1').text()).toBe('Draft')
    expect(wrapper.findAllComponents(RouterLinkStub).some(link => link.props('to') === '/model-plaza')).toBe(true)
    await wrapper.setProps({ settings: { ...settings, homepage_template: 'custom', home_content: '' } })
    await flushPromises()
    await vi.waitFor(() => expect(wrapper.find('[data-testid="compact-home"]').exists()).toBe(true))
    expect(load).not.toHaveBeenCalled()
  })
})
