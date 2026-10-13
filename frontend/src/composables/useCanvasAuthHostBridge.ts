import { onBeforeUnmount, watch } from 'vue'
import { useAuthStore } from '@/stores/auth'
import {
  CANVAS_AUTH_PROTOCOL_VERSION,
  DEFAULT_CANVAS_ORIGIN,
  isCanvasReadyMessage,
  normalizeCanvasOrigin,
  type CanvasAuthClearMessage,
} from '@/features/canvasHost/protocol'

function configuredCanvasOrigin(): string {
  try {
    return normalizeCanvasOrigin(import.meta.env.VITE_CANVAS_ORIGIN || DEFAULT_CANVAS_ORIGIN)
  } catch {
    return DEFAULT_CANVAS_ORIGIN
  }
}

function mainApiBase(): string {
  return new URL('/api/v1', window.location.origin).toString().replace(/\/+$/, '')
}

function isClosed(target: Window): boolean {
  try {
    return target.closed
  } catch {
    return true
  }
}

/**
 * Keeps the main-site session authoritative for embedded and opener canvas
 * windows. The target set is intentionally in memory and is never persisted.
 */
export function useCanvasAuthHostBridge() {
  const authStore = useAuthStore()
  const origin = configuredCanvasOrigin()
  const trustedTargets = new Set<Window>()

  const sendState = (target: Window, reason: CanvasAuthClearMessage['reason'] = 'logout') => {
    if (isClosed(target)) {
      trustedTargets.delete(target)
      return
    }
    if (authStore.isAuthenticated && authStore.token) {
      target.postMessage(
        {
          type: 'sub2api.auth.init',
          version: CANVAS_AUTH_PROTOCOL_VERSION,
          token: authStore.token,
          apiBase: mainApiBase(),
          user: authStore.user ? { id: authStore.user.id, role: authStore.user.role } : undefined,
        },
        origin,
      )
      return
    }
    target.postMessage({ type: 'sub2api.auth.clear', version: CANVAS_AUTH_PROTOCOL_VERSION, reason }, origin)
  }

  const onMessage = (event: MessageEvent<unknown>) => {
    if (event.origin !== origin || !isCanvasReadyMessage(event.data) || !event.source || event.source === window) return
    const target = event.source as Window
    trustedTargets.add(target)
    sendState(target, 'expired')
  }

  window.addEventListener('message', onMessage)
  const stopAuthWatch = watch(
    () => [authStore.token, authStore.isAuthenticated, authStore.user?.id, authStore.user?.role],
    () => {
      trustedTargets.forEach((target) => sendState(target))
    },
    { flush: 'sync' },
  )

  onBeforeUnmount(() => {
    stopAuthWatch()
    window.removeEventListener('message', onMessage)
    trustedTargets.clear()
  })
}
