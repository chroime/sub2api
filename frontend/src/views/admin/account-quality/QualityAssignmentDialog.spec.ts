import { enableAutoUnmount, flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import api from '@/api/admin/account-quality'
import accountsAPI from '@/api/admin/accounts'
import groupsAPI from '@/api/admin/groups'
import type { AccountListItem } from '@/types'
import QualityAssignmentDialog from './QualityAssignmentDialog.vue'
import { newPolicy } from './quality'

vi.mock('@/api/admin/account-quality', () => ({ default: { assign: vi.fn() } }))
vi.mock('@/api/admin/accounts', () => ({ default: { list: vi.fn(), getById: vi.fn() } }))
vi.mock('@/api/admin/groups', () => ({ default: { getAll: vi.fn() } }))
enableAutoUnmount(afterEach)
afterEach(() => vi.useRealTimers())

const accounts = [
  { id: 39, name: '039 primary', platform: 'openai', type: 'apikey', status: 'active' },
  { id: 40, name: '040 backup', platform: 'openai', type: 'oauth', status: 'inactive' },
  { id: 41, name: '041 other', platform: 'openai', type: 'apikey', status: 'active' },
  { id: 51, name: 'Claude account', platform: 'anthropic', type: 'apikey', status: 'active' },
] as AccountListItem[]
const policies = [
  { ...newPolicy(), id: 1, name: 'OpenAI reasoning', platform: 'openai', model: 'gpt-5' },
  { ...newPolicy(), id: 2, name: 'Claude reasoning', platform: 'anthropic', model: 'claude-sonnet-4-6', api_mode: 'anthropic' },
]
function page(items: AccountListItem[]) { return { items, page: 1, pages: 1, page_size: 1000, total: items.length } }
function render(accountIds: number[] = []) {
  return mount(QualityAssignmentDialog, { props: { policies, policyId: 1, accountIds }, global: { stubs: { teleport: true } } })
}
async function openOptions(wrapper: VueWrapper, selector: string) {
  const trigger = wrapper.get(`${selector} button.select-trigger`)
  if (trigger.attributes('aria-expanded') !== 'true') await trigger.trigger('click')
  return wrapper.get(`${selector} [role=listbox]`)
}
async function pick(wrapper: VueWrapper, selector: string, name: string) {
  const options = await openOptions(wrapper, selector)
  const option = options.findAll('[role=option]').find(item => item.text().includes(name))
  expect(option, `missing selectable ${name}`).toBeDefined()
  await option!.trigger('click')
  await flushPromises()
}

beforeEach(() => {
  vi.resetAllMocks()
  vi.mocked(groupsAPI.getAll).mockResolvedValue([])
  vi.mocked(api.assign).mockResolvedValue([])
  vi.mocked(accountsAPI.list).mockImplementation(async (_page, _size, filters) => page(accounts.filter(account => (!filters?.platform || account.platform === filters.platform) && (!filters?.search || account.name.toLowerCase().includes(filters.search.toLowerCase())))))
})

describe('quality assignment account pickers', () => {
  it('uses themed Select controls for every assignment dropdown', async () => {
    const wrapper = render()
    await flushPromises()
    expect(wrapper.findAll('select')).toHaveLength(0)
    expect(wrapper.find('[data-test=quality-assignment-policy] button.select-trigger').exists()).toBe(true)
    expect(wrapper.find('[data-test=quality-assignment-mode] button.select-trigger').exists()).toBe(true)
  })

  it('defaults to single-account selection and preserves a preselected account using only lite reads', async () => {
    const wrapper = render([39])
    await flushPromises()
    expect(wrapper.get('[data-test=quality-assignment-mode] .select-value').text()).toBe('单个账号')
    const picker = wrapper.get('[data-test=quality-account-picker]')
    expect(picker.get('button.select-trigger').text()).toContain('039 primary')
    expect(picker.text()).toContain('#39')
    expect(picker.text()).toContain('openai')
    expect(picker.text()).toContain('apikey')
    expect(wrapper.find('[data-test=quality-assignment-ids]').exists()).toBe(false)
    expect(accountsAPI.getById).not.toHaveBeenCalled()
    expect(vi.mocked(accountsAPI.list).mock.calls.every(([, , filters]) => filters?.lite === '1' && filters?.include_scheduler_score === '0')).toBe(true)
    await wrapper.get('form').trigger('submit')
    await flushPromises()
    expect(api.assign).toHaveBeenCalledWith(expect.objectContaining({ policy_id: 1, account_ids: [39] }))
  })

  it('keeps batch preselection visible, adds an account from the dropdown and removes a chip', async () => {
    const wrapper = render([39, 40])
    await flushPromises()
    expect(wrapper.get('[data-test=quality-assignment-mode] .select-value').text()).toBe('批量选择账号')
    expect(wrapper.get('[data-test=quality-account-chip-39]').text()).toContain('039 primary')
    expect(wrapper.get('[data-test=quality-account-chip-40]').text()).toContain('040 backup')
    await pick(wrapper, '[data-test=quality-account-picker]', '041 other')
    await wrapper.get('[data-test=quality-remove-account-40]').trigger('click')
    await wrapper.get('form').trigger('submit')
    await flushPromises()
    expect(api.assign).toHaveBeenCalledWith(expect.objectContaining({ account_ids: [39, 41] }))
  })

  it('searches policy names and models and shows only accounts matching the chosen policy platform', async () => {
    const wrapper = render()
    await flushPromises()
    const options = await openOptions(wrapper, '[data-test=quality-assignment-policy]')
    await options.get('input.select-search-input').setValue('claude-sonnet')
    expect(options.findAll('[role=option]').map(item => item.text())).toEqual([expect.stringContaining('Claude reasoning')])
    await options.get('[role=option]').trigger('click')
    await flushPromises()
    const accountOptions = await openOptions(wrapper, '[data-test=quality-account-picker]')
    expect(accountOptions.text()).toContain('Claude account')
    expect(accountOptions.text()).not.toContain('039 primary')
  })

  it('debounces account search and sends a platform-filtered lite request', async () => {
    vi.useFakeTimers()
    const wrapper = render()
    await flushPromises()
    const options = await openOptions(wrapper, '[data-test=quality-account-picker]')
    const before = vi.mocked(accountsAPI.list).mock.calls.length
    await options.get('input.select-search-input').setValue('039')
    expect(accountsAPI.list).toHaveBeenCalledTimes(before)
    await vi.advanceTimersByTimeAsync(300)
    await flushPromises()
    expect(accountsAPI.list).toHaveBeenLastCalledWith(1, 50, expect.objectContaining({ platform: 'openai', search: '039', lite: '1', include_scheduler_score: '0' }), expect.objectContaining({ signal: expect.any(AbortSignal) }))
    const current = wrapper.get('[data-test=quality-account-picker] [role=listbox]')
    expect(current.text()).toContain('039 primary')
    expect(current.text()).not.toContain('041 other')
  })

  it('marks an incompatible preserved selection and blocks stale IDs after a policy change', async () => {
    const wrapper = render([39])
    await flushPromises()
    await pick(wrapper, '[data-test=quality-assignment-policy]', 'Claude reasoning')
    expect(wrapper.get('[data-test=quality-account-selection-warning]').text()).toBeTruthy()
    await wrapper.get('form').trigger('submit')
    await flushPromises()
    expect(api.assign).not.toHaveBeenCalled()
    await pick(wrapper, '[data-test=quality-account-picker]', 'Claude account')
    await wrapper.get('form').trigger('submit')
    await flushPromises()
    expect(api.assign).toHaveBeenCalledWith(expect.objectContaining({ policy_id: 2, account_ids: [51] }))
  })

  it('ignores a stale account-search response after switching policy platforms', async () => {
    vi.useFakeTimers()
    let resolveOld!: (value: ReturnType<typeof page>) => void
    vi.mocked(accountsAPI.list).mockImplementation(async (_page, _size, filters) => filters?.search === 'old' ? new Promise(resolve => { resolveOld = resolve }) : page(accounts.filter(account => account.platform === filters?.platform)))
    const wrapper = render()
    await flushPromises()
    const options = await openOptions(wrapper, '[data-test=quality-account-picker]')
    await options.get('input.select-search-input').setValue('old')
    await vi.advanceTimersByTimeAsync(300)
    const signal = vi.mocked(accountsAPI.list).mock.calls.at(-1)?.[3]?.signal
    await pick(wrapper, '[data-test=quality-assignment-policy]', 'Claude reasoning')
    resolveOld(page([accounts[0]!]))
    await flushPromises()
    expect(signal?.aborted).toBe(true)
    const current = await openOptions(wrapper, '[data-test=quality-account-picker]')
    expect(current.text()).toContain('Claude account')
    expect(current.text()).not.toContain('039 primary')
  })

  it('shows a retry action after loading fails and reports empty search results', async () => {
    vi.useFakeTimers()
    vi.mocked(accountsAPI.list).mockRejectedValueOnce(new Error('temporary outage'))
    const wrapper = render()
    await flushPromises()
    expect(wrapper.get('[data-test=quality-accounts-retry]').exists()).toBe(true)
    expect(api.assign).not.toHaveBeenCalled()
    await wrapper.get('[data-test=quality-accounts-retry]').trigger('click')
    await flushPromises()
    const options = await openOptions(wrapper, '[data-test=quality-account-picker]')
    expect(options.text()).toContain('039 primary')
    await options.get('input.select-search-input').setValue('no such account')
    await vi.advanceTimersByTimeAsync(300)
    await flushPromises()
    expect(wrapper.get('[data-test=quality-account-picker] .select-empty').text()).toContain('没有匹配')
  })

  it('keeps an unverified preselected ID visible and blocks submission until hydration succeeds', async () => {
    let hydrateFails = true
    vi.mocked(accountsAPI.list).mockImplementation(async (_page, _size, filters) => {
      if (!filters?.platform && hydrateFails) throw new Error('could not verify selected account')
      return page(filters?.platform ? [accounts[2]!] : [accounts[0]!])
    })
    const wrapper = render([39])
    await flushPromises()
    expect(wrapper.get('[data-test=quality-account-picker]').text()).toContain('#39')
    await wrapper.get('form').trigger('submit')
    await flushPromises()
    expect(api.assign).not.toHaveBeenCalled()
    hydrateFails = false
    await wrapper.get('[data-test=quality-accounts-retry]').trigger('click')
    await flushPromises()
    await wrapper.get('form').trigger('submit')
    await flushPromises()
    expect(api.assign).toHaveBeenCalledWith(expect.objectContaining({ account_ids: [39] }))
  })

  it('hydrates a preselected account outside the first catalog page and blocks submission while it is loading', async () => {
    let resolveSecond!: (value: ReturnType<typeof page>) => void
    vi.mocked(accountsAPI.list).mockImplementation(async (number, _size, filters) => {
      if (filters?.platform) return page([accounts[2]!])
      if (number === 1) return { ...page([accounts[1]!]), total: 1001, pages: 2 }
      return new Promise(resolve => { resolveSecond = resolve })
    })
    const wrapper = render([39])
    await flushPromises()
    expect(wrapper.get('[data-test=quality-target-accounts] [role=status]').text()).toContain('正在核对')
    expect(wrapper.get('[data-test=quality-assignment-save]').attributes('disabled')).toBeDefined()
    await wrapper.get('form').trigger('submit')
    expect(api.assign).not.toHaveBeenCalled()
    resolveSecond(page([accounts[0]!]))
    await flushPromises()
    expect(wrapper.get('[data-test=quality-account-picker] button.select-trigger').text()).toContain('039 primary')
    expect(accountsAPI.list).toHaveBeenCalledWith(2, 1000, { lite: '1', include_scheduler_score: '0' }, expect.objectContaining({ signal: expect.any(AbortSignal) }))
    await wrapper.get('form').trigger('submit')
    await flushPromises()
    expect(api.assign).toHaveBeenCalledWith(expect.objectContaining({ account_ids: [39] }))
  })

  it('keeps group and platform assignment modes scoped to the selected policy platform', async () => {
    vi.mocked(groupsAPI.getAll).mockResolvedValue([{ id: 7, name: 'Primary group', platform: 'openai' }] as Awaited<ReturnType<typeof groupsAPI.getAll>>)
    const wrapper = render([51])
    await flushPromises()
    await pick(wrapper, '[data-test=quality-assignment-mode]', '分组')
    await pick(wrapper, '[data-test=quality-assignment-group]', 'Primary group')
    await wrapper.get('form').trigger('submit')
    await flushPromises()
    expect(accountsAPI.list).toHaveBeenCalledWith(1, 100, expect.objectContaining({ group: '7', platform: 'openai', lite: '1', include_scheduler_score: '0' }))
    expect(api.assign).toHaveBeenLastCalledWith(expect.objectContaining({ policy_id: 1, account_ids: [39, 40, 41], source: 'group:7' }))
    await pick(wrapper, '[data-test=quality-assignment-mode]', '平台')
    await wrapper.get('form').trigger('submit')
    await flushPromises()
    expect(api.assign).toHaveBeenLastCalledWith(expect.objectContaining({ policy_id: 1, account_ids: [39, 40, 41], source: 'platform:openai' }))
  })
})
