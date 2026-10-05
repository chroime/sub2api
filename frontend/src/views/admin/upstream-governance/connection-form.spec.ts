import { describe, expect, it } from 'vitest'
import { clearConnectionSecrets, connectionBlocked, connectionInput, continueChallenge, newConnectionForm } from './connection-form'

describe('typed authorization inputs', () => {
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

  it('clears all single-use provider proofs while retaining CAPTCHA credentials', () => {
    const form = newConnectionForm()
    Object.assign(form, { password: 'fixture-password', captchaToken: 'proof', tencentTicket: 'ticket', tencentRandstr: 'random', otp: '123456' })
    continueChallenge(form, { kind: 'captcha', provider: 'tencent' })
    expect(form.password).toBe('fixture-password')
    expect([form.captchaToken, form.tencentTicket, form.tencentRandstr, form.otp]).toEqual(['', '', '', ''])
  })
})
