import { describe, expect, it, vi } from 'vitest'
import { START_LOCATION } from 'vue-router'

const appStore = vi.hoisted(() => ({ siteName: 'Sub2API', backendModeEnabled: false, cachedPublicSettings: null }))
vi.mock('@/stores/auth', () => ({ useAuthStore: () => ({ checkAuth: vi.fn(), isAuthenticated: false, isAdmin: false }) }))
vi.mock('@/stores/app', () => ({ useAppStore: () => appStore }))
vi.mock('@/views/public/PublicDocsView.vue', () => ({ default: { template: '<div />' } }))
vi.mock('@/views/auth/LoginView.vue', () => ({ default: { template: '<div />' } }))
vi.mock('@/stores/adminSettings', () => ({ useAdminSettingsStore: () => ({ customMenuItems: [] }) }))
vi.mock('@/composables/useNavigationLoading', () => ({ useNavigationLoadingState: () => ({ startNavigation: vi.fn(), endNavigation: vi.fn(), isLoading: { value: false } }) }))
vi.mock('@/composables/useRoutePrefetch', () => ({ useRoutePrefetch: () => ({ triggerPrefetch: vi.fn(), cancelPendingPrefetch: vi.fn(), resetPrefetchState: vi.fn() }) }))

describe('public documentation navigation', () => {
  it('registers documentation without authentication or admin requirements', async () => {
    const { default: router } = await import('@/router')
    const route = router.resolve('/docs')
    expect(route.name).toBe('PublicDocs')
    expect(route.meta.requiresAuth).toBe(false)
    expect(route.meta.requiresAdmin).not.toBe(true)
    expect(route.meta.titleKey).toBe('publicDocs.title')
  })

  it.each(['#channels', '#top'])('scrolls to the remaining homepage section %s', async (hash) => {
    const { default: router } = await import('@/router')
    const target = router.resolve(`/home${hash}`)
    const from = router.resolve('/docs')
    const scroll = await router.options.scrollBehavior?.(target, from, null)
    expect(scroll).toEqual({ el: hash, top: 24 })
    expect(await router.options.scrollBehavior?.(target, from, { left: 0, top: 350 })).toEqual({ left: 0, top: 350 })
  })

  it('does not try to scroll to the removed homepage pricing section', async () => {
    const { default: router } = await import('@/router')
    const target = router.resolve('/home#pricing')
    const from = router.resolve('/docs')
    expect(await router.options.scrollBehavior?.(target, from, null)).toEqual({ top: 0 })
  })

  it('keeps documentation public in backend-only mode', async () => {
    const { default: router } = await import('@/router')
    appStore.backendModeEnabled = true
    vi.spyOn(window, 'scrollTo').mockImplementation(() => {})
    await router.push('/docs')
    expect(router.currentRoute.value.path).toBe('/docs')
    appStore.backendModeEnabled = false
  })
})

describe('homepage scroll restoration', () => {
  it.each([
    ['/home', null],
    ['/home', { left: 0, top: 1050 }],
    ['/home?source=bookmark', { left: 0, top: 1050 }],
    ['/home/', { left: 0, top: 1050 }],
  ])('opens %s at the top on initial navigation regardless of saved scroll', async (path, savedPosition) => {
    const { default: router } = await import('@/router')
    const target = router.resolve(path)

    expect(await router.options.scrollBehavior?.(target, START_LOCATION, savedPosition)).toEqual({
      left: 0,
      top: 0,
      behavior: 'instant',
    })
  })

  it('preserves the homepage position on browser back and forward', async () => {
    const { default: router } = await import('@/router')
    const target = router.resolve('/home')
    const from = router.resolve('/model-plaza')

    expect(await router.options.scrollBehavior?.(target, from, { left: 0, top: 1050 })).toEqual({ left: 0, top: 1050 })
  })

  it('preserves homepage scroll on a browser history return that reloads the document', async () => {
    const { default: router } = await import('@/router')
    const getEntries = vi.spyOn(performance, 'getEntriesByType').mockReturnValue([
      { entryType: 'navigation', type: 'back_forward' } as PerformanceNavigationTiming,
    ])

    try {
      expect(await router.options.scrollBehavior?.(router.resolve('/home'), START_LOCATION, { left: 0, top: 1050 })).toEqual({ left: 0, top: 1050 })
    } finally {
      getEntries.mockRestore()
    }
  })

  it('opens the homepage at the top during ordinary navigation', async () => {
    const { default: router } = await import('@/router')

    expect(await router.options.scrollBehavior?.(router.resolve('/home'), router.resolve('/docs'), null)).toEqual({ top: 0 })
  })

  it('honors the homepage anchor on initial navigation with a saved scroll position', async () => {
    const { default: router } = await import('@/router')

    expect(await router.options.scrollBehavior?.(router.resolve('/home#top'), START_LOCATION, { left: 0, top: 1050 })).toEqual({ el: '#top', top: 24 })
  })

  it.each(['initial', 'back'])('keeps saved scroll restoration for other pages on %s navigation', async (navigation) => {
    const { default: router } = await import('@/router')
    const from = navigation === 'initial' ? START_LOCATION : router.resolve('/home')

    expect(await router.options.scrollBehavior?.(router.resolve('/docs'), from, { left: 0, top: 350 })).toEqual({ left: 0, top: 350 })
  })
})
