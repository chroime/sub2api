import { computed, onScopeDispose, ref, toValue, watch, type MaybeRefOrGetter } from 'vue'
import { analytics, list } from '@/api/channelMonitor'
import type { UserMonitorView } from '@/api/channelMonitor'
import { buildGroupMonitorStates, type GroupMonitorLoadState } from '@/utils/groupMonitor'

const REFRESH_MS = 30000

/** Cache belongs to this user view, never to a shared module or another login. */
export function useGroupMonitorStatus(
  open: MaybeRefOrGetter<boolean>,
  groups: MaybeRefOrGetter<Array<{ id: number; platform: string }>>,
  enabled: MaybeRefOrGetter<boolean>,
) {
  const items = ref<UserMonitorView[]>([])
  const state = ref<GroupMonitorLoadState>('loading')
  const intelligenceState = ref<GroupMonitorLoadState>('loading')
  const now = ref(Date.now())
  let lastSuccess = 0
  let timer: ReturnType<typeof setInterval> | undefined
  let controller: AbortController | undefined
  let generation = 0

  const statuses = computed(() => buildGroupMonitorStates(toValue(groups), items.value, {
    now: now.value, state: toValue(enabled) ? state.value : 'disabled',
    intelligenceState: intelligenceState.value,
  }))

  async function refresh() {
    if (controller || !toValue(open) || !toValue(enabled)) return
    const request = new AbortController()
    controller = request
    const currentGeneration = ++generation
    if (!lastSuccess) state.value = 'loading'
    if (intelligenceState.value !== 'ready') intelligenceState.value = 'loading'
    try {
      const response = await list({ signal: request.signal })
      if (request.signal.aborted || generation !== currentGeneration) return
      items.value = response.items || []
      state.value = 'ready'
      lastSuccess = now.value = Date.now()
      if (!items.value.length) {
        intelligenceState.value = 'ready'
        return
      }
      intelligenceState.value = 'loading'
      try {
        const details = await analytics({ signal: request.signal })
        if (request.signal.aborted || generation !== currentGeneration) return
        const byID = new Map((details.items || []).map(item => [item.id, item]))
        items.value = items.value.map(item => ({
          ...item, intelligence: byID.get(item.id)?.intelligence ?? item.intelligence ?? null,
        }))
        intelligenceState.value = 'ready'
        now.value = Date.now()
      } catch {
        if (request.signal.aborted || generation !== currentGeneration) return
        intelligenceState.value = 'unavailable'
      }
    } catch {
      if (request.signal.aborted || generation !== currentGeneration) return
      items.value = []
      state.value = 'unavailable'
      intelligenceState.value = 'unavailable'
      lastSuccess = 0
    } finally {
      if (controller === request) controller = undefined
    }
  }

  function stop() {
    generation++
    controller?.abort()
    controller = undefined
    if (timer) clearInterval(timer)
    timer = undefined
  }

  watch(() => [toValue(open), toValue(enabled)], ([isOpen, isEnabled]) => {
    stop()
    if (!isEnabled) {
      items.value = []
      lastSuccess = 0
      state.value = 'loading'
      intelligenceState.value = 'loading'
      return
    }
    if (!isOpen) return
    now.value = Date.now()
    if (!lastSuccess || now.value - lastSuccess >= REFRESH_MS || intelligenceState.value !== 'ready') void refresh()
    timer = setInterval(() => {
      now.value = Date.now()
      if (!document.hidden) void refresh()
    }, REFRESH_MS)
  }, { immediate: true })
  onScopeDispose(stop)
  return { statuses }
}
