import assert from 'node:assert/strict';
import { spawn } from 'node:child_process';
import path from 'node:path';
import test from 'node:test';
import { launchOptions, validateStart, cookieCandidates } from './runtime.mjs';

const input = {
  base_url: 'https://upstream.example', platform: 'sub2api', username: '', password: '',
  executable_path: process.execPath, profile_dir: path.resolve('temporary-profile'),
  proxy_server: 'http://127.0.0.1:3128', proxy_username: 'job', proxy_password: 'random-secret'
};

test('launch config keeps the sandbox and guarded proxy and never overrides browser identity', () => {
  assert.equal(validateStart(input), true);
  const options = launchOptions(input);
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
