import { describe, expect, it, vi } from 'vitest'

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

  it('keeps documentation public in backend-only mode', async () => {
    const { default: router } = await import('@/router')
    appStore.backendModeEnabled = true
    vi.spyOn(window, 'scrollTo').mockImplementation(() => {})
    await router.push('/docs')
    expect(router.currentRoute.value.path).toBe('/docs')
    appStore.backendModeEnabled = false
  })
})
