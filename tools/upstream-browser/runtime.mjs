import path from 'node:path';
import { LoginCapture, WIDTH, HEIGHT, MAX_RESPONSE_BYTES, allowedRequest, allowedSSORequest, validateAction, validateInput } from './contract.mjs';

const startFields = ['base_url', 'platform', 'username', 'password', 'sso_token', 'user_code', 'verification_url', 'consent_url', 'proxy_server', 'proxy_username', 'proxy_password', 'executable_path', 'profile_dir'];
// Grok SSO approval pacing. The backend keeps polling the token endpoint, so
// these only bound how long the page is driven before reporting failure.
export const SSO_APPROVAL_MS = 110000;
export const SSO_SETTLE_MS = 1500;
export const SSO_RECLICK_MS = 6000;
export const SSO_MAX_CLICKS = 3;
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
  const headless = process.env.GOVERNANCE_BROWSER_HEADLESS === 'true';
  const args = ['--proxy-bypass-list=<-loopback>'];
  if (input.platform !== 'grok-sso') {
    args.push(
      '--disable-quic',
      '--dns-prefetch-disable',
      '--host-resolver-rules=MAP * ~NOTFOUND, EXCLUDE 127.0.0.1',
      '--force-webrtc-ip-handling-policy=disable_non_proxied_udp',
      '--webrtc-ip-handling-policy=disable_non_proxied_udp',
      '--disable-features=DirectSockets,DirectSocketsInServiceWorkers,DirectSocketsInSharedWorkers,WebTransport,MediaRouter'
    );
  }
  return {
    executablePath: input.executable_path,
    // Authorization is an interactive workflow, so use a headed browser by
    // default. Tests or deployments without a display may explicitly opt in
    // to headless mode; neither mode guarantees upstream challenge acceptance.
    headless,
    chromiumSandbox: true,
    serviceWorkers: input.platform === 'grok-sso' ? 'allow' : 'block',
    acceptDownloads: false,
    ignoreHTTPSErrors: false,
    bypassCSP: false,
    viewport: { width: WIDTH, height: HEIGHT },
    screen: { width: WIDTH, height: HEIGHT },
    deviceScaleFactor: 1,
    proxy: { server: input.proxy_server, username: input.proxy_username, password: input.proxy_password, bypass: '<-loopback>' },
    args,
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
    this.ssoApproved = false;
    this.ssoApprovalResponse = false;
    this.ssoClicks = new Map();
    this.ssoVisibleSince = 0;
    this.ssoTask = null;
    this.closed = false;
    this.observedSession = '';
  }

  async start(input) {
    if (this.context || this.closed || !validateStart(input)) throw new BrowserError('browser_invalid_input');
    this.origin = new URL(input.base_url).origin;
    this.platform = input.platform;
    this.parser = this.platform === 'grok-sso' ? null : new LoginCapture(this.origin, input.platform);
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
    if (this.platform !== 'grok-sso') {
      await this.context.addInitScript(() => {
        // Security capability removal, not anti-detection or fingerprint masking.
        for (const name of ['RTCPeerConnection', 'webkitRTCPeerConnection', 'WebTransport']) {
          try { Object.defineProperty(globalThis, name, { value: undefined, configurable: false, writable: false }); } catch {}
        }
      });
      await this.context.routeWebSocket('**/*', (socket) => socket.close());
    }
    await this.context.route('**/*', async (route) => {
      const request = route.request();
      try {
        let permitted;
        if (this.platform === 'grok-sso') {
          // Service-worker and worker requests have no frame; Playwright
          // throws from request.frame() for them. Castle and Cloudflare both
          // use workers, so treat them as same-page subresources.
          let frame = null;
          try { frame = request.frame(); } catch {}
          permitted = !this.ssoApproved && allowedSSORequest({ url: request.url(),
            pageMatches: frame ? frame.page() === this.page : true, isMainFrame: frame ? frame === this.page.mainFrame() : false,
            isNavigation: frame ? request.isNavigationRequest() : false });
        } else {
          const frame = request.frame();
          permitted = !this.capture && allowedRequest({ url: request.url(),
            pageMatches: frame.page() === this.page, isMainFrame: frame === this.page.mainFrame(),
            isNavigation: request.isNavigationRequest() }, this.origin);
        }
        if (permitted) await route.continue(); else await route.abort('blockedbyclient');
      } catch { await route.abort('blockedbyclient').catch(() => {}); }
    });
    this.context.on('page', (page) => { if (page !== this.page) void page.close().catch(() => {}); });
    this.page.on('download', (download) => { void download.cancel().catch(() => {}); });
    this.page.on('dialog', (dialog) => { void dialog.dismiss().catch(() => {}); });
    this.page.on('close', () => {
      if (!this.capture && !this.ssoApproved && !this.closed) { this.status = 'failed'; this.errorCode = 'browser_page_closed'; }
    });
    if (this.platform !== 'grok-sso') this.page.on('response', (response) => { void this.inspectResponse(response).catch(() => {}); });
    if (this.platform === 'grok-sso') {
      this.page.on('response', (response) => {
        try {
          const url = new URL(response.url());
          if (!url.pathname.toLowerCase().includes('/oauth2/device/approve')) return;
          // Only a redirect to the done page is a positive signal; a 200 may
          // be Castle's "block" body rendered in place of the consent page.
          const location = response.headers().location || '';
          if (response.status() >= 300 && response.status() < 400 && /\/device\/(done|approved|success)/i.test(location)) {
            this.ssoApprovalResponse = true;
          }
        } catch {}
      });
      return this.startGrokSSO(input);
    }
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

  async startGrokSSO(input) {
    // Only the SSO cookie is injected. Device-flow cookies minted by the Go
    // HTTP client are bound to its TLS/User-Agent identity and would make this
    // browser session look forged to xAI's edge, so the page mints its own.
    await this.context.addCookies([
      { name: 'sso', value: input.sso_token, domain: '.x.ai', path: '/', secure: true, httpOnly: true },
      { name: 'sso-rw', value: input.sso_token, domain: '.x.ai', path: '/', secure: true, httpOnly: true }
    ]);
    this.status = 'waiting';
    // Approval runs in the background so the start RPC returns promptly; the
    // backend observes progress through snapshot() and the token endpoint.
    this.ssoTask = this.runGrokSSO(input.verification_url).catch(() => {}).finally(() => {
      if (this.status === 'waiting' && !this.closed) {
        this.status = 'failed';
        this.errorCode = 'browser_sso_approval_failed';
      }
    });
    return { browser_pid: this.browserPID };
  }

  async runGrokSSO(verificationURL) {
    try { await this.page.goto(verificationURL, { waitUntil: 'load', timeout: 30000 }); }
    catch { if (!/^https:\/\/([a-z0-9-]+\.)*x\.ai\//i.test(this.page.url())) return; }
    const deadline = Date.now() + SSO_APPROVAL_MS;
    let reloads = 0;
    while (!this.closed && Date.now() < deadline) {
      if (await this.grokSSODone()) {
        this.ssoApproved = true;
        this.status = 'ready';
        return;
      }
      if (await this.grokSSOBlocked()) {
        // Castle rejected this attempt. Reload once so the page mints a fresh
        // request token; a second rejection is terminal.
        if (reloads++ >= 1) return;
        this.ssoClicks.clear();
        try { await this.page.goto(verificationURL, { waitUntil: 'load', timeout: 30000 }); } catch {}
        continue;
      }
      await this.grokSSOClickAllow();
      await this.page.waitForTimeout(500);
    }
  }

  async grokSSOClickAllow() {
    try {
      const point = await this.page.evaluate(() => {
        const visible = (element) => {
          const style = getComputedStyle(element);
          const rect = element.getBoundingClientRect();
          return style.visibility !== 'hidden' && style.display !== 'none' && rect.width > 0 && rect.height > 0 &&
            rect.bottom > 0 && rect.right > 0 && rect.top < innerHeight && rect.left < innerWidth;
        };
        const enabled = (element) => !element.disabled && element.getAttribute('aria-disabled') !== 'true';
        const label = (element) => (element.innerText || element.value || element.getAttribute('aria-label') || '').trim();
        const allowed = /^(allow|approve|authorize|accept|continue|confirm)(\s+.*)?$|^(同意|允许|授权|继续|确认)/i;
        const candidates = Array.from(document.querySelectorAll('button, input[type="submit"], a, [role="button"]'));
        const target = candidates.find((element) => visible(element) && enabled(element) && allowed.test(label(element)));
        if (!target) return null;
        const rect = target.getBoundingClientRect();
        return { x: rect.left + rect.width / 2, y: rect.top + rect.height / 2, key: `${location.href}|${label(target)}`,
          ready: document.readyState === 'complete' };
      });
      if (!point || !point.ready || !Number.isFinite(point.x) || !Number.isFinite(point.y)) {
        this.ssoVisibleSince = 0;
        return;
      }
      const now = Date.now();
      // The page's verification script initialises after the control renders.
      // Let the control stay visible briefly before the first click.
      if (!this.ssoVisibleSince) this.ssoVisibleSince = now;
      if (now - this.ssoVisibleSince < SSO_SETTLE_MS) return;
      const previous = this.ssoClicks.get(point.key);
      // Re-click the same control after a pause if the page did not advance
      // (e.g. the first click landed before the request token was ready).
      if (previous && (previous.count >= SSO_MAX_CLICKS || now - previous.at < SSO_RECLICK_MS)) return;
      // Use Chromium input dispatch instead of Element#click(). The latter
      // produces an untrusted DOM event and xAI's consent page may reject it
      // as an automated approval, returning the bare `block` response.
      await this.page.mouse.move(point.x, point.y, { steps: 8 });
      await this.page.mouse.click(point.x, point.y, { delay: 60 });
      this.ssoClicks.set(point.key, { at: now, count: (previous?.count || 0) + 1 });
    } catch {}
  }

  async grokSSOBlocked() {
    try {
      return await this.page.evaluate(() =>
        /couldn.t verify this request|could not verify this request|request token required|access denied/i.test(document.body?.innerText || ''));
    } catch { return false; }
  }

  async grokSSODone() {
    try {
      if (this.ssoApprovalResponse) return true;
      const url = new URL(this.page.url());
      if (/\/device\/(done|approved|success)\/?$/i.test(url.pathname)) return true;
      return await this.page.evaluate(() => /authorization\s+(complete|successful)|device\s+approved|已授权|授权成功/i.test(document.body?.innerText || ''));
    } catch { return false; }
  }

  async inspectResponse(response) {
    if (this.closed || this.capture || this.captureBusy || this.platform === 'grok-sso') return;
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

  failUnsupportedRoute() {
    if (this.status === 'failed') return true;
    if (this.capture || this.captureBusy || loginPage(this.page.url(), this.origin)) return false;
    this.status = 'failed';
    this.errorCode = 'browser_unsupported_route';
    return true;
  }

  async snapshot() {
    const view = () => ({ status: this.status, width: WIDTH, height: HEIGHT, ...(this.errorCode ? { error_code: this.errorCode } : {}) });
    if (!this.context || this.closed) throw new BrowserError('browser_closed');
    if (this.platform === 'grok-sso') return view();
    if (this.capture || this.captureBusy || this.page.isClosed() || this.failUnsupportedRoute()) return view();
    try {
      const image = await this.page.screenshot({ type: 'jpeg', quality: 65, fullPage: false, timeout: 5000 });
      if (this.capture || this.captureBusy || this.page.isClosed() || this.failUnsupportedRoute()) return view();
      if (image.length > 2 * 1024 * 1024) throw new Error('oversized');
      return { ...view(), image: image.toString('base64') };
    } catch {
      if (this.capture || this.captureBusy || this.page.isClosed() || this.failUnsupportedRoute()) return view();
      return { ...view(), error_code: 'browser_render_failed' };
    }
  }

  async action(action) {
    if (!validateAction(action)) throw new BrowserError('browser_invalid_input');
    if (!this.context || this.closed) throw new BrowserError('browser_closed');
    if (this.capture || this.captureBusy || this.page.isClosed()) return null;
    if (this.failUnsupportedRoute()) throw new BrowserError(this.errorCode);
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
