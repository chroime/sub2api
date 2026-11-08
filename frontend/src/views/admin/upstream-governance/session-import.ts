const MAX_JSON_BYTES = 128 * 1024
const MAX_TOKEN_LENGTH = 16384
const MAX_LIFETIME_SECONDS = 366 * 24 * 60 * 60

export type SessionImportErrorCode =
  | 'invalid_json' | 'too_large' | 'unsuccessful_response' | 'access_token_required'
  | 'api_key_not_session' | 'invalid_token' | 'invalid_expiry' | 'expired'
  | 'refresh_expiry_required' | 'invalid_user_agent'

export class SessionImportError extends Error {
  readonly reason: string

  constructor(readonly code: SessionImportErrorCode) {
    // Only fixed codes may leave the parser; never include pasted credentials.
    super(`session_import_${code}`)
    this.name = 'SessionImportError'
    this.reason = `session_import_${code}`
  }
}

export interface ImportedSession {
  sessionToken: string
  refreshToken: string
  userAgent: string
  expiresIn?: number
  /** Absolute milliseconds, retained until submission to avoid resetting a copied lifetime. */
  sessionExpiresAt?: number
}

function object(value: unknown): value is Record<string, unknown> {
  return value !== null && typeof value === 'object' && !Array.isArray(value)
}

function hasOwn(value: Record<string, unknown>, key: string): boolean {
  return Object.prototype.hasOwnProperty.call(value, key)
}

function token(value: unknown, required = false): string {
  if (value === undefined || value === '') {
    if (required) throw new SessionImportError('access_token_required')
    return ''
  }
  if (typeof value !== 'string' || value.length > MAX_TOKEN_LENGTH || !/^[\x21-\x7e]+$/.test(value)) {
    throw new SessionImportError('invalid_token')
  }
  return value
}

function jwtExpiry(value: string): number | undefined {
  const parts = value.split('.')
  if (parts.length !== 3 || !/^[A-Za-z0-9_-]+$/.test(parts[1])) return
  let payload: unknown
  try {
    const base64 = parts[1].replace(/-/g, '+').replace(/_/g, '/')
    payload = JSON.parse(atob(base64.padEnd(Math.ceil(base64.length / 4) * 4, '=')))
  } catch {
    return // Opaque dashboard tokens are valid inputs too; identity is checked by the backend.
  }
  if (!object(payload) || !hasOwn(payload, 'exp')) return
  if (!Number.isSafeInteger(payload.exp) || (payload.exp as number) <= 0) throw new SessionImportError('invalid_expiry')
  // Decoding is only a conservative time limit, never signature or identity verification.
  return (payload.exp as number) * 1000
}

function absoluteExpiry(value: unknown): number {
  if (typeof value === 'number' && Number.isSafeInteger(value) && value > 0) return value * 1000
  if (typeof value === 'string' && /^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(?:\.\d{1,3})?(?:Z|[+-]\d{2}:\d{2})$/.test(value)) {
    const parsed = Date.parse(value)
    if (Number.isFinite(parsed)) return parsed
  }
  throw new SessionImportError('invalid_expiry')
}

function incomplete(value: Record<string, unknown>): boolean {
  return value.success === false || value.requires_2fa === true || value.requires_totp === true ||
    !!value.challenge_token || !!value.challenge
}

export function parseSessionImport(raw: string, nowMs = Date.now()): ImportedSession {
  if (raw.length > MAX_JSON_BYTES || new TextEncoder().encode(raw).byteLength > MAX_JSON_BYTES) {
    throw new SessionImportError('too_large')
  }
  let parsed: unknown
  try { parsed = JSON.parse(raw) } catch { throw new SessionImportError('invalid_json') }
  if (!object(parsed)) throw new SessionImportError('invalid_json')
  let data = parsed
  if (incomplete(parsed)) throw new SessionImportError('unsuccessful_response')
  if (hasOwn(parsed, 'code')) {
    if (parsed.code !== 0 || !object(parsed.data)) throw new SessionImportError('unsuccessful_response')
    data = parsed.data
  }
  if (incomplete(data)) throw new SessionImportError('unsuccessful_response')

  const sessionToken = token(data.access_token, true)
  if (/^sk-/i.test(sessionToken)) throw new SessionImportError('api_key_not_session')
  const refreshToken = token(data.refresh_token)
  const userAgent = data.user_agent === undefined ? '' : data.user_agent
  if (typeof userAgent !== 'string' || userAgent.length > 1024 || /[^\x20-\x7e]/.test(userAgent)) {
    throw new SessionImportError('invalid_user_agent')
  }

  const deadlines: number[] = []
  if (hasOwn(data, 'expires_in')) {
    if (!Number.isSafeInteger(data.expires_in) || (data.expires_in as number) <= 0 || (data.expires_in as number) > MAX_LIFETIME_SECONDS) {
      throw new SessionImportError('invalid_expiry')
    }
    deadlines.push(nowMs + (data.expires_in as number) * 1000)
  }
  if (hasOwn(data, 'expires_at')) deadlines.push(absoluteExpiry(data.expires_at))
  const jwtExpiresAt = jwtExpiry(sessionToken)
  if (jwtExpiresAt !== undefined) deadlines.push(jwtExpiresAt)
  if (deadlines.some(deadline => !Number.isSafeInteger(deadline) || deadline - nowMs > MAX_LIFETIME_SECONDS * 1000)) {
    throw new SessionImportError('invalid_expiry')
  }
  const sessionExpiresAt = deadlines.length ? Math.min(...deadlines) : undefined
  const expiresIn = sessionExpiresAt === undefined ? undefined : Math.floor((sessionExpiresAt - nowMs) / 1000)
  if (expiresIn !== undefined && expiresIn <= 0) throw new SessionImportError('expired')
  if (refreshToken && expiresIn === undefined) throw new SessionImportError('refresh_expiry_required')

  return {
    sessionToken, refreshToken, userAgent,
    ...(sessionExpiresAt !== undefined ? { sessionExpiresAt, expiresIn } : {}),
  }
}
