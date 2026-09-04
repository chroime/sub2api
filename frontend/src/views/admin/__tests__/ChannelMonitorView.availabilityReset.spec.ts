import { defineComponent } from 'vue'
import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import type { ChannelMonitor } from '@/api/admin/channelMonitor'
import MonitorActionsCell from '@/components/admin/monitor/MonitorActionsCell.vue'
import ChannelMonitorView from '@/views/admin/ChannelMonitorView.vue'

const {
  listMonitors,
  availabilityReset,
  clearAvailabilityReset,
  showSuccess,
  showError,
} = vi.hoisted(() => ({
  listMonitors: vi.fn(),
  availabilityReset: vi.fn(),
  clearAvailabilityReset: vi.fn(),
  showSuccess: vi.fn(),
  showError: vi.fn(),
}))

vi.mock('@/utils/featureFlags', () => ({
  isChannelMonitorV1Mode: () => true,
  isChannelMonitorV2Mode: () => false,
  getChannelMonitorMode: () => 'v1' as const,
}))

vi.mock('@/features/channel-monitor-v2/MonitorSettingsPanel.vue', () => ({
  default: { name: 'MonitorSettingsPanel', template: '<div />' },
}))

vi.mock('@/api/admin', () => ({
  adminAPI: {
    channelMonitor: {
      list: listMonitors,
      availabilityReset,
      clearAvailabilityReset,
      duplicate: vi.fn(),
      update: vi.fn(),
      runNow: vi.fn(),
      del: vi.fn(),
    },
  },
}))

vi.mock('@/stores/app', () => ({
  useAppStore: () => ({ showSuccess, showError }),
}))

vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return { ...actual, useI18n: () => ({ t: (key: string) => key }) }
})

const AppLayoutStub = defineComponent({ template: '<main><slot /></main>' })
const TablePageLayoutStub = defineComponent({ template: '<section><slot name="filters" /><slot name="table" /><slot name="pagination" /></section>' })
const DataTableStub = defineComponent({
  props: { data: { type: Array, default: () => [] } },
  template: '<div><div v-for="row in data" :key="row.id"><slot name="cell-actions" :row="row" /></div></div>',
})
const ResetDialogStub = defineComponent({
  props: { show: Boolean, monitor: Object, submitting: Boolean },
  emits: ['submit', 'close'],
  template: '<div v-if="show" data-testid="availability-reset-dialog"><button data-testid="dialog-submit" @click="$emit(\'submit\', { availability_pct: 91.25, degraded_bars: 3 })" /></div>',
})

function makeMonitor(overrides: Partial<ChannelMonitor> = {}): ChannelMonitor {
  return {
    id: 42,
    name: 'primary',
    provider: 'openai',
    api_mode: 'chat_completions',
    endpoint: 'https://api.example.com',
    api_key_masked: 'sk-t***',
    primary_model: 'gpt-4o-mini',
    extra_models: [],
    group_name: '',
    enabled: true,
    interval_seconds: 60,
    jitter_seconds: 0,
    last_checked_at: null,
    created_by: 1,
    created_at: '2026-07-16T00:00:00Z',
    updated_at: '2026-07-16T00:00:00Z',
    primary_status: '',
    primary_latency_ms: null,
    availability_7d: 73.25,
    availability_reset_active: false,
    extra_models_status: [],
    template_id: null,
    extra_headers: {},
    body_override_mode: 'off',
    body_override: null,
    check_mode: 'probe',
    account_id: null,
    ...overrides,
  }
}

function mountView() {
  return mount(ChannelMonitorView, {
    global: {
      stubs: {
        AppLayout: AppLayoutStub,
        TablePageLayout: TablePageLayoutStub,
        DataTable: DataTableStub,
        MonitorFiltersBar: true,
        Pagination: true,
        ConfirmDialog: true,
        EmptyState: true,
        HelpTooltip: true,
        Toggle: true,
        MonitorFormDialog: true,
        MonitorTemplateManagerDialog: true,
        MonitorRunResultDialog: true,
        MonitorPrimaryModelCell: true,
        MonitorAvailabilityResetDialog: ResetDialogStub,
      },
    },
  })
}

describe('ChannelMonitorView availability reset', () => {
  const monitor = makeMonitor()

  beforeEach(() => {
    for (const fn of [listMonitors, availabilityReset, clearAvailabilityReset, showSuccess, showError]) fn.mockReset()
    listMonitors.mockResolvedValue({ items: [monitor], total: 1, page: 1, page_size: 20, pages: 1 })
    availabilityReset.mockResolvedValue(makeMonitor({ availability_reset_active: true }))
    clearAvailabilityReset.mockResolvedValue(makeMonitor({ availability_reset_active: false }))
  })

  it('opens the dialog and submits the selected baseline', async () => {
    const wrapper = mountView()
    await flushPromises()

    wrapper.findComponent(MonitorActionsCell).vm.$emit('availability-reset', monitor)
    await wrapper.vm.$nextTick()
    expect(wrapper.get('[data-testid="availability-reset-dialog"]').exists()).toBe(true)

    await wrapper.get('[data-testid="dialog-submit"]').trigger('click')
    await flushPromises()

    expect(availabilityReset).toHaveBeenCalledWith(42, { availability_pct: 91.25, degraded_bars: 3 })
    expect(showSuccess).toHaveBeenCalledWith('admin.channelMonitor.availabilityReset.success')
    expect(listMonitors.mock.calls.length).toBeGreaterThan(1)
    wrapper.unmount()
  })

  it('cancels an active reset and refreshes the list', async () => {
    const active = makeMonitor({ availability_reset_active: true })
    listMonitors.mockResolvedValue({ items: [active], total: 1, page: 1, page_size: 20, pages: 1 })
    const wrapper = mountView()
    await flushPromises()

    wrapper.findComponent(MonitorActionsCell).vm.$emit('availability-reset-cancel', active)
    await flushPromises()

    expect(clearAvailabilityReset).toHaveBeenCalledWith(42)
    expect(showSuccess).toHaveBeenCalledWith('admin.channelMonitor.availabilityReset.cancelSuccess')
    expect(listMonitors.mock.calls.length).toBeGreaterThan(1)
    wrapper.unmount()
  })
})
