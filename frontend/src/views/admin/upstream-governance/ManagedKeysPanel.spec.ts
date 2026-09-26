import { mount, flushPromises } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import ManagedKeysPanel from './ManagedKeysPanel.vue'
import api, { type ManagedKey } from '@/api/admin/upstream-governance'

vi.mock('@/api/admin/upstream-governance', () => ({ default: { keys: vi.fn(), createKeys: vi.fn(), revealKey: vi.fn() } }))
vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string) => key }) }))
const key: ManagedKey = { id: 9, site_id: 1, remote_group_id: 'r', platform: 'openai', remote_key_id: '55', marker: 'fixture', has_key: true, created_at: 'now', updated_at: 'now' }
const props = { siteId: 1, snapshotId: 3, groups: [{ id: 'r', name: 'Remote' }], selections: [{ remote_group_id: 'r', platform: 'openai' as const }] }

describe('managed group keys', () => {
  beforeEach(() => {
    vi.resetAllMocks()
    vi.mocked(api.keys).mockResolvedValue([])
    Object.defineProperty(navigator, 'clipboard', { configurable: true, value: { writeText: vi.fn().mockResolvedValue(undefined) } })
  })
  it('retains successful batch keys when a later batch times out, stops, and never automatically retries creation', async () => {
    const groups = Array.from({ length: 23 }, (_, index) => ({ id: String(index + 1), name: 'Group ' + (index + 1) }))
    const selections = groups.map(group => ({ remote_group_id: group.id, platform: 'openai' as const }))
    vi.mocked(api.createKeys).mockResolvedValueOnce({ items: groups.slice(0, 10).map((group, index) => ({ remote_group_id: group.id, platform: 'openai', status: 'created' as const, managed_key: { ...key, id: index + 1, remote_group_id: group.id }, key: 'secret-' + (index + 1) })) }).mockRejectedValueOnce({ reason: 'timeout' })
    const wrapper = mount(ManagedKeysPanel, { props: { ...props, groups, selections } })
    await flushPromises()
    await wrapper.get('[data-test=create-keys]').trigger('click')
    await flushPromises()
    expect(api.createKeys).toHaveBeenCalledTimes(2)
    expect(api.createKeys).toHaveBeenNthCalledWith(1, 1, { snapshot_id: 3, selections: selections.slice(0, 10) })
    expect(api.createKeys).toHaveBeenNthCalledWith(2, 1, { snapshot_id: 3, selections: selections.slice(10, 20) })
    expect(wrapper.findAll('[data-test=key-secret]')).toHaveLength(10)
    expect(wrapper.text()).toContain('secret-1')
    expect(wrapper.text()).toContain('governance.timeout')
    expect(wrapper.text()).toContain('governance.keyInterrupted')
    expect(wrapper.text()).toContain('governance.keyNotAttempted')
    await wrapper.get('[data-test=copy-all-keys]').trigger('click')
    expect(navigator.clipboard.writeText).toHaveBeenCalledWith('secret-1\nsecret-2\nsecret-3\nsecret-4\nsecret-5\nsecret-6\nsecret-7\nsecret-8\nsecret-9\nsecret-10')
  })
  it('creates selected group keys without a local mapping and retains per-item partial failures', async () => {
    vi.mocked(api.createKeys).mockResolvedValue({ items: [
      { remote_group_id: 'r', platform: 'openai', status: 'created', managed_key: key, key: 'fixture-secret' },
      { remote_group_id: 's', platform: 'anthropic', status: 'failed', error: 'timeout' },
    ] })
    const wrapper = mount(ManagedKeysPanel, { props: { ...props, groups: [...props.groups, { id: 's', name: 'Second' }], selections: [...props.selections, { remote_group_id: 's', platform: 'anthropic' }] } })
    await flushPromises()
    await wrapper.get('[data-test=create-keys]').trigger('click')
    await flushPromises()
    expect(api.createKeys).toHaveBeenCalledWith(1, { snapshot_id: 3, selections: [{ remote_group_id: 'r', platform: 'openai' }, { remote_group_id: 's', platform: 'anthropic' }] })
    expect(wrapper.get('[data-test=key-secret]').text()).toContain('fixture-secret')
    expect(wrapper.text()).toContain('governance.keyCreated')
    expect(wrapper.text()).toContain('governance.timeout')
    expect(wrapper.find('[data-test=reveal-key]').exists()).toBe(false)
    await wrapper.get('[data-test=copy-all-keys]').trigger('click')
    expect(navigator.clipboard.writeText).toHaveBeenCalledWith('fixture-secret')
  })
  it('automatically displays full saved keys and clears them when the site changes', async () => {
    vi.mocked(api.keys).mockResolvedValueOnce([key]).mockResolvedValue([])
    vi.mocked(api.revealKey).mockResolvedValue({ managed_key: key, key: 'revealed-secret' })
    const wrapper = mount(ManagedKeysPanel, { props })
    await flushPromises()
    expect(api.revealKey).toHaveBeenCalledWith(1, 9)
    expect(wrapper.get('[data-test=key-secret]').text()).toBe('revealed-secret')
    expect(wrapper.find('[data-test=reveal-key]').exists()).toBe(false)
    await wrapper.get('[data-test=copy-all-keys]').trigger('click')
    expect(navigator.clipboard.writeText).toHaveBeenCalledWith('revealed-secret')
    await wrapper.setProps({ siteId: 2 })
    expect(wrapper.text()).not.toContain('revealed-secret')
    expect(wrapper.find('[data-test=copy-all-keys]').exists()).toBe(false)
  })
  it('does not reveal a reserved key whose upstream creation is incomplete', async () => {
    vi.mocked(api.keys).mockResolvedValue([{ ...key, has_key: false }])
    const wrapper = mount(ManagedKeysPanel, { props })
    await flushPromises()
    expect(wrapper.find('[data-test=reveal-key]').exists()).toBe(false)
    expect(wrapper.text()).toContain('governance.keyPending')
    expect(api.revealKey).not.toHaveBeenCalled()
  })
  it('ignores a reveal response arriving after changing sites', async () => {
    vi.mocked(api.keys).mockResolvedValueOnce([key]).mockResolvedValue([])
    let resolve!: (value: { managed_key: ManagedKey; key: string }) => void
    vi.mocked(api.revealKey).mockImplementation(() => new Promise(r => { resolve = r }))
    const wrapper = mount(ManagedKeysPanel, { props })
    await flushPromises()
    expect(api.revealKey).toHaveBeenCalledTimes(1)
    await wrapper.setProps({ siteId: 2 })
    resolve({ managed_key: key, key: 'late-secret' })
    await flushPromises()
    expect(wrapper.text()).not.toContain('late-secret')
  })
  it('reads saved keys sequentially and displays each full value as it arrives', async () => {
    const second = { ...key, id: 10, remote_group_id: 's' }
    vi.mocked(api.keys).mockResolvedValue([key, second])
    let resolve!: (value: { managed_key: ManagedKey; key: string }) => void
    vi.mocked(api.revealKey).mockImplementationOnce(() => new Promise(r => { resolve = r })).mockResolvedValueOnce({ managed_key: second, key: 'second-full-key' })
    const wrapper = mount(ManagedKeysPanel, { props })
    await flushPromises()
    expect(api.revealKey).toHaveBeenCalledTimes(1)
    resolve({ managed_key: key, key: 'first-full-key' })
    await flushPromises()
    expect(api.revealKey).toHaveBeenNthCalledWith(2, 1, 10)
    expect(wrapper.findAll('[data-test=key-secret]').map(node => node.text())).toEqual(['first-full-key', 'second-full-key'])
    await wrapper.get('[data-test=copy-all-keys]').trigger('click')
    expect(navigator.clipboard.writeText).toHaveBeenCalledWith('first-full-key\nsecond-full-key')
  })
  it('retains loaded keys when another key fails to decrypt and offers a retry for that row', async () => {
    const second = { ...key, id: 10, remote_group_id: 's' }
    vi.mocked(api.keys).mockResolvedValue([key, second])
    vi.mocked(api.revealKey).mockResolvedValueOnce({ managed_key: key, key: 'first-full-key' }).mockRejectedValueOnce({ reason: 'operation_failed' }).mockResolvedValueOnce({ managed_key: second, key: 'recovered-full-key' })
    const wrapper = mount(ManagedKeysPanel, { props })
    await flushPromises()
    expect(wrapper.get('[data-test=key-secret]').text()).toBe('first-full-key')
    expect(wrapper.get('[data-test=key-read-error]').text()).toContain('governance.keyReadFailed')
    const retry = wrapper.get('#governance-reveal-key-10')
    expect(retry.text()).toBe('governance.rereadKey')
    await retry.trigger('click')
    await flushPromises()
    expect(wrapper.find('[data-test=key-read-error]').exists()).toBe(false)
    expect(wrapper.findAll('[data-test=key-secret]').map(node => node.text())).toEqual(['first-full-key', 'recovered-full-key'])
  })
})
