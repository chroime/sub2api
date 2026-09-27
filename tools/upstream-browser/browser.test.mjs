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
document.body.textContent='Authenticated fixture dashboard: must never be returned as a screenshot';};</script></body></html>`;

test('real sandboxed Chromium exercises autofill, JPEG, pointer input and capture using synthetic fixtures',
  { skip: !executable, timeout: 45000 }, async (t) => {
    const { chromium } = await import('playwright-core');
    const root = path.resolve(process.env.GOVERNANCE_BROWSER_TEST_TEMP_DIR || os.tmpdir());
    await fs.mkdir(root, { recursive: true });
    const profile = await fs.mkdtemp(path.join(root, 'browser-fixture-'));
    const guard = http.createServer((_req, response) => response.writeHead(502).end());
    const sockets = new Set();
    guard.on('connection', (socket) => { sockets.add(socket); socket.on('close', () => sockets.delete(socket)); });
    guard.on('connect', (_req, socket) => socket.end('HTTP/1.1 502 Bad Gateway\r\nContent-Length: 0\r\n\r\n'));
    await new Promise((resolve) => guard.listen(0, '127.0.0.1', resolve));
    const runtime = new BrowserRuntime();
    const originalLaunch = chromium.launchPersistentContext.bind(chromium);
    const observed = [];
    const patch = mock.method(chromium, 'launchPersistentContext', async (directory, options) => {
      const context = await originalLaunch(directory, options);
      const originalRoute = context.route.bind(context);
      context.route = (pattern, handler) => originalRoute(pattern, async (route) => {
        const url = new URL(route.request().url());
        route.continue = async () => {
          observed.push(url.origin + url.pathname);
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
      patch.mock.restore();
      await runtime.close();
      for (const socket of sockets) socket.destroy();
      guard.closeAllConnections();
      await new Promise((resolve) => guard.close(resolve));
      assert.equal(path.dirname(profile), root);
      await fs.rm(profile, { recursive: true, force: true, maxRetries: 10, retryDelay: 100 });
    });
    await runtime.start({ base_url: fixtureOrigin, platform: 'sub2api', username: 'fixture@example.test', password: 'synthetic-password',
      executable_path: executable, profile_dir: profile, proxy_server: `http://127.0.0.1:${guard.address().port}`,
      proxy_username: 'fixture', proxy_password: 'fixture-secret' });
    assert.equal(await runtime.page.locator('[name=email]').inputValue(), 'fixture@example.test');
    assert.equal(await runtime.page.locator('[name=password]').inputValue(), 'synthetic-password');
    assert.equal(runtime.result(), null);
    const waiting = await runtime.snapshot();
    assert.equal(waiting.status, 'waiting');
    assert.equal(waiting.width, 1024);
    assert.equal(waiting.height, 720);
    const pixels = Buffer.from(waiting.image, 'base64');
    assert.equal(pixels.readUInt16BE(0), 0xffd8);
    assert.ok(pixels.length > 2000);
    const artifacts = process.env.GOVERNANCE_BROWSER_TEST_ARTIFACT_DIR;
    if (artifacts) {
      await fs.mkdir(artifacts, { recursive: true });
      await fs.writeFile(path.join(artifacts, 'upstream-browser-login-fixture.jpg'), pixels);
    }
    assert.ok(!JSON.stringify(waiting).includes('synthetic-password'));
    const button = await runtime.page.locator('button').boundingBox();
    const closed = runtime.page.waitForEvent('close');
    await runtime.action({ type: 'pointer_down', x: button.x + button.width / 2, y: button.y + button.height / 2 });
    await runtime.action({ type: 'pointer_up', x: button.x + button.width / 2, y: button.y + button.height / 2 });
    await closed;
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
      ready_has_no_image: true, viewport: { width: 1024, height: 720 }
    }, null, 2) + '\n');
  });
