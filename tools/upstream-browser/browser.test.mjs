import assert from 'node:assert/strict';
import fs from 'node:fs/promises';
import http from 'node:http';
import os from 'node:os';
import path from 'node:path';
import test, { mock } from 'node:test';
import { BrowserRuntime } from './runtime.mjs';

const executable = process.env.GOVERNANCE_BROWSER_TEST_EXECUTABLE;
const fixtureOrigin = 'https://browser-fixture.example';
const loginHTML = `<!doctype html><html><head><meta charset="utf-8"><title>Login fixture</title></head>
<body style="font:18px sans-serif;background:#fff;color:#111"><form style="margin:80px;width:360px;display:grid;gap:16px">
<label>Email<input name="email" type="email" style="display:block;width:330px;height:32px"></label>
<label>Password<input name="password" type="password" style="display:block;width:330px;height:32px"></label>
<button style="height:40px" type="submit">Login</button></form>
<iframe title="External frame" src="https://other-fixture.example/frame"></iframe>
<script>document.querySelector('form').onsubmit=async(event)=>{event.preventDefault();
await fetch('/api/v1/auth/login',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({
email:document.querySelector('[name=email]').value,password:document.querySelector('[name=password]').value})});
history.replaceState({},'', '/dashboard');
document.body.textContent='Authenticated fixture dashboard: must never be returned as a screenshot';};</script></body></html>`;

test('real sandboxed Chromium exercises frame recovery and delayed redirected capture using synthetic fixtures',
  { skip: !executable, timeout: 45000 }, async (t) => {
    const started = Date.now();
    const progress = (stage) => process.stdout.write(`# browser-fixture ${Date.now() - started}ms ${stage}\n`);
    const bounded = async (pending, milliseconds, stage) => {
      let timer;
      try {
        return await Promise.race([pending, new Promise((_resolve, reject) => {
          timer = setTimeout(() => reject(new Error(`fixture ${stage} timed out`)), milliseconds);
        })]);
      } finally { clearTimeout(timer); }
    };
    progress('fixture:start');
    const { chromium } = await import('playwright-core');
    const root = path.resolve(process.env.GOVERNANCE_BROWSER_TEST_TEMP_DIR || os.tmpdir());
    await fs.mkdir(root, { recursive: true });
    const profile = await fs.mkdtemp(path.join(root, 'browser-fixture-'));
    const artifacts = process.env.GOVERNANCE_BROWSER_TEST_ARTIFACT_DIR;
    if (artifacts) {
      await fs.mkdir(artifacts, { recursive: true });
      await fs.writeFile(path.join(artifacts, 'upstream-browser-fixture-run.json'), JSON.stringify({
        runner_pid: process.pid, profile, headless: process.env.GOVERNANCE_BROWSER_HEADLESS === 'true'
      }) + '\n');
    }
    const guard = http.createServer((_req, response) => response.writeHead(502).end());
    const sockets = new Set();
    const guardErrors = [];
    const socketError = (error) => {
      progress(`guard:socket-error:${error.code || 'unknown'}`);
      if (!['ECONNRESET', 'EPIPE'].includes(error.code)) guardErrors.push(error);
    };
    guard.on('connection', (socket) => {
      sockets.add(socket);
      socket.on('error', socketError);
      socket.on('close', () => sockets.delete(socket));
    });
    guard.on('clientError', (error, socket) => { socketError(error); socket.destroy(); });
    guard.on('connect', (_req, socket) => socket.end('HTTP/1.1 502 Bad Gateway\r\nContent-Length: 0\r\n\r\n'));
    await new Promise((resolve) => guard.listen(0, '127.0.0.1', resolve));
    progress('guard:listening');
    const runtime = new BrowserRuntime();
    let releaseBody;
    const bodyGate = new Promise((resolve) => { releaseBody = resolve; });
    const originalInspect = runtime.inspectResponse.bind(runtime);
    const delayCapture = mock.method(runtime, 'inspectResponse', async (response) => {
      if (response.url() !== fixtureOrigin + '/api/v1/auth/login') return originalInspect(response);
      const originalBody = response.body.bind(response);
      const delayBody = mock.method(response, 'body', async () => { await bodyGate; return originalBody(); });
      try { return await originalInspect(response); }
      finally { delayBody.mock.restore(); }
    });
    const originalLaunch = chromium.launchPersistentContext.bind(chromium);
    const observed = [];
    const patch = mock.method(chromium, 'launchPersistentContext', async (directory, options) => {
      progress(`launch:start:${options.headless ? 'headless' : 'headed'}`);
      const context = await originalLaunch(directory, options);
      progress('launch:complete');
      const mark = (target, method, stage) => {
        const original = target[method].bind(target);
        mock.method(target, method, async (...args) => {
          progress(`${stage}:start`);
          try { const result = await original(...args); progress(`${stage}:complete`); return result; }
          catch (error) { progress(`${stage}:failed:${error.name || 'error'}`); throw error; }
        });
      };
      const browser = context.browser();
      const originalCDP = browser.newBrowserCDPSession.bind(browser);
      mock.method(browser, 'newBrowserCDPSession', async () => {
        progress('cdp-session:start');
        const session = await originalCDP();
        progress('cdp-session:complete');
        mark(session, 'send', 'cdp-command');
        mark(session, 'detach', 'cdp-detach');
        return session;
      });
      for (const page of context.pages()) {
        mark(page, 'evaluate', 'identity');
        mark(page, 'goto', 'login-navigation');
      }
      mark(context, 'addInitScript', 'capabilities');
      mark(context, 'routeWebSocket', 'websocket-route');
      const originalRoute = context.route.bind(context);
      context.route = (pattern, handler) => originalRoute(pattern, async (route) => {
        const url = new URL(route.request().url());
        route.continue = async () => {
          observed.push(url.origin + url.pathname);
          if (url.origin === fixtureOrigin && url.pathname === '/login') progress('login-route:fulfill');
          if (url.origin === fixtureOrigin && url.pathname === '/login')
            return route.fulfill({ status: 200, contentType: 'text/html', body: loginHTML });
          if (url.origin === fixtureOrigin && url.pathname === '/api/v1/auth/login')
            return route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({ code: 0,
              data: { access_token: 'synthetic-access-token', refresh_token: 'synthetic-refresh-token', expires_in: 120, user: { id: 42 } } }) });
          if (url.origin === 'https://other-fixture.example' && url.pathname === '/frame')
            return route.fulfill({ status: 200, contentType: 'text/html', body: '<p>External content fixture</p>' });
          return route.abort('blockedbyclient');
        };
        return handler(route);
      });
      return context;
    });
    t.after(async () => {
      releaseBody();
      delayCapture.mock.restore();
      patch.mock.restore();
      progress('cleanup:start');
      try { await bounded(runtime.close(), 5000, 'browser close'); }
      finally {
        for (const socket of sockets) socket.destroy();
        guard.closeAllConnections();
        await bounded(new Promise((resolve) => guard.close(resolve)), 1000, 'guard close');
      }
      assert.equal(path.dirname(profile), root);
      await fs.rm(profile, { recursive: true, force: true, maxRetries: 10, retryDelay: 100 });
      assert.deepEqual(guardErrors, [], 'unexpected synthetic proxy socket errors');
      progress('cleanup:complete');
    });
    progress('runtime:start');
    await runtime.start({ base_url: fixtureOrigin, platform: 'sub2api', username: 'fixture@example.test', password: 'synthetic-password',
      executable_path: executable, profile_dir: profile, proxy_server: `http://127.0.0.1:${guard.address().port}`,
      proxy_username: 'fixture', proxy_password: 'fixture-secret' });
    progress('runtime:started');
    assert.equal(await runtime.page.locator('[name=email]').inputValue(), 'fixture@example.test');
    assert.equal(await runtime.page.locator('[name=password]').inputValue(), 'synthetic-password');
    assert.equal(runtime.result(), null);
    const renderFailure = mock.method(runtime.page, 'screenshot', async () => { throw new Error('synthetic-render-failure'); });
    try {
      assert.deepEqual(await runtime.snapshot(), { status: 'waiting', width: 1024, height: 720,
        error_code: 'browser_render_failed' });
    } finally { renderFailure.mock.restore(); }
    progress('snapshot:start');
    const waiting = await runtime.snapshot();
    progress('snapshot:complete');
    assert.equal(waiting.status, 'waiting');
    assert.equal(waiting.error_code, undefined);
    assert.equal(waiting.width, 1024);
    assert.equal(waiting.height, 720);
    const pixels = Buffer.from(waiting.image, 'base64');
    assert.equal(pixels.readUInt16BE(0), 0xffd8);
    assert.ok(pixels.length > 2000);
    if (artifacts) {
      await fs.mkdir(artifacts, { recursive: true });
      await fs.writeFile(path.join(artifacts, 'upstream-browser-login-fixture.jpg'), pixels);
    }
    assert.ok(!JSON.stringify(waiting).includes('synthetic-password'));
    const button = await runtime.page.locator('button').boundingBox();
    const closed = runtime.page.waitForEvent('close');
    progress('pointer-login:start');
    await runtime.action({ type: 'pointer_down', x: button.x + button.width / 2, y: button.y + button.height / 2 });
    await runtime.action({ type: 'pointer_up', x: button.x + button.width / 2, y: button.y + button.height / 2 });
    await runtime.page.waitForURL(fixtureOrigin + '/dashboard');
    progress('pointer-login:redirected');
    assert.deepEqual(await runtime.snapshot(), { status: 'waiting', width: 1024, height: 720 });
    assert.equal(runtime.result(), null);
    releaseBody();
    await closed;
    progress('capture:ready');
    assert.equal(runtime.result().access_token, 'synthetic-access-token');
    assert.equal(runtime.result().refresh_token, 'synthetic-refresh-token');
    assert.equal(runtime.result().verified_login.password, 'synthetic-password');
    assert.equal(runtime.result().user_id, 42);
    assert.ok(runtime.result().user_agent.includes('Chrome/'));
    assert.equal((await runtime.snapshot()).status, 'ready');
    assert.equal((await runtime.snapshot()).image, undefined);
    assert.ok(observed.includes(fixtureOrigin + '/api/v1/auth/login'));
    if (artifacts) await fs.writeFile(path.join(artifacts, 'upstream-browser-fixture-report.json'), JSON.stringify({
      synthetic_fixture: true, real_upstream_contacted: false, real_credentials_used: false, sandbox_enabled: true,
      autofill_verified: true, pointer_login_verified: true, token_pair_captured: true, verified_login_from_request: true,
      transient_render_recovered: true, delayed_redirect_capture_verified: true, pending_redirect_has_no_image: true,
      ready_has_no_image: true, viewport: { width: 1024, height: 720 }
    }, null, 2) + '\n');
  });
