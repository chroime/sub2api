import assert from 'node:assert/strict';
import test from 'node:test';
import { LoginCapture, allowedRequest, validateAction, validateInput } from './contract.mjs';

const origin = 'https://upstream.example';
const now = new Date('2026-09-28T01:00:00Z');
const response = (overrides = {}) => ({
  url: `${origin}/api/v1/auth/login`, method: 'POST', frameIsMain: true,
  pageMatches: true, topLevelURL: `${origin}/login`, status: 200,
  contentType: 'application/json; charset=utf-8',
  requestBody: JSON.stringify({ email: 'admin@example.test', password: 'saved-password' }),
  body: JSON.stringify({ code: 0, data: {
    access_token: 'access-token', refresh_token: 'refresh-token', expires_in: 3600, user: { id: 7 }
  } }), cookies: {}, userAgent: 'Actual Chromium User Agent', now,
  ...overrides
});

test('capture accepts a real same-origin login and anchors expiry to response time', () => {
  const capture = new LoginCapture(origin, 'sub2api').accept(response());
  assert.equal(capture.access_token, 'access-token');
  assert.equal(capture.refresh_token, 'refresh-token');
  assert.equal(capture.issued_at, '2026-09-28T01:00:00.000Z');
  assert.equal(capture.expires_at, '2026-09-28T02:00:00.000Z');
  assert.equal(capture.user_agent, 'Actual Chromium User Agent');
  assert.deepEqual(capture.verified_login, { username: 'admin@example.test', password: 'saved-password' });
});

test('foreign origins, subframes, non-login endpoints and failures never yield tokens', () => {
  for (const overrides of [
    { url: 'https://evil.example/api/v1/auth/login' },
    { url: `${origin}/api/v1/auth/login/extra` },
    { url: `${origin}/api/v1/auth/login%2f` },
    { url: `${origin}/api/v1/auth/refresh` },
    { frameIsMain: false }, { pageMatches: false },
    { topLevelURL: 'https://evil.example/login' }, { method: 'GET' },
    { status: 401 }, { contentType: 'text/html' },
    { body: JSON.stringify({ code: 1, data: { access_token: 'bad' } }) },
    { body: '{broken' }, { body: ' '.repeat(128 * 1024 + 1) }
  ]) assert.equal(new LoginCapture(origin, 'sub2api').accept(response(overrides)), null);
});

test('bearer-only login stays usable but does not claim refresh support', () => {
  for (const extra of [{}, { refresh_token: 'refresh', expires_in: -3 }, { refresh_token: 'refresh', expires_in: '3600' }]) {
    const result = new LoginCapture(origin, 'sub2api').accept(response({
      body: JSON.stringify({ code: 0, data: { access_token: 'access', ...extra } })
    }));
    assert.equal(result.access_token, 'access');
    assert.equal(result.refresh_token, undefined);
    assert.equal(result.expires_at, undefined);
  }
});

test('bearer-only login retains an upstream-declared expiry even without a refresh token', () => {
  const result = new LoginCapture(origin, 'sub2api').accept(response({
    body: JSON.stringify({ code: 0, data: { access_token: 'access', expires_in: 3600 } })
  }));
  assert.equal(result.refresh_token, undefined);
  assert.equal(result.expires_in, 3600);
  assert.equal(result.expires_at, '2026-09-28T02:00:00.000Z');
});

test('encrypted login never certifies the saved plaintext password', () => {
  const result = new LoginCapture(origin, 'sub2api').accept(response({
    requestBody: JSON.stringify({ email: 'admin@example.test', password_encrypted: 'ciphertext' })
  }));
  assert.equal(result.verified_login, undefined);
});

test('two-factor completion carries only the password bound to its exact challenge', () => {
  const parser = new LoginCapture(origin, 'sub2api');
  assert.equal(parser.accept(response({ body: JSON.stringify({ code: 0, data: { requires_2fa: true, temp_token: 'challenge-A' } }) })), null);
  const second = { url: `${origin}/api/v1/auth/login/2fa`, requestBody: JSON.stringify({ temp_token: 'challenge-B', totp_code: '123456' }) };
  assert.equal(parser.accept(response(second)), null);
  const result = parser.accept(response({ ...second, requestBody: JSON.stringify({ temp_token: 'challenge-A', totp_code: '123456' }) }));
  assert.equal(result.verified_login.password, 'saved-password');
});

test('New API legacy requires successful login, positive user id and returned session cookie', () => {
  const entry = response({ url: `${origin}/api/user/login`, requestBody: JSON.stringify({ username: 'admin', password: 'password' }),
    body: JSON.stringify({ success: true, data: { id: 9 } }), cookies: { session: 'session-cookie', cf_clearance: 'clearance', forbidden: 'secret' } });
  const result = new LoginCapture(origin, 'newapi').accept(entry);
  assert.equal(result.auth_variant, 'newapi_legacy_cookie');
  assert.deepEqual(result.cookies, { session: 'session-cookie', cf_clearance: 'clearance' });
  assert.equal(new LoginCapture(origin, 'newapi').accept({ ...entry, cookies: {} }), null);
  assert.equal(new LoginCapture(origin, 'newapi').accept({ ...entry, body: '{"success":true,"data":{"id":0}}' }), null);
  assert.equal(new LoginCapture(origin, 'newapi').accept({ ...entry, body: '{"success":true,"data":{"id":9,"access_token":""}}' }).auth_variant, 'newapi_legacy_cookie');
});

test('token lengths and types are bounded and challenge responses are not successes', () => {
  for (const data of [ { access_token: 7 }, { access_token: 'a'.repeat(16385) }, { access_token: 'bad\nvalue' },
    { access_token: 'a', requires_2fa: true }, { access_token: 'a', require_verification: true } ]) {
    assert.equal(new LoginCapture(origin, 'sub2api').accept(response({ body: JSON.stringify({ code: 0, data }) })), null);
  }
});

test('main-page navigation is origin bound while external challenge resources remain possible', () => {
  const request = { url: `${origin}/login`, pageMatches: true, isNavigation: true, isMainFrame: true };
  assert.equal(allowedRequest(request, origin), true);
  assert.equal(allowedRequest({ ...request, url: 'https://captcha.example/frame' }, origin), false);
  assert.equal(allowedRequest({ ...request, url: 'https://captcha.example/frame', isMainFrame: false }, origin), true);
  for (const url of ['file:///etc/passwd', 'data:text/html,hello', 'javascript:alert(1)', 'ftp://example.test/a'])
    assert.equal(allowedRequest({ ...request, url }, origin), false);
  assert.equal(allowedRequest({ ...request, pageMatches: false }, origin), false);
});

test('only bounded input actions are accepted', () => {
  for (const action of [ { type: 'pointer_down', x: 0, y: 0 }, { type: 'pointer_up', x: 1023, y: 719 },
    { type: 'pointer_move', x: 30.5, y: 11 }, { type: 'wheel', delta_x: 0, delta_y: 500 },
    { type: 'text', text: '123456' }, { type: 'key', key: 'Control+A' } ]) assert.equal(validateAction(action), true);
  for (const action of [ { type: 'eval', text: 'process.exit()' }, { type: 'navigate', text: origin },
    { type: 'pointer_down', x: -1, y: 0 }, { type: 'pointer_move', x: 1024, y: 0 },
    { type: 'pointer_move', x: Infinity, y: 0 }, { type: 'text', text: 'a'.repeat(2049) },
    { type: 'text', text: 'a\n' }, { type: 'key', key: 'Alt+F4' },
    { type: 'wheel', delta_y: 100000 }, { type: 'text', text: 'ok', code: 'anything' } ]) assert.equal(validateAction(action), false);
});

test('configuration never accepts non-HTTPS or path-bearing upstream origins', () => {
  const good = { base_url: origin, platform: 'sub2api', username: '', password: '' };
  assert.equal(validateInput(good), true);
  for (const base_url of ['http://upstream.example', `${origin}/path`, `${origin}?query`, 'https://user:pass@upstream.example', 'https://upstream.example:8443', 'file:///tmp/a'])
    assert.equal(validateInput({ ...good, base_url }), false);
  assert.equal(validateInput({ ...good, platform: 'unknown' }), false);
});
