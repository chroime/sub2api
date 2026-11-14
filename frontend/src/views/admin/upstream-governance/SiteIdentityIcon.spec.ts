import { enableAutoUnmount, flushPromises, mount } from '@vue/test-utils'
import { afterEach, describe, expect, it, vi } from 'vitest'
import api, { type Site } from '@/api/admin/upstream-governance'
import SiteIdentityIcon from './SiteIdentityIcon.vue'

vi.mock('@/api/admin/upstream-governance', () => ({ default: { siteIcon: vi.fn() } }))
enableAutoUnmount(afterEach)
afterEach(() => vi.resetAllMocks())

const fixtureIcon = 'data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAAAEAAAAB'
const site: Site = { id: 401, version: 1, name: 'Fixture', platform: 'newapi', base_url: 'https://fixture.example', proxy_id: 47, enabled: false, interval_minutes: 15, has_credential: false, status: 'disconnected', last_error: '', last_sync_at: null }

describe('site identity icons', () => {
  it('loads custom branding through the authenticated backend even before authorization succeeds', async () => {
    vi.mocked(api.siteIcon).mockResolvedValue({ data_url: fixtureIcon })
    const wrapper = mount(SiteIdentityIcon, { props: { site } })
    await flushPromises()
    expect(api.siteIcon).toHaveBeenCalledWith(site.id)
    expect(wrapper.get('[data-test=site-favicon]').attributes('src')).toBe(fixtureIcon)
    expect(wrapper.get('[data-test=site-platform-logo]').attributes('alt')).toBe('New API')
    expect(wrapper.html()).not.toContain('https://fixture.example/favicon.ico')
  })

  it('falls back locally after an image failure and reloads when the site changes', async () => {
    vi.mocked(api.siteIcon).mockResolvedValue({ data_url: fixtureIcon })
    const wrapper = mount(SiteIdentityIcon, { props: { site: { ...site, id: 402 } } })
    await flushPromises()
    await wrapper.get('[data-test=site-favicon]').trigger('error')
    expect(wrapper.find('[data-test=site-icon-fallback]').exists()).toBe(true)
    await wrapper.setProps({ site: { ...site, id: 403, version: 2, base_url: 'https://changed.example' } })
    await flushPromises()
    expect(api.siteIcon).toHaveBeenLastCalledWith(403)
    expect(wrapper.find('[data-test=site-favicon]').exists()).toBe(true)
  })

  it('does not apply a late icon response after switching sites', async () => {
    let finish!: (icon: { data_url: string }) => void
    vi.mocked(api.siteIcon).mockReturnValueOnce(new Promise(resolve => { finish = resolve }))
    vi.mocked(api.siteIcon).mockResolvedValueOnce({ data_url: fixtureIcon })
    const wrapper = mount(SiteIdentityIcon, { props: { site: { ...site, id: 404 } } })
    await wrapper.setProps({ site: { ...site, id: 405, base_url: 'https://changed.example' } })
    await flushPromises()
    finish({ data_url: 'data:image/gif;base64,bGF0ZQ==' })
    await flushPromises()
    expect(wrapper.get('[data-test=site-favicon]').attributes('src')).toBe(fixtureIcon)
  })
})
