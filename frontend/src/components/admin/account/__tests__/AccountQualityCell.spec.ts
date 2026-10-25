import { mount } from '@vue/test-utils'
import { afterEach, describe, expect, it, vi } from 'vitest'
import api, { type Binding, type Run } from '@/api/admin/account-quality'
import zh from '@/i18n/locales/zh/accountQuality'
import AccountQualityCell from '../AccountQualityCell.vue'

function sample(id: string, changes: Partial<Run & { result_preview?: string }> = {}): Run & { result_preview?: string } {
  return { id, binding_id: 2, account_id: 7, policy_id: 1, policy_version: 1, binding_version: 1, model: 'gpt-6-astra', template: 'logic', template_version: '1', execution_status: 'completed', quality_verdict: 'pass', created_at: '2026-10-25T04:00:00Z', finished_at: '2026-10-25T04:00:02Z', duration_ms: 2000, ...changes }
}

function binding(changes: Partial<Binding> = {}): Binding {
  return { id: 2, version: 3, account_id: 7, account_name: '039', account_type: 'apikey', platform: 'openai', policy_id: 1, policy_name: '检测策略', policy_version: 2, model: 'gpt-6-astra', auto_isolate: false, control_mode: 'auto_quarantine', auto_control_eligible: true, interval_seconds: 600, failure_threshold: 2, state: 'healthy', failures: 0, recovery_passes: 0, isolated: false, recent_runs: [sample('latest', { result_preview: '21' })], ...changes }
}

function render(bindings: Binding[] = [binding()], props: { disabled?: boolean; loading?: boolean; error?: boolean } = {}) {
  return mount(AccountQualityCell, { props: { bindings, ...props } })
}

afterEach(() => vi.restoreAllMocks())

describe('AccountQualityCell compact evidence and controls', () => {
  it('opens audited controls for the exact selected binding without changing it or calling an API', async () => {
    const original = binding({ id: 4, isolated: true, state: 'quarantined' })
    const calls = [vi.spyOn(api, 'control'), vi.spyOn(api, 'run'), vi.spyOn(api, 'runDetail'), vi.spyOn(api, 'account')]
    const wrapper = render([binding(), original])
    expect(wrapper.emitted('control')).toBeUndefined()
    await wrapper.get('[data-test="quality-manual-review"]').trigger('click')
    await wrapper.get('[role="switch"]').trigger('click')
    expect(wrapper.emitted('control')).toEqual([[original, 'confirm_false_positive'], [original, 'set_auto']])
    expect(original.auto_isolate).toBe(false)
    expect(wrapper.get('[role="switch"]').attributes('aria-checked')).toBe('false')
    for (const call of calls) expect(call).not.toHaveBeenCalled()
  })

  it.each([
    { account_type: 'oauth', auto_control_eligible: false, control_mode: 'auto_quarantine' as const, title: zh.autoUnsupported },
    { account_type: 'apikey', auto_control_eligible: true, control_mode: 'record_only' as const, title: zh.autoModeRequired },
    { account_type: 'apikey', auto_control_eligible: undefined, control_mode: 'auto_quarantine' as const, title: zh.autoUnsupported },
  ])('does not offer automatic enable for an unsupported capability or policy: $account_type/$control_mode', async ({ title, ...changes }) => {
    const wrapper = render([binding(changes)])
    const toggle = wrapper.get('[role="switch"]')
    expect(toggle.attributes('disabled')).toBeDefined()
    expect(toggle.attributes('title')).toBe(title)
    await toggle.trigger('click')
    expect(wrapper.emitted('control')).toBeUndefined()
  })

  it('allows requesting disable when automatic isolation is already on even after capability removal', async () => {
    const original = binding({ auto_isolate: true, auto_control_eligible: false, control_mode: 'record_only' })
    const wrapper = render([original])
    const toggle = wrapper.get('[role="switch"]')
    expect(toggle.attributes('disabled')).toBeUndefined()
    expect(toggle.attributes('aria-checked')).toBe('true')
    await toggle.trigger('click')
    expect(wrapper.emitted('control')).toEqual([[original, 'set_auto']])
    expect(original.auto_isolate).toBe(true)
  })

  it('respects the busy state for all write-intent actions', async () => {
    const wrapper = render([binding()], { disabled: true })
    for (const selector of ['[data-test="quality-manual-review"]', '[role="switch"]', '[data-test="quality-run-now"]']) {
      expect(wrapper.get(selector).attributes('disabled')).toBeDefined()
      await wrapper.get(selector).trigger('click')
    }
    expect(wrapper.emitted('control')).toBeUndefined()
    expect(wrapper.emitted('run')).toBeUndefined()
    await wrapper.get('[data-test="quality-state"]').trigger('click')
    expect(wrapper.emitted('details')).toHaveLength(1)
  })

  it('orders actual timestamps and uses the newest run for verdict, time, duration, preview and evidence', async () => {
    const newest = sample('newest', { finished_at: '2026-10-25T05:00:05Z', duration_ms: 4200, result_preview: '分析过程\n\n最终结果：21\n', quality_verdict: 'fail' })
    const oldest = sample('oldest', { finished_at: '2026-10-25T03:00:00Z', result_preview: '旧答案' })
    const middle = sample('middle', { finished_at: null, started_at: '2026-10-25T04:00:00Z' })
    const runs = [newest, oldest, middle]
    const wrapper = render([binding({ recent_runs: runs, last_duration_ms: 999999, last_run_at: oldest.finished_at })])
    expect(wrapper.findAll('[data-test="quality-sample"]').map(item => item.attributes('data-run-id'))).toEqual(['oldest', 'middle', 'newest'])
    expect(wrapper.get('[data-test="quality-latest-result"]').text()).toContain(zh.verdict_fail)
    expect(wrapper.get('[data-test="quality-latest-result"]').text()).toContain('4.2s')
    expect(wrapper.get('[data-test="quality-latest-result"] time').attributes('datetime')).toBe(newest.finished_at)
    expect(wrapper.get('[data-test="quality-output-preview"]').text()).toBe('最终结果：21')
    await wrapper.get('[data-test="quality-output-preview"]').trigger('click')
    expect(wrapper.emitted('evidence')).toEqual([[newest]])
    expect(runs.map(item => item.id)).toEqual(['newest', 'oldest', 'middle'])
  })

  it('limits to the latest 30 samples and keeps the trend in a single non-wrapping row', () => {
    const runs = Array.from({ length: 35 }, (_, index) => sample(`run-${index}`, { finished_at: new Date(Date.UTC(2026, 9, 25, 0, index)).toISOString() })).reverse()
    const wrapper = render([binding({ recent_runs: runs })])
    expect(wrapper.findAll('[data-test="quality-sample"]').map(item => item.attributes('data-run-id'))).toEqual(Array.from({ length: 30 }, (_, index) => `run-${index + 5}`))
    const row = wrapper.get('[data-test="quality-sample-row"]')
    expect(row.classes()).toContain('flex-nowrap')
    expect(row.classes()).not.toContain('flex-wrap')
    expect(wrapper.findAll('[data-test="quality-sample"]').every(item => item.attributes('title')?.includes(item.attributes('data-run-id')!))).toBe(true)
  })

  it('does not reuse an old duration or expected answer while the newest sample is still queued', () => {
    const queued = sample('queued', { execution_status: 'queued', quality_verdict: 'not_evaluated', finished_at: null, started_at: null, created_at: '2026-10-25T07:00:00Z', duration_ms: undefined, expected: '21' })
    const wrapper = render([binding({ recent_runs: [queued, sample('finished')], last_duration_ms: 999999 })])
    expect(wrapper.get('[data-test="quality-latest-result"]').text()).not.toContain('1000.0s')
    expect(wrapper.get('[data-test="quality-latest-result"]').text()).toContain(zh.execution_queued)
    expect(wrapper.get('[data-test="quality-output-preview"]').text()).toBe(zh.noOutput)
    expect(wrapper.get('[data-test="quality-latest-result"] time').attributes('datetime')).toBe(queued.created_at)
  })

  it('shows a friendly network timeout without presenting it as a model quality failure or an expected answer', () => {
    const wrapper = render([binding({ state: 'execution_error', recent_runs: [sample('network', { execution_status: 'execution_error', quality_verdict: 'not_evaluated', error_class: 'network_timeout', result_preview: undefined, expected: '21' })] })])
    expect(wrapper.get('[data-test="quality-latest-verdict"]').text()).toBe(zh.networkTimeout)
    expect(wrapper.get('[data-test="quality-latest-verdict"]').classes()).toContain('text-amber-700')
    expect(wrapper.get('[data-test="quality-sample"]').classes()).toContain('bg-amber-400')
    expect(wrapper.get('[data-test="quality-output-preview"]').text()).toBe(zh.networkTimeoutHint)
    expect(wrapper.text()).not.toContain(zh.verdict_fail)
    expect(wrapper.get('[data-test="quality-output-preview"]').text()).not.toContain('21')
  })

  it.each([
    { execution_status: 'completed', quality_verdict: 'pass', color: 'bg-emerald-500' },
    { execution_status: 'completed', quality_verdict: 'fail', color: 'bg-red-500' },
    { execution_status: 'truncated', quality_verdict: 'fail', color: 'bg-amber-400' },
    { execution_status: 'running', quality_verdict: 'not_evaluated', color: 'bg-gray-300' },
  ])('keeps result colors semantic for $execution_status/$quality_verdict', ({ color, ...changes }) => {
    const wrapper = render([binding({ recent_runs: [sample('tone', changes)] })])
    expect(wrapper.get('[data-test="quality-sample"]').classes()).toContain(color)
  })

  it('bounds a full response preview, ignores empty/fence lines and renders malicious output as text', () => {
    const text = `<img src=x onerror="alert(1)">${'答'.repeat(300)}`
    const wrapper = render([binding({ recent_runs: [sample('long', { response_text: `推理过程\n\n\`\`\`text\n${text}\n\`\`\`\n`, expected: '这是参考答案' })] })])
    const preview = wrapper.get('[data-test="quality-output-preview"]')
    expect(Array.from(preview.text()).length).toBeLessThanOrEqual(241)
    expect(preview.text().endsWith('答'.repeat(240))).toBe(true)
    expect(preview.text()).not.toContain('参考答案')
    const malicious = render([binding({ recent_runs: [sample('html', { result_preview: '<img src=x onerror="alert(1)">' })] })])
    expect(malicious.get('[data-test="quality-output-preview"]').text()).toBe('<img src=x onerror="alert(1)">')
    expect(malicious.find('img').exists()).toBe(false)
  })

  it('keeps no evidence, loading and failure states explicit and emits the original run intent', async () => {
    const empty = render([])
    expect(empty.text()).toContain(zh.state_untested)
    expect(empty.find('[role="switch"]').exists()).toBe(false)
    expect(render([], { loading: true }).text()).toContain(zh.loading)
    expect(render([], { error: true }).text()).toContain(zh.loadFailed)
    const original = binding({ recent_runs: [] })
    const wrapper = render([original])
    expect(wrapper.get('[data-test="quality-output-preview"]').text()).toBe(zh.noOutput)
    await wrapper.get('[data-test="quality-run-now"]').trigger('click')
    expect(wrapper.emitted('run')).toEqual([[original]])
  })
})
