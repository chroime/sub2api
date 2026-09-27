export const WIDTH = 1024;
export const HEIGHT = 720;
export const MAX_LINE_BYTES = 32 * 1024;
export const MAX_RESPONSE_BYTES = 128 * 1024;
export const SESSION_MS = 10 * 60 * 1000;

const object = (value) => value !== null && typeof value === 'object' && !Array.isArray(value);
const bounded = (value, max) => typeof value === 'string' && value.length > 0 && Buffer.byteLength(value) <= max && !/[\x00-\x20\x7f]/.test(value);
const credential = (value, max) => typeof value === 'string' && value.length > 0 && Buffer.byteLength(value) <= max && !/[\x00-\x1f\x7f]/.test(value);
const parseObject = (raw, max) => {
  if (typeof raw !== 'string' || Buffer.byteLength(raw) > max) return null;
  try { const value = JSON.parse(raw); return object(value) ? value : null; } catch { return null; }
};
const endpointURL = (value) => {
  try { const url = new URL(value); return !url.username && !url.password && !url.hash ? url : null; } catch { return null; }
};

export function validateInput(input) {
  if (!object(input)) return false;
  const url = endpointURL(input.base_url);
  return Boolean(url && url.protocol === 'https:' && (!url.port || url.port === '443') &&
    (url.pathname === '/' || url.pathname === '') && !url.search &&
    ['sub2api', 'newapi'].includes(input.platform) &&
    typeof input.username === 'string' && Buffer.byteLength(input.username) <= 512 &&
    typeof input.password === 'string' && Buffer.byteLength(input.password) <= 4096 &&
    !/[\x00-\x1f\x7f]/.test(input.username + input.password));
}

export function allowedRequest(request, origin) {
  if (!request.pageMatches) return false;
  const url = endpointURL(request.url);
  if (!url || !['https:', 'http:'].includes(url.protocol)) return false;
  return !(request.isNavigation && request.isMainFrame) || url.origin === origin;
}

export function validateAction(action) {
  if (!object(action)) return false;
  const fields = ['type', 'x', 'y', 'delta_x', 'delta_y', 'text', 'key'];
  if (Object.keys(action).some((key) => !fields.includes(key))) return false;
  switch (action.type) {
    case 'pointer_down': case 'pointer_move': case 'pointer_up':
      return Number.isFinite(action.x) && action.x >= 0 && action.x < WIDTH &&
        Number.isFinite(action.y) && action.y >= 0 && action.y < HEIGHT;
    case 'wheel':
      return Number.isFinite(action.delta_x ?? 0) && Number.isFinite(action.delta_y ?? 0) &&
        Math.abs(action.delta_x ?? 0) <= 2000 && Math.abs(action.delta_y ?? 0) <= 2000;
    case 'text': return credential(action.text, 2048);
    case 'key': return ['Enter', 'Tab', 'Backspace', 'Delete', 'ArrowLeft', 'ArrowRight', 'ArrowUp', 'ArrowDown',
      'Escape', 'Home', 'End', 'PageUp', 'PageDown', 'Space', 'Control+A', 'Meta+A'].includes(action.key);
    default: return false;
  }
}

function verifiedLogin(body, platform) {
  const username = platform === 'sub2api' ? body?.email : body?.username;
  if (!credential(username, 512) || !credential(body?.password, 4096) || body.password_encrypted !== undefined) return undefined;
  return { username, password: body.password };
}

function captureCookies(cookies, includeSession) {
  const result = {};
  if (!object(cookies)) return result;
  for (const name of includeSession ? ['session', 'cf_clearance'] : ['cf_clearance']) {
    if (bounded(cookies[name], 8192) && !/[;,"\\]/.test(cookies[name])) result[name] = cookies[name];
  }
  return result;
}

export class LoginCapture {
  constructor(origin, platform) {
    this.origin = origin;
    this.platform = platform;
    this.pending = null;
  }

  accept(exchange) {
    const url = endpointURL(exchange.url);
    const top = endpointURL(exchange.topLevelURL);
    const loginPath = this.platform === 'sub2api' ? '/api/v1/auth/login' : '/api/user/login';
    if (!exchange.frameIsMain || !exchange.pageMatches || exchange.method !== 'POST' ||
      !url || url.origin !== this.origin || !top || top.origin !== this.origin ||
      ![loginPath, `${loginPath}/2fa`].includes(url.pathname)) return null;
    const secondFactor = url.pathname.endsWith('/2fa');
    if (!secondFactor) this.pending = null;
    if (!Number.isInteger(exchange.status) || exchange.status < 200 || exchange.status >= 300 ||
      typeof exchange.contentType !== 'string' || !/^application\/json(?:\s*;|$)/i.test(exchange.contentType)) return null;
    const envelope = parseObject(exchange.body, MAX_RESPONSE_BYTES);
    const request = parseObject(exchange.requestBody, MAX_LINE_BYTES);
    if (!envelope || !request || (this.platform === 'sub2api' ? envelope.code !== 0 : envelope.success !== true) || !object(envelope.data)) return null;
    const data = envelope.data;
    const now = exchange.now instanceof Date ? exchange.now : new Date();
    if (!Number.isFinite(now.getTime())) return null;
    let login = verifiedLogin(request, this.platform);
    if (secondFactor) {
      if (!this.pending || now.getTime() - this.pending.created > SESSION_MS || now.getTime() < this.pending.created ||
        (this.pending.token && request[this.pending.field] !== this.pending.token)) return null;
      login = this.pending.login;
    }
    if (data.requires_2fa || data.require_2fa || data.require_verification) {
      if (secondFactor) return null;
      if (this.platform === 'sub2api' && data.requires_2fa === true && bounded(data.temp_token, 16384))
        this.pending = { field: 'temp_token', token: data.temp_token, login, created: now.getTime() };
      if (this.platform === 'newapi' && data.require_verification === true && bounded(data.flow_token, 16384))
        this.pending = { field: 'flow_token', token: data.flow_token, login, created: now.getTime() };
      if (this.platform === 'newapi' && data.require_2fa === true)
        this.pending = { token: '', login, created: now.getTime() };
      return null;
    }
    if (!credential(exchange.userAgent, 1024)) return null;
    const result = { issued_at: now.toISOString(), user_agent: exchange.userAgent, auth_variant: 'bearer' };
    const id = data.user?.id ?? data.id;
    if (Number.isSafeInteger(id) && id > 0) result.user_id = id;
    if (bounded(data.access_token, 16384)) {
      result.access_token = data.access_token;
      result.cookies = captureCookies(exchange.cookies, false);
      if (Number.isSafeInteger(data.expires_in) && data.expires_in > 0 && data.expires_in <= 31536000) {
        result.expires_in = data.expires_in;
        result.expires_at = new Date(now.getTime() + data.expires_in * 1000).toISOString();
        if (bounded(data.refresh_token, 16384)) result.refresh_token = data.refresh_token;
      }
    } else if (this.platform === 'newapi' && Number.isSafeInteger(data.id) && data.id > 0 && (data.access_token === undefined || data.access_token === '')) {
      result.cookies = captureCookies(exchange.cookies, true);
      if (!result.cookies.session) return null;
      result.auth_variant = 'newapi_legacy_cookie';
    } else return null;
    if (login) result.verified_login = login;
    this.pending = null;
    return result;
  }
}
