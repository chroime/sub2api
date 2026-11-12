import { defineComponent } from 'vue'
import { flushPromises, mount } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import GroupsView from '../GroupsView.vue'

const { list, create, update } = vi.hoisted(() => ({ list: vi.fn(), create: vi.fn(), update: vi.fn() }))
vi.mock('@/api/admin', () => ({ adminAPI: { groups: {
  list, create, update, getAll: vi.fn(async () => []),
  getModelAllowlistCandidates: vi.fn(async () => []), getUsageSummary: vi.fn(async () => []),
  getCapacitySummary: vi.fn(async () => []), getLiveCapability: vi.fn(async () => ({ supported: false }))
}, accounts: { list: vi.fn(async () => ({ items: [] })) } } }))
vi.mock('@/stores/app', () => ({ useAppStore: () => ({ showError: vi.fn(), showSuccess: vi.fn() }) }))
vi.mock('@/stores/auth', () => ({ useAuthStore: () => ({ isSimpleMode: false, isAdmin: true }) }))
vi.mock('@/stores/onboarding', () => ({ useOnboardingStore: () => ({ isCurrentStep: () => false, nextStep: vi.fn() }) }))
vi.mock('vue-i18n', async () => ({ ...await vi.importActual<typeof import('vue-i18n')>('vue-i18n'), useI18n: () => ({ t: (key: string) => key }) }))

const Validator = defineComponent({ setup(_, { expose }) { expose({ validate: () => true, resetValidation: () => {} }) }, template: '<div />' })
const group = { id: 42, name: 'Grok Ultra', platform: 'grok', status: 'active', rate_multiplier: 0.6,
  subscription_type: 'standard', description: 'Group description', supported_model_scopes: [],
  identity: { label: '自营', color: 'teal' }, long_context_pricing_enabled: true,
  allow_messages_dispatch: false, allow_live: false, require_oauth_only: false, require_privacy_set: false }
const mountView = () => mount(GroupsView, { global: { stubs: {
  AppLayout: { template: '<main><slot /></main>' },
  TablePageLayout: { template: '<main><slot name="filters" /><slot name="table" /></main>' },
  DataTable: { props: ['data'], template: '<div><div v-for="row in data" :key="row.id"><slot name="cell-name" :value="row.name" :row="row" /><slot name="cell-actions" :row="row" /></div></div>' },
  BaseDialog: { props: ['show'], template: '<div v-if="show"><slot /><slot name="footer" /></div>' },
  Select: true, Icon: true, PlatformIcon: true, Pagination: true, ConfirmDialog: true, EmptyState: true,
  GroupCapacityBadge: true, VueDraggable: true, GroupRateMultipliersModal: true,
  GroupRPMOverridesModal: true, GroupBalancePrechargeSettings: true,
  GroupInputRoutingEditor: Validator, ReasoningEffortPolicyFields: Validator, CodexManifestAccountsField: Validator,
  PricingEntryCard: true,
} } })

describe('GroupsView identity metadata', () => {
  beforeEach(() => { localStorage.clear(); vi.clearAllMocks(); list.mockResolvedValue({ items: [group], total: 1, pages: 1 }); create.mockResolvedValue(group); update.mockResolvedValue(group) })
  afterEach(() => vi.restoreAllMocks())

  it('shows the saved identity and edits it independently of the group name and rate', async () => {
    const w = mountView()
    try {
      await flushPromises()
      expect(w.text()).toContain('自营')
      await w.findAll('button').find(b => b.text() === 'common.edit')!.trigger('click'); await flushPromises()
      const label = w.get('#edit-group-identity-label')
      expect((label.element as HTMLInputElement).value).toBe('自营')
      await label.setValue('合作渠道')
      await w.get('#edit-group-form').trigger('submit'); await flushPromises()
      expect(update).toHaveBeenCalledWith(42, expect.objectContaining({ name: 'Grok Ultra', rate_multiplier: 0.6, identity: { label: '合作渠道', color: 'teal' } }))
    } finally { w.unmount() }
  })

  it('creates a group with an explicitly chosen identity', async () => {
    const w = mountView()
    try {
      await flushPromises(); await w.get('[data-tour="groups-create-btn"]').trigger('click'); await flushPromises()
      await w.get('#create-group-form input').setValue('Claude Max')
      await w.get('#create-group-identity-label').setValue('自营')
      await w.get('#create-group-form').trigger('submit'); await flushPromises()
      expect(create).toHaveBeenCalledWith(expect.objectContaining({ name: 'Claude Max', identity: { label: '自营', color: 'teal' } }))
    } finally { w.unmount() }
  })
})
