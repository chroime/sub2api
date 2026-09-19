import { flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import CodexTicketMonitor from '../CodexTicketMonitor.vue'

const { getMonitor } = vi.hoisted(() => ({ getMonitor: vi.fn() }))
vi.mock('@/api/admin/settings', () => ({ getCodexTicketMonitor: getMonitor }))
vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string) => key, locale: { value: 'en-US' } }) }))

function snapshot(name = 'Account A') {
  return {
    updated_at: '2026-09-20T08:00:00Z',
    states: [
      { mode: '292', account_id: 41, account_name: name, model: 'gpt-6-astra', status: 'ready', phase: 'capture', proxy_id: 7, proxy_name: 'Route A', length: 292, expires_at: '2026-09-20T09:00:00Z', last_error: '', updated_at: '2026-09-20T08:00:00Z', uses: 4 },
      { mode: '332', account_id: 42, account_name: 'Account B', model: 'gpt-5.6-sol', status: 'cooldown', phase: 'cooldown', proxy_name: 'Route B', length: 0, next_attempt_at: '2026-09-20T08:05:00Z', last_error: 'rate_limited', updated_at: '2026-09-20T08:00:00Z', uses: 0 },
    ],
    events: [
      { id: 1, time: '2026-09-20T08:00:00Z', mode: '292', account_id: 41, account_name: name, model: 'gpt-6-astra', phase: 'persist', status: 'ready', proxy_name: 'Route A', http_status: 200, length: 292, error_code: '' },
      { id: 2, time: '2026-09-20T08:00:01Z', mode: '332', account_id: 42, account_name: 'Account B', model: 'gpt-5.6-sol', phase: 'cooldown', status: 'cooldown', proxy_name: 'Route B', http_status: 429, length: 0, error_code: 'rate_limited' },
    ],
  }
}

describe('CodexTicketMonitor', () => {
  let wrapper: VueWrapper | undefined
  let visibility: DocumentVisibilityState

  beforeEach(() => {
    vi.useFakeTimers()
    visibility = 'visible'
    vi.spyOn(document, 'visibilityState', 'get').mockImplementation(() => visibility)
    getMonitor.mockReset().mockResolvedValue(snapshot())
  })

  afterEach(() => {
    wrapper?.unmount()
    wrapper = undefined
    vi.useRealTimers()
    vi.restoreAllMocks()
  })

  it('shows translated state and events with mode/account filters', async () => {
    wrapper = mount(CodexTicketMonitor)
    await flushPromises()
    expect(wrapper.text()).toContain('admin.settings.codexTickets.monitor.instance')
    expect(wrapper.findAll('[data-testid="ticket-monitor-state"]')).toHaveLength(2)
    expect(wrapper.text()).toContain('admin.settings.codexTickets.monitor.errors.rate_limited')
    await wrapper.get('#codex-ticket-monitor-mode').setValue('332')
    expect(wrapper.findAll('[data-testid="ticket-monitor-state"]')).toHaveLength(1)
    expect(wrapper.findAll('[data-testid="ticket-monitor-event"]')).toHaveLength(1)
    expect(wrapper.text()).not.toContain('Account A')
    await wrapper.get('#codex-ticket-monitor-account').setValue('41')
    expect(wrapper.findAll('[data-testid="ticket-monitor-state"]')).toHaveLength(0)
    await wrapper.get('#codex-ticket-monitor-mode').setValue('')
    expect(wrapper.findAll('[data-testid="ticket-monitor-state"]')).toHaveLength(1)
    expect(wrapper.text()).toContain('Account A')
  })

  it('does not overlap requests, pauses while hidden and stops after unmount', async () => {
    let resolve!: (value: ReturnType<typeof snapshot>) => void
    getMonitor.mockReturnValueOnce(new Promise(done => { resolve = done }))
    wrapper = mount(CodexTicketMonitor)
    await vi.advanceTimersByTimeAsync(15000)
    expect(getMonitor).toHaveBeenCalledTimes(1)
    expect(wrapper.get('#codex-ticket-monitor-refresh').attributes('disabled')).toBeDefined()
    resolve(snapshot())
    await flushPromises()
    await vi.advanceTimersByTimeAsync(5000)
    expect(getMonitor).toHaveBeenCalledTimes(2)
    visibility = 'hidden'
    document.dispatchEvent(new Event('visibilitychange'))
    await vi.advanceTimersByTimeAsync(15000)
    expect(getMonitor).toHaveBeenCalledTimes(2)
    visibility = 'visible'
    document.dispatchEvent(new Event('visibilitychange'))
    await flushPromises()
    expect(getMonitor).toHaveBeenCalledTimes(3)
    wrapper.unmount()
    wrapper = undefined
    await vi.advanceTimersByTimeAsync(15000)
    document.dispatchEvent(new Event('visibilitychange'))
    expect(getMonitor).toHaveBeenCalledTimes(3)
  })

  it('ignores an old hidden response and refreshes once the pending request settles', async () => {
    let resolve!: (value: ReturnType<typeof snapshot>) => void
    getMonitor.mockReturnValueOnce(new Promise(done => { resolve = done }))
    wrapper = mount(CodexTicketMonitor)
    const signal = getMonitor.mock.calls[0][0].signal as AbortSignal
    visibility = 'hidden'
    document.dispatchEvent(new Event('visibilitychange'))
    expect(signal.aborted).toBe(true)
    visibility = 'visible'
    document.dispatchEvent(new Event('visibilitychange'))
    expect(getMonitor).toHaveBeenCalledTimes(1)
    getMonitor.mockResolvedValueOnce(snapshot('Fresh account'))
    resolve(snapshot('Stale account'))
    await flushPromises()
    expect(getMonitor).toHaveBeenCalledTimes(2)
    expect(wrapper.text()).toContain('Fresh account')
    expect(wrapper.text()).not.toContain('Stale account')
  })

  it('preserves the last successful display on failure and recovers with manual refresh', async () => {
    wrapper = mount(CodexTicketMonitor)
    await flushPromises()
    getMonitor.mockRejectedValueOnce(new Error('private raw transport details'))
    await wrapper.get('#codex-ticket-monitor-refresh').trigger('click')
    await flushPromises()
    expect(wrapper.text()).toContain('Account A')
    expect(wrapper.get('[role="alert"]').text()).toBe('admin.settings.codexTickets.monitor.loadFailed')
    expect(wrapper.text()).not.toContain('private raw transport details')
    getMonitor.mockResolvedValueOnce(snapshot('Recovered account'))
    await wrapper.get('#codex-ticket-monitor-refresh').trigger('click')
    await flushPromises()
    expect(wrapper.text()).toContain('Recovered account')
    expect(wrapper.find('[role="alert"]').exists()).toBe(false)
  })

  it('does not display unknown enum values and paginates large snapshots', async () => {
    const data = snapshot()
    data.states = Array.from({ length: 51 }, (_, i) => ({ ...data.states[0], account_id: i + 1, account_name: `Account ${i + 1}`, last_error: 'secret-error-value', status: 'secret-status-value', phase: 'secret-phase-value' }))
    getMonitor.mockResolvedValueOnce(data)
    wrapper = mount(CodexTicketMonitor)
    await flushPromises()
    expect(wrapper.findAll('[data-testid="ticket-monitor-state"]')).toHaveLength(25)
    expect(wrapper.text()).not.toMatch(/secret-error-value|secret-status-value|secret-phase-value/)
    await wrapper.get('#codex-ticket-monitor-states-next').trigger('click')
    expect(wrapper.findAll('[data-testid="ticket-monitor-state"]')).toHaveLength(25)
    expect(wrapper.findAll('[data-testid="ticket-monitor-state"]')[0].text()).toContain('Account 26')
    await wrapper.get('#codex-ticket-monitor-account').setValue('Account 51')
    expect(wrapper.findAll('[data-testid="ticket-monitor-state"]')).toHaveLength(1)
  })

  it('waits to load until visible and aborts an active request on unmount', async () => {
    visibility = 'hidden'
    wrapper = mount(CodexTicketMonitor)
    await flushPromises()
    expect(getMonitor).not.toHaveBeenCalled()
    getMonitor.mockReturnValueOnce(new Promise(() => {}))
    visibility = 'visible'
    document.dispatchEvent(new Event('visibilitychange'))
    const signal = getMonitor.mock.calls[0][0].signal as AbortSignal
    wrapper.unmount()
    wrapper = undefined
    expect(signal.aborted).toBe(true)
  })
})
