import { mount } from '@vue/test-utils'
import { describe, expect, it, vi } from 'vitest'
import MonitorTimeline from '../monitor/MonitorTimeline.vue'

vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string, params?: { n: number }) => key === 'monitorCommon.history60pts' ? `History (${params?.n})` : key }) }))

describe('monitor timeline', () => {
  it('counts actual records and renders newest data on the right with neutral padding', () => {
    const wrapper = mount(MonitorTimeline, { props: { countdownSeconds: 58, length: 4, buckets: [
      { status: 'operational', latency_ms: 3000, ping_latency_ms: 20, checked_at: '2026-11-13T08:02:00Z' },
      { status: 'failed', latency_ms: null, ping_latency_ms: null, checked_at: '2026-11-13T08:01:00Z' },
    ] } })
    expect(wrapper.text()).toContain('History (2)')
    const bars = wrapper.findAll('[data-testid="history-bar"]')
    expect(bars).toHaveLength(4)
    expect(bars[0].attributes('data-status')).toBe('empty')
    expect(bars[2].attributes('data-status')).toBe('failed')
    expect(bars[3].attributes('data-status')).toBe('operational')
  })

  it('shows no successful records when no checks exist', () => {
    const wrapper = mount(MonitorTimeline, { props: { countdownSeconds: 58, length: 4 } })
    expect(wrapper.text()).toContain('History (0)')
    expect(wrapper.findAll('[data-status="operational"]')).toHaveLength(0)
  })

  it('shows actual measured milliseconds, including zero', () => {
    const wrapper = mount(MonitorTimeline, { props: { countdownSeconds: 0, length: 2, buckets: [
      { status: 'operational', latency_ms: 1234, ping_latency_ms: 20, checked_at: '2026-11-14T03:40:00Z' },
      { status: 'operational', latency_ms: 0, ping_latency_ms: 0, checked_at: '2026-11-14T03:39:00Z' },
    ] } })
    const bars = wrapper.findAll('[data-testid="history-bar"]')
    expect(bars[1].attributes('title')).toContain('1234ms')
    expect(bars[0].attributes('title')).toContain('0ms')
    expect(bars[1].attributes('title')).not.toContain('monitorCommon.resetBaseline')
  })

  it('uses the same tooltip format for generated reset latencies', () => {
    const wrapper = mount(MonitorTimeline, { props: { countdownSeconds: 0, length: 1, buckets: [
      { status: 'operational', latency_ms: 1832, ping_latency_ms: null, checked_at: '2026-11-14T03:39:00Z' },
    ] } })
    const title = wrapper.get('[data-testid="history-bar"]').attributes('title')
    expect(title).toContain('1832ms')
    expect(title.split(' · ')).toHaveLength(3)
  })

  it.each([null, -1, Number.NaN, Number.POSITIVE_INFINITY])('does not append ms to missing or invalid measurements (%s)', latency => {
    const wrapper = mount(MonitorTimeline, { props: { countdownSeconds: 0, length: 1, buckets: [
      { status: 'operational', latency_ms: latency, ping_latency_ms: null, checked_at: '2026-11-14T03:39:00Z' },
    ] } })
    const title = wrapper.get('[data-testid="history-bar"]').attributes('title')
    expect(title).toContain('monitorCommon.latencyEmpty')
    expect(title).not.toContain('ms')
  })
})
