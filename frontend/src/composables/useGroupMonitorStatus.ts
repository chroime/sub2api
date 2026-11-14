import { computed, onScopeDispose, ref, toValue, watch, type MaybeRefOrGetter } from 'vue'
import { list } from '@/api/channelMonitor'
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
  const now = ref(Date.now())
  let lastSuccess = 0
  let timer: ReturnType<typeof setInterval> | undefined
  let controller: AbortController | undefined
  let generation = 0

  const statuses = computed(() => buildGroupMonitorStates(toValue(groups), items.value, {
    now: now.value, state: toValue(enabled) ? state.value : 'disabled',
  }))

  async function refresh() {
    if (controller || !toValue(open) || !toValue(enabled)) return
    const request = new AbortController()
    controller = request
    const currentGeneration = ++generation
    if (!lastSuccess) state.value = 'loading'
    try {
      const response = await list({ signal: request.signal })
      if (request.signal.aborted || generation !== currentGeneration) return
      items.value = response.items || []
      state.value = 'ready'
      lastSuccess = now.value = Date.now()
    } catch {
      if (request.signal.aborted || generation !== currentGeneration) return
      items.value = []
      state.value = 'unavailable'
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
    if (!isOpen || !isEnabled) return
    now.value = Date.now()
    if (!lastSuccess || now.value - lastSuccess >= REFRESH_MS) void refresh()
    timer = setInterval(() => {
      now.value = Date.now()
      if (!document.hidden) void refresh()
    }, REFRESH_MS)
  }, { immediate: true })
  onScopeDispose(stop)
  return { statuses }
}
