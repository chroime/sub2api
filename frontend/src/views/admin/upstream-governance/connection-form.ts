import type { CaptchaProvider, LoginChallenge, LoginInput } from '@/api/admin/upstream-governance'
import { SessionImportError } from './session-import'

export interface ConnectionForm {
  mode: 'password' | 'session'
  username: string
  password: string
  otp: string
  challengeToken: string
  captchaToken: string
  captchaProvider?: CaptchaProvider
  tencentTicket: string
  tencentRandstr: string
  sessionToken: string
  refreshToken: string
  expiresIn?: number
  sessionExpiresAt?: number
  userAgent: string
  userId?: number
}

export function newConnectionForm(): ConnectionForm {
  return { mode: 'password', username: '', password: '', otp: '', challengeToken: '', captchaToken: '', tencentTicket: '', tencentRandstr: '', sessionToken: '', refreshToken: '', userAgent: '' }
}

export function connectionInput(form: ConnectionForm): LoginInput {
  if (form.mode === 'session') {
    if (!form.sessionToken.trim()) throw new SessionImportError('access_token_required')
    let expiresIn = form.expiresIn
    if (form.sessionExpiresAt !== undefined) {
      if (!Number.isSafeInteger(form.sessionExpiresAt)) throw new SessionImportError('invalid_expiry')
      const remaining = Math.floor((form.sessionExpiresAt - Date.now()) / 1000)
      if (remaining <= 0) throw new SessionImportError('expired')
      expiresIn = typeof expiresIn === 'number' && expiresIn > 0 ? Math.min(expiresIn, remaining) : remaining
    }
    return {
      session_token: form.sessionToken,
      user_id: form.userId,
      ...(form.refreshToken ? { refresh_token: form.refreshToken } : {}),
      ...(typeof expiresIn === 'number' && expiresIn > 0 ? { expires_in: expiresIn } : {}),
      ...(form.userAgent ? { user_agent: form.userAgent } : {}),
    }
  }
  return {
    username: form.username.trim(),
    password: form.password,
    otp: form.otp || undefined,
    challenge_token: form.challengeToken || undefined,
    ...(form.captchaProvider === 'turnstile' ? { turnstile_token: form.captchaToken || undefined } : {}),
    ...(form.captchaProvider === 'tencent' ? { tencent_captcha_ticket: form.tencentTicket || undefined, tencent_captcha_randstr: form.tencentRandstr || undefined } : {}),
    ...(form.captchaProvider == null ? { captcha_token: form.captchaToken || undefined } : {}),
  }
}

export function clearConnectionProofs(form: ConnectionForm) {
  form.otp = ''
  form.captchaToken = ''
  form.tencentTicket = ''
  form.tencentRandstr = ''
}

export function clearConnectionSecrets(form: ConnectionForm) {
  form.password = ''
  clearConnectionProofs(form)
  form.challengeToken = ''
  form.sessionToken = ''
  form.refreshToken = ''
  form.expiresIn = undefined
  form.sessionExpiresAt = undefined
  form.userAgent = ''
  form.userId = undefined
}

export function continueChallenge(form: ConnectionForm, challenge: LoginChallenge) {
  // CAPTCHA retries repeat password login; TOTP uses the opaque continuation token.
  const password = challenge.kind === 'captcha' ? form.password : ''
  clearConnectionSecrets(form)
  form.password = password
  form.challengeToken = challenge.token || ''
  form.captchaProvider = challenge.provider
}

export function connectionBlocked(form: ConnectionForm, challenge: string) {
  return form.mode === 'password' && !!challenge && (
    (challenge !== 'captcha' && challenge !== 'totp') ||
    (challenge === 'captcha' && (
      form.captchaProvider === 'aliyun' || form.captchaProvider === 'unknown' ||
      (form.captchaProvider === 'tencent' ? !form.tencentTicket.trim() || !form.tencentRandstr.trim() : !form.captchaToken.trim())
    ))
  )
}
