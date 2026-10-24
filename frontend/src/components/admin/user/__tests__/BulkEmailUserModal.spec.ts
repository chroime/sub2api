import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import BulkEmailUserModal from '../BulkEmailUserModal.vue'
import zhOverview from '@/i18n/locales/zh/admin/overview'
import enOverview from '@/i18n/locales/en/admin/overview'

const { bulkEmail, previewBulkEmail, translate } = vi.hoisted(() => ({
  bulkEmail: vi.fn(),
  previewBulkEmail: vi.fn(),
  translate: vi.fn()
}))
const defaultLinks = {
  platform_url: 'https://platform.example.com',
  docs_url: 'https://platform.example.com/docs',
  model_plaza_url: 'https://platform.example.com/model-plaza',
  canvas_url: 'https://canvas.example.com'
}
vi.mock('@/api/admin', () => ({ adminAPI: { users: { bulkEmail, previewBulkEmail } } }))
vi.mock('vue-i18n', () => ({ useI18n: () => ({
  locale: { value: 'zh' },
  t: translate
}) }))

const mountModal = (selectedIds = [4, 7]) => mount(BulkEmailUserModal, {
  props: { show: true, selectedIds },
  global: { stubs: {
    BaseDialog: {
      name: 'BaseDialog',
      props: ['show', 'width'], emits: ['close'],
      template: '<div v-if="show"><slot /><slot name="footer" /></div>'
    },
    Select: {
      props: ['modelValue', 'options', 'disabled'], emits: ['update:modelValue'],
      template: '<select :value="modelValue" :disabled="disabled" @change="$emit(\'update:modelValue\', $event.target.value)"><option v-for="option in options" :key="option.value" :value="option.value">{{ option.label }}</option></select>'
    },
    DataTable: {
      props: ['data', 'columns'],
      template: '<div><div v-for="row in data" :key="row.user_id" :data-test="`result-${row.user_id}`"><template v-for="column in columns" :key="column.key"><slot :name="`cell-${column.key}`" :row="row" :value="row[column.key]">{{ row[column.key] }}</slot></template></div></div>'
    },
    Icon: true
  } }
})

describe('BulkEmailUserModal', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    translate.mockImplementation((key: string, values?: Record<string, unknown>) => values ? `${key}:${JSON.stringify(values)}` : key)
    previewBulkEmail.mockResolvedValue({ subject: 'Current system status', html: '<p>GPT Pro: 0.06x</p>', links: defaultLinks })
    bulkEmail.mockResolvedValue({ total: 2, sent: 1, failed: 1, skipped: 0, items: [
      { user_id: 4, email: 'four@example.com', status: 'sent' },
      { user_id: 7, email: 'seven@example.com', status: 'failed', error: 'SMTP rejected recipient' }
    ] })
  })

  it('requires a fresh dynamic preview before sending and shows per-recipient results', async () => {
    const wrapper = mountModal()
    expect(wrapper.findComponent({ name: 'BaseDialog' }).props('width')).toBe('extra-wide')
    expect(wrapper.get('[data-test="send-email"]').attributes('disabled')).toBeDefined()
    await wrapper.get('[data-test="preview-email"]').trigger('click')
    await flushPromises()
    expect(previewBulkEmail).toHaveBeenCalledWith({ event: 'system.health_snapshot', locale: 'zh', user_id: 4 })
    expect(wrapper.get('[data-test="email-preview-subject"]').text()).toBe('Current system status')
    expect(wrapper.get('iframe').attributes('srcdoc')).toContain('GPT Pro: 0.06x')
    expect(wrapper.get('iframe').attributes('sandbox')).toBe('')
    expect(bulkEmail).not.toHaveBeenCalled()

    await wrapper.get('[data-test="send-email"]').trigger('click')
    await flushPromises()
    expect(bulkEmail).toHaveBeenCalledWith({ user_ids: [4, 7], all: false, event: 'system.health_snapshot', locale: 'zh', links: defaultLinks })
    expect(wrapper.get('[data-test="result-4"]').text()).toContain('admin.users.bulkEmail.sent')
    expect(wrapper.get('[data-test="result-7"]').text()).toContain('SMTP rejected recipient')
    expect(wrapper.get('[data-test="email-results"]').text()).toContain('"sent":1,"failed":1,"skipped":0')
    expect(wrapper.find('[data-test="send-email"]').exists()).toBe(false)
    wrapper.unmount()
  })

  it('requires explicit all-user scope and sends no selected IDs in that scope', async () => {
    const wrapper = mountModal([])
    expect(wrapper.get('[data-test="preview-email"]').attributes('disabled')).toBeDefined()
    expect(wrapper.get('[data-test="scope-selected"]').attributes('disabled')).toBeDefined()
    await wrapper.get('[data-test="scope-all"]').setValue()
    await wrapper.get('[data-test="preview-email"]').trigger('click')
    await flushPromises()
    expect(previewBulkEmail).toHaveBeenCalledWith({ event: 'system.health_snapshot', locale: 'zh' })
    await wrapper.get('[data-test="send-email"]').trigger('click')
    await flushPromises()
    expect(bulkEmail).toHaveBeenCalledWith({ user_ids: [], all: true, event: 'system.health_snapshot', locale: 'zh', links: defaultLinks })
    wrapper.unmount()
  })

  it('limits choices to safe notification templates and invalidates preview after changing template', async () => {
    const wrapper = mountModal()
    expect(wrapper.get('[data-test="email-template"]').findAll('option').map(option => option.attributes('value')))
      .toEqual(['system.health_snapshot', 'balance.low'])
    await wrapper.get('[data-test="preview-email"]').trigger('click')
    await flushPromises()
    await wrapper.get('[data-test="email-template"]').setValue('balance.low')
    expect(wrapper.find('iframe').exists()).toBe(false)
    expect(wrapper.get('[data-test="send-email"]').attributes('disabled')).toBeDefined()
    await wrapper.get('[data-test="preview-email"]').trigger('click')
    await flushPromises()
    await wrapper.get('[data-test="send-email"]').trigger('click')
    await flushPromises()
    expect(bulkEmail).toHaveBeenCalledWith({ user_ids: [4, 7], all: false, event: 'balance.low', locale: 'zh' })
    wrapper.unmount()
  })

  it.each([
    ['zh', '分组与倍率更新', '平台、文档、模型广场和画布入口', '非上游实测'],
    ['en', 'Groups and rates update', 'platform, documentation, model plaza, and canvas links', 'not a live upstream test']
  ])('describes the groups and rates update template and return links in %s', async (language, label, destinations, limitation) => {
    const dictionary = { admin: language === 'zh' ? zhOverview : enOverview }
    translate.mockImplementation((key: string) => {
      const value = key.split('.').reduce<unknown>((node, part) =>
        node && typeof node === 'object' ? (node as Record<string, unknown>)[part] : undefined, dictionary)
      return typeof value === 'string' ? value : key
    })
    const wrapper = mountModal()
    expect(wrapper.get('[data-test="email-template"] option').text()).toBe(label)
    expect(wrapper.text()).toContain(destinations)
    expect(wrapper.text()).toContain(limitation)
    wrapper.unmount()
  })

  it('discards a stale preview response after the target scope changes', async () => {
    let resolvePreview!: (value: { subject: string; html: string }) => void
    previewBulkEmail.mockImplementationOnce(() => new Promise(resolve => { resolvePreview = resolve }))
    const wrapper = mountModal()
    await wrapper.get('[data-test="preview-email"]').trigger('click')
    await wrapper.get('[data-test="scope-all"]').setValue()
    resolvePreview({ subject: 'Old preview', html: '<p>Old recipient</p>' })
    await flushPromises()
    expect(wrapper.find('iframe').exists()).toBe(false)
    expect(wrapper.get('[data-test="send-email"]').attributes('disabled')).toBeDefined()
    wrapper.unmount()
  })

  it('fills configured destination URLs from the preview without invalidating it', async () => {
    const wrapper = mountModal()
    for (const name of Object.keys(defaultLinks)) {
      expect(wrapper.get(`[data-test="email-link-${name}"]`).attributes('type')).toBe('url')
    }
    expect(wrapper.find('[data-test="email-links-warning"]').exists()).toBe(true)
    await wrapper.get('[data-test="preview-email"]').trigger('click')
    await flushPromises()
    for (const [name, url] of Object.entries(defaultLinks)) {
      expect((wrapper.get(`[data-test="email-link-${name}"]`).element as HTMLInputElement).value).toBe(url)
    }
    expect(wrapper.find('[data-test="email-links-warning"]').exists()).toBe(false)
    expect(wrapper.find('iframe').exists()).toBe(true)
    expect(wrapper.get('[data-test="send-email"]').attributes('disabled')).toBeUndefined()
    wrapper.unmount()
  })

  it('requires all four absolute HTTP URLs and a fresh preview after editing a link', async () => {
    previewBulkEmail.mockResolvedValueOnce({ subject: 'No defaults', html: '<p>Preview</p>', links: {} })
    const wrapper = mountModal()
    await wrapper.get('[data-test="preview-email"]').trigger('click')
    await flushPromises()
    expect(wrapper.find('iframe').exists()).toBe(true)
    expect(wrapper.get('[data-test="send-email"]').attributes('disabled')).toBeDefined()
    expect(wrapper.get('[data-test="email-links-warning"]').text()).toBe('admin.users.bulkEmail.linksMissing')
    for (const [name, url] of Object.entries(defaultLinks)) {
      await wrapper.get(`[data-test="email-link-${name}"]`).setValue(url)
    }
    expect(wrapper.find('iframe').exists()).toBe(false)
    expect(wrapper.get('[data-test="send-email"]').attributes('disabled')).toBeDefined()
    await wrapper.get('[data-test="preview-email"]').trigger('click')
    await flushPromises()
    expect(previewBulkEmail).toHaveBeenLastCalledWith({ event: 'system.health_snapshot', locale: 'zh', user_id: 4, links: defaultLinks })
    expect(wrapper.get('[data-test="send-email"]').attributes('disabled')).toBeUndefined()

    await wrapper.get('[data-test="email-link-canvas_url"]').setValue('/canvas')
    expect(wrapper.find('iframe').exists()).toBe(false)
    expect(wrapper.get('[data-test="email-links-warning"]').text()).toBe('admin.users.bulkEmail.linksInvalid')
    expect(wrapper.get('[data-test="send-email"]').attributes('disabled')).toBeDefined()
    await wrapper.get('[data-test="email-link-canvas_url"]').setValue('javascript:alert(1)')
    expect(wrapper.get('[data-test="send-email"]').attributes('disabled')).toBeDefined()
    expect(bulkEmail).not.toHaveBeenCalled()
    wrapper.unmount()
  })

  it('discards an in-flight preview after a destination URL is changed', async () => {
    let resolvePreview!: (value: { subject: string; html: string; links: typeof defaultLinks }) => void
    previewBulkEmail.mockImplementationOnce(() => new Promise(resolve => { resolvePreview = resolve }))
    const wrapper = mountModal()
    await wrapper.get('[data-test="preview-email"]').trigger('click')
    await wrapper.get('[data-test="email-link-platform_url"]').setValue('https://edited.example.com')
    resolvePreview({ subject: 'Old links', html: '<p>Stale</p>', links: defaultLinks })
    await flushPromises()
    expect(wrapper.find('iframe').exists()).toBe(false)
    expect((wrapper.get('[data-test="email-link-platform_url"]').element as HTMLInputElement).value).toBe('https://edited.example.com')
    wrapper.unmount()
  })

  it('blocks duplicate submissions while sending and reports uncertain failures without auto-retry', async () => {
    let rejectSend!: (reason: unknown) => void
    bulkEmail.mockImplementationOnce(() => new Promise((_, reject) => { rejectSend = reject }))
    const wrapper = mountModal()
    await wrapper.get('[data-test="preview-email"]').trigger('click')
    await flushPromises()
    await wrapper.get('[data-test="send-email"]').trigger('click')
    expect(wrapper.get('[data-test="send-email"]').attributes('disabled')).toBeDefined()
    expect(wrapper.get('[data-test="close-email"]').attributes('disabled')).toBeDefined()
    rejectSend({ message: 'Connection reset' })
    await flushPromises()
    expect(wrapper.get('[role="alert"]').text()).toContain('Connection reset')
    expect(wrapper.get('[role="alert"]').text()).toContain('admin.users.bulkEmail.unknownResult')
    expect(wrapper.get('[data-test="send-email"]').attributes('disabled')).toBeDefined()
    expect(bulkEmail).toHaveBeenCalledTimes(1)
    wrapper.unmount()
  })

  it.each([
    ['zh', '用户不存在或已被删除'],
    ['en', 'The user no longer exists or has been deleted.']
  ])('explains machine-coded recipient results in %s and preserves unknown server details', async (language, missingUserMessage) => {
    const dictionary = { admin: language === 'zh' ? zhOverview : enOverview }
    translate.mockImplementation((key: string) => {
      const value = key.split('.').reduce<unknown>((node, part) =>
        node && typeof node === 'object' ? (node as Record<string, unknown>)[part] : undefined, dictionary)
      return typeof value === 'string' ? value : key
    })
    const codes = [
      'user_not_found', 'unsubscribed', 'invalid_email', 'balance_above_threshold',
      'balance_threshold_not_configured', 'notification_data_unavailable', 'email_delivery_failed',
      'notification_suppressed', 'user_lookup_failed', 'notification_preference_unavailable', 'request_cancelled'
    ]
    bulkEmail.mockResolvedValue({ total: 12, sent: 0, failed: 12, skipped: 0, items: [
      ...codes.map((code, index) => ({ user_id: index + 1, email: `user${index}@example.com`, status: 'failed', error: code })),
      { user_id: 12, email: 'future@example.com', status: 'failed', error: 'Recipient rejected by provider' }
    ] })
    const wrapper = mountModal()
    await wrapper.get('[data-test="preview-email"]').trigger('click')
    await flushPromises()
    await wrapper.get('[data-test="send-email"]').trigger('click')
    await flushPromises()
    expect(wrapper.get('[data-test="result-1"]').text()).toContain(missingUserMessage)
    for (const [index, code] of codes.entries()) {
      const detail = wrapper.get(`[data-test="result-${index + 1}"]`).findAll('span').at(-1)!.text()
      expect(detail).not.toBe(code)
      expect(detail).not.toContain('admin.users.bulkEmail.errors')
    }
    expect(wrapper.get('[data-test="result-12"]').text()).toContain('Recipient rejected by provider')
    wrapper.unmount()
  })

  it.each([
    ['zh', '未完整配置平台、文档、模型广场和画布地址，此邮件已跳过；请补全入口并重新预览后发送。', '已跳过'],
    ['en', 'The platform, documentation, model plaza, or canvas URL is missing. This email was skipped. Complete all four links and preview again before sending.', 'Skipped']
  ])('explains a skipped delivery caused by missing service links in %s', async (language, message, status) => {
    const dictionary = { admin: language === 'zh' ? zhOverview : enOverview }
    translate.mockImplementation((key: string) => {
      const value = key.split('.').reduce<unknown>((node, part) =>
        node && typeof node === 'object' ? (node as Record<string, unknown>)[part] : undefined, dictionary)
      return typeof value === 'string' ? value : key
    })
    bulkEmail.mockResolvedValue({ total: 1, sent: 0, failed: 0, skipped: 1, items: [
      { user_id: 4, email: 'four@example.com', status: 'skipped', error: 'service_links_not_configured' }
    ] })
    const wrapper = mountModal([4])
    await wrapper.get('[data-test="preview-email"]').trigger('click')
    await flushPromises()
    await wrapper.get('[data-test="send-email"]').trigger('click')
    await flushPromises()
    expect(wrapper.get('[data-test="result-4"]').text()).toContain(message)
    expect(wrapper.get('[data-test="result-4"]').text()).toContain(status)
    wrapper.unmount()
  })
})
