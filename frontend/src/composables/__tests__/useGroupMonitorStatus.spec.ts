import { effectScope, nextTick, ref } from 'vue'
import { flushPromises } from '@vue/test-utils'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { useGroupMonitorStatus } from '../useGroupMonitorStatus'

const { list } = vi.hoisted(() => ({ list: vi.fn() }))
vi.mock('@/api/channelMonitor', () => ({ list }))

afterEach(() => { vi.useRealTimers(); list.mockReset() })

describe('batched group monitoring lifecycle', () => {
  it('fetches once for many groups, reuses the cache and polls only while open', async () => {
    vi.useFakeTimers()
    list.mockResolvedValue({ items: [] })
    const open = ref(false)
    const scope = effectScope()
    const result = scope.run(() => useGroupMonitorStatus(open, ref(Array.from({ length: 100 }, (_, i) => ({ id: i + 1, platform: 'openai' }))), ref(true)))!
    expect(list).not.toHaveBeenCalled()
    open.value = true
    await nextTick(); await flushPromises()
    expect(list).toHaveBeenCalledTimes(1)
    expect(result.statuses.value.size).toBe(100)
    open.value = false
    await nextTick()
    open.value = true
    await nextTick(); await flushPromises()
    expect(list).toHaveBeenCalledTimes(1)
    await vi.advanceTimersByTimeAsync(30000)
    expect(list).toHaveBeenCalledTimes(2)
    open.value = false
    await nextTick()
    await vi.advanceTimersByTimeAsync(60000)
    expect(list).toHaveBeenCalledTimes(2)
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
