import path from 'node:path';
import { LoginCapture, WIDTH, HEIGHT, MAX_RESPONSE_BYTES, allowedRequest, validateAction, validateInput } from './contract.mjs';

const startFields = ['base_url', 'platform', 'username', 'password', 'proxy_server', 'proxy_username', 'proxy_password', 'executable_path', 'profile_dir'];
const loginPage = (value, origin) => {
  try { const url = new URL(value); return url.origin === origin && ['/login', '/login/'].includes(url.pathname); } catch { return false; }
};

export class BrowserError extends Error {
  constructor(code) { super(code); this.code = code; }
}

export function validateStart(input) {
  if (!validateInput(input) || Object.keys(input).some((key) => !startFields.includes(key)) ||
    typeof input.executable_path !== 'string' || !path.isAbsolute(input.executable_path) ||
    typeof input.profile_dir !== 'string' || !path.isAbsolute(input.profile_dir)) return false;
  for (const key of ['proxy_username', 'proxy_password']) {
    if (typeof input[key] !== 'string' || !input[key] || input[key].length > 128 || /[^\x21-\x7e]/.test(input[key])) return false;
  }
  try {
    const proxy = new URL(input.proxy_server);
    return proxy.protocol === 'http:' && proxy.hostname === '127.0.0.1' && Boolean(proxy.port) &&
      !proxy.username && !proxy.password && proxy.pathname === '/' && !proxy.search && !proxy.hash;
  } catch { return false; }
}

export function launchOptions(input) {
  return {
    executablePath: input.executable_path,
    headless: true,
    chromiumSandbox: true,
    serviceWorkers: 'block',
    acceptDownloads: false,
    ignoreHTTPSErrors: false,
    bypassCSP: false,
    viewport: { width: WIDTH, height: HEIGHT },
    screen: { width: WIDTH, height: HEIGHT },
    deviceScaleFactor: 1,
    proxy: { server: input.proxy_server, username: input.proxy_username, password: input.proxy_password, bypass: '<-loopback>' },
    args: [
      '--proxy-bypass-list=<-loopback>',
      '--disable-quic',
      '--dns-prefetch-disable',
      '--host-resolver-rules=MAP * ~NOTFOUND, EXCLUDE 127.0.0.1',
      '--force-webrtc-ip-handling-policy=disable_non_proxied_udp',
      '--webrtc-ip-handling-policy=disable_non_proxied_udp',
      '--disable-features=DirectSockets,DirectSocketsInServiceWorkers,DirectSocketsInSharedWorkers,WebTransport,MediaRouter'
    ],
    timeout: 20000
  };
}

export function cookieCandidates(cookies, host, observedSession) {
  const result = {};
  for (const cookie of cookies) {
    if (cookie.domain.replace(/^\./, '').toLowerCase() !== host.toLowerCase() || cookie.path !== '/') continue;
    if (cookie.name === 'cf_clearance' || (cookie.name === 'session' && observedSession && cookie.value === observedSession))
      result[cookie.name] = cookie.value;
  }
  return result;
}

export class BrowserRuntime {
  constructor() {
    this.status = 'starting';
    this.errorCode = '';
    this.capture = null;
    this.captureBusy = false;
    this.closed = false;
    this.observedSession = '';
  }

  async start(input) {
    if (this.context || this.closed || !validateStart(input)) throw new BrowserError('browser_invalid_input');
    this.origin = new URL(input.base_url).origin;
    this.platform = input.platform;
    this.parser = new LoginCapture(this.origin, input.platform);
    let chromium;
    try { ({ chromium } = await import('playwright-core')); } catch { throw new BrowserError('browser_dependency_missing'); }
    try { this.context = await chromium.launchPersistentContext(input.profile_dir, launchOptions(input)); }
    catch { throw new BrowserError('browser_launch_failed'); }
    this.context.setDefaultTimeout(2500);
    this.page = this.context.pages()[0] || await this.context.newPage();
    const browserSession = await this.context.browser().newBrowserCDPSession();
    const processes = await browserSession.send('SystemInfo.getProcessInfo');
    await browserSession.detach();
    this.browserPID = processes.processInfo.find((process) => process.type === 'browser')?.id;
    if (!Number.isSafeInteger(this.browserPID) || this.browserPID <= 0) throw new BrowserError('browser_launch_failed');
    // Read the unmodified browser identity before loading untrusted site code.
    this.userAgent = await this.page.evaluate(() => navigator.userAgent);
    await this.context.addInitScript(() => {
      // Security capability removal, not anti-detection or fingerprint masking.
      for (const name of ['RTCPeerConnection', 'webkitRTCPeerConnection', 'WebTransport']) {
        try { Object.defineProperty(globalThis, name, { value: undefined, configurable: false, writable: false }); } catch {}
      }
    });
    await this.context.routeWebSocket('**/*', (socket) => socket.close());
    await this.context.route('**/*', async (route) => {
      const request = route.request();
      try {
        const frame = request.frame();
        const permitted = !this.capture && allowedRequest({ url: request.url(),
          pageMatches: frame.page() === this.page, isMainFrame: frame === this.page.mainFrame(),
          isNavigation: request.isNavigationRequest() }, this.origin);
        if (permitted) await route.continue(); else await route.abort('blockedbyclient');
      } catch { await route.abort('blockedbyclient').catch(() => {}); }
    });
    this.context.on('page', (page) => { if (page !== this.page) void page.close().catch(() => {}); });
    this.page.on('download', (download) => { void download.cancel().catch(() => {}); });
    this.page.on('dialog', (dialog) => { void dialog.dismiss().catch(() => {}); });
    this.page.on('close', () => {
      if (!this.capture && !this.closed) { this.status = 'failed'; this.errorCode = 'browser_page_closed'; }
    });
    this.page.on('response', (response) => { void this.inspectResponse(response).catch(() => {}); });
    this.status = 'waiting';
    try { await this.page.goto(`${this.origin}/login`, { waitUntil: 'domcontentloaded', timeout: 20000 }); }
    catch {
      if (!loginPage(this.page.url(), this.origin)) { this.status = 'failed'; this.errorCode = 'browser_navigation_failed'; }
    }
    let filling = false;
    const fill = async () => {
      if (filling || this.closed || this.capture || !input.username || !input.password || !loginPage(this.page.url(), this.origin)) return;
      filling = true;
      try {
        const usernames = this.page.locator('input[type="email"], input[autocomplete="username"], input[name="email"], input[name="username"]').filter({ visible: true });
        const passwords = this.page.locator('input[type="password"]').filter({ visible: true });
        if (await usernames.count() !== 1 || await passwords.count() !== 1 || !await usernames.isEnabled() || !await passwords.isEnabled()) return;
        const current = await usernames.inputValue();
        if ((current && current !== input.username) || await passwords.inputValue()) return;
        await usernames.fill(input.username, { timeout: 1000 });
        await passwords.fill(input.password, { timeout: 1000 });
        input.username = '';
        input.password = '';
        clearInterval(this.fillTimer);
      } catch {} finally { filling = false; }
    };
    this.fillTimer = setInterval(() => { void fill(); }, 750);
    this.fillTimer.unref();
    await fill();
    return { browser_pid: this.browserPID };
  }

  async inspectResponse(response) {
    if (this.closed || this.capture || this.captureBusy) return;
    const request = response.request();
    const frame = request.frame();
    if (frame !== this.page.mainFrame() || frame.page() !== this.page || request.method() !== 'POST') return;
    const url = new URL(response.url());
    const endpoint = this.platform === 'sub2api' ? '/api/v1/auth/login' : '/api/user/login';
    if (url.origin !== this.origin || ![endpoint, `${endpoint}/2fa`].includes(url.pathname)) return;
    const receivedAt = new Date();
    this.captureBusy = true;
    try {
      const headers = await response.allHeaders();
      if (headers['content-length'] && (!/^\d+$/.test(headers['content-length']) || Number(headers['content-length']) > MAX_RESPONSE_BYTES)) return;
      if (!/^application\/json(?:\s*;|$)/i.test(headers['content-type'] || '')) return;
      const body = await response.body();
      if (body.length > MAX_RESPONSE_BYTES) return;
      const text = body.toString('utf8');
      let envelope;
      try { envelope = JSON.parse(text); } catch { return; }
      if (this.platform === 'sub2api' ? envelope?.code === 0 : envelope?.success === true) {
        for (const header of await response.headersArray()) {
          if (header.name.toLowerCase() !== 'set-cookie') continue;
          const match = /^session=([^;\s,]*)/.exec(header.value);
          if (match && match[1].length <= 8192) this.observedSession = match[1];
        }
      }
      const cookies = cookieCandidates(await this.context.cookies([this.origin]), url.hostname, this.observedSession);
      const capture = this.parser.accept({ url: response.url(), method: request.method(), frameIsMain: true, pageMatches: true,
        topLevelURL: this.page.url(), status: response.status(), contentType: headers['content-type'] || '',
        requestBody: request.postData() || '', body: text, cookies, userAgent: this.userAgent, now: receivedAt });
      if (capture) {
        this.capture = capture;
        this.status = 'ready';
        this.errorCode = '';
        clearInterval(this.fillTimer);
        await this.page.close().catch(() => {});
      }
    } finally { this.captureBusy = false; }
  }

  async snapshot() {
    const view = () => ({ status: this.status, width: WIDTH, height: HEIGHT, ...(this.errorCode ? { error_code: this.errorCode } : {}) });
    if (!this.context || this.closed) throw new BrowserError('browser_closed');
    if (this.capture || this.captureBusy || this.page.isClosed() || !loginPage(this.page.url(), this.origin)) return view();
    try {
      const image = await this.page.screenshot({ type: 'jpeg', quality: 65, fullPage: false, timeout: 5000 });
      if (this.capture || this.captureBusy || !loginPage(this.page.url(), this.origin)) return view();
      if (image.length > 2 * 1024 * 1024) throw new Error('oversized');
      return { ...view(), image: image.toString('base64') };
    } catch {
      if (this.capture) return view();
      return { ...view(), error_code: 'browser_render_failed' };
    }
  }

  async action(action) {
    if (!validateAction(action)) throw new BrowserError('browser_invalid_input');
    if (!this.context || this.closed) throw new BrowserError('browser_closed');
    if (this.capture || this.captureBusy || this.page.isClosed()) return null;
    if (!loginPage(this.page.url(), this.origin)) throw new BrowserError('browser_invalid_input');
    try {
      switch (action.type) {
        case 'pointer_down':
          await this.page.mouse.move(action.x, action.y);
          await this.page.mouse.down({ button: 'left' });
          break;
        case 'pointer_move': await this.page.mouse.move(action.x, action.y); break;
        case 'pointer_up':
          await this.page.mouse.move(action.x, action.y);
          await this.page.mouse.up({ button: 'left' });
          break;
        case 'wheel': await this.page.mouse.wheel(action.delta_x ?? 0, action.delta_y ?? 0); break;
        case 'text': await this.page.keyboard.insertText(action.text); break;
        case 'key': await this.page.keyboard.press(action.key); break;
      }
      return null;
    } catch {
      if (this.capture) return null;
      throw new BrowserError('browser_action_failed');
    }
  }

  result() { return this.capture; }

  async close() {
    if (this.closed) return;
    this.closed = true;
    clearInterval(this.fillTimer);
    this.capture = null;
    this.parser = null;
    if (this.context) await this.context.close().catch(() => {});
  }
}
