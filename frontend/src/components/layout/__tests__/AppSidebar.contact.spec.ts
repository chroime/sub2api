import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { enableAutoUnmount, RouterLinkStub, shallowMount } from '@vue/test-utils'
import { nextTick, reactive, ref } from 'vue'

import AppSidebar from '../AppSidebar.vue'
import VersionBadge from '@/components/common/VersionBadge.vue'

const appStore = reactive({
  siteName: 'Test gateway',
  siteLogo: '',
  siteVersion: 'v0.2.4',
  contactInfo: '',
  publicSettingsLoaded: true,
  sidebarCollapsed: false,
  sidebarScrollTop: 0,
  mobileOpen: false,
  backendModeEnabled: false,
  cachedPublicSettings: null,
})
const authStore = reactive({ isAdmin: false, isSimpleMode: false })
const adminSettingsStore = {
  customMenuItems: [],
  fetch: vi.fn().mockResolvedValue(null),
}

vi.mock('@/stores', () => ({
  useAppStore: () => appStore,
  useAuthStore: () => authStore,
  useAdminSettingsStore: () => adminSettingsStore,
  useOnboardingStore: () => ({}),
}))
vi.mock('@/stores/app', () => ({ useAppStore: () => appStore }))
vi.mock('@/api/admin/system', () => ({
  performUpdate: vi.fn(),
  restartService: vi.fn(),
  getRollbackVersions: vi.fn(),
  rollback: vi.fn(),
}))
vi.mock('vue-i18n', async (importOriginal) => ({
  ...await importOriginal<typeof import('vue-i18n')>(),
  useI18n: () => ({ t: (key: string) => key }),
}))
vi.mock('vue-router', () => ({
  useRoute: () => ({ path: '/dashboard' }),
  useRouter: () => ({ push: vi.fn() }),
}))
vi.mock('@/composables/useBatchImageAccess', () => ({
  useBatchImageAccess: () => ({
    canUseBatchImage: ref(false),
    refreshBatchImageAccess: vi.fn().mockResolvedValue(false),
  }),
}))

enableAutoUnmount(afterEach)

function mountSidebar() {
  return shallowMount(AppSidebar, {
    global: { stubs: { RouterLink: RouterLinkStub } },
  })
}

describe('AppSidebar customer contact', () => {
  beforeEach(() => {
    appStore.contactInfo = 'QQ group: 421497493\nWeChat: 46656666'
    appStore.sidebarCollapsed = false
    authStore.isAdmin = false
    vi.clearAllMocks()
  })

  it('replaces the user version badge with the first configured contact line', () => {
    const wrapper = mountSidebar()

    expect(wrapper.get('.sidebar-brand-contact').text()).toBe('QQ group: 421497493')
    expect(wrapper.get('.sidebar-brand').text()).not.toContain('46656666')
    expect(wrapper.findComponent(VersionBadge).exists()).toBe(false)
  })

  it.each([
    ['QQ: 123\r\nWeChat: 456', 'QQ: 123'],
    ['QQ: 123\rWeChat: 456', 'QQ: 123'],
    [' \r\n\t\n  QQ: 123  \nWeChat: 456', 'QQ: 123'],
    ['421497493', '421497493'],
    ['QQ: 123; backup: 456 | support\nWeChat: 789', 'QQ: 123; backup: 456 | support'],
  ])('preserves the first nonempty physical line of %j', (raw, expected) => {
    appStore.contactInfo = raw
    const wrapper = mountSidebar()

    expect(wrapper.get('.sidebar-brand-contact').text()).toBe(expected)
  })

  it.each(['', ' \r\n\t '])('hides the user subtitle for empty contact info %j', (raw) => {
    appStore.contactInfo = raw
    const wrapper = mountSidebar()

    expect(wrapper.find('.sidebar-brand-contact').exists()).toBe(false)
    expect(wrapper.findComponent(VersionBadge).exists()).toBe(false)
  })

  it('keeps the existing version badge for administrators', () => {
    authStore.isAdmin = true
    const wrapper = mountSidebar()

    expect(wrapper.find('.sidebar-brand-contact').exists()).toBe(false)
    expect(wrapper.getComponent(VersionBadge).props('version')).toBe('v0.2.4')
  })

  it('updates and clears the line when public settings change', async () => {
    const wrapper = mountSidebar()

    appStore.contactInfo = 'Support: new-contact\nQQ: 123'
    await nextTick()
    expect(wrapper.get('.sidebar-brand-contact').text()).toBe('Support: new-contact')

    appStore.contactInfo = ''
    await nextTick()
    expect(wrapper.find('.sidebar-brand-contact').exists()).toBe(false)
    expect(wrapper.findComponent(VersionBadge).exists()).toBe(false)
  })

  it('keeps long contact text on one line and exposes the full value on hover', () => {
    const contact = `Support: ${'1234567890'.repeat(20)}`
    appStore.contactInfo = `${contact}\nSecond line`
    const wrapper = mountSidebar()
    const subtitle = wrapper.get('.sidebar-brand-contact')

    expect(subtitle.text()).toBe(contact)
    expect(subtitle.classes()).toContain('truncate')
    expect(subtitle.attributes('title')).toBe(contact)
  })

  it('renders configured markup as plain text', () => {
    appStore.contactInfo = '<img src=x onerror=alert(1)>\nOther contact'
    const wrapper = mountSidebar()
    const subtitle = wrapper.get('.sidebar-brand-contact')

    expect(subtitle.text()).toBe('<img src=x onerror=alert(1)>')
    expect(subtitle.find('img').exists()).toBe(false)
  })

  it('hides the contact together with the brand when the sidebar is collapsed', async () => {
    const wrapper = mountSidebar()

    appStore.sidebarCollapsed = true
    await nextTick()

    expect(wrapper.get('.sidebar-brand').attributes('aria-hidden')).toBe('true')
    expect(wrapper.get('.sidebar-brand').classes()).toContain('sidebar-brand-collapsed')
    expect(wrapper.get('.sidebar-brand-contact').text()).toBe('QQ group: 421497493')
  })
})
