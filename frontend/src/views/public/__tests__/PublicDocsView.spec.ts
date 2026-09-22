import { afterAll, beforeEach, describe, expect, it, vi } from 'vitest'
import { reactive } from 'vue'
import { flushPromises, mount, RouterLinkStub } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import zh from '@/i18n/locales/zh/publicDocs'
import en from '@/i18n/locales/en/publicDocs'
import zhLanding from '@/i18n/locales/zh/landing'
import enLanding from '@/i18n/locales/en/landing'
import PublicDocsView from '../PublicDocsView.vue'

vi.hoisted(() => { vi.stubGlobal('__INTLIFY_JIT_COMPILATION__', true) })
afterAll(() => vi.unstubAllGlobals())

const { useAppStore, useAuthStore } = vi.hoisted(() => ({ useAppStore: vi.fn(), useAuthStore: vi.fn() }))
vi.mock('@/stores/app', () => ({ useAppStore }))
vi.mock('@/stores/auth', () => ({ useAuthStore }))

function mountDocs() {
  const i18n = createI18n({ legacy: false, locale: 'zh', messages: { zh: { ...zh, ...zhLanding }, en: { ...en, ...enLanding } } })
  const wrapper = mount(PublicDocsView, {
    global: { plugins: [i18n], stubs: { RouterLink: RouterLinkStub, LocaleSwitcher: true, Icon: true } },
  })
  return { wrapper, i18n }
}

describe('public documentation', () => {
  let store: ReturnType<typeof reactive<{
    cachedPublicSettings: { site_name: string; docs_title: string; docs_content: string; api_base_url?: string } | null
    fetchPublicSettings: ReturnType<typeof vi.fn>
  }>>

  beforeEach(() => {
    store = reactive({
      cachedPublicSettings: { site_name: 'Test gateway', docs_title: '开发文档', docs_content: '## 快速开始\n\n```sh\ncurl https://example.com/v1/models\n```' },
      fetchPublicSettings: vi.fn().mockResolvedValue(null),
    })
    useAppStore.mockReturnValue(store)
    useAuthStore.mockReturnValue({ isAuthenticated: false, isAdmin: false })
    Object.defineProperty(navigator, 'clipboard', { configurable: true, value: { writeText: vi.fn().mockResolvedValue(undefined) } })
  })

  it('renders public settings as safe Markdown with a Chinese directory and code copying', async () => {
    const { wrapper } = mountDocs()
    await flushPromises()
    expect(wrapper.get('h1').text()).toBe('开发文档')
    expect(wrapper.text()).toContain('章节目录')
    expect(wrapper.get('article h2').text()).toBe('快速开始')
    expect(wrapper.get('nav[aria-label="章节目录"] a').attributes('href')).toContain('#docs-')
    expect(wrapper.findAllComponents(RouterLinkStub).some(link => link.props('to') === '/login')).toBe(true)
    const modelPlazaLink = wrapper.findAllComponents(RouterLinkStub).find(link => link.props('to') === '/model-plaza')
    expect(modelPlazaLink?.text()).toBe('模型广场')
    expect(wrapper.findAllComponents(RouterLinkStub).some(link => link.props('to') === '/home#pricing')).toBe(false)
    await wrapper.get('[data-docs-copy]').trigger('click')
    await flushPromises()
    expect(navigator.clipboard.writeText).toHaveBeenCalledWith('curl https://example.com/v1/models\n')
    expect(wrapper.text()).toContain('已复制')
    wrapper.unmount()
  })

  it('shows a localized client onboarding guide when no custom document has been published', async () => {
    store.cachedPublicSettings!.docs_content = ''
    store.cachedPublicSettings!.docs_title = ''
    store.cachedPublicSettings!.api_base_url = 'https://gateway.example/v1/'
    const { wrapper, i18n } = mountDocs()
    expect(wrapper.get('h1').text()).toBe('接入文档')
    for (const client of ['Claude Code', 'Codex CLI', 'Gemini CLI', 'Cursor', 'Cherry Studio']) {
      expect(wrapper.get('article').text()).toContain(client)
    }
    expect(wrapper.get('article').text()).toContain('https://gateway.example/v1')
    expect(wrapper.findAll('article a[href="/model-plaza"]')).toHaveLength(2)
    expect(wrapper.find('a[href="/home#pricing"]').exists()).toBe(false)
    expect(wrapper.text()).not.toContain('{{BASE_URL}}')
    expect(wrapper.text()).not.toContain('/v1/v1')
    i18n.global.locale.value = 'en'
    await flushPromises()
    expect(wrapper.get('h1').text()).toBe('Integration Guide')
    expect(wrapper.get('article').text()).toContain('Before you connect')
    expect(wrapper.findAll('article a[href="/model-plaza"]')).toHaveLength(2)
    expect(wrapper.findAllComponents(RouterLinkStub).find(link => link.props('to') === '/model-plaza')?.text()).toBe('Model Plaza')
    expect(wrapper.find('a[href="/home#pricing"]').exists()).toBe(false)
    wrapper.unmount()
  })

  it('recovers from failed public settings loading with retry', async () => {
    store.cachedPublicSettings = null
    const { wrapper } = mountDocs()
    await flushPromises()
    expect(wrapper.get('[role="alert"]').text()).toContain('文档加载失败')
    expect(wrapper.find('article').exists()).toBe(false)
    store.fetchPublicSettings.mockImplementation(async () => {
      store.cachedPublicSettings = { site_name: 'Test', docs_title: 'Recovered', docs_content: '## Connected' }
      return store.cachedPublicSettings
    })
    await wrapper.get('[role="alert"] button').trigger('click')
    await flushPromises()
    expect(wrapper.get('h1').text()).toBe('Recovered')
    expect(wrapper.get('article h2').text()).toBe('Connected')
    wrapper.unmount()
  })
})
