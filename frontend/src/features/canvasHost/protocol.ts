export const CANVAS_AUTH_PROTOCOL_VERSION = 1 as const
export const DEFAULT_CANVAS_ORIGIN = 'https://canvas.oreniva.com'

export type CanvasReadyMessage = {
  type: 'sub2api.canvas.ready'
  version: typeof CANVAS_AUTH_PROTOCOL_VERSION
}

export type CanvasAuthInitMessage = {
  type: 'sub2api.auth.init'
  version: typeof CANVAS_AUTH_PROTOCOL_VERSION
  token: string
  apiBase: string
  user?: { id: number; role?: string }
}

export type CanvasAuthClearMessage = {
  type: 'sub2api.auth.clear'
  version: typeof CANVAS_AUTH_PROTOCOL_VERSION
  reason?: 'logout' | 'expired' | 'parent-closed' | 'unauthorized'
}

export type CanvasHostMessage = CanvasAuthInitMessage | CanvasAuthClearMessage

function isRecord(value: unknown): value is Record<string, unknown> {
  return Boolean(value && typeof value === 'object' && !Array.isArray(value))
}

export function isCanvasReadyMessage(value: unknown): value is CanvasReadyMessage {
  return isRecord(value) && value.type === 'sub2api.canvas.ready' && value.version === CANVAS_AUTH_PROTOCOL_VERSION
}

export function isCanvasAuthClearMessage(value: unknown): value is CanvasAuthClearMessage {
  return isRecord(value) && value.type === 'sub2api.auth.clear' && value.version === CANVAS_AUTH_PROTOCOL_VERSION
}

export function normalizeCanvasOrigin(value: unknown): string {
  const raw = String(value || DEFAULT_CANVAS_ORIGIN).trim() || DEFAULT_CANVAS_ORIGIN
  let parsed: URL
  try {
    parsed = new URL(raw)
  } catch {
    throw new Error('Canvas origin must be an absolute HTTP(S) origin')
  }
  if (!['http:', 'https:'].includes(parsed.protocol) || parsed.pathname !== '/' || parsed.search || parsed.hash || parsed.username || parsed.password) {
    throw new Error('Canvas origin must not contain a path, query, hash, or credentials')
  }
  return parsed.origin
}

export function buildCanvasUrl(origin: string): string {
  return new URL('/canvas', normalizeCanvasOrigin(origin)).toString()
}
