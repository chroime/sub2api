import { mount } from '@vue/test-utils'
import { describe, expect, it, vi } from 'vitest'
import type { UserMonitorView } from '@/api/channelMonitor'
import MonitorCardGrid from '../monitor/MonitorCardGrid.vue'
import MonitorCard from '../monitor/MonitorCard.vue'

vi.mock('vue-i18n', async () => ({
  ...await vi.importActual<typeof import('vue-i18n')>('vue-i18n'),
  useI18n: () => ({ t: (key: string) => key }),
}))

function item(id: number, provider: UserMonitorView['provider'], name: string): UserMonitorView {
  return { id, name, provider, group_name: '', primary_model: 'test-model', primary_status: 'operational', primary_latency_ms: 500, primary_ping_latency_ms: 50, availability_7d: 99, extra_models: [], timeline: [] }
}
const items = [item(9, 'grok', 'Grok first'), item(5, 'openai', 'OpenAI first'), item(3, 'grok', 'Grok second'), item(1, 'anthropic', 'Claude first')]

function open(window: '7d' | '15d' | '30d' = '7d') {
  return mount(MonitorCardGrid, { props: { items, window, countdownSeconds: 58, loading: false, detailCache: {} }, global: { stubs: { MonitorCard: { props: ['item', 'availabilityValue'], emits: ['click'], template: '<button @click="$emit(\'click\')">{{ item.name }}</button>' }, ProviderIcon: true } } })
}

describe('channel status platform sections', () => {
  it('groups platforms in first-seen order and keeps channel order within each platform', () => {
    const wrapper = open()
    const sections = wrapper.findAll('section[data-provider]')
    expect(sections.map(section => section.attributes('data-provider'))).toEqual(['grok', 'openai', 'anthropic'])
    expect(sections[0].findAllComponents(MonitorCard).map(card => card.props('item').id)).toEqual([9, 3])
    expect(sections[0].get('h2').text()).toContain('monitorCommon.providers.grok')
    expect(sections[0].get('[data-testid="provider-count"]').text()).toBe('2')
  })

  it('forwards the original monitor when a card is selected', async () => {
    const wrapper = open()
    await wrapper.findAllComponents(MonitorCard)[1].trigger('click')
    expect(wrapper.emitted('cardClick')).toEqual([[items[2]]])
  })

  it('uses a neutral value until the selected window has detail data', () => {
    const wrapper = open('30d')
    expect(wrapper.findComponent(MonitorCard).props('availabilityValue')).toBeNull()
  })
})
