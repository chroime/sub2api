import { mount } from '@vue/test-utils'
import { describe, expect, it, vi } from 'vitest'
import type { MonitorIntelligence, MonitorIntelligenceSeries } from '@/api/channelMonitor'
import MonitorIntelligenceView from '../monitor/MonitorIntelligence.vue'

vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string) => key }) }))

describe('local group intelligence history', () => {
  const end = '2026-11-14T02:59:30Z'
  const summary = (points: MonitorIntelligenceSeries['points'] = [], model = 'gpt-6-astra', effort = 'medium'): MonitorIntelligence => ({
    window_end: end, series: [{ model, effort, points }],
  })

  it('keeps 60 chronological check slots without inventing passes for missing samples', () => {
    const wrapper = mount(MonitorIntelligenceView, { props: { summary: summary([
      { status: 'pass', checked_at: '2026-11-14T02:59:00Z' },
      { status: 'fail', checked_at: '2026-11-14T02:57:00Z' },
      { status: 'error', checked_at: '2026-11-14T02:55:00Z' },
    ]) } })
    const bars = wrapper.findAll('[data-testid="intelligence-bar"]')
    expect(bars).toHaveLength(60)
    expect(bars[59].attributes('data-status')).toBe('pass')
    expect(bars[58].attributes('data-status')).toBe('fail')
    expect(bars[57].attributes('data-status')).toBe('error')
    expect(bars[56].attributes('data-status')).toBe('unknown')
    expect(wrapper.text()).toContain('gpt-6-astra')
    expect(wrapper.text()).toContain('medium')
  })

  it.each([undefined, null, { window_end: end, series: [] }])('hides the entire section without local group monitoring (%s)', value => {
    const wrapper = mount(MonitorIntelligenceView, { props: { summary: value } })
    expect(wrapper.find('[data-testid="monitor-intelligence"]').exists()).toBe(false)
    expect(wrapper.findAll('[data-testid="intelligence-bar"]')).toHaveLength(0)
  })

  it('retains model and explicit thinking effort on one line without recent results', () => {
    const model = 'claude-opus-5.5-with-a-very-long-routing-suffix'
    const wrapper = mount(MonitorIntelligenceView, { props: { summary: summary([], model) } })
    const description = wrapper.get('[data-testid="intelligence-description"]')
    expect(description.text()).toContain(model)
    expect(description.text()).toContain('monitorCommon.intelligence.thinkingEffort')
    expect(description.text().indexOf('medium')).toBeLessThan(description.text().indexOf('monitorCommon.intelligence.windowShort'))
    expect(description.classes()).toContain('flex-nowrap')
    expect(description.classes()).toContain('whitespace-nowrap')
    expect(wrapper.get('[data-testid="intelligence-effort"]').classes()).toContain('shrink-0')
    expect(wrapper.get('[data-testid="intelligence-model"]').classes()).toContain('truncate')
    expect(wrapper.get('[data-testid="intelligence-model"]').attributes('title')).toBe(model)
    expect(description.attributes('title')).toContain(model)
    expect(wrapper.findAll('[data-status="unknown"]')).toHaveLength(60)
    expect(wrapper.find('time').exists()).toBe(false)
  })

  it('labels an unspecified effort as default', () => {
    const wrapper = mount(MonitorIntelligenceView, { props: { summary: summary([], 'grok-4.7', '') } })
    expect(wrapper.get('[data-testid="intelligence-effort"]').text()).toContain('monitorCommon.intelligence.defaultEffort')
    expect(wrapper.text()).not.toContain('medium')
  })

  it('retains old checks but excludes future and invalid timestamps, including the current minute', () => {
    const wrapper = mount(MonitorIntelligenceView, { props: { summary: summary([
      { status: 'pass', checked_at: '2026-11-14T01:59:00Z' },
      { status: 'pass', checked_at: '2026-11-14T03:00:00Z' },
      { status: 'pass', checked_at: '2026-11-14T02:59:50Z' },
      { status: 'pass', checked_at: 'invalid' },
    ]) } })
    expect(wrapper.findAll('[data-status="unknown"]')).toHaveLength(59)
    const bar = wrapper.get('[data-status="pass"]')
    expect(bar.attributes('title')).toContain(new Date('2026-11-14T01:59:00Z').toLocaleString())
    expect(wrapper.get('time').attributes('datetime')).toBe('2026-11-14T01:59:00Z')
  })

  it('retains the last 60 half-hour checks across days, without merging checks in the same minute', () => {
    const points = Array.from({ length: 65 }, (_, index) => ({
      status: 'pass' as const, checked_at: new Date(Date.parse(end) - index * 30 * 60000).toISOString(),
    }))
    points.push({ status: 'pass', checked_at: '2026-11-14T02:59:10Z' })
    const wrapper = mount(MonitorIntelligenceView, { props: { summary: summary(points) } })
    const bars = wrapper.findAll('[data-testid="intelligence-bar"]')
    expect(bars).toHaveLength(60)
    expect(wrapper.findAll('[data-status="pass"]')).toHaveLength(60)
    expect(bars[0].attributes('title')).toContain(new Date(points[58].checked_at).toLocaleString())
    expect(bars[58].attributes('title')).toContain(new Date('2026-11-14T02:59:10Z').toLocaleString())
    expect(bars[59].attributes('title')).toContain(new Date(end).toLocaleString())
  })

  it('shows generated but unreviewed work as pending, never as a pass', () => {
    const wrapper = mount(MonitorIntelligenceView, { props: { summary: summary([
      { status: 'pending', checked_at: '2026-11-14T02:59:00Z' },
    ]) } })
    expect(wrapper.findAll('[data-status="pending"]')).toHaveLength(1)
    expect(wrapper.findAll('[data-status="pass"]')).toHaveLength(0)
    expect(wrapper.text()).toContain('monitorCommon.intelligence.pending')
  })

  it('keeps distinct model and effort results in separate histories', () => {
    const wrapper = mount(MonitorIntelligenceView, { props: { summary: {
      window_end: end,
      series: [
        { model: 'model-a', effort: 'medium', points: [{ status: 'pass', checked_at: end }] },
        { model: 'model-a', effort: 'high', points: [{ status: 'fail', checked_at: end }] },
        { model: 'model-b', effort: 'high', points: [] },
      ],
    } } })
    const rows = wrapper.findAll('[data-testid="intelligence-series"]')
    expect(rows).toHaveLength(3)
    expect(rows[0].text()).toContain('medium')
    expect(rows[0].findAll('[data-status="pass"]')).toHaveLength(1)
    expect(rows[1].text()).toContain('high')
    expect(rows[1].findAll('[data-status="fail"]')).toHaveLength(1)
    expect(rows[2].text()).toContain('model-b')
    expect(rows[2].findAll('[data-status="unknown"]')).toHaveLength(60)
  })

  it('removes old history when configuration is removed during refresh', async () => {
    const wrapper = mount(MonitorIntelligenceView, { props: { summary: summary([{ status: 'pass', checked_at: end }]) } })
    await wrapper.setProps({ summary: null })
    expect(wrapper.find('[data-testid="monitor-intelligence"]').exists()).toBe(false)
  })
})
