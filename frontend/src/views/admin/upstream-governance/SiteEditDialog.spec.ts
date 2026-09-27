import { flushPromises, mount } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import SiteEditDialog from './SiteEditDialog.vue'
import Select from '@/components/common/Select.vue'
import api, { type Site } from '@/api/admin/upstream-governance'

vi.mock('@/api/admin/upstream-governance', () => ({ default: { list: vi.fn(), loginCredentials: vi.fn(), update: vi.fn() } }))
vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string) => key }) }))
const site: Site = { id: 2, name: 'Fixture upstream', platform: 'sub2api', base_url: 'https://fixture.example', proxy_id: null, enabled: true, interval_minutes: 15, version: 7, has_credential: true, status: 'healthy', last_error: '', last_sync_at: null }
const credentials = { username: 'fixture-user@example.test', password: 'fixture-visible-password', version: 7 }
const wrappers: ReturnType<typeof mount>[] = []
function setup() {
  const wrapper = mount(SiteEditDialog, {
    props: { site, proxies: [{ id: 4, name: 'Fixture proxy' }] },
    global: { stubs: { teleport: true, transition: false } },
  })
  wrappers.push(wrapper)
  return wrapper
}
describe('site edit dialog', () => {
  beforeEach(() => {
    vi.resetAllMocks()
    vi.mocked(api.list).mockResolvedValue([site])
    vi.mocked(api.loginCredentials).mockResolvedValue(credentials)
    vi.mocked(api.update).mockResolvedValue({ ...site, version: 8 })
  })
  afterEach(() => { wrappers.splice(0).forEach(wrapper => wrapper.unmount()) })
  it('loads saved username and password as plain text and uses the shared dropdowns', async () => {
    const wrapper = setup()
    expect(wrapper.get('#governance-edit-save').attributes('disabled')).toBeDefined()
    await flushPromises()
    expect((wrapper.get('#governance-edit-username').element as HTMLInputElement).value).toBe(credentials.username)
    expect((wrapper.get('#governance-edit-password').element as HTMLInputElement).value).toBe(credentials.password)
    expect(wrapper.get('#governance-edit-password').attributes('type')).toBe('text')
    expect(wrapper.findAllComponents(Select)).toHaveLength(2)
    expect(wrapper.find('select').exists()).toBe(false)
    expect(wrapper.get('#governance-edit-save').attributes('disabled')).toBeUndefined()
    expect(api.update).not.toHaveBeenCalled()
  })
  it('saves changed login details with all site fields and the matching version', async () => {
    const wrapper = setup()
    await flushPromises()
    await wrapper.get('#governance-edit-username').setValue(' next-user ')
    await wrapper.get('#governance-edit-password').setValue(' next-password ')
    await wrapper.getComponent(Select).vm.$emit('update:modelValue', 'newapi')
    await wrapper.get('#governance-edit-form').trigger('submit')
    await flushPromises()
    expect(api.update).toHaveBeenCalledWith(2, { name: site.name, platform: 'newapi', base_url: site.base_url, proxy_id: null, enabled: true, interval_minutes: 15, version: 7, login_credentials: { username: 'next-user', password: ' next-password ' } })
    expect(wrapper.emitted('saved')?.[0]).toEqual([{ ...site, version: 8 }])
  })
  it('omits unchanged login details during an ordinary metadata edit', async () => {
    const wrapper = setup()
    await flushPromises()
    await wrapper.get('#governance-edit-name').setValue('Renamed')
    await wrapper.get('#governance-edit-form').trigger('submit')
    await flushPromises()
    const input = vi.mocked(api.update).mock.calls[0]![1]
    expect(input.name).toBe('Renamed')
    expect(input).not.toHaveProperty('login_credentials')
  })
  it.each([1, 1441, 10081, 2147483647])('saves a freely entered collection interval of %s minutes unchanged', async minutes => {
    const wrapper = setup()
    await flushPromises()
    const input = wrapper.get('#governance-edit-interval')
    expect(input.attributes('min')).toBe('1')
    expect(input.attributes('step')).toBe('1')
    expect(input.attributes('max')).toBeUndefined()
    await input.setValue(String(minutes))
    expect((input.element as HTMLInputElement).checkValidity()).toBe(true)
    await wrapper.get('#governance-edit-form').trigger('submit')
    await flushPromises()
    expect(api.update).toHaveBeenCalledWith(site.id, expect.objectContaining({ interval_minutes: minutes }))
  })
  it.each(['', '0', '-1', '1.5', 'Infinity', '1e309', '2147483648'])('rejects invalid collection interval %j before saving', async minutes => {
    const wrapper = setup()
    await flushPromises()
    await wrapper.get('#governance-edit-interval').setValue(minutes)
    await wrapper.get('#governance-edit-form').trigger('submit')
    expect(api.update).not.toHaveBeenCalled()
    expect(wrapper.text()).toContain(minutes === '2147483648' ? 'governance.intervalTooLarge' : 'governance.intervalPositiveInteger')
  })
  it('clears old login details when changing the upstream origin', async () => {
    const wrapper = setup()
    await flushPromises()
    await wrapper.get('#governance-edit-url').setValue('https://other.example')
    expect((wrapper.get('#governance-edit-username').element as HTMLInputElement).value).toBe('')
    expect((wrapper.get('#governance-edit-password').element as HTMLInputElement).value).toBe('')
    await wrapper.get('#governance-edit-form').trigger('submit')
    await flushPromises()
    expect(api.update).toHaveBeenCalledWith(2, expect.objectContaining({ base_url: 'https://other.example', login_credentials: { username: '', password: '' } }))
  })
  it('preserves login details when only URL whitespace or a root slash changes', async () => {
    const wrapper = setup()
    await flushPromises()
    await wrapper.get('#governance-edit-url').setValue(' https://fixture.example/ ')
    expect((wrapper.get('#governance-edit-password').element as HTMLInputElement).value).toBe(credentials.password)
    await wrapper.get('#governance-edit-form').trigger('submit')
    await flushPromises()
    expect(vi.mocked(api.update).mock.calls[0]![1]).not.toHaveProperty('login_credentials')
  })
  it('does not clear saved credentials while retyping the same URL', async () => {
    const wrapper = setup()
    await flushPromises()
    const url = wrapper.get('#governance-edit-url')
    ;(url.element as HTMLInputElement).value = 'https://partial.example'
    await url.trigger('input')
    expect((wrapper.get('#governance-edit-password').element as HTMLInputElement).value).toBe(credentials.password)
    ;(url.element as HTMLInputElement).value = site.base_url
    await url.trigger('input')
    await url.trigger('change')
    expect((wrapper.get('#governance-edit-password').element as HTMLInputElement).value).toBe(credentials.password)
  })
  it('supports legacy sites with no saved login and clears a saved pair only when both fields are empty', async () => {
    const wrapper = setup()
    await flushPromises()
    await wrapper.get('#governance-edit-password').setValue('')
    await wrapper.get('#governance-edit-form').trigger('submit')
    expect(api.update).not.toHaveBeenCalled()
    expect(wrapper.text()).toContain('governance.loginPairRequired')
    await wrapper.get('#governance-edit-username').setValue('')
    await wrapper.get('#governance-edit-form').trigger('submit')
    await flushPromises()
    expect(api.update).toHaveBeenCalledWith(2, expect.objectContaining({ login_credentials: { username: '', password: '' } }))
    vi.mocked(api.loginCredentials).mockResolvedValue({ username: '', password: '', version: 7 })
    const legacy = setup()
    await flushPromises()
    expect(legacy.text()).toContain('governance.loginDetailsMissing')
    expect(legacy.get('#governance-edit-save').attributes('disabled')).toBeUndefined()
  })
  it('blocks a failed credential load and reloads the latest site and credentials together', async () => {
    vi.mocked(api.loginCredentials).mockRejectedValueOnce({ reason: 'operation_failed' })
    const wrapper = setup()
    await flushPromises()
    await wrapper.get('#governance-edit-form').trigger('submit')
    expect(api.update).not.toHaveBeenCalled()
    expect(wrapper.get('#governance-edit-save').attributes('disabled')).toBeDefined()
    vi.mocked(api.list).mockResolvedValue([{ ...site, version: 8, name: 'Updated elsewhere' }])
    vi.mocked(api.loginCredentials).mockResolvedValue({ ...credentials, version: 8, password: 'updated-password' })
    await wrapper.get('#governance-edit-reload').trigger('click')
    await flushPromises()
    expect((wrapper.get('#governance-edit-name').element as HTMLInputElement).value).toBe('Updated elsewhere')
    expect((wrapper.get('#governance-edit-password').element as HTMLInputElement).value).toBe('updated-password')
    expect(wrapper.get('#governance-edit-save').attributes('disabled')).toBeUndefined()
  })
  it('rejects mixed versions without overwriting a newer site', async () => {
    vi.mocked(api.loginCredentials).mockResolvedValue({ ...credentials, version: 8 })
    const wrapper = setup()
    await flushPromises()
    expect(wrapper.text()).toContain('governance.siteEditStale')
    await wrapper.get('#governance-edit-form').trigger('submit')
    expect(api.update).not.toHaveBeenCalled()
  })
  it('does not restore credentials after the dialog is closed while loading', async () => {
    let finish!: (value: typeof credentials) => void
    vi.mocked(api.loginCredentials).mockImplementationOnce(() => new Promise(resolve => { finish = resolve }))
    const wrapper = setup()
    await wrapper.get('[aria-label="Close modal"]').trigger('click')
    finish(credentials)
    await flushPromises()
    expect(wrapper.emitted('close')).toHaveLength(1)
    expect((wrapper.get('#governance-edit-password').element as HTMLInputElement).value).toBe('')
  })
})
