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
      { protocol: 'http', host: '203.0.113.9', port: 8443, username: 'reviewed-user', password: 'reviewed:secret@value' },
      { protocol: 'https', host: 'proxy.example.com', port: 443, username: 'other', password: 'secret' }
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
      { protocol: 'socks5', host: '203.0.113.8', port: 1080, username: 'user', password: 'pass' }
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
})
