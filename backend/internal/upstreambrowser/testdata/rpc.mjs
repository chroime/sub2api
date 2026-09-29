import readline from 'node:readline';
let input;
const lines = readline.createInterface({ input: process.stdin });
for await (const line of lines) {
  const request = JSON.parse(line);
  let result = null;
  if (request.method === 'start') {
    input = request.params;
    const error = {
      'https://launch-failed.example': 'browser_launch_failed',
      'https://dependency-missing.example': 'browser_dependency_missing',
      'https://raw-error.example': 'https://secret-canary.example/token?password=secret-canary',
    }[input.base_url];
    if (error) {
      process.stdout.write(JSON.stringify({ id: request.id, ok: false, error }) + '\n');
      continue;
    }
    result = { browser_pid: process.pid };
  }
  if (request.method === 'snapshot') {
    if (input.base_url === 'https://blocked.example') continue;
    if (input.base_url === 'https://malformed.example') { process.stdout.write('{malformed\n'); continue; }
    result = { status: 'waiting', width: 1024, height: 720 };
  }
  if (request.method === 'action') {
    const error = {
      'https://unsupported-route.example': 'browser_unsupported_route',
      'https://raw-action-error.example': 'browser_unsupported_route: secret-canary',
    }[input.base_url];
    if (error) {
      process.stdout.write(JSON.stringify({ id: request.id, ok: false, error }) + '\n');
      continue;
    }
  }
  process.stdout.write(JSON.stringify({ id: request.id, ok: true, result }) + '\n');
}
