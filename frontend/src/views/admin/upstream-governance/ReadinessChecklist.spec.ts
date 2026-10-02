import { enableAutoUnmount, mount } from '@vue/test-utils'
import { afterAll, afterEach, describe, expect, it, vi } from 'vitest'
import { createI18n } from 'vue-i18n'
import zh from '@/i18n/locales/zh/governance'
import ReadinessChecklist from './ReadinessChecklist.vue'
import type { ReadinessOverview } from '@/api/admin/upstream-governance'

vi.hoisted(() => { vi.stubGlobal('__INTLIFY_JIT_COMPILATION__', true) })
afterAll(() => vi.unstubAllGlobals())
enableAutoUnmount(afterEach)

const overview: ReadinessOverview = {
  site_id: 1,
  evaluated_at: '2026-10-01T10:00:00Z',
  checks: [
    { key: 'authorization', state: 'configured', detail: 'authorized', target_tab: 'overview', count: 0 },
    { key: 'catalog', state: 'pending', detail: 'groups_incomplete', target_tab: 'overview', count: 0 },
    { key: 'bindings', state: 'configured', detail: 'bindings_ready', target_tab: 'import', count: 2 },
    { key: 'managed_keys', state: 'read_failed', detail: 'storage_unavailable', target_tab: 'import', count: 0 },
    { key: 'automation', state: 'not_enabled', detail: 'policy_disabled', target_tab: 'monitor', count: 0 },
    { key: 'balance_monitor', state: 'configured', detail: 'monitor_ready', target_tab: 'monitor', count: 0 },
    { key: 'pricing_protection', state: 'not_configured', detail: 'no_policy', target_tab: 'monitor', count: 0 },
    { key: 'notifications', state: 'not_enabled', detail: 'policy_disabled', target_tab: 'monitor', count: 0 },
  ],
}

function render(props: Record<string, unknown> = {}) {
  return mount(ReadinessChecklist, {
    props: { overview, ...props },
    global: {
      plugins: [createI18n({ legacy: false, locale: 'zh', messages: { zh: { governance: zh } } })],
    },
  })
}

describe('ReadinessChecklist', () => {
  it('renders all eight states, counts, and a UTC+8 evaluation time without exposing technical english', () => {
    const wrapper = render()
    expect(wrapper.get('[data-test="readiness-checklist"]')).toBeTruthy()
    expect(wrapper.findAll('[data-test="readiness-item"]')).toHaveLength(8)
    expect(wrapper.text()).toContain('已配置')
    expect(wrapper.text()).toContain('数据待确认')
    expect(wrapper.text()).toContain('读取失败')
    expect(wrapper.text()).toContain('未启用')
    expect(wrapper.text()).toContain('已绑定 2 项')
    expect(wrapper.text()).not.toContain('groups_incomplete')
    expect(wrapper.text()).not.toContain('storage_unavailable')
    expect(wrapper.text()).toContain('2026-10-01 18:00:00')
  })

  it('emits navigation for configured entries and keeps buttons disabled when locked', async () => {
    const wrapper = render()
    const buttons = wrapper.findAll('[data-test="readiness-navigate"]')
    expect(buttons.length).toBeGreaterThan(0)
    await buttons.find(button => button.attributes('data-target') === 'monitor')!.trigger('click')
    expect(wrapper.emitted('navigate')).toEqual([['monitor']])

    const disabled = render({ disabled: true })
    expect(disabled.findAll('[data-test="readiness-navigate"]').every(button => button.attributes('disabled') !== undefined)).toBe(true)
  })

  it('does not render an undefined count when the API omits zero counts', () => {
    const wrapper = render({ overview: { ...overview, checks: [{ ...overview.checks[0], key: 'bindings', count: undefined }] } })
    expect(wrapper.text()).not.toContain('undefined')
    expect(wrapper.text()).not.toContain('已绑定')
  })

  it('does not show an unknown-detail message when configured checks omit detail', () => {
    const wrapper = render({ overview: { ...overview, checks: [{ ...overview.checks[0], detail: '' }] } })
    expect(wrapper.text()).not.toContain('当前状态需要进一步确认')
  })
})
