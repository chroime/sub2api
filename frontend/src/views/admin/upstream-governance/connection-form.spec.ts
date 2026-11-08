import { describe, expect, it, vi } from 'vitest'
import { clearConnectionSecrets, connectionBlocked, connectionInput, continueChallenge, newConnectionForm } from './connection-form'

describe('typed authorization inputs', () => {
  it.each([undefined, 'turnstile', 'tencent'] as const)('blocks manual CAPTCHA continuation without the %s single-use proof', provider => {
    const form = newConnectionForm()
    continueChallenge(form, { kind: 'captcha', provider })
    expect(connectionBlocked(form, 'captcha')).toBe(true)
    if (provider === 'tencent') {
      form.tencentTicket = 'ticket'
      expect(connectionBlocked(form, 'captcha')).toBe(true)
      form.tencentRandstr = 'randstr'
    } else form.captchaToken = 'proof'
    expect(connectionBlocked(form, 'captcha')).toBe(false)
    form.mode = 'session'
    expect(connectionBlocked(form, 'captcha')).toBe(false)
  })
  it('uses only the detected Turnstile proof field', () => {
    const form = newConnectionForm()
    form.password = 'fixture-password'
    continueChallenge(form, { kind: 'captcha', provider: 'turnstile' })
    form.captchaToken = 'fixture-proof'
    expect(connectionInput(form)).toMatchObject({ password: 'fixture-password', turnstile_token: 'fixture-proof' })
    expect(connectionInput(form)).not.toHaveProperty('captcha_token')
    expect(connectionInput(form)).not.toHaveProperty('tencent_captcha_ticket')
  })

  it('uses both Tencent proof values without a Turnstile token', () => {
    const form = newConnectionForm()
    continueChallenge(form, { kind: 'captcha', provider: 'tencent' })
    form.tencentTicket = 'fixture-ticket'
    form.tencentRandstr = 'fixture-random'
    form.captchaToken = 'stale-other-proof'
    expect(connectionInput(form)).toMatchObject({ tencent_captcha_ticket: 'fixture-ticket', tencent_captcha_randstr: 'fixture-random' })
    expect(connectionInput(form)).not.toHaveProperty('captcha_token')
    expect(connectionInput(form)).not.toHaveProperty('turnstile_token')
  })

  it('keeps the legacy untyped challenge compatible', () => {
    const form = newConnectionForm()
    continueChallenge(form, { kind: 'captcha' })
    form.captchaToken = 'fixture-legacy'
    expect(connectionInput(form).captcha_token).toBe('fixture-legacy')
  })

  it.each(['aliyun', 'unknown'] as const)('requires browser or session authorization for %s', provider => {
    const form = newConnectionForm()
    continueChallenge(form, { kind: 'captcha', provider })
    form.captchaToken = 'must-not-submit'
    expect(connectionBlocked(form, 'captcha')).toBe(true)
    expect(connectionInput(form)).not.toHaveProperty('captcha_token')
    expect(connectionInput(form)).not.toHaveProperty('turnstile_token')
    form.mode = 'session'
    expect(connectionBlocked(form, 'captcha')).toBe(false)
  })

  it('imports only supplied session metadata without inventing an expiry', () => {
    const form = newConnectionForm()
    form.mode = 'session'
    form.sessionToken = 'fixture-access'
    expect(connectionInput(form)).toEqual({ session_token: 'fixture-access', user_id: undefined })
    form.refreshToken = 'fixture-refresh'
    form.expiresIn = 3600
    form.userAgent = 'fixture-browser'
    expect(connectionInput(form)).toEqual({ session_token: 'fixture-access', user_id: undefined, refresh_token: 'fixture-refresh', expires_in: 3600, user_agent: 'fixture-browser' })
    clearConnectionSecrets(form)
    expect(form.refreshToken).toBe('')
    expect(form.expiresIn).toBeUndefined()
    expect(form.userAgent).toBe('')
  })

  it('reduces imported lifetime by time spent editing before submitting', () => {
    const now = Date.UTC(2026, 10, 8)
    vi.spyOn(Date, 'now').mockReturnValue(now + 90_100)
    try {
      const form = newConnectionForm()
      Object.assign(form, { mode: 'session', sessionToken: 'fixture-access', refreshToken: 'fixture-refresh', expiresIn: 120, sessionExpiresAt: now + 120_000 })
      expect(connectionInput(form).expires_in).toBe(29)
      clearConnectionSecrets(form)
      expect(form.sessionExpiresAt).toBeUndefined()
    } finally { vi.restoreAllMocks() }
  })

  it('never extends manually shortened lifetime using imported absolute metadata', () => {
    vi.spyOn(Date, 'now').mockReturnValue(1_000_000)
    try {
      const form = newConnectionForm()
      Object.assign(form, { mode: 'session', sessionToken: 'fixture-access', expiresIn: 20, sessionExpiresAt: 1_060_000 })
      expect(connectionInput(form).expires_in).toBe(20)
    } finally { vi.restoreAllMocks() }
  })

  it.each([999_999, 1_000_000, 1_000_500])('rejects an imported session that has expired while editing (%s)', sessionExpiresAt => {
    vi.spyOn(Date, 'now').mockReturnValue(1_000_000)
    try {
      const form = newConnectionForm()
      Object.assign(form, { mode: 'session', sessionToken: 'fixture-access', sessionExpiresAt })
      expect(() => connectionInput(form)).toThrowError(expect.objectContaining({ reason: 'session_import_expired' }))
    } finally { vi.restoreAllMocks() }
  })

  it.each([NaN, Infinity, -Infinity])('rejects invalid imported absolute expiry (%s)', sessionExpiresAt => {
    const form = newConnectionForm()
    Object.assign(form, { mode: 'session', sessionToken: 'fixture-access', sessionExpiresAt })
    expect(() => connectionInput(form)).toThrowError(expect.objectContaining({ reason: 'session_import_invalid_expiry' }))
  })

  it('clears all single-use provider proofs while retaining CAPTCHA credentials', () => {
    const form = newConnectionForm()
    Object.assign(form, { password: 'fixture-password', captchaToken: 'proof', tencentTicket: 'ticket', tencentRandstr: 'random', otp: '123456' })
    continueChallenge(form, { kind: 'captcha', provider: 'tencent' })
    expect(form.password).toBe('fixture-password')
    expect([form.captchaToken, form.tencentTicket, form.tencentRandstr, form.otp]).toEqual(['', '', '', ''])
  })
})
