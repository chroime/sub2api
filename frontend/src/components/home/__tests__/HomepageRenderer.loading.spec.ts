import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount, RouterLinkStub, type VueWrapper } from '@vue/test-utils'
import HomepageRenderer from '../HomepageRenderer.vue'

const { pendingBrand, importStarted } = vi.hoisted(() => {
  let resolve!: () => void
  const promise = new Promise<void>(done => { resolve = done })
  return { pendingBrand: { promise, resolve }, importStarted: vi.fn() }
})
vi.mock('@/stores', () => ({ useAuthStore: () => ({ isAuthenticated: false, isAdmin: false }) }))
vi.mock('../BrandHome.vue', async () => {
  importStarted()
  await pendingBrand.promise
  const { h } = await import('vue')
  return { __esModule: true, default: { render: () => h('main', { 'data-testid': 'loaded-brand' }, 'Brand homepage') } }
})
vi.mock('../PublicHomeExperience.vue', () => { throw new Error('Homepage resource unavailable') })

const errors = vi.fn()
const wrappers: VueWrapper[] = []
function render(template: 'brand' | 'classic' | 'compact') {
  const wrapper = mount(HomepageRenderer, {
    props: { settings: { homepage_template: template, site_name: 'Test site' } },
    global: {
      stubs: { RouterLink: RouterLinkStub, LocaleSwitcher: true },
      config: { errorHandler: errors },
    },
  })
  wrappers.push(wrapper)
  return wrapper
}

describe('homepage template loading', () => {
  beforeEach(() => { vi.useFakeTimers(); errors.mockClear() })
  afterEach(() => { wrappers.splice(0).forEach(wrapper => wrapper.unmount()); vi.useRealTimers() })

  it('never displays another homepage while the selected brand resource is still loading', async () => {
    const wrapper = render('brand')
    try {
      await vi.waitFor(() => expect(importStarted).toHaveBeenCalled())
      await vi.advanceTimersByTimeAsync(1500)
      expect(wrapper.find('[data-testid="compact-home"]').exists()).toBe(false)
      expect(wrapper.find('[data-testid="loaded-brand"]').exists()).toBe(false)
      expect(wrapper.find('[aria-busy="true"]').exists()).toBe(true)
    } finally {
      pendingBrand.resolve()
      await flushPromises()
    }
    await vi.waitFor(() => expect(wrapper.html()).toContain('data-testid="loaded-brand"'))
    expect(errors).not.toHaveBeenCalled()
    expect(wrapper.find('[aria-busy="true"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="compact-home"]').exists()).toBe(false)
  })

  it('still provides compact fallback when a template resource actually fails', async () => {
    const wrapper = render('classic')
    await flushPromises()
    await vi.waitFor(() => expect(wrapper.find('[data-testid="compact-home"]').exists()).toBe(true))
    expect(wrapper.get('[role="alert"]').text()).toBeTruthy()
  })

  it('renders compact immediately when it is the saved selection', () => {
    const wrapper = render('compact')
    expect(wrapper.find('[data-testid="compact-home"]').exists()).toBe(true)
    expect(wrapper.find('[aria-busy="true"]').exists()).toBe(false)
  })
})
