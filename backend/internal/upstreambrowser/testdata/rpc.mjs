import readline from 'node:readline';
let input;
const lines = readline.createInterface({ input: process.stdin });
for await (const line of lines) {
  const request = JSON.parse(line);
  let result = null;
  if (request.method === 'start') { input = request.params; result = { browser_pid: process.pid }; }
  if (request.method === 'snapshot') {
    if (input.base_url === 'https://blocked.example') continue;
    if (input.base_url === 'https://malformed.example') { process.stdout.write('{malformed\n'); continue; }
    result = { status: 'waiting', width: 1024, height: 720 };
  }
  process.stdout.write(JSON.stringify({ id: request.id, ok: true, result }) + '\n');
}
