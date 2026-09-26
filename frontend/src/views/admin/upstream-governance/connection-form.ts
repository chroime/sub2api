import type { LoginInput } from '@/api/admin/upstream-governance'

export interface ConnectionForm {
  mode: 'password' | 'session'
  username: string
  password: string
  otp: string
  challengeToken: string
  captchaToken: string
  sessionToken: string
  userId?: number
}

export function newConnectionForm(): ConnectionForm {
  return { mode: 'password', username: '', password: '', otp: '', challengeToken: '', captchaToken: '', sessionToken: '' }
}

export function connectionInput(form: ConnectionForm): LoginInput {
  if (form.mode === 'session') return { session_token: form.sessionToken, user_id: form.userId }
  return {
    username: form.username.trim(),
    password: form.password,
    otp: form.otp || undefined,
    challenge_token: form.challengeToken || undefined,
    captcha_token: form.captchaToken || undefined,
  }
}

export function clearConnectionSecrets(form: ConnectionForm) {
  form.password = ''
  form.otp = ''
  form.challengeToken = ''
  form.captchaToken = ''
  form.sessionToken = ''
}

export function continueChallenge(form: ConnectionForm, challenge: { kind: string; token?: string }) {
  // CAPTCHA retries repeat password login; TOTP uses the opaque continuation token.
  const password = challenge.kind === 'captcha' ? form.password : ''
  clearConnectionSecrets(form)
  form.password = password
  form.challengeToken = challenge.token || ''
}

export function connectionBlocked(form: ConnectionForm, challenge: string) {
  return form.mode === 'password' && !!challenge && challenge !== 'captcha' && challenge !== 'totp'
}
