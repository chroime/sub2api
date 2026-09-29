import assert from 'node:assert/strict';
import { spawn } from 'node:child_process';
import path from 'node:path';
import test from 'node:test';
import { BrowserError, BrowserRuntime, launchOptions, validateStart, cookieCandidates } from './runtime.mjs';
import { LoginCapture } from './contract.mjs';

const input = {
  base_url: 'https://upstream.example', platform: 'sub2api', username: '', password: '',
  executable_path: process.execPath, profile_dir: path.resolve('temporary-profile'),
  proxy_server: 'http://127.0.0.1:3128', proxy_username: 'job', proxy_password: 'random-secret'
};

test('launch config keeps the sandbox and guarded proxy and never overrides browser identity', () => {
  const previous = process.env.GOVERNANCE_BROWSER_HEADLESS;
  delete process.env.GOVERNANCE_BROWSER_HEADLESS;
  try {
    assert.equal(validateStart(input), true);
    const options = launchOptions(input);
    assert.equal(options.headless, false, 'interactive authorization uses a headed browser by default');
    assert.equal(options.chromiumSandbox, true);
    assert.equal(options.serviceWorkers, 'block');
    assert.equal(options.acceptDownloads, false);
    assert.equal(options.ignoreHTTPSErrors, false);
    assert.equal(options.userAgent, undefined);
    assert.equal(options.proxy.server, input.proxy_server);
    assert.equal(options.proxy.username, 'job');
    assert.equal(options.proxy.bypass, '<-loopback>');
    assert.ok(options.args.includes('--proxy-bypass-list=<-loopback>'));
    assert.ok(options.args.includes('--disable-quic'));
    assert.ok(options.args.includes('--force-webrtc-ip-handling-policy=disable_non_proxied_udp'));
    assert.ok(!options.args.some((arg) => arg.includes('--no-sandbox') || arg.includes('--disable-web-security')));
    process.env.GOVERNANCE_BROWSER_HEADLESS = 'true';
    assert.equal(launchOptions(input).headless, true, 'synthetic tests may opt in to headless mode');
  } finally {
    if (previous === undefined) delete process.env.GOVERNANCE_BROWSER_HEADLESS;
    else process.env.GOVERNANCE_BROWSER_HEADLESS = previous;
  }
});

test('runtime only accepts a local authenticated guard, explicit browser and absolute profile', () => {
  for (const override of [ { proxy_server: 'http://evil.example:3128' }, { proxy_server: 'socks5://127.0.0.1:3128' },
    { proxy_server: 'http://user:pass@127.0.0.1:3128' }, { proxy_password: '' },
    { executable_path: 'chromium' }, { profile_dir: 'relative' }, { arbitrary: true } ])
    assert.equal(validateStart({ ...input, ...override }), false);
});

test('cookies never cross host boundaries and legacy session must match observed login header', () => {
  const cookies = [
    { name: 'session', value: 'session-A', domain: 'upstream.example', path: '/' },
    { name: 'cf_clearance', value: 'clearance', domain: '.upstream.example', path: '/' },
    { name: 'private_token', value: 'secret', domain: 'upstream.example', path: '/' },
    { name: 'session', value: 'foreign', domain: '.example', path: '/' }
  ];
  assert.deepEqual(cookieCandidates(cookies, 'upstream.example', 'session-A'), { session: 'session-A', cf_clearance: 'clearance' });
  assert.deepEqual(cookieCandidates(cookies, 'upstream.example', ''), { cf_clearance: 'clearance' });
  assert.deepEqual(cookieCandidates([{ name: 'cf_clearance', value: 'foreign', domain: 'captcha.example', path: '/' }], 'upstream.example', ''), {});
});

function runtimeFixture() {
  const state = { url: `${input.base_url}/login`, closed: false, screenshots: 0, inputs: 0 };
  const runtime = new BrowserRuntime();
  const frame = { page: () => runtime.page };
  runtime.origin = input.base_url;
  runtime.platform = input.platform;
  runtime.parser = new LoginCapture(runtime.origin, runtime.platform);
  runtime.userAgent = 'Synthetic Chromium User Agent';
  runtime.status = 'waiting';
  runtime.context = { cookies: async () => [], close: async () => {} };
  runtime.page = {
    url: () => state.url,
    isClosed: () => state.closed,
    mainFrame: () => frame,
    screenshot: async () => { state.screenshots++; return Buffer.from('synthetic-image'); },
    close: async () => { state.closed = true; },
    keyboard: { insertText: async () => { state.inputs++; } }
  };
  return { runtime, state };
}

function loginResponse(runtime, body) {
  return {
    request: () => ({ frame: () => runtime.page.mainFrame(), method: () => 'POST',
      postData: () => JSON.stringify({ email: 'fixture@example.test', password: 'synthetic-password' }) }),
    url: () => `${input.base_url}/api/v1/auth/login`,
    status: () => 200,
    allHeaders: async () => ({ 'content-type': 'application/json' }),
    headersArray: async () => [],
    body: async () => Buffer.from(await body)
  };
}

test('snapshots remain available on the exact login paths', async () => {
  for (const suffix of ['/login', '/login/', '/login?next=%2Fdashboard#challenge']) {
    const { runtime, state } = runtimeFixture();
    state.url = input.base_url + suffix;
    const view = await runtime.snapshot();
    assert.equal(view.status, 'waiting');
    assert.equal(view.error_code, undefined);
    assert.equal(Buffer.from(view.image, 'base64').toString(), 'synthetic-image');
  }
});

test('leaving the supported login route fails with a bounded code instead of a blank waiting frame', async () => {
  for (const url of [`${input.base_url}/dashboard`, `${input.base_url}/login/2fa`,
    `${input.base_url}/login-help`, 'https://other-fixture.example/login', 'about:blank']) {
    const { runtime, state } = runtimeFixture();
    state.url = url;
    assert.deepEqual(await runtime.snapshot(), { status: 'failed', width: 1024, height: 720,
      error_code: 'browser_unsupported_route' });
    assert.equal(state.screenshots, 0, 'unsupported pages must not be rendered');
    assert.equal(runtime.result(), null);
  }
});

test('an existing terminal navigation failure retains its diagnostic instead of becoming an unsupported route', async () => {
  for (const url of ['about:blank', 'chrome-error://chromewebdata/', `${input.base_url}/login`]) {
    const { runtime, state } = runtimeFixture();
    runtime.status = 'failed';
    runtime.errorCode = 'browser_navigation_failed';
    state.url = url;
    assert.deepEqual(await runtime.snapshot(), { status: 'failed', width: 1024, height: 720,
      error_code: 'browser_navigation_failed' });
    await assert.rejects(runtime.action({ type: 'text', text: 'synthetic-input' }),
      (error) => error instanceof BrowserError && error.code === 'browser_navigation_failed');
    assert.equal(state.screenshots, 0);
    assert.equal(state.inputs, 0);
  }
});

test('navigation during rendering discards pixels and reports the unsupported route', async () => {
  const { runtime, state } = runtimeFixture();
  runtime.page.screenshot = async () => {
    state.url = `${input.base_url}/dashboard`;
    return Buffer.from('authenticated-page-must-not-leak');
  };
  assert.deepEqual(await runtime.snapshot(), { status: 'failed', width: 1024, height: 720,
    error_code: 'browser_unsupported_route' });
});

test('render rejection after navigation reports the route error without renderer details', async () => {
  const { runtime, state } = runtimeFixture();
  runtime.page.screenshot = async () => {
    state.url = `${input.base_url}/dashboard`;
    throw new Error('private-renderer-diagnostic');
  };
  assert.deepEqual(await runtime.snapshot(), { status: 'failed', width: 1024, height: 720,
    error_code: 'browser_unsupported_route' });
});

test('actions on an unsupported route fail without forwarding user input', async () => {
  const { runtime, state } = runtimeFixture();
  state.url = `${input.base_url}/dashboard`;
  await assert.rejects(runtime.action({ type: 'text', text: 'synthetic-input' }),
    (error) => error instanceof BrowserError && error.code === 'browser_unsupported_route');
  assert.equal(state.inputs, 0);
  assert.equal((await runtime.snapshot()).error_code, 'browser_unsupported_route');
});

test('a transient render failure clears on the next successful login-page snapshot', async () => {
  const { runtime } = runtimeFixture();
  const screenshot = runtime.page.screenshot;
  runtime.page.screenshot = async () => { throw new Error('private-renderer-diagnostic'); };
  assert.deepEqual(await runtime.snapshot(), { status: 'waiting', width: 1024, height: 720,
    error_code: 'browser_render_failed' });
  runtime.page.screenshot = screenshot;
  const recovered = await runtime.snapshot();
  assert.equal(recovered.status, 'waiting');
  assert.equal(recovered.error_code, undefined);
  assert.equal(Buffer.from(recovered.image, 'base64').toString(), 'synthetic-image');
});

test('an observed login response may finish after a same-origin redirect without route failure', async () => {
  const { runtime, state } = runtimeFixture();
  let resolveBody;
  const body = new Promise((resolve) => { resolveBody = resolve; });
  const inspection = runtime.inspectResponse(loginResponse(runtime, body));
  state.url = `${input.base_url}/dashboard`;
  assert.deepEqual(await runtime.snapshot(), { status: 'waiting', width: 1024, height: 720 });
  await runtime.action({ type: 'text', text: 'ignored-during-capture' });
  assert.equal(state.inputs, 0);
  resolveBody(JSON.stringify({ code: 0, data: { access_token: 'synthetic-access-token',
    refresh_token: 'synthetic-refresh-token', expires_in: 120, user: { id: 42 } } }));
  await inspection;
  assert.equal(runtime.result().access_token, 'synthetic-access-token');
  assert.equal(runtime.result().verified_login.password, 'synthetic-password');
  assert.deepEqual(await runtime.snapshot(), { status: 'ready', width: 1024, height: 720 });
  assert.equal(state.screenshots, 0);
});

test('an unsuccessful delayed response exposes the unsupported route after inspection finishes', async () => {
  const { runtime, state } = runtimeFixture();
  let resolveBody;
  const body = new Promise((resolve) => { resolveBody = resolve; });
  const inspection = runtime.inspectResponse(loginResponse(runtime, body));
  state.url = `${input.base_url}/dashboard`;
  assert.deepEqual(await runtime.snapshot(), { status: 'waiting', width: 1024, height: 720 });
  resolveBody(JSON.stringify({ code: 1, data: {} }));
  await inspection;
  assert.equal(runtime.result(), null);
  assert.deepEqual(await runtime.snapshot(), { status: 'failed', width: 1024, height: 720,
    error_code: 'browser_unsupported_route' });
});

test('render rejection during capture does not replace the pending login state with a render error', async () => {
  const { runtime, state } = runtimeFixture();
  let resolveBody;
  const body = new Promise((resolve) => { resolveBody = resolve; });
  let inspection;
  runtime.page.screenshot = async () => {
    inspection = runtime.inspectResponse(loginResponse(runtime, body));
    state.url = `${input.base_url}/dashboard`;
    throw new Error('navigation-interrupted-render');
  };
  assert.deepEqual(await runtime.snapshot(), { status: 'waiting', width: 1024, height: 720 });
  resolveBody(JSON.stringify({ code: 0, data: { access_token: 'synthetic-access-token' } }));
  await inspection;
  assert.deepEqual(await runtime.snapshot(), { status: 'ready', width: 1024, height: 720 });
});

async function runHelper(content) {
  const child = spawn(process.execPath, ['main.mjs'], { cwd: import.meta.dirname, stdio: ['pipe', 'pipe', 'pipe'] });
  const timer = setTimeout(() => child.kill(), 5000);
  const stdout = [];
  const stderr = [];
  child.stdout.on('data', (chunk) => stdout.push(chunk));
  child.stderr.on('data', (chunk) => stderr.push(chunk));
  child.stdin.end(content);
  await new Promise((resolve) => child.on('close', resolve));
  clearTimeout(timer);
  return { out: Buffer.concat(stdout).toString(), err: Buffer.concat(stderr).toString() };
}

test('RPC rejects arbitrary commands and never reflects input secrets', async () => {
  const result = await runHelper(JSON.stringify({ id: 1, method: 'evaluate', params: { secret: 'password-that-must-not-leak' } }) + '\n');
  assert.equal(result.err, '');
  assert.ok(!result.out.includes('password-that-must-not-leak'));
  assert.equal(JSON.parse(result.out).error, 'browser_protocol_error');
});

test('oversized, malformed and missing-newline input closes the helper', async () => {
  for (const content of ['x'.repeat(32 * 1024 + 1), '{malformed}\n', '{"id":1,"method":"snapshot"}']) {
    const result = await runHelper(content);
    assert.equal(result.err, '');
    assert.ok(result.out.length < 300);
  }
});
