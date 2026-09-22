import { defineComponent } from 'vue'
import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import ChannelMonitorView from '@/views/admin/ChannelMonitorView.vue'

const { list, getSortOrder, updateSortOrder, showSuccess, showError } = vi.hoisted(() => ({
  list: vi.fn(),
  getSortOrder: vi.fn(),
  updateSortOrder: vi.fn(),
  showSuccess: vi.fn(),
  showError: vi.fn(),
}))

vi.mock('@/utils/featureFlags', () => ({ isChannelMonitorV1Mode: () => true }))
vi.mock('@/features/channel-monitor-v2/MonitorSettingsPanel.vue', () => ({
  default: { template: '<div />' },
}))
vi.mock('@/api/admin', () => ({
  adminAPI: { channelMonitor: { list, getSortOrder, updateSortOrder } },
}))
vi.mock('@/stores/app', () => ({ useAppStore: () => ({ showSuccess, showError }) }))
vi.mock('vue-i18n', async () => ({
  ...await vi.importActual<typeof import('vue-i18n')>('vue-i18n'),
  useI18n: () => ({
    t: (key: string, params?: { name?: string }) => params?.name ? `${key}: ${params.name}` : key,
  }),
}))

const order = [
  { id: 9, name: 'Zulu', provider: 'openai', enabled: true, sort_order: 0 },
  { id: 2, name: 'Alpha', provider: 'anthropic', enabled: false, sort_order: 10 },
  { id: 6, name: 'Middle', provider: 'gemini', enabled: true, sort_order: 20 },
]

function mountView() {
  return mount(ChannelMonitorView, {
    global: {
      stubs: {
        AppLayout: { template: '<main><slot /></main>' },
        TablePageLayout: { template: '<section><slot name="filters" /><slot name="table" /><slot name="pagination" /></section>' },
        DataTable: true,
        BaseDialog: defineComponent({
          props: ['show', 'title'],
          template: '<section v-if="show" role="dialog"><h2>{{ title }}</h2><slot /><slot name="footer" /></section>',
        }),
        Pagination: true,
        Select: true,
        Icon: true,
        Toggle: true,
        HelpTooltip: true,
        ConfirmDialog: true,
        EmptyState: true,
        MonitorFormDialog: true,
        MonitorTemplateManagerDialog: true,
        MonitorRunResultDialog: true,
        MonitorAvailabilityResetDialog: true,
      },
    },
  })
}

type View = ReturnType<typeof mountView>

async function openSort(wrapper: View) {
  await flushPromises()
  await wrapper.get('button[title="admin.channelMonitor.sort.button"]').trigger('click')
  await flushPromises()
}

function dialogButton(wrapper: View, label: string) {
  return wrapper.findAll('[role="dialog"] button').find(button => button.text() === label)!
}

function ids(wrapper: View) {
  return wrapper.findAll('[data-monitor-sort-id]').map(row => Number(row.attributes('data-monitor-sort-id')))
}

describe('ChannelMonitorView saved display order', () => {
  beforeEach(() => {
    localStorage.clear()
    for (const mock of [list, getSortOrder, updateSortOrder, showSuccess, showError]) mock.mockReset()
    list.mockResolvedValue({ items: [order[0]], total: 3, page: 2, page_size: 1, pages: 3 })
    getSortOrder.mockImplementation(async () => order.map(item => ({ ...item })))
    updateSortOrder.mockResolvedValue({ message: 'Sort order updated' })
  })

  it('loads all monitors for sorting, including disabled monitors outside the filtered page', async () => {
    const wrapper = mountView()
    await wrapper.get('input').setValue('Zulu')
    await openSort(wrapper)

    expect(ids(wrapper)).toEqual([9, 2, 6])
    expect(wrapper.get('[data-monitor-sort-id="2"]').text()).toContain('common.disabled')
    expect(getSortOrder).toHaveBeenCalledWith({ signal: expect.any(AbortSignal) })
    wrapper.unmount()
  })

  it('saves the displayed order and refreshes the management list', async () => {
    const wrapper = mountView()
    await openSort(wrapper)
    await wrapper.get('button[aria-label="admin.channelMonitor.sort.moveUp: Alpha"]').trigger('click')
    expect(ids(wrapper)).toEqual([2, 9, 6])
    const loadsBeforeSave = list.mock.calls.length
    await dialogButton(wrapper, 'common.save').trigger('click')
    await flushPromises()

    expect(updateSortOrder).toHaveBeenCalledWith([
      { id: 2, sort_order: 0 }, { id: 9, sort_order: 10 }, { id: 6, sort_order: 20 },
    ])
    expect(showSuccess).toHaveBeenCalledWith('admin.channelMonitor.sort.success')
    expect(wrapper.find('[role="dialog"]').exists()).toBe(false)
    expect(list.mock.calls.length).toBeGreaterThan(loadsBeforeSave)
    wrapper.unmount()
  })

  it('discards an unsaved reorder on cancel and reloads persisted order next time', async () => {
    const wrapper = mountView()
    await openSort(wrapper)
    await wrapper.get('button[aria-label="admin.channelMonitor.sort.moveUp: Alpha"]').trigger('click')
    await dialogButton(wrapper, 'common.cancel').trigger('click')
    await openSort(wrapper)

    expect(updateSortOrder).not.toHaveBeenCalled()
    expect(ids(wrapper)).toEqual([9, 2, 6])
    expect(getSortOrder).toHaveBeenCalledTimes(2)
    wrapper.unmount()
  })

  it('retains the reordered list after a save failure and allows retry', async () => {
    updateSortOrder.mockRejectedValueOnce(new Error('save unavailable'))
    const wrapper = mountView()
    await openSort(wrapper)
    await wrapper.get('button[aria-label="admin.channelMonitor.sort.moveUp: Alpha"]').trigger('click')
    await dialogButton(wrapper, 'common.save').trigger('click')
    await flushPromises()

    expect(ids(wrapper)).toEqual([2, 9, 6])
    expect(showError).toHaveBeenCalledWith('save unavailable')
    expect(showSuccess).not.toHaveBeenCalled()
    await dialogButton(wrapper, 'common.save').trigger('click')
    await flushPromises()
    expect(updateSortOrder).toHaveBeenCalledTimes(2)
    expect(wrapper.find('[role="dialog"]').exists()).toBe(false)
    wrapper.unmount()
  })

  it('blocks empty or failed loads from being saved and can reload', async () => {
    getSortOrder.mockRejectedValueOnce(new Error('load unavailable'))
    const wrapper = mountView()
    await openSort(wrapper)

    expect(dialogButton(wrapper, 'common.save').attributes('disabled')).toBeDefined()
    expect(showError).toHaveBeenCalledWith('load unavailable')
    await dialogButton(wrapper, 'common.refresh').trigger('click')
    await flushPromises()
    expect(ids(wrapper)).toEqual([9, 2, 6])
    expect(dialogButton(wrapper, 'common.save').attributes('disabled')).toBeUndefined()
    wrapper.unmount()
  })

  it('prevents a second submission and changing order while a save is pending', async () => {
    let finishSave!: (value: { message: string }) => void
    updateSortOrder.mockImplementationOnce(() => new Promise(resolve => { finishSave = resolve }))
    const wrapper = mountView()
    await openSort(wrapper)
    await dialogButton(wrapper, 'common.save').trigger('click')

    expect(dialogButton(wrapper, 'common.saving').attributes('disabled')).toBeDefined()
    expect(dialogButton(wrapper, 'common.cancel').attributes('disabled')).toBeDefined()
    expect(wrapper.get('button[aria-label="admin.channelMonitor.sort.moveUp: Alpha"]').attributes('disabled')).toBeDefined()
    expect(updateSortOrder).toHaveBeenCalledTimes(1)
    finishSave({ message: 'saved' })
    await flushPromises()
    wrapper.unmount()
  })

  it('ignores an old load if the dialog is closed and reopened', async () => {
    let finishOldLoad!: (value: typeof order) => void
    getSortOrder.mockImplementationOnce(() => new Promise(resolve => { finishOldLoad = resolve }))
    const wrapper = mountView()
    await openSort(wrapper)
    await dialogButton(wrapper, 'common.cancel').trigger('click')
    await openSort(wrapper)
    expect(ids(wrapper)).toEqual([9, 2, 6])

    finishOldLoad([...order].reverse())
    await flushPromises()
    expect(ids(wrapper)).toEqual([9, 2, 6])
    wrapper.unmount()
  })
})
