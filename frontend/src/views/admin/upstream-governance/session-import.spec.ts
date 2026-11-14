import { describe, expect, it } from 'vitest'
import { parseSessionImport, SessionImportError } from './session-import'

const now = Date.UTC(2026, 10, 8, 3, 0)
const jwt = (payload: unknown) => `eyJhbGciOiJIUzI1NiJ9.${btoa(JSON.stringify(payload)).replace(/=/g, '').replace(/\+/g, '-').replace(/\//g, '_')}.fixture-signature`
const response = (data: Record<string, unknown>) => JSON.stringify({ code: 0, message: 'success', data })
function rejects(raw: string, code: string) {
  try {
    parseSessionImport(raw, now)
    expect.fail('Expected a rejected session import')
  } catch (error) {
    expect(error).toBeInstanceOf(SessionImportError)
    expect(error).toMatchObject({ code, reason: `session_import_${code}` })
    expect(String(error)).not.toContain('fixture-secret')
  }
}

describe('one-time login response import', () => {
  it('imports a NewAPI success envelope and keeps the access deadline separate from the login session', () => {
    expect(parseSessionImport(JSON.stringify({ success: true, message: '', data: {
      access_token: 'fixture-newapi-access', access_expires_at: now / 1000 + 900, token_type: 'Bearer',
      user: { id: 42, username: 'ignored-user', quota: 5000000 },
      session: { sid: 'fixture-session', user_agent: 'Fixture Browser/2', expires_at: now / 1000 + 30 * 86400 },
    } }), now)).toEqual({
      sessionToken: 'fixture-newapi-access', refreshToken: '', userAgent: 'Fixture Browser/2',
      userId: 42, sessionId: 'fixture-session', expiresIn: 900, sessionExpiresAt: now + 900_000,
    })
  })

  it('caps a NewAPI access deadline using an earlier JWT expiry', () => {
    const result = parseSessionImport(JSON.stringify({ success: true, data: {
      access_token: jwt({ exp: now / 1000 + 60 }), access_expires_at: now / 1000 + 900,
    } }), now)
    expect(result.expiresIn).toBe(60)
  })

  it('does not use a long NewAPI session expiry as the access-token lifetime', () => {
    const result = parseSessionImport(JSON.stringify({ success: true, data: {
      access_token: 'opaque-access', session: { sid: 'fixture-session', expires_at: now / 1000 + 30 * 86400 },
    } }), now)
    expect(result.expiresIn).toBeUndefined()
    expect(result.sessionExpiresAt).toBeUndefined()
  })

  it.each([
    { success: 'true', data: { access_token: 'fixture-secret' } },
    { success: true, data: null },
    { success: true, data: { require_verification: true, access_token: 'fixture-secret' } },
    { success: true, data: { require_2fa: true, access_token: 'fixture-secret' } },
  ])('rejects malformed or incomplete NewAPI success responses', raw => rejects(JSON.stringify(raw), 'unsuccessful_response'))

  it.each([null, '900', 0, now / 1000 - 1])('validates NewAPI access_expires_at %s', access_expires_at => {
    rejects(JSON.stringify({ success: true, data: { access_token: 'fixture-access', access_expires_at } }),
      access_expires_at === now / 1000 - 1 ? 'expired' : 'invalid_expiry')
  })

  it.each([
    { user: { id: '42' } }, { user: { id: -1 } },
    { session: { sid: 'fixture\r\nsecret' } }, { session: { sid: 42 } },
  ])('rejects invalid imported NewAPI identity metadata', metadata => {
    rejects(JSON.stringify({ success: true, data: { access_token: 'fixture-access', ...metadata } }), 'invalid_identity')
  })

  it('validates a NewAPI nested session User-Agent', () => {
    rejects(JSON.stringify({ success: true, data: {
      access_token: 'fixture-access', session: { sid: 'fixture-session', user_agent: 'fixture-secret\n' },
    } }), 'invalid_user_agent')
  })

  it('extracts only session fields from a successful Sub2API login response', () => {
    expect(parseSessionImport(response({
      access_token: 'fixture-access', refresh_token: 'fixture-refresh', expires_in: 3600,
      user_agent: 'Fixture Browser/1.0', user: { id: 42, role: 'admin' },
      username: 'ignored', password: 'fixture-secret', verified_login: true,
    }), now)).toEqual({
      sessionToken: 'fixture-access', refreshToken: 'fixture-refresh', userAgent: 'Fixture Browser/1.0',
      expiresIn: 3600, sessionExpiresAt: now + 3600_000,
    })
  })

  it('accepts a direct token object without inventing refresh or expiry metadata', () => {
    expect(parseSessionImport(JSON.stringify({ access_token: 'fixture-access' }), now)).toEqual({
      sessionToken: 'fixture-access', refreshToken: '', userAgent: '',
    })
  })

  it('uses JWT exp only to cap the imported remaining lifetime', () => {
    const token = jwt({ exp: now / 1000 + 120 })
    const result = parseSessionImport(response({ access_token: token, refresh_token: 'fixture-refresh', expires_in: 3600 }), now)
    expect(result.expiresIn).toBe(120)
    expect(result.sessionExpiresAt).toBe(now + 120_000)
    expect(result.sessionToken).toBe(token)
  })

  it('can derive known expiry from JWT exp when the response omits expires_in', () => {
    const result = parseSessionImport(response({ access_token: jwt({ exp: now / 1000 + 100 }), refresh_token: 'fixture-refresh' }), now)
    expect(result.expiresIn).toBe(100)
  })

  it('does not extend a shorter response lifetime to match a later JWT exp', () => {
    const result = parseSessionImport(response({ access_token: jwt({ exp: now / 1000 + 3600 }), expires_in: 60 }), now)
    expect(result.expiresIn).toBe(60)
    expect(result.sessionExpiresAt).toBe(now + 60_000)
  })

  it.each([now / 1000 + 90, '2026-11-08T03:01:30Z'])('caps expiry using explicit expires_at %s', expires_at => {
    expect(parseSessionImport(response({ access_token: 'fixture-access', expires_in: 3600, expires_at }), now).expiresIn).toBe(90)
  })

  it('keeps absolute expiry rather than resetting the lifetime when copied again later', () => {
    const raw = response({ access_token: jwt({ exp: now / 1000 + 100 }), expires_in: 3600 })
    expect(parseSessionImport(raw, now + 70_000).expiresIn).toBe(30)
    expect(parseSessionImport(raw, now + 70_000).sessionExpiresAt).toBe(now + 100_000)
  })

  it.each(['', 'fixture-secret', 'null', '[]', '"fixture-secret"', '{"access_token":'])('rejects invalid JSON or non-object %s', raw => {
    rejects(raw, 'invalid_json')
  })

  it('rejects oversized JSON before parsing it', () => {
    rejects(' '.repeat(128 * 1024 + 1), 'too_large')
  })

  it.each([
    { code: 401, data: { access_token: 'fixture-secret' } },
    { code: '0', data: { access_token: 'fixture-secret' } },
    { success: false, access_token: 'fixture-secret' },
    { code: 0, data: { requires_2fa: true, access_token: 'fixture-secret' } },
    { code: 0, data: { challenge_token: 'fixture-secret' } },
  ])('rejects unsuccessful or incomplete login responses', raw => rejects(JSON.stringify(raw), 'unsuccessful_response'))

  it.each([{}, { data: { access_token: 'fixture-secret' } }, { access_token: '' }, { token: 'fixture-secret' }])('requires an explicit access_token', raw => {
    rejects(JSON.stringify(raw), 'access_token_required')
  })

  it.each(['sk-fixture-secret', 'SK-fixture-secret'])('rejects an inference API key as the dashboard session', access_token => {
    rejects(response({ access_token }), 'api_key_not_session')
  })

  it.each([123, 'fixture-secret\r\nheader', 'fixture-secret with spaces', 'Bearer fixture-secret', '令牌', 'a'.repeat(16385)])('rejects invalid access token values', access_token => {
    rejects(response({ access_token }), 'invalid_token')
  })

  it.each([123, 'fixture-secret\n', 'r'.repeat(16385)])('validates the optional refresh token', refresh_token => {
    rejects(response({ access_token: 'fixture-access', refresh_token, expires_in: 3600 }), 'invalid_token')
  })

  it.each([0, -1, 0.5, '3600', 366 * 24 * 60 * 60 + 1, null])('rejects invalid expires_in %s', expires_in => {
    rejects(response({ access_token: 'fixture-access', expires_in }), 'invalid_expiry')
  })

  it.each([null, true, 'yesterday', '2026-11-08', '2026-11-08T03:01:30', 1794106890000])('rejects ambiguous or invalid expires_at %s', expires_at => {
    rejects(response({ access_token: 'fixture-access', expires_at }), 'invalid_expiry')
  })

  it('requires known expiry before accepting a refresh token', () => {
    rejects(response({ access_token: 'fixture-access', refresh_token: 'fixture-secret' }), 'refresh_expiry_required')
  })

  it.each([now / 1000, now / 1000 - 1])('rejects expired JWT metadata', exp => {
    rejects(response({ access_token: jwt({ exp }), expires_in: 3600 }), 'expired')
  })

  it('rejects expired absolute metadata', () => rejects(response({ access_token: 'fixture-access', expires_at: now / 1000 - 60 }), 'expired'))

  it.each(['3600', null, -1])('rejects invalid JWT exp claim %s', exp => {
    rejects(response({ access_token: jwt({ exp }) }), 'invalid_expiry')
  })

  it('allows opaque tokens and JWT payloads with no exp without inventing an expiry', () => {
    expect(parseSessionImport(response({ access_token: 'opaque.fixture.token' }), now).expiresIn).toBeUndefined()
    expect(parseSessionImport(response({ access_token: jwt({ sub: 'fixture' }) }), now).expiresIn).toBeUndefined()
  })

  it.each([12, 'fixture-secret\n', 'a'.repeat(1025), '测试浏览器'])('validates optional User-Agent metadata', user_agent => {
    rejects(response({ access_token: 'fixture-access', user_agent }), 'invalid_user_agent')
  })
})
