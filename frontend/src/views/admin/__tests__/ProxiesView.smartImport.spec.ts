import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, shallowMount } from '@vue/test-utils'
import ProxiesView from '../ProxiesView.vue'

const { list, getAllWithCount, batchCreate, showSuccess, showError, showInfo } = vi.hoisted(() => ({
  list: vi.fn(), getAllWithCount: vi.fn(), batchCreate: vi.fn(),
  showSuccess: vi.fn(), showError: vi.fn(), showInfo: vi.fn()
}))

vi.mock('@/api/admin', () => ({ adminAPI: { proxies: { list, getAllWithCount, batchCreate } } }))
vi.mock('@/stores/app', () => ({ useAppStore: () => ({ showSuccess, showError, showInfo }) }))
vi.mock('vue-i18n', async () => ({
  ...await vi.importActual<typeof import('vue-i18n')>('vue-i18n'),
  useI18n: () => ({ t: (key: string, params?: { count?: number }) => params?.count === undefined ? key : `${key}:${params.count}` })
}))

const mountView = () => shallowMount(ProxiesView, {
  global: { stubs: {
    AppLayout: { template: '<div><slot /></div>' },
    TablePageLayout: { template: '<div><slot name="filters" /><slot name="table" /></div>' },
    BaseDialog: { props: ['show'], template: '<div v-if="show"><slot /><slot name="footer" /></div>' },
    Select: {
      props: ['modelValue', 'options'], emits: ['update:modelValue'],
      template: `<select :value="modelValue" @change="$emit('update:modelValue', $event.target.value)">
        <option v-for="option in options" :key="option.value" :value="option.value">{{ option.label }}</option>
      </select>`
    }
  } }
})

let wrapper: ReturnType<typeof mountView> | undefined

beforeEach(() => {
  vi.clearAllMocks()
  list.mockResolvedValue({ items: [], total: 0, pages: 0 })
  getAllWithCount.mockResolvedValue([])
  batchCreate.mockResolvedValue({ created: 2, skipped: 0 })
})
afterEach(() => wrapper?.unmount())

async function openQuickAdd(input: string) {
  wrapper = mountView()
  await flushPromises()
  await wrapper.findAll('button').find(button => button.text() === 'admin.proxies.createProxy')!.trigger('click')
  await wrapper.findAll('button').find(button => button.text() === 'admin.proxies.batchAdd')!.trigger('click')
  await wrapper.get('[data-testid="proxy-batch-input"]').setValue(input)
  await flushPromises()
  return wrapper
}

describe('proxy quick-add recognition and review', () => {
  it('previews mixed formats with SOCKS5 defaults and sends only reviewed fields on import', async () => {
    const view = await openQuickAdd('203.0.113.8:7391:sample-user:sample:password\nhttps://other:secret@proxy.example.com:443')
    const rows = view.findAll('[data-testid="proxy-preview-row"]')
    expect(rows).toHaveLength(2)
    expect((rows[0].get('[data-field="protocol"]').element as HTMLSelectElement).value).toBe('socks5')
    expect((rows[0].get('[data-field="host"]').element as HTMLInputElement).value).toBe('203.0.113.8')
    expect((rows[0].get('[data-field="password"]').element as HTMLInputElement).value).toBe('sample:password')
    expect(batchCreate).not.toHaveBeenCalled()

    await rows[0].get('[data-field="protocol"]').setValue('http')
    await rows[0].get('[data-field="host"]').setValue('203.0.113.9')
    await rows[0].get('[data-field="port"]').setValue('8443')
    await rows[0].get('[data-field="username"]').setValue('reviewed-user')
    await rows[0].get('[data-field="password"]').setValue('reviewed:secret@value')
    await view.get('[data-testid="proxy-batch-import"]').trigger('click')
    await flushPromises()

    expect(batchCreate).toHaveBeenCalledTimes(1)
    expect(batchCreate).toHaveBeenCalledWith([
      { name: 'us001', protocol: 'http', host: '203.0.113.9', port: 8443, username: 'reviewed-user', password: 'reviewed:secret@value' },
      { name: 'us002', protocol: 'https', host: 'proxy.example.com', port: 443, username: 'other', password: 'secret' }
    ])
    expect(showError).not.toHaveBeenCalled()
  })

  it('keeps unrecognized lines visible and prevents import until they are repaired or removed', async () => {
    const view = await openQuickAdd('203.0.113.8:1080\nnot-a-proxy')
    let rows = view.findAll('[data-testid="proxy-preview-row"]')
    expect(rows).toHaveLength(2)
    expect(rows[1].attributes('data-status')).toBe('invalid')
    expect(view.get('[data-testid="proxy-batch-import"]').attributes('disabled')).toBeDefined()

    await rows[1].get('[data-field="host"]').setValue('proxy.example.com')
    await rows[1].get('[data-field="port"]').setValue('1080')
    rows = view.findAll('[data-testid="proxy-preview-row"]')
    expect(rows[1].attributes('data-status')).toBe('valid')
    expect(view.get('[data-testid="proxy-batch-import"]').attributes('disabled')).toBeUndefined()

    await rows[1].get('[data-field="port"]').setValue('70000')
    expect(view.get('[data-testid="proxy-batch-import"]').attributes('disabled')).toBeDefined()
    await rows[1].get('[data-action="remove"]').trigger('click')
    expect(view.findAll('[data-testid="proxy-preview-row"]')).toHaveLength(1)
    expect(view.get('[data-testid="proxy-batch-import"]').attributes('disabled')).toBeUndefined()
  })

  it('rechecks duplicates after edits and imports each backend identity once', async () => {
    const view = await openQuickAdd('203.0.113.8:1080:user:pass\nhttp://user:pass@203.0.113.8:1080')
    let rows = view.findAll('[data-testid="proxy-preview-row"]')
    expect(rows[1].attributes('data-status')).toBe('duplicate')
    expect(view.get('[data-testid="proxy-batch-import"]').text()).toContain(':1')

    await rows[1].get('[data-field="username"]').setValue('another-user')
    rows = view.findAll('[data-testid="proxy-preview-row"]')
    expect(rows[1].attributes('data-status')).toBe('valid')
    expect(view.get('[data-testid="proxy-batch-import"]').text()).toContain(':2')
    await rows[1].get('[data-field="username"]').setValue('user')
    await view.get('[data-testid="proxy-batch-import"]').trigger('click')
    await flushPromises()
    expect(batchCreate).toHaveBeenCalledTimes(1)
    expect(batchCreate).toHaveBeenCalledWith([
      { name: 'us001', protocol: 'socks5', host: '203.0.113.8', port: 1080, username: 'user', password: 'pass' }
    ])
  })

  it('can reveal passwords for review without changing credentials', async () => {
    const view = await openQuickAdd('203.0.113.8:1080:user:sample-secret')
    expect(view.get('[data-field="password"]').attributes('type')).toBe('password')
    await view.get('[data-testid="proxy-preview-show-passwords"]').setValue(true)
    expect(view.get('[data-field="password"]').attributes('type')).toBe('text')
    expect((view.get('[data-field="password"]').element as HTMLInputElement).value).toBe('sample-secret')
    expect(batchCreate).not.toHaveBeenCalled()
  })

  it('blocks partial authentication and unsupported credential characters after editing', async () => {
    const view = await openQuickAdd('203.0.113.8:1080:user:sample-secret')
    await view.get('[data-field="password"]').setValue('')
    expect(view.get('[data-testid="proxy-preview-row"]').attributes('data-status')).toBe('invalid')
    expect(view.get('[data-testid="proxy-batch-import"]').attributes('disabled')).toBeDefined()
    await view.get('[data-field="password"]').setValue('secret\u0000tail')
    expect(view.get('[data-testid="proxy-batch-import"]').attributes('disabled')).toBeDefined()
    await view.get('[data-field="password"]').setValue('repaired-secret')
    expect(view.get('[data-testid="proxy-batch-import"]').attributes('disabled')).toBeUndefined()
    expect(batchCreate).not.toHaveBeenCalled()
  })

  it('previews country and sequence rules and saves names matching the preview', async () => {
    const view = await openQuickAdd('203.0.113.8:1080\n203.0.113.9:1080')
    const rows = view.findAll('[data-testid="proxy-preview-row"]')
    expect((rows[0].get('[data-field="name"]').element as HTMLInputElement).value).toBe('us001')
    await view.get('[data-testid="proxy-naming-country"]').setValue('jp')
    await view.get('[data-testid="proxy-naming-start"]').setValue('8')
    await view.get('[data-testid="proxy-naming-digits"]').setValue('4')
    await view.get('[data-testid="proxy-naming-separator"]').setValue('-')
    expect((rows[0].get('[data-field="name"]').element as HTMLInputElement).value).toBe('jp-0008')
    expect((rows[1].get('[data-field="name"]').element as HTMLInputElement).value).toBe('jp-0009')
    await rows[1].get('[data-field="country"]').setValue('us')
    expect((rows[1].get('[data-field="name"]').element as HTMLInputElement).value).toBe('us-0009')
    await view.get('[data-testid="proxy-batch-import"]').trigger('click')
    await flushPromises()
    expect(batchCreate.mock.calls[0][0].map((proxy: { name: string }) => proxy.name)).toEqual(['jp-0008', 'us-0009'])
    expect(batchCreate.mock.calls[0][0][0]).not.toHaveProperty('country')
    expect(batchCreate.mock.calls[0][0][0]).not.toHaveProperty('manualName')
  })

  it('preserves manual names when rules change and can restore automatic names', async () => {
    const view = await openQuickAdd('203.0.113.8:1080\n203.0.113.9:1080')
    const rows = view.findAll('[data-testid="proxy-preview-row"]')
    await rows[0].get('[data-field="name"]').setValue('My ')
    expect((rows[0].get('[data-field="name"]').element as HTMLInputElement).value).toBe('My ')
    await rows[0].get('[data-field="name"]').setValue('My custom proxy')
    await view.get('[data-testid="proxy-naming-country"]').setValue('jp')
    expect((rows[0].get('[data-field="name"]').element as HTMLInputElement).value).toBe('My custom proxy')
    expect((rows[1].get('[data-field="name"]').element as HTMLInputElement).value).toBe('jp001')
    await rows[0].get('[data-action="auto-name"]').trigger('click')
    expect((rows[0].get('[data-field="name"]').element as HTMLInputElement).value).toBe('jp001')
    expect((rows[1].get('[data-field="name"]').element as HTMLInputElement).value).toBe('jp002')
  })

  it('blocks empty, duplicate and overlong names and invalid sequence settings', async () => {
    const view = await openQuickAdd('203.0.113.8:1080\n203.0.113.9:1080')
    const rows = view.findAll('[data-testid="proxy-preview-row"]')
    const submit = () => view.get('[data-testid="proxy-batch-import"]')
    await rows[0].get('[data-field="name"]').setValue(' ')
    expect(submit().attributes('disabled')).toBeDefined()
    await rows[0].get('[data-field="name"]').setValue('custom')
    await rows[1].get('[data-field="name"]').setValue(' CUSTOM ')
    expect(submit().attributes('disabled')).toBeDefined()
    await rows[1].get('[data-field="name"]').setValue('x'.repeat(101))
    expect(submit().attributes('disabled')).toBeDefined()
    await rows[1].get('[data-field="name"]').setValue('another')
    expect(submit().attributes('disabled')).toBeUndefined()
    await view.get('[data-testid="proxy-naming-start"]').setValue('0')
    expect(submit().attributes('disabled')).toBeDefined()
    await submit().trigger('click')
    expect(batchCreate).not.toHaveBeenCalled()
  })

  it('skips existing names across all statuses when allocating numbers', async () => {
    list.mockImplementation((_page: number, _pageSize: number, filters?: { sort_by?: string }) => Promise.resolve({
      items: filters?.sort_by === 'name' ? [{ name: 'US001', status: 'inactive' }, { name: 'us002', status: 'expired' }] : [],
      total: filters?.sort_by === 'name' ? 2 : 0, pages: 1
    }))
    const view = await openQuickAdd('203.0.113.8:1080\n203.0.113.9:1080')
    const rows = view.findAll('[data-testid="proxy-preview-row"]')
    expect((rows[0].get('[data-field="name"]').element as HTMLInputElement).value).toBe('us003')
    expect((rows[1].get('[data-field="name"]').element as HTMLInputElement).value).toBe('us004')
    await rows[0].get('[data-field="name"]').setValue('US001')
    expect(view.get('[data-testid="proxy-batch-import"]').attributes('disabled')).toBeDefined()
    expect(view.text()).toContain('admin.proxies.previewExistingName')
  })

  it('resets naming rules and per-row changes after closing the import', async () => {
    const view = await openQuickAdd('203.0.113.8:1080')
    await view.get('[data-testid="proxy-naming-country"]').setValue('jp')
    await view.get('[data-testid="proxy-naming-start"]').setValue('12')
    await view.findAll('button').find(button => button.text() === 'common.cancel')!.trigger('click')
    await view.findAll('button').find(button => button.text() === 'admin.proxies.createProxy')!.trigger('click')
    await view.findAll('button').find(button => button.text() === 'admin.proxies.batchAdd')!.trigger('click')
    await view.get('[data-testid="proxy-batch-input"]').setValue('203.0.113.8:1080')
    await flushPromises()
    expect((view.get('[data-field="name"]').element as HTMLInputElement).value).toBe('us001')
  })

  it('keeps import blocked when the existing name inventory cannot be loaded', async () => {
    list.mockImplementation((_page: number, _pageSize: number, filters?: { sort_by?: string }) => (
      filters?.sort_by === 'name' ? Promise.reject(new Error('unavailable')) : Promise.resolve({ items: [], total: 0, pages: 0 })
    ))
    const view = await openQuickAdd('203.0.113.8:1080')
    expect(view.get('[data-testid="proxy-batch-import"]').attributes('disabled')).toBeDefined()
    expect(view.text()).toContain('admin.proxies.namingLoadFailed')
    expect(batchCreate).not.toHaveBeenCalled()
  })
})
