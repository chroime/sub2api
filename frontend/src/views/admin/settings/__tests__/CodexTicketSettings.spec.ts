import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import CodexTicketSettings from '../CodexTicketSettings.vue'

const { getSettings, updateSettings, getProxies } = vi.hoisted(() => ({ getSettings: vi.fn(), updateSettings: vi.fn(), getProxies: vi.fn() }))
vi.mock('@/api/admin/settings', () => ({ getSettings, updateSettings }))
vi.mock('@/api/admin/proxies', () => ({ getAll: getProxies }))
vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string) => key }) }))

describe('CodexTicketSettings', () => {
  beforeEach(() => {
    getSettings.mockReset().mockResolvedValue({})
    getProxies.mockReset().mockResolvedValue([])
    updateSettings.mockReset().mockImplementation(async payload => payload)
  })

  it.each(['292', '332'] as const)('saves the missing-ticket policy for %s without touching the other mechanism', async mode => {
    const wrapper = mount(CodexTicketSettings, { props: { mode } })
    expect(wrapper.get(`#codex-ticket-${mode}-enabled`).attributes('disabled')).toBeDefined()
    await flushPromises()
    expect(wrapper.get(`#codex-ticket-${mode}-enabled`).attributes('aria-checked')).toBe('false')
    const policy = wrapper.get(`#codex-ticket-${mode}-fail-closed`)
    expect(policy.attributes('aria-checked')).toBe(mode === '292' ? 'true' : 'false')
    await policy.trigger('click')
    await wrapper.get(`#codex-ticket-${mode}-save`).trigger('click')
    await flushPromises()
    const prefix = mode === '292' ? 'openai_codex_ticket' : 'openai_codex_ticket_332'
    expect(updateSettings).toHaveBeenCalledWith({
      [`${prefix}_enabled`]: false,
      [`${prefix}_fail_closed`]: mode === '332',
      [`${prefix}_harvest_proxy_url`]: '',
      [`${prefix}_verify_enabled`]: false,
      [`${prefix}_harvest_proxy_ids`]: [],
      [`${prefix}_harvest_concurrency`]: 3,
    })
    expect(wrapper.get('[role="status"]').text()).toBe('admin.settings.codexTickets.saved')
    wrapper.unmount()
  })

  it.each(['292', '332'] as const)('saves verification, a proxy pool and bounded concurrency only for %s', async mode => {
    const prefix = mode === '292' ? 'openai_codex_ticket' : 'openai_codex_ticket_332'
    getSettings.mockResolvedValue({ [`${prefix}_harvest_proxy_ids`]: [9] })
    getProxies.mockResolvedValue([
      { id: 7, name: 'Primary route', protocol: 'socks5', status: 'active', expires_at: null, username: 'private-user', password: 'private-password', host: 'private-host' },
      { id: 8, name: 'Expired route', protocol: 'http', status: 'active', expires_at: '2000-01-01T00:00:00Z' },
      { id: 10, name: 'Disabled route', protocol: 'http', status: 'inactive', expires_at: null },
    ])
    const wrapper = mount(CodexTicketSettings, { props: { mode } })
    await flushPromises()
    expect(wrapper.get(`#codex-ticket-${mode}-verify`).attributes('aria-checked')).toBe('false')
    expect(wrapper.get<HTMLInputElement>(`#codex-ticket-${mode}-concurrency`).element.value).toBe('3')
    expect(wrapper.text()).not.toMatch(/private-user|private-password|private-host|Expired route|Disabled route/)
    expect(wrapper.get<HTMLInputElement>(`#codex-ticket-${mode}-proxy-9`).element.checked).toBe(true)
    await wrapper.get(`#codex-ticket-${mode}-proxy-9`).setValue(false)
    await wrapper.get(`#codex-ticket-${mode}-proxy-7`).setValue(true)
    await wrapper.get(`#codex-ticket-${mode}-verify`).trigger('click')
    await wrapper.get(`#codex-ticket-${mode}-concurrency`).setValue('6')
    await wrapper.get(`#codex-ticket-${mode}-save`).trigger('click')
    await flushPromises()
    expect(updateSettings).toHaveBeenLastCalledWith({
      [`${prefix}_enabled`]: false,
      [`${prefix}_fail_closed`]: mode === '292',
      [`${prefix}_harvest_proxy_url`]: '',
      [`${prefix}_verify_enabled`]: true,
      [`${prefix}_harvest_proxy_ids`]: [7],
      [`${prefix}_harvest_concurrency`]: 6,
    })
    wrapper.unmount()
  })

  it('blocks invalid concurrency and allows correcting it', async () => {
    const wrapper = mount(CodexTicketSettings, { props: { mode: '292' } })
    await flushPromises()
    for (const value of ['', '0', '17', '1.5']) {
      await wrapper.get('#codex-ticket-292-concurrency').setValue(value)
      expect(wrapper.get('#codex-ticket-292-save').attributes('disabled')).toBeDefined()
      expect(wrapper.text()).toContain('admin.settings.codexTickets.invalidConcurrency')
    }
    expect(updateSettings).not.toHaveBeenCalled()
    await wrapper.get('#codex-ticket-292-concurrency').setValue('16')
    expect(wrapper.get('#codex-ticket-292-save').attributes('disabled')).toBeUndefined()
    wrapper.unmount()
  })

  it('limits pool selection to 32 while keeping selected routes removable', async () => {
    getSettings.mockResolvedValue({ openai_codex_ticket_harvest_proxy_ids: Array.from({ length: 32 }, (_, i) => i + 1) })
    getProxies.mockResolvedValue(Array.from({ length: 33 }, (_, i) => ({ id: i + 1, name: `Route ${i + 1}`, protocol: 'http', status: 'active', expires_at: null })))
    const wrapper = mount(CodexTicketSettings, { props: { mode: '292' } })
    await flushPromises()
    expect(wrapper.get('#codex-ticket-292-proxy-33').attributes('disabled')).toBeDefined()
    expect(wrapper.get('#codex-ticket-292-proxy-1').attributes('disabled')).toBeUndefined()
    await wrapper.get('#codex-ticket-292-proxy-1').setValue(false)
    expect(wrapper.get('#codex-ticket-292-proxy-33').attributes('disabled')).toBeUndefined()
    wrapper.unmount()
  })

  it('preserves selected IDs on proxy-list failure and can reload the list', async () => {
    getSettings.mockResolvedValue({ openai_codex_ticket_harvest_proxy_ids: [9] })
    getProxies.mockRejectedValueOnce(new Error('private-proxy-url'))
    const wrapper = mount(CodexTicketSettings, { props: { mode: '292' } })
    await flushPromises()
    expect(wrapper.text()).toContain('admin.settings.codexTickets.proxyLoadFailed')
    expect(wrapper.text()).not.toContain('private-proxy-url')
    await wrapper.get('#codex-ticket-292-save').trigger('click')
    await flushPromises()
    expect(updateSettings.mock.calls[0][0].openai_codex_ticket_harvest_proxy_ids).toEqual([9])
    getProxies.mockResolvedValueOnce([{ id: 9, name: 'Recovered route', protocol: 'https', status: 'active', expires_at: null }])
    await wrapper.get('#codex-ticket-292-proxy-reload').trigger('click')
    await flushPromises()
    expect(wrapper.text()).toContain('Recovered route')
    expect(wrapper.text()).not.toContain('admin.settings.codexTickets.proxyLoadFailed')
    wrapper.unmount()
  })

  it('replaces a proxy with the server mask and only shows success for a confirmed save', async () => {
    const wrapper = mount(CodexTicketSettings, { props: { mode: '332' } })
    await flushPromises()
    const input = wrapper.get<HTMLInputElement>('#codex-ticket-332-harvest-proxy')
    await input.setValue('socks5h://user:secret@example.test:1080')
    updateSettings.mockRejectedValueOnce(new Error('offline'))
    await wrapper.get('#codex-ticket-332-save').trigger('click')
    await flushPromises()
    expect(wrapper.get('[role="alert"]').text()).toBe('admin.settings.codexTickets.saveFailed')
    expect(wrapper.text()).not.toContain('admin.settings.codexTickets.saved')
    updateSettings.mockResolvedValueOnce({
      openai_codex_ticket_332_enabled: false,
      openai_codex_ticket_332_fail_closed: false,
      openai_codex_ticket_332_harvest_proxy_url: 'socks5h://user:***@example.test:1080',
      openai_codex_ticket_332_harvest_proxy_configured: true,
    })
    await wrapper.get('#codex-ticket-332-save').trigger('click')
    await flushPromises()
    expect(input.element.value).toBe('socks5h://user:***@example.test:1080')
    expect(wrapper.text()).toContain('admin.settings.codexTickets.proxyConfigured')
    expect(wrapper.text()).not.toContain('secret')
    await wrapper.get('#codex-ticket-332-enabled').trigger('click')
    expect(wrapper.text()).not.toContain('admin.settings.codexTickets.saved')
    wrapper.unmount()
  })

  it('does not permit writes when settings have not loaded and supports retry', async () => {
    getSettings.mockRejectedValueOnce(new Error('offline'))
    const wrapper = mount(CodexTicketSettings, { props: { mode: '292' } })
    await flushPromises()
    expect(wrapper.get('#codex-ticket-292-save').attributes('disabled')).toBeDefined()
    expect(wrapper.get('[role="alert"]').text()).toBe('admin.settings.codexTickets.loadFailed')
    expect(updateSettings).not.toHaveBeenCalled()
    getSettings.mockResolvedValueOnce({ openai_codex_ticket_enabled: true, openai_codex_ticket_fail_closed: false })
    await wrapper.findAll('button').find(button => button.text() === 'admin.settings.codexTickets.retry')!.trigger('click')
    await flushPromises()
    expect(wrapper.get('#codex-ticket-292-enabled').attributes('aria-checked')).toBe('true')
    expect(wrapper.get('#codex-ticket-292-fail-closed').attributes('aria-checked')).toBe('false')
    expect(wrapper.find('[role="alert"]').exists()).toBe(false)
    wrapper.unmount()
  })
})
