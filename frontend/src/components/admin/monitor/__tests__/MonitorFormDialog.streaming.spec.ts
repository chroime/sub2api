import { afterEach, describe, expect, it, vi } from 'vitest'
import { enableAutoUnmount, flushPromises, shallowMount } from '@vue/test-utils'
import type { ChannelMonitor } from '@/api/admin/channelMonitor'
import MonitorFormDialog from '../MonitorFormDialog.vue'
import MonitorAdvancedRequestConfig from '../MonitorAdvancedRequestConfig.vue'
import Select from '@/components/common/Select.vue'

vi.mock('vue-i18n', async importOriginal => ({
  ...await importOriginal<typeof import('vue-i18n')>(),
  useI18n: () => ({ t: (key: string) => key }),
}))
vi.mock('@/stores/app', () => ({ useAppStore: () => ({ showError: vi.fn(), showSuccess: vi.fn() }) }))

const combos = [
  ['openai', 'chat_completions', 'max_tokens'],
  ['openai', 'responses', 'max_output_tokens'],
  ['anthropic', 'chat_completions', 'max_tokens'],
  ['grok', 'chat_completions', 'max_tokens'],
  ['kimi', 'chat_completions', 'max_tokens'],
  ['zhipu', 'chat_completions', 'max_tokens'],
  ['deepseek', 'chat_completions', 'max_tokens'],
  ['minimax', 'chat_completions', 'max_tokens'],
] as const
const templates = combos.map(([provider, api_mode, tokenKey], index) => ({
  id: index + 100, name: '低 Token 流式可用性探测模板', provider, api_mode,
  extra_headers: {}, body_override_mode: 'merge', body_override: { stream: true, [tokenKey]: 16 },
}))
vi.mock('@/api/admin', () => ({ adminAPI: {
  channelMonitorTemplate: { list: vi.fn(async () => ({ items: templates })) },
  groups: { getAll: vi.fn(async () => []) },
} }))
enableAutoUnmount(afterEach)

describe('streaming probe as a saved request template', () => {
  it.each(combos)('applies %s/%s from the request-template dropdown', async (provider, api_mode, tokenKey) => {
    const monitor = {
      id: 1, name: 'existing', provider, api_mode, check_mode: 'probe', endpoint: 'https://example.com',
      primary_model: 'configured-model', extra_models: [], group_name: '', enabled: true,
      interval_seconds: 60, jitter_seconds: 0, template_id: null, extra_headers: { 'X-Old': 'old' },
      body_override_mode: 'off', body_override: null, account_id: null,
    } as ChannelMonitor
    const wrapper = shallowMount(MonitorFormDialog, { props: { show: true, monitor },
      global: { stubs: { BaseDialog: { template: '<div><slot /><slot name="footer" /></div>' } } },
    })
    await flushPromises()
    const selected = templates.find(t => t.provider === provider && t.api_mode === api_mode)!
    const select = wrapper.findAllComponents(Select).find(s =>
      (s.props('options') as { value: string }[]).some(o => o.value === String(selected.id)))
    expect(select).toBeDefined()
    select!.vm.$emit('update:modelValue', String(selected.id))
    await flushPromises()
    const advanced = wrapper.getComponent(MonitorAdvancedRequestConfig)
    expect(advanced.props('bodyOverrideMode')).toBe('merge')
    expect(advanced.props('bodyOverride')).toEqual({ stream: true, [tokenKey]: 16 })
    expect(select!.props('modelValue')).toBe(String(selected.id))
  })
})
