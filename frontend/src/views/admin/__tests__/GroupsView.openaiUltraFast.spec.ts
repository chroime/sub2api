import { defineComponent } from 'vue'
import { flushPromises, mount } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import GroupsView from '../GroupsView.vue'
import Select from '@/components/common/Select.vue'
import type { AdminGroup } from '@/types'

const { listGroups, createGroup, updateGroup, showError } = vi.hoisted(() => ({
  listGroups: vi.fn(), createGroup: vi.fn(), updateGroup: vi.fn(), showError: vi.fn()
}))

vi.mock('@/api/admin', () => ({
  adminAPI: {
    groups: {
      list: listGroups, create: createGroup, update: updateGroup,
      getAll: vi.fn(async () => []), getModelAllowlistCandidates: vi.fn(async () => []),
      getUsageSummary: vi.fn(async () => []), getCapacitySummary: vi.fn(async () => []),
      getLiveCapability: vi.fn(async () => ({ supported: false }))
    },
    accounts: { list: vi.fn(async () => ({ items: [] })), getById: vi.fn() }
  }
}))
vi.mock('@/stores/app', () => ({ useAppStore: () => ({ showError, showSuccess: vi.fn() }) }))
vi.mock('@/stores/auth', () => ({ useAuthStore: () => ({ isSimpleMode: false, isAdmin: true }) }))
vi.mock('@/stores/onboarding', () => ({
  useOnboardingStore: () => ({ isCurrentStep: () => false, nextStep: vi.fn() })
}))
vi.mock('vue-i18n', async () => ({
  ...await vi.importActual<typeof import('vue-i18n')>('vue-i18n'),
  useI18n: () => ({ t: (key: string) => key })
}))

function group(overrides: Partial<AdminGroup> = {}): AdminGroup {
  return {
    id: 42, name: 'OpenAI', platform: 'openai', status: 'active', subscription_type: 'standard',
    rate_multiplier: 1, rpm_limit: 0, is_exclusive: false, force_openai_fast: false, free_openai_fast: false,
    model_pricing: [], model_routing: null, model_routing_enabled: false,
    supported_model_scopes: [], account_count: 0, active_account_count: 0,
    ...overrides
  } as AdminGroup
}

const DialogStub = defineComponent({
  props: ['show'], emits: ['close'],
  template: '<div v-if="show"><button data-testid="close-dialog" @click="$emit(\'close\')">close</button><slot /><slot name="footer" /></div>'
})
const ValidationFieldStub = defineComponent({
  setup(_, { expose }) {
    expose({ validate: () => true, resetValidation: () => {} })
    return () => null
  }
})

function mountView() {
  return mount(GroupsView, {
    global: {
      stubs: {
        AppLayout: { template: '<main><slot /></main>' },
        TablePageLayout: { template: '<section><slot name="filters" /><slot name="table" /></section>' },
        DataTable: {
          props: ['data'],
          template: '<div><div v-for="row in data" :key="row.id"><slot name="cell-actions" :row="row" /></div></div>'
        },
        BaseDialog: DialogStub, Select: true, Icon: true, PlatformIcon: true, Pagination: true,
        ConfirmDialog: true, EmptyState: true, GroupCapacityBadge: true, VueDraggable: true,
        GroupRateMultipliersModal: true, GroupRPMOverridesModal: true, GroupBalancePrechargeSettings: true,
        GroupInputRoutingEditor: ValidationFieldStub, ReasoningEffortPolicyFields: ValidationFieldStub,
        CodexManifestAccountsField: ValidationFieldStub, PricingEntryCard: true
      }
    }
  })
}

async function setCreatePlatform(wrapper: ReturnType<typeof mountView>, platform: string) {
  const selector = wrapper.findAllComponents(Select).find(control => control.attributes('data-tour') === 'group-form-platform')!
  selector.vm.$emit('update:modelValue', platform)
  await flushPromises()
}

async function openCreate(wrapper: ReturnType<typeof mountView>) {
  await flushPromises()
  await wrapper.get('[data-tour="groups-create-btn"]').trigger('click')
  await setCreatePlatform(wrapper, 'openai')
  await wrapper.get('#create-group-form input').setValue('New group')
}

async function openEdit(wrapper: ReturnType<typeof mountView>, index = 0) {
  await flushPromises()
  await wrapper.findAll('button').filter(button => button.text() === 'common.edit')[index]!.trigger('click')
  await flushPromises()
}

const checked = (wrapper: ReturnType<typeof mountView>, control: string) =>
  wrapper.get(`[data-testid="${control}"]`).attributes('aria-checked')

describe('GroupsView forced UltraFast', () => {
  beforeEach(() => {
    localStorage.clear()
    vi.clearAllMocks()
    listGroups.mockResolvedValue({ items: [group()], total: 1, page: 1, page_size: 20, pages: 1 })
    createGroup.mockResolvedValue(group())
    updateGroup.mockResolvedValue(group())
  })
  afterEach(() => vi.restoreAllMocks())

  it('places UltraFast immediately after Fast and creates it with Fast disabled while preserving free Fast', async () => {
    const wrapper = mountView()
    try {
      await openCreate(wrapper)
      expect(checked(wrapper, 'create-force-openai-ultrafast')).toBe('false')
      expect(wrapper.findAll('[data-testid^="create-"][role="switch"]').map(control => control.attributes('data-testid')))
        .toEqual(['create-force-openai-fast', 'create-force-openai-ultrafast', 'create-free-openai-fast'])
      await wrapper.get('[data-testid="create-force-openai-fast"]').trigger('click')
      await wrapper.get('[data-testid="create-free-openai-fast"]').trigger('click')
      await wrapper.get('[data-testid="create-force-openai-ultrafast"]').trigger('click')
      expect(checked(wrapper, 'create-force-openai-fast')).toBe('false')
      expect(checked(wrapper, 'create-force-openai-ultrafast')).toBe('true')
      expect(checked(wrapper, 'create-free-openai-fast')).toBe('true')
      await wrapper.get('#create-group-form').trigger('submit')
      await flushPromises()
      expect(createGroup).toHaveBeenCalledWith(expect.objectContaining({
        platform: 'openai', force_openai_fast: false, force_openai_ultrafast: true, free_openai_fast: true
      }))
      expect(showError).not.toHaveBeenCalled()
    } finally { wrapper.unmount() }
  })

  it('loads saved UltraFast and turns it off when Fast is enabled before updating', async () => {
    listGroups.mockResolvedValue({ items: [group({ force_openai_ultrafast: true, free_openai_fast: true })], total: 1 })
    const wrapper = mountView()
    try {
      await openEdit(wrapper)
      expect(checked(wrapper, 'edit-force-openai-ultrafast')).toBe('true')
      await wrapper.get('[data-testid="edit-force-openai-fast"]').trigger('click')
      expect(checked(wrapper, 'edit-force-openai-ultrafast')).toBe('false')
      await wrapper.get('#edit-group-form').trigger('submit')
      await flushPromises()
      expect(updateGroup).toHaveBeenCalledWith(42, expect.objectContaining({
        force_openai_fast: true, force_openai_ultrafast: false, free_openai_fast: true
      }))
    } finally { wrapper.unmount() }
  })

  it('saves UltraFast from the edit form and normalizes conflicting legacy flags to UltraFast', async () => {
    listGroups.mockResolvedValue({ items: [group({ force_openai_fast: true, force_openai_ultrafast: true })], total: 1 })
    const wrapper = mountView()
    try {
      await openEdit(wrapper)
      expect(checked(wrapper, 'edit-force-openai-fast')).toBe('false')
      expect(checked(wrapper, 'edit-force-openai-ultrafast')).toBe('true')
      await wrapper.get('[data-testid="edit-force-openai-ultrafast"]').trigger('click')
      await wrapper.get('[data-testid="edit-force-openai-fast"]').trigger('click')
      await wrapper.get('[data-testid="edit-force-openai-ultrafast"]').trigger('click')
      await wrapper.get('#edit-group-form').trigger('submit')
      await flushPromises()
      expect(updateGroup).toHaveBeenCalledWith(42, expect.objectContaining({
        force_openai_fast: false, force_openai_ultrafast: true
      }))
    } finally { wrapper.unmount() }
  })

  it('clears UltraFast when leaving OpenAI and preserves existing composite Fast support', async () => {
    const wrapper = mountView()
    try {
      await openCreate(wrapper)
      await wrapper.get('[data-testid="create-force-openai-ultrafast"]').trigger('click')
      await setCreatePlatform(wrapper, 'composite')
      expect(wrapper.find('[data-testid="create-force-openai-ultrafast"]').exists()).toBe(false)
      await wrapper.get('[data-testid="create-force-openai-fast"]').trigger('click')
      await wrapper.get('#create-group-form').trigger('submit')
      await flushPromises()
      expect(createGroup).toHaveBeenCalledWith(expect.objectContaining({
        platform: 'composite', force_openai_fast: true, force_openai_ultrafast: false
      }))
    } finally { wrapper.unmount() }
  })

  it('resets create state after closing and does not restore UltraFast after a platform round trip', async () => {
    const wrapper = mountView()
    try {
      await openCreate(wrapper)
      await wrapper.get('[data-testid="create-force-openai-ultrafast"]').trigger('click')
      await setCreatePlatform(wrapper, 'anthropic')
      expect(wrapper.find('[data-testid="create-force-openai-ultrafast"]').exists()).toBe(false)
      await setCreatePlatform(wrapper, 'openai')
      expect(checked(wrapper, 'create-force-openai-ultrafast')).toBe('false')
      await wrapper.get('[data-testid="create-force-openai-ultrafast"]').trigger('click')
      await wrapper.get('[data-testid="close-dialog"]').trigger('click')
      await openCreate(wrapper)
      expect(checked(wrapper, 'create-force-openai-ultrafast')).toBe('false')
      expect(checked(wrapper, 'create-force-openai-fast')).toBe('false')
      await wrapper.get('#create-group-form').trigger('submit')
      await flushPromises()
      expect(createGroup).toHaveBeenCalledWith(expect.objectContaining({
        force_openai_fast: false, force_openai_ultrafast: false
      }))
    } finally { wrapper.unmount() }
  })

  it('defaults missing fields to false when opening another edit form after an UltraFast group', async () => {
    listGroups.mockResolvedValue({ items: [group({ force_openai_ultrafast: true }), group({ id: 43, name: 'Legacy' })], total: 2 })
    const wrapper = mountView()
    try {
      await openEdit(wrapper)
      expect(checked(wrapper, 'edit-force-openai-ultrafast')).toBe('true')
      await wrapper.get('[data-testid="close-dialog"]').trigger('click')
      await openEdit(wrapper, 1)
      expect(checked(wrapper, 'edit-force-openai-ultrafast')).toBe('false')
      await wrapper.get('#edit-group-form').trigger('submit')
      await flushPromises()
      expect(updateGroup).toHaveBeenCalledWith(43, expect.objectContaining({ force_openai_ultrafast: false }))
    } finally { wrapper.unmount() }
  })

  it.each(['composite', 'anthropic'] as const)('clears stale UltraFast on an unsupported %s group update', async platform => {
    listGroups.mockResolvedValue({ items: [group({
      platform, force_openai_fast: true, force_openai_ultrafast: true, free_openai_fast: true
    })], total: 1 })
    const wrapper = mountView()
    try {
      await openEdit(wrapper)
      expect(wrapper.find('[data-testid="edit-force-openai-ultrafast"]').exists()).toBe(false)
      await wrapper.get('#edit-group-form').trigger('submit')
      await flushPromises()
      expect(updateGroup).toHaveBeenCalledWith(42, expect.objectContaining({
        platform, force_openai_fast: platform === 'composite',
        force_openai_ultrafast: false, free_openai_fast: platform === 'composite'
      }))
    } finally { wrapper.unmount() }
  })
})
