import { afterAll, describe, expect, it, vi } from 'vitest'
import { mount, RouterLinkStub } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import zh from '@/i18n/locales/zh/publicDocs'
import settings from '@/i18n/locales/zh/admin/settings'
import en from '@/i18n/locales/en/publicDocs'
import enSettings from '@/i18n/locales/en/admin/settings'
import PublicDocsEditor from '../PublicDocsEditor.vue'

vi.hoisted(() => { vi.stubGlobal('__INTLIFY_JIT_COMPILATION__', true) })
afterAll(() => vi.unstubAllGlobals())

function mountEditor(content = '', locale = 'zh') {
  return mount(PublicDocsEditor, {
    props: { title: '接入文档', content },
    global: {
      plugins: [createI18n({ legacy: false, locale, messages: { zh: { ...zh, admin: settings }, en: { ...en, admin: enSettings } } })],
      stubs: { Icon: true, RouterLink: RouterLinkStub },
    },
  })
}

describe('documentation editor', () => {
  it('emits title and Markdown edits without losing raw content', async () => {
    const wrapper = mountEditor('## Existing')
    await wrapper.get('#public-docs-title').setValue('接入指南')
    await wrapper.get('#public-docs-markdown').setValue('## 流式响应\n\n```js\nconst stream = true\n```')
    expect(wrapper.emitted('update:title')?.[0]).toEqual(['接入指南'])
    expect(wrapper.emitted('update:content')?.[0]?.[0]).toContain('const stream = true')
  })

  it('previews sanitized Markdown using the public document layout', async () => {
    const wrapper = mountEditor('## 接入\n\n<script>alert(1)</script><iframe src="https://example.com"></iframe>')
    await wrapper.get('[data-testid="docs-preview-tab"]').trigger('click')
    expect(wrapper.get('.public-docs-content article h2').text()).toBe('接入')
    expect(wrapper.find('script, iframe').exists()).toBe(false)
    expect(wrapper.get('[data-testid="docs-preview-tab"]').attributes('aria-selected')).toBe('true')
  })

  it('offers a quickstart template only when content is empty', async () => {
    const wrapper = mountEditor()
    await wrapper.get('[data-testid="docs-insert-template"]').trigger('click')
    expect(wrapper.emitted('update:content')?.[0]?.[0]).toContain('## 接入准备')
    expect(wrapper.emitted('update:content')?.[0]?.[0]).toContain('## Claude Code')
    await wrapper.setProps({ content: '## Existing' })
    expect(wrapper.find('[data-testid="docs-insert-template"]').exists()).toBe(false)
  })

  it('uses an English template and the configured endpoint for English administrators', async () => {
    const wrapper = mountEditor('', 'en')
    await wrapper.setProps({ baseUrl: 'https://gateway.example/' })
    await wrapper.get('[data-testid="docs-insert-template"]').trigger('click')
    const content = wrapper.emitted('update:content')?.[0]?.[0] as string
    expect(content).toContain('## Before you connect')
    expect(content).toContain('https://gateway.example/v1/chat/completions')
    expect(content).toContain('wire_api = "responses"')
    expect(content).toContain('GOOGLE_GEMINI_BASE_URL')
    expect(content).not.toContain('{{GATEWAY_URL}}')
  })

  it('previews the built-in guide when the document is empty without mutating saved content', async () => {
    const wrapper = mountEditor()
    await wrapper.get('[data-testid="docs-preview-tab"]').trigger('click')
    expect(wrapper.get('article').text()).toContain('Claude Code')
    expect(wrapper.emitted('update:content')).toBeUndefined()
  })
})
