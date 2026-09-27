import { once } from 'node:events';
import { BrowserError, BrowserRuntime } from './runtime.mjs';
import { MAX_LINE_BYTES, SESSION_MS } from './contract.mjs';

const runtime = new BrowserRuntime();
let stopping = false;
const stop = async () => {
  if (stopping) return;
  stopping = true;
  await runtime.close();
  process.exit(0);
};
const timeout = setTimeout(() => { void stop(); }, SESSION_MS);
timeout.unref();
process.on('SIGTERM', () => { void stop(); });
process.on('SIGINT', () => { void stop(); });
process.on('uncaughtException', () => { void stop(); });
process.on('unhandledRejection', () => { void stop(); });
process.stdout.on('error', () => { void stop(); });

const write = async (response) => {
  const line = JSON.stringify(response) + '\n';
  if (Buffer.byteLength(line) > 4 * 1024 * 1024) throw new BrowserError('browser_protocol_error');
  if (!process.stdout.write(line)) await once(process.stdout, 'drain');
};

let pending = Buffer.alloc(0);
let lastID = 0;
try {
  for await (const chunk of process.stdin) {
    if (stopping) break;
    if (pending.length + chunk.length > MAX_LINE_BYTES) throw new BrowserError('browser_protocol_error');
    pending = Buffer.concat([pending, chunk]);
    let index;
    while ((index = pending.indexOf(10)) !== -1) {
      const line = pending.subarray(0, index);
      pending = pending.subarray(index + 1);
      let request;
      try { request = JSON.parse(line.toString('utf8')); } catch { throw new BrowserError('browser_protocol_error'); }
      if (!request || typeof request !== 'object' || Array.isArray(request) || Object.keys(request).some((key) => !['id', 'method', 'params'].includes(key)) ||
        !Number.isSafeInteger(request.id) || request.id <= lastID || !['start', 'snapshot', 'action', 'result', 'close'].includes(request.method)) {
        await write({ id: Number.isSafeInteger(request?.id) ? request.id : 0, ok: false, error: 'browser_protocol_error' });
        await stop();
      }
      lastID = request.id;
      try {
        let result;
        switch (request.method) {
          case 'start': result = await runtime.start(request.params); break;
          case 'snapshot': result = await runtime.snapshot(); break;
          case 'action': result = await runtime.action(request.params); break;
          case 'result': result = runtime.result(); break;
          case 'close': await runtime.close(); result = null; break;
        }
        await write({ id: request.id, ok: true, result });
      } catch (error) {
        await write({ id: request.id, ok: false, error: error instanceof BrowserError ? error.code : 'browser_protocol_error' });
        await stop();
      }
      if (request.method === 'close') await stop();
    }
  }
} catch {
  await write({ id: 0, ok: false, error: 'browser_protocol_error' }).catch(() => {});
} finally { await stop(); }
