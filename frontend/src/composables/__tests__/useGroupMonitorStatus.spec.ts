import { effectScope, nextTick, ref } from 'vue'
import { flushPromises } from '@vue/test-utils'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { useGroupMonitorStatus } from '../useGroupMonitorStatus'
import type { UserMonitorAnalytics, UserMonitorView } from '@/api/channelMonitor'

const { list, analytics } = vi.hoisted(() => ({ list: vi.fn(), analytics: vi.fn() }))
vi.mock('@/api/channelMonitor', () => ({ list, analytics }))

afterEach(() => { vi.useRealTimers(); list.mockReset(); analytics.mockReset() })

function monitor(): UserMonitorView {
  return {
    id: 7, group_id: 1, name: 'Monitor', group_name: 'Group', provider: 'openai',
    primary_model: 'gpt-6.1-sol', primary_status: 'operational', primary_latency_ms: 1200,
    primary_ping_latency_ms: null, availability_7d: 100, extra_models: [],
    probe_valid_until: new Date(Date.now() + 120000).toISOString(),
    timeline: [{ status: 'operational', checked_at: new Date().toISOString(), latency_ms: 1200, ping_latency_ms: null }]
  }
}

describe('batched group monitoring lifecycle', () => {
  it('renders availability immediately and merges separately loaded model checks by monitor ID', async () => {
    let resolveAnalytics!: (value: { items: UserMonitorAnalytics[] }) => void
    const item = monitor()
    list.mockResolvedValue({ items: [item] })
    analytics.mockImplementation(() => new Promise(resolve => { resolveAnalytics = resolve }))
    const scope = effectScope()
    const result = scope.run(() => useGroupMonitorStatus(ref(true), ref([{ id: 1, platform: 'openai' }]), ref(true)))!
    try {
      await flushPromises()
      expect(result.statuses.value.get(1)?.availability.state).toBe('ready')
      expect(result.statuses.value.get(1)?.intelligence.state).toBe('loading')
      expect(analytics).toHaveBeenCalledTimes(1)
      resolveAnalytics({ items: [
        { id: 999, intelligence: { window_end: new Date().toISOString(), series: [
          { model: 'wrong-model', effort: 'high', points: [{ status: 'fail', checked_at: item.timeline[0]!.checked_at }] }
        ] } },
        { id: item.id, intelligence: { window_end: new Date().toISOString(), series: [
          { model: item.primary_model, effort: 'high', points: [{ status: 'pass', checked_at: item.timeline[0]!.checked_at }] }
        ] } }
      ] })
      await flushPromises()
      const intelligence = result.statuses.value.get(1)!.intelligence
      expect(intelligence.state).toBe('ready')
      expect(intelligence.bars.at(-1)?.status).toBe('pass')
      expect(intelligence.details.map(detail => detail.model)).toEqual([item.primary_model])
    } finally { scope.stop() }
  })

  it('retains availability when model-check loading fails and retries on reopening', async () => {
    list.mockResolvedValue({ items: [monitor()] })
    analytics.mockRejectedValueOnce(new Error('offline')).mockResolvedValue({ items: [] })
    const open = ref(true), scope = effectScope()
    const result = scope.run(() => useGroupMonitorStatus(open, ref([{ id: 1, platform: 'openai' }]), ref(true)))!
    try {
      await flushPromises()
      expect(result.statuses.value.get(1)?.availability.state).toBe('ready')
      expect(result.statuses.value.get(1)?.intelligence.state).toBe('unavailable')
      open.value = false
      await nextTick()
      open.value = true
      await nextTick(); await flushPromises()
      expect(analytics).toHaveBeenCalledTimes(2)
      expect(result.statuses.value.get(1)?.availability.state).toBe('ready')
      expect(result.statuses.value.get(1)?.intelligence.state).toBe('unconfigured')
    } finally { scope.stop() }
  })

  it('aborts model-check loading on close and ignores its late response', async () => {
    let resolveAnalytics!: (value: { items: UserMonitorAnalytics[] }) => void
    list.mockResolvedValue({ items: [monitor()] })
    analytics.mockImplementation(() => new Promise(resolve => { resolveAnalytics = resolve }))
    const open = ref(true), scope = effectScope()
    const result = scope.run(() => useGroupMonitorStatus(open, ref([{ id: 1, platform: 'openai' }]), ref(true)))!
    try {
      await flushPromises()
      const signal = analytics.mock.calls[0]![0].signal as AbortSignal
      open.value = false
      await nextTick()
      expect(signal.aborted).toBe(true)
      resolveAnalytics({ items: [] })
      await flushPromises()
      expect(result.statuses.value.get(1)?.intelligence.state).toBe('loading')
    } finally { scope.stop() }
  })

  it('fetches once for many groups, reuses the cache and polls only while open', async () => {
    vi.useFakeTimers()
    list.mockResolvedValue({ items: [monitor()] })
    analytics.mockResolvedValue({ items: [] })
    const open = ref(false)
    const scope = effectScope()
    const result = scope.run(() => useGroupMonitorStatus(open, ref(Array.from({ length: 100 }, (_, i) => ({ id: i + 1, platform: 'openai' }))), ref(true)))!
    expect(list).not.toHaveBeenCalled()
    open.value = true
    await nextTick(); await flushPromises()
    expect(list).toHaveBeenCalledTimes(1)
    expect(analytics).toHaveBeenCalledTimes(1)
    expect(result.statuses.value.size).toBe(100)
    open.value = false
    await nextTick()
    open.value = true
    await nextTick(); await flushPromises()
    expect(list).toHaveBeenCalledTimes(1)
    expect(analytics).toHaveBeenCalledTimes(1)
    await vi.advanceTimersByTimeAsync(30000)
    expect(list).toHaveBeenCalledTimes(2)
    expect(analytics).toHaveBeenCalledTimes(2)
    open.value = false
    await nextTick()
    await vi.advanceTimersByTimeAsync(60000)
    expect(list).toHaveBeenCalledTimes(2)
    expect(analytics).toHaveBeenCalledTimes(2)
    scope.stop()
  })

  it('aborts on close and ignores a late response from a cancelled request', async () => {
    let resolve!: (value: { items: never[] }) => void
    list.mockImplementation(() => new Promise(r => { resolve = r }))
    const open = ref(true), scope = effectScope()
    const result = scope.run(() => useGroupMonitorStatus(open, ref([{ id: 1, platform: 'openai' }]), ref(true)))!
    const signal = list.mock.calls[0]![0].signal as AbortSignal
    open.value = false
    await nextTick()
    expect(signal.aborted).toBe(true)
    resolve({ items: [] })
    await flushPromises()
    expect(result.statuses.value.get(1)?.availability.state).not.toBe('ready')
    scope.stop()
  })

  it('shows neutral error states and retries on reopening without blocking selection', async () => {
    list.mockRejectedValueOnce(new Error('offline')).mockResolvedValue({ items: [] })
    const open = ref(true), scope = effectScope()
    const result = scope.run(() => useGroupMonitorStatus(open, ref([{ id: 1, platform: 'openai' }]), ref(true)))!
    await flushPromises()
    expect(result.statuses.value.get(1)?.availability.state).toBe('unavailable')
    open.value = false
    await nextTick()
    open.value = true
    await nextTick(); await flushPromises()
    expect(list).toHaveBeenCalledTimes(2)
    expect(result.statuses.value.get(1)?.availability.state).toBe('unconfigured')
    scope.stop()
  })

  it('does not query when the channel feature is disabled and cleans up polling', async () => {
    vi.useFakeTimers()
    list.mockResolvedValue({ items: [] })
    const enabled = ref(false), scope = effectScope()
    const result = scope.run(() => useGroupMonitorStatus(ref(true), ref([{ id: 1, platform: 'openai' }]), enabled))!
    expect(list).not.toHaveBeenCalled()
    expect(result.statuses.value.get(1)?.intelligence.state).toBe('disabled')
    enabled.value = true
    await nextTick(); await flushPromises()
    expect(list).toHaveBeenCalledTimes(1)
    scope.stop()
    await vi.advanceTimersByTimeAsync(60000)
    expect(list).toHaveBeenCalledTimes(1)
  })
})
