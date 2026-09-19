import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import CodexTicketSettings from '../CodexTicketSettings.vue'

const { getSettings, updateSettings } = vi.hoisted(() => ({ getSettings: vi.fn(), updateSettings: vi.fn() }))
vi.mock('@/api/admin/settings', () => ({ getSettings, updateSettings }))
vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string) => key }) }))

describe('CodexTicketSettings', () => {
  beforeEach(() => {
    getSettings.mockReset().mockResolvedValue({})
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
    })
    expect(wrapper.get('[role="status"]').text()).toBe('admin.settings.codexTickets.saved')
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
