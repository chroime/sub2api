import { defineComponent } from 'vue'
import { flushPromises, mount } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import type { AdminGroup } from '@/types'
import GroupsView from '@/views/admin/GroupsView.vue'
import { adminAPI } from '@/api/admin'
import GroupBalancePrechargeSettings from '@/components/admin/group/GroupBalancePrechargeSettings.vue'
import Select from '@/components/common/Select.vue'

vi.mock('@/api/admin/settings', () => ({
  getGroupBalancePrechargeSettings: vi.fn(async (groupId: number) => ({
    group_id: groupId,
    settings: { mode: 'inherit', threshold: 0, amount: 0 },
    global: { enabled: false, threshold: 0, amount: 0 },
    effective: { enabled: false, threshold: 0, amount: 0 },
  })),
  updateGroupBalancePrechargeSettings: vi.fn(),
}))

const {
  listGroups,
  duplicateGroup,
  updateGroup,
  getModelAllowlistCandidates,
  getUsageSummary,
  getCapacitySummary,
  getLiveCapability,
  showSuccess,
  showError
} = vi.hoisted(() => ({
  listGroups: vi.fn(),
  duplicateGroup: vi.fn(),
  updateGroup: vi.fn(),
  getModelAllowlistCandidates: vi.fn(),
  getUsageSummary: vi.fn(),
  getCapacitySummary: vi.fn(),
  getLiveCapability: vi.fn(),
  showSuccess: vi.fn(),
  showError: vi.fn()
}))

const authState = vi.hoisted(() => ({ isSimpleMode: false }))

vi.mock('@/api/admin', () => ({
  adminAPI: {
    groups: {
      list: listGroups,
      duplicate: duplicateGroup,
      getModelAllowlistCandidates,
      getUsageSummary,
      getCapacitySummary,
      getLiveCapability,
      getAll: vi.fn(),
      create: vi.fn(),
      update: updateGroup,
      delete: vi.fn(),
      updateSortOrder: vi.fn()
    },
    accounts: {
      list: vi.fn(),
      getById: vi.fn()
    }
  }
}))

vi.mock('@/stores/app', () => ({
  useAppStore: () => ({ showSuccess, showError })
}))

vi.mock('@/stores/auth', () => ({
  useAuthStore: () => authState
}))

vi.mock('@/stores/onboarding', () => ({
  useOnboardingStore: () => ({
    isCurrentStep: vi.fn(() => false),
    nextStep: vi.fn()
  })
}))

vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return {
    ...actual,
    useI18n: () => ({ t: (key: string) => key })
  }
})

const sourceGroup: AdminGroup = {
  id: 42,
  name: 'Primary',
  description: null,
  platform: 'openai',
  rate_multiplier: 1,
  rpm_limit: 0,
  is_exclusive: false,
  status: 'active',
  subscription_type: 'standard',
  daily_limit_usd: null,
  weekly_limit_usd: null,
  monthly_limit_usd: null,
  allow_image_generation: false,
  allow_batch_image_generation: false,
  image_rate_independent: false,
  image_rate_multiplier: 1,
  batch_image_discount_multiplier: 0.5,
  batch_image_hold_multiplier: 0.6,
  image_price_1k: null,
  image_price_2k: null,
  image_price_4k: null,
  video_rate_independent: false,
  video_rate_multiplier: 1,
  video_price_480p: null,
  video_price_720p: null,
  video_price_1080p: null,
  web_search_price_per_call: null,
  peak_rate_enabled: false,
  peak_start: '',
  peak_end: '',
  peak_rate_multiplier: 1,
  claude_code_only: false,
  fallback_group_id: null,
  fallback_group_id_on_invalid_request: null,
  allow_messages_dispatch: false,
  default_mapped_model: '',
  messages_dispatch_model_config: undefined,
  require_oauth_only: false,
  require_privacy_set: false,
  created_at: '2026-07-16T00:00:00Z',
  updated_at: '2026-07-16T00:00:00Z',
  model_routing: null,
  model_routing_enabled: false,
  mcp_xml_inject: true,
  supported_model_scopes: [],
  account_count: 1,
  active_account_count: 1,
  rate_limited_account_count: 0,
  model_allowlist: undefined,
  sort_order: 10
}

const AppLayoutStub = defineComponent({
  template: '<main><slot /></main>'
})

const TablePageLayoutStub = defineComponent({
  template: '<section><slot name="filters" /><slot name="table" /><slot name="pagination" /></section>'
})

const DataTableStub = defineComponent({
  props: {
    data: { type: Array, default: () => [] },
    columns: { type: Array, default: () => [] },
    loading: { type: Boolean, default: false }
  },
  template: '<div><div v-for="row in data" :key="row.id"><slot name="cell-streaming_ack_enabled" :value="row.streaming_ack_enabled" :row="row" /><slot name="cell-actions" :row="row" /></div></div>'
})

const BaseDialogStub = defineComponent({
  props: {
    show: { type: Boolean, default: false }
  },
  template: '<div v-if="show"><slot /><slot name="footer" /></div>'
})

function mountView() {
  return mount(GroupsView, {
    global: {
      stubs: {
        AppLayout: AppLayoutStub,
        TablePageLayout: TablePageLayoutStub,
        DataTable: DataTableStub,
        Pagination: true,
        BaseDialog: BaseDialogStub,
        ConfirmDialog: true,
        EmptyState: true,
        Select: true,
        PlatformIcon: true,
        Icon: true,
        GroupCapacityBadge: true,
        GroupRateMultipliersModal: true,
        GroupRPMOverridesModal: true,
        VueDraggable: true
      }
    }
  })
}

describe('GroupsView duplicate action', () => {
  beforeEach(() => {
    authState.isSimpleMode = false
    localStorage.clear()
    vi.spyOn(console, 'error').mockImplementation(() => {})
    for (const fn of [
      listGroups,
      duplicateGroup,
      updateGroup,
      getModelAllowlistCandidates,
      getUsageSummary,
      getCapacitySummary,
      getLiveCapability,
      showSuccess,
      showError
    ]) {
      fn.mockReset()
    }

    listGroups.mockResolvedValue({
      items: [sourceGroup],
      total: 1,
      page: 1,
      page_size: 20,
      pages: 1
    })
    duplicateGroup.mockResolvedValue({
      ...sourceGroup,
      id: 43,
      name: 'Primary (Copy)',
      status: 'inactive'
    })
    getModelAllowlistCandidates.mockResolvedValue([])
    getUsageSummary.mockResolvedValue([])
    getCapacitySummary.mockResolvedValue([])
    getLiveCapability.mockResolvedValue({ supported: false })
    vi.mocked(adminAPI.groups.create).mockReset()
    vi.mocked(adminAPI.groups.getAll).mockResolvedValue([])
  })

  afterEach(() => {
    vi.restoreAllMocks()
  })

  it.each([
    { value: null, label: 'admin.groups.streamingACK.legacy' },
    { value: false, label: 'admin.groups.streamingACK.disabled' },
    { value: true, label: 'admin.groups.streamingACK.enabled' }
  ])('shows group ACK status for $value', async ({ value, label }) => {
    listGroups.mockResolvedValue({ items: [{ ...sourceGroup, streaming_ack_enabled: value }], total: 1, page: 1, page_size: 20, pages: 1 })
    const wrapper = mountView()
    try {
      await flushPromises()
      expect(wrapper.text()).toContain(label)
    } finally { wrapper.unmount() }
  })

  it.each(['openai', 'anthropic', 'gemini', 'antigravity', 'grok', 'kimi', 'zhipu', 'deepseek', 'minimax', 'opencode_go', 'composite'])('saves group ACK independently for %s', async (platform) => {
    listGroups.mockResolvedValue({ items: [{ ...sourceGroup, platform, streaming_ack_enabled: null }], total: 1, page: 1, page_size: 20, pages: 1 })
    updateGroup.mockResolvedValue(sourceGroup)
    const wrapper = mountView()
    try {
      await flushPromises()
      await wrapper.findAll('button').find((button) => button.text() === 'common.edit')!.trigger('click')
      await flushPromises()
      const control = wrapper.get('[data-testid="group-streaming-ack"]')
      expect(control.text()).toContain('admin.groups.streamingACK.legacyHint')
      await control.get('[data-testid="group-ack-enabled"]').trigger('click')
      await wrapper.get('#edit-group-form').trigger('submit')
      await flushPromises()
      expect(updateGroup).toHaveBeenCalledWith(42, expect.objectContaining({ streaming_ack_enabled: true }))
    } finally { wrapper.unmount() }
  })

  it('keeps legacy group ACK omitted when saving unrelated settings', async () => {
    updateGroup.mockResolvedValue(sourceGroup)
    const wrapper = mountView()
    try {
      await flushPromises()
      await wrapper.findAll('button').find((button) => button.text() === 'common.edit')!.trigger('click')
      await flushPromises()
      expect(wrapper.get('[data-testid="group-streaming-ack"]').text()).toContain('admin.groups.streamingACK.legacyHint')
      await wrapper.get('#edit-group-form').trigger('submit')
      await flushPromises()
      expect(updateGroup).toHaveBeenCalledTimes(1)
      expect(updateGroup.mock.calls[0]![1]).not.toHaveProperty('streaming_ack_enabled')
    } finally { wrapper.unmount() }
  })

  it('retains an explicit group ACK choice after save fails', async () => {
    updateGroup.mockRejectedValue(new Error('save failed'))
    const wrapper = mountView()
    try {
      await flushPromises()
      await wrapper.findAll('button').find((button) => button.text() === 'common.edit')!.trigger('click')
      await flushPromises()
      await wrapper.get('[data-testid="group-ack-disabled"]').trigger('click')
      await wrapper.get('#edit-group-form').trigger('submit')
      await flushPromises()
      expect(updateGroup).toHaveBeenCalledWith(42, expect.objectContaining({ streaming_ack_enabled: false }))
      expect(wrapper.get('[data-testid="group-ack-disabled"]').attributes('aria-pressed')).toBe('true')
      expect(showError).toHaveBeenCalledWith('save failed')
    } finally { wrapper.unmount() }
  })

  it('defaults newly created groups to ACK disabled', async () => {
    vi.mocked(adminAPI.groups.create).mockResolvedValue(sourceGroup)
    const wrapper = mountView()
    try {
      await flushPromises()
      await wrapper.get('[data-tour="groups-create-btn"]').trigger('click')
      await flushPromises()
      expect(wrapper.get('[data-testid="group-ack-disabled"]').attributes('aria-pressed')).toBe('true')
      await wrapper.get('#create-group-form input').setValue('New group')
      await wrapper.get('#create-group-form').trigger('submit')
      await flushPromises()
      expect(adminAPI.groups.create).toHaveBeenCalledWith(expect.objectContaining({ streaming_ack_enabled: false }))
    } finally { wrapper.unmount() }
  })

  it('loads precharge settings for the saved group being edited and omits them in simple mode', async () => {
    const wrapper = mountView()
    await flushPromises()
    expect(wrapper.findComponent(GroupBalancePrechargeSettings).exists()).toBe(false)
    const editButton = wrapper.findAll('button').find((button) => button.text() === 'common.edit')!
    await editButton.trigger('click')
    await flushPromises()
    expect(wrapper.getComponent(GroupBalancePrechargeSettings).props('groupId')).toBe(42)
    wrapper.unmount()

    authState.isSimpleMode = true
    const simple = mountView()
    await flushPromises()
    await simple.findAll('button').find((button) => button.text() === 'common.edit')!.trigger('click')
    await flushPromises()
    expect(simple.findComponent(GroupBalancePrechargeSettings).exists()).toBe(false)
    expect(simple.find('[data-testid="group-streaming-ack"]').exists()).toBe(true)
    simple.unmount()
  })

  it('duplicates the selected group, reports success, and refreshes the list', async () => {
    const wrapper = mountView()
    await flushPromises()

    await wrapper.get('[data-testid="group-duplicate"]').trigger('click')
    await flushPromises()

    expect(duplicateGroup).toHaveBeenCalledTimes(1)
    expect(duplicateGroup).toHaveBeenCalledWith(42)
    expect(showSuccess).toHaveBeenCalledWith('admin.groups.duplicateSuccess')
    expect(listGroups).toHaveBeenCalledTimes(2)
    wrapper.unmount()
  })

  it('hides advanced group actions in simple mode', async () => {
    authState.isSimpleMode = true
    const compositeGroup = { ...sourceGroup, platform: 'composite' }
    listGroups.mockResolvedValueOnce({ items: [compositeGroup], total: 1, page: 1, page_size: 20, pages: 1 })
    const wrapper = mountView()
    await flushPromises()

    expect(wrapper.find('[data-testid="group-duplicate"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="group-composite-routes"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="group-rate-multipliers"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="group-rpm-overrides"]').exists()).toBe(false)
    wrapper.unmount()
  })

  it('ignores repeated clicks while the duplicate request is in flight', async () => {
    let resolveDuplicate!: (value: AdminGroup) => void
    duplicateGroup.mockImplementationOnce(
      () => new Promise<AdminGroup>((resolve) => { resolveDuplicate = resolve })
    )
    const wrapper = mountView()
    await flushPromises()

    const button = wrapper.get('[data-testid="group-duplicate"]')
    void button.trigger('click')
    void button.trigger('click')
    await wrapper.vm.$nextTick()

    expect(duplicateGroup).toHaveBeenCalledTimes(1)
    expect(button.attributes('disabled')).toBeDefined()
    expect(button.attributes('title')).toBe('admin.groups.duplicating')

    resolveDuplicate({ ...sourceGroup, id: 43, name: 'Primary (Copy)', status: 'inactive' })
    await flushPromises()
    expect(wrapper.get('[data-testid="group-duplicate"]').attributes('disabled')).toBeUndefined()
    wrapper.unmount()
  })

  it('shows the API error and restores the action when duplication fails', async () => {
    duplicateGroup.mockRejectedValueOnce(new Error('duplicate failed'))
    const wrapper = mountView()
    await flushPromises()

    await wrapper.get('[data-testid="group-duplicate"]').trigger('click')
    await flushPromises()

    expect(showError).toHaveBeenCalledWith('duplicate failed')
    expect(wrapper.get('[data-testid="group-duplicate"]').attributes('disabled')).toBeUndefined()
    wrapper.unmount()
  })

  it('does not report a successful duplicate as failed when the refresh fails', async () => {
    listGroups
      .mockResolvedValueOnce({
        items: [sourceGroup],
        total: 1,
        page: 1,
        page_size: 20,
        pages: 1
      })
      .mockRejectedValueOnce(new Error('refresh failed'))
    const wrapper = mountView()
    await flushPromises()

    await wrapper.get('[data-testid="group-duplicate"]').trigger('click')
    await flushPromises()

    expect(showSuccess).toHaveBeenCalledWith('admin.groups.duplicateSuccess')
    expect(showError).toHaveBeenCalledWith('admin.groups.failedToLoad')
    expect(showError).not.toHaveBeenCalledWith('admin.groups.duplicateFailed')
    wrapper.unmount()
  })

  it('shows the standardized API message when updating a group fails', async () => {
    updateGroup.mockRejectedValueOnce({
      status: 409,
      code: 409,
      message: 'group name already exists',
      reason: 'GROUP_EXISTS'
    })
    const wrapper = mountView()
    await flushPromises()

    const editButton = wrapper.findAll('button').find((button) => button.text() === 'common.edit')
    expect(editButton).toBeTruthy()
    await editButton!.trigger('click')
    await flushPromises()
    await wrapper.get('#edit-group-form').trigger('submit')
    await flushPromises()

    expect(updateGroup).toHaveBeenCalledTimes(1)
    expect(showError).toHaveBeenCalledWith('group name already exists')
    wrapper.unmount()
  })

  it('loads, edits, and saves custom reasoning multipliers for group pricing', async () => {
    const group = {
      ...sourceGroup,
      model_pricing: [{
        platform: 'openai', models: ['example-model'], billing_mode: 'token',
        input_price: 3e-6, output_price: 15e-6, cache_write_price: null, cache_read_price: null,
        image_input_price: null, image_output_price: null, per_request_price: null,
        reasoning_effort_multipliers: { high: 1.5, max: 3 }, intervals: [], time_pricing: null,
      }],
    }
    listGroups.mockResolvedValue({ items: [group], total: 1, page: 1, page_size: 20, pages: 1 })
    updateGroup.mockResolvedValue(group)
    const wrapper = mountView()
    await flushPromises()
    await wrapper.findAll('button').find(button => button.text() === 'common.edit')!.trigger('click')
    await flushPromises()

    expect(wrapper.get<HTMLInputElement>('[data-reasoning-effort="high"]').element.value).toBe('1.5')
    await wrapper.get('[data-reasoning-effort="high"]').setValue('0.5')
    await wrapper.get('[data-reasoning-effort="max"]').setValue('')
    await wrapper.get('#edit-group-form').trigger('submit')
    await flushPromises()
    expect(updateGroup).toHaveBeenCalledWith(42, expect.objectContaining({
      model_pricing: [expect.objectContaining({ reasoning_effort_multipliers: { high: 0.5 } })],
    }))
    wrapper.unmount()
  })

  it('blocks saving an invalid group reasoning multiplier and allows clearing it', async () => {
    const group = {
      ...sourceGroup,
      model_pricing: [{
        platform: 'openai', models: ['example-model'], billing_mode: 'token',
        input_price: 3e-6, output_price: 15e-6, cache_write_price: null, cache_read_price: null,
        image_input_price: null, image_output_price: null, per_request_price: null,
        reasoning_effort_multipliers: { high: 1.5 }, intervals: [], time_pricing: null,
      }],
    }
    listGroups.mockResolvedValue({ items: [group], total: 1, page: 1, page_size: 20, pages: 1 })
    updateGroup.mockResolvedValue(group)
    const wrapper = mountView()
    await flushPromises()
    await wrapper.findAll('button').find(button => button.text() === 'common.edit')!.trigger('click')
    await flushPromises()

    await wrapper.get('[data-reasoning-effort="high"]').setValue('0')
    await wrapper.get('#edit-group-form').trigger('submit')
    expect(updateGroup).not.toHaveBeenCalled()
    expect(showError).toHaveBeenCalledWith(expect.stringContaining('reasoningEffortMultiplierPositive'))

    await wrapper.get('[data-testid="reasoning-effort-multipliers"] button').trigger('click')
    await wrapper.get('#edit-group-form').trigger('submit')
    await flushPromises()
    expect(updateGroup).toHaveBeenCalledWith(42, expect.objectContaining({
      model_pricing: [expect.objectContaining({ reasoning_effort_multipliers: null })],
    }))
    wrapper.unmount()
  })

  it('updates manifest controls immediately and submits the displayed selection', async () => {
    vi.useFakeTimers()
    vi.mocked(adminAPI.accounts.list).mockResolvedValue({
      items: [{ id: 5, name: 'Manifest account' }]
    } as never)
    updateGroup.mockResolvedValue(sourceGroup)
    const wrapper = mountView()
    try {
      await flushPromises()
      const editButton = wrapper.findAll('button').find((button) => button.text() === 'common.edit')!
      await editButton.trigger('click')
      await flushPromises()

      const toggle = wrapper.get('[data-testid="codex-manifest-toggle"]')
      await toggle.trigger('click')
      expect(toggle.attributes('aria-checked')).toBe('true')
      const search = wrapper.get('[data-testid="codex-manifest-search"]')
      await search.trigger('focus')
      await vi.advanceTimersByTimeAsync(300)
      await flushPromises()
      expect(adminAPI.accounts.list).toHaveBeenCalledWith(
        1, 20, { search: '', platform: 'openai', group: '42' }, expect.anything()
      )
      await wrapper.get('[data-testid="codex-manifest-dropdown"] button').trigger('click')
      expect(wrapper.get('[data-testid="codex-manifest-selected-tags"]').text()).toContain('Manifest account')

      await wrapper.get('[aria-label="remove account 5"]').trigger('click')
      expect(wrapper.find('[data-testid="codex-manifest-selected-tags"]').exists()).toBe(false)
      await wrapper.get('#edit-group-form').trigger('submit')
      expect(updateGroup).not.toHaveBeenCalled()
      expect(wrapper.find('[data-testid="codex-manifest-validation-error"]').exists()).toBe(true)

      await search.trigger('focus')
      await wrapper.get('[data-testid="codex-manifest-dropdown"] button').trigger('click')
      expect(wrapper.get('[data-testid="codex-manifest-selected-tags"]').text()).toContain('Manifest account')
      const fallback = wrapper.get('[data-testid="codex-manifest-fallback-toggle"]')
      await fallback.trigger('click')
      expect(fallback.attributes('aria-checked')).toBe('true')
      await fallback.trigger('click')
      expect(fallback.attributes('aria-checked')).toBe('false')
      await fallback.trigger('click')

      await toggle.trigger('click')
      expect(wrapper.find('[data-testid="codex-manifest-search"]').exists()).toBe(false)
      await toggle.trigger('click')
      expect(wrapper.get('[data-testid="codex-manifest-selected-tags"]').text()).toContain('Manifest account')
      await wrapper.get('#edit-group-form').trigger('submit')
      await flushPromises()
      expect(updateGroup).toHaveBeenCalledWith(42, expect.objectContaining({
        codex_models_manifest_config: {
          enabled: true, account_ids: [5], fallback_to_scheduler: true
        }
      }))

      // Reopening reads the saved group afresh, without retaining the prior draft.
      await editButton.trigger('click')
      await flushPromises()
      expect(wrapper.get('[data-testid="codex-manifest-toggle"]').attributes('aria-checked')).toBe('false')
      expect(wrapper.find('[data-testid="codex-manifest-search"]').exists()).toBe(false)
    } finally {
      wrapper.unmount()
      vi.useRealTimers()
    }
  })

  it('submits disabled input routing for a new group and resets discarded rule drafts', async () => {
    const wrapper = mountView()
    await flushPromises()
    const openCreate = () => wrapper.findAll('button').find(button => button.text() === 'admin.groups.createGroup')!
    await openCreate().trigger('click')
    await flushPromises()
    expect(wrapper.find('[data-testid="input-routing-toggle"]').exists()).toBe(true)
    expect(wrapper.get('[data-testid="input-routing-toggle"]').attributes('aria-checked')).toBe('false')
    await wrapper.get('[data-testid="input-routing-add-rule"]').trigger('click')
    expect(wrapper.findAll('[data-testid="input-routing-rule"]')).toHaveLength(1)
    await wrapper.findAll('button').find(button => button.text() === 'common.cancel')!.trigger('click')
    await openCreate().trigger('click')
    await flushPromises()
    expect(wrapper.findAll('[data-testid="input-routing-rule"]')).toHaveLength(0)
    await wrapper.get('[data-tour="group-form-name"]').setValue('Fresh group')
    await wrapper.get('#create-group-form').trigger('submit')
    await flushPromises()
    expect(adminAPI.groups.create).toHaveBeenCalledWith(expect.objectContaining({
      input_routing: { enabled: false, rules: [] },
    }))
    wrapper.unmount()
  })

  it('loads input routing without mutating the source and persists edited values and explicit clearing', async () => {
    const rule = {
      id: 'small-stream', name: 'Small stream', enabled: true, request_type: 'stream',
      input_tokens_mode: 'range', min_input_tokens: 0, max_input_tokens: 8000, account_ids: [5], group_ids: [], fallback_to_original: false,
    }
    const group = { ...sourceGroup, input_routing: { enabled: true, rules: [rule] } }
    listGroups.mockResolvedValue({ items: [group], total: 1, page: 1, page_size: 20, pages: 1 })
    updateGroup.mockResolvedValue(group)
    const wrapper = mountView()
    await flushPromises()
    const edit = () => wrapper.findAll('button').find(button => button.text() === 'common.edit')!
    await edit().trigger('click')
    await flushPromises()
    expect(wrapper.findAll('[data-testid="input-routing-rule"]')).toHaveLength(1)
    expect(wrapper.get<HTMLInputElement>('[data-testid="input-routing-min-tokens"]').element.value).toBe('0')
    expect(wrapper.get<HTMLInputElement>('[data-testid="input-routing-max-tokens"]').element.value).toBe('8000')
    await wrapper.get('[data-testid="input-routing-max-tokens"]').setValue('4096')
    await wrapper.get('[data-testid="input-routing-fallback"]').trigger('click')
    expect(group.input_routing.rules[0]).toEqual(rule)
    expect(group.input_routing.rules[0]!.max_input_tokens).toBe(8000)
    await wrapper.get('#edit-group-form').trigger('submit')
    await flushPromises()
    expect(updateGroup).toHaveBeenCalledWith(42, expect.objectContaining({
      input_routing: { enabled: true, rules: [{ ...rule, max_input_tokens: 4096, fallback_to_original: true }] },
    }))
    await edit().trigger('click')
    await flushPromises()
    expect(wrapper.get<HTMLInputElement>('[data-testid="input-routing-max-tokens"]').element.value).toBe('8000')
    await wrapper.get('[data-testid="input-routing-remove-rule"]').trigger('click')
    await wrapper.get('[data-testid="input-routing-toggle"]').trigger('click')
    await wrapper.get('#edit-group-form').trigger('submit')
    await flushPromises()
    expect(updateGroup).toHaveBeenLastCalledWith(42, expect.objectContaining({ input_routing: { enabled: false, rules: [] } }))
    wrapper.unmount()
  })

  it('blocks incomplete create rules and sends the configured routing after selecting a target', async () => {
    vi.mocked(adminAPI.groups.getAll).mockResolvedValue([{ ...sourceGroup, id: 88, name: 'Candidate group', platform: 'anthropic' }])
    const wrapper = mountView()
    await flushPromises()
    await wrapper.findAll('button').find(button => button.text() === 'admin.groups.createGroup')!.trigger('click')
    await flushPromises()
    await wrapper.get('[data-tour="group-form-name"]').setValue('Routed group')
    await wrapper.get('[data-testid="input-routing-toggle"]').trigger('click')
    await wrapper.get('[data-testid="input-routing-add-rule"]').trigger('click')
    await wrapper.get('[data-testid="input-routing-rule-toggle"]').trigger('click')
    await wrapper.get('[data-testid="input-routing-name"]').setValue('Small sync')
    wrapper.findAllComponents(Select).find(select => select.attributes('data-testid') === 'input-routing-request-type')!.vm.$emit('update:modelValue', 'sync')
    await wrapper.vm.$nextTick()
    wrapper.findAllComponents(Select).find(select => select.attributes('data-testid') === 'input-routing-input-mode')!.vm.$emit('update:modelValue', 'range')
    await wrapper.vm.$nextTick()
    await wrapper.get('[data-testid="input-routing-max-tokens"]').setValue('1000')
    await wrapper.get('#create-group-form').trigger('submit')
    expect(adminAPI.groups.create).not.toHaveBeenCalled()
    expect(wrapper.get('[data-testid="input-routing-validation-error"]').text()).toContain('targetsError')
    await wrapper.get('[data-group-id="88"]').setValue(true)
    await wrapper.get('#create-group-form').trigger('submit')
    await flushPromises()
    expect(adminAPI.groups.create).toHaveBeenCalledWith(expect.objectContaining({
      input_routing: { enabled: true, rules: [{
        id: expect.any(String), name: 'Small sync', enabled: true, request_type: 'sync',
        input_tokens_mode: 'range', min_input_tokens: 0, max_input_tokens: 1000, account_ids: [], group_ids: [88], fallback_to_original: false,
      }] },
    }))
    wrapper.unmount()
  })

  it.each(['range', 'all'] as const)('loads and saves server-copied %s input routing when editing a duplicated group', async mode => {
    const inputRouting = { enabled: true, rules: [{
      id: 'copy-route', name: 'Copied route', enabled: true, request_type: 'all' as const,
      input_tokens_mode: mode, min_input_tokens: 0, max_input_tokens: 2000, account_ids: [5], group_ids: [], fallback_to_original: false,
    }] }
    const copy = { ...sourceGroup, id: 43, name: 'Primary (Copy)', input_routing: inputRouting }
    duplicateGroup.mockResolvedValue(copy)
    listGroups.mockResolvedValueOnce({ items: [sourceGroup], total: 1, page: 1, page_size: 20, pages: 1 })
      .mockResolvedValue({ items: [copy], total: 1, page: 1, page_size: 20, pages: 1 })
    const wrapper = mountView()
    await flushPromises()
    await wrapper.get('[data-testid="group-duplicate"]').trigger('click')
    await flushPromises()
    await wrapper.findAll('button').find(button => button.text() === 'common.edit')!.trigger('click')
    await flushPromises()
    expect(wrapper.get('[data-testid="input-routing-toggle"]').attributes('aria-checked')).toBe('true')
    expect(wrapper.get<HTMLInputElement>('[data-testid="input-routing-name"]').element.value).toBe('Copied route')
    const inputMode = wrapper.findAllComponents(Select).find(select => select.attributes('data-testid') === 'input-routing-input-mode')!
    expect(inputMode.props('modelValue')).toBe(mode)
    expect(wrapper.find('[data-testid="input-routing-max-tokens"]').exists()).toBe(mode === 'range')
    if (mode === 'range') expect(wrapper.get<HTMLInputElement>('[data-testid="input-routing-max-tokens"]').element.value).toBe('2000')
    await wrapper.get('#edit-group-form').trigger('submit')
    await flushPromises()
    expect(updateGroup).toHaveBeenCalledWith(43, expect.objectContaining({ input_routing: inputRouting }))
    wrapper.unmount()
  })

})
