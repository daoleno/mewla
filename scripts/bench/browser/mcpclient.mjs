// Minimal MCP stdio client for the benchmark: spawn a server, call tools,
// time every call.
import { spawn } from 'node:child_process';

export class McpClient {
  constructor(argv, env) {
    this.argv = argv;
    this.env = env;
    this.nextId = 1;
    this.pending = new Map();
    this.buf = '';
  }
  start() {
    const t0 = performance.now();
    this.proc = spawn(this.argv[0], this.argv.slice(1), { env: this.env, stdio: ['pipe', 'pipe', 'pipe'] });
    this.stderr = '';
    this.proc.stderr.on('data', d => { this.stderr += d; });
    this.proc.stdout.on('data', d => {
      this.buf += d;
      let i;
      while ((i = this.buf.indexOf('\n')) >= 0) {
        const line = this.buf.slice(0, i);
        this.buf = this.buf.slice(i + 1);
        if (!line.trim()) continue;
        let msg;
        try { msg = JSON.parse(line); } catch { continue; }
        const p = this.pending.get(msg.id);
        if (p) { this.pending.delete(msg.id); p(msg); }
      }
    });
    return this.request('initialize', { protocolVersion: '2025-06-18', capabilities: {}, clientInfo: { name: 'bench', version: '1' } })
      .then(r => { this.notify('notifications/initialized'); return { ms: performance.now() - t0, result: r.result }; });
  }
  notify(method, params) { this.proc.stdin.write(JSON.stringify({ jsonrpc: '2.0', method, params }) + '\n'); }
  request(method, params) {
    const id = this.nextId++;
    return new Promise(res => {
      this.pending.set(id, res);
      this.proc.stdin.write(JSON.stringify({ jsonrpc: '2.0', id, method, params }) + '\n');
    });
  }
  async tools() { return (await this.request('tools/list', {})).result.tools; }
  // call returns { ms, bytes, text, isError }
  async call(name, args = {}) {
    const t0 = performance.now();
    const msg = await this.request('tools/call', { name, arguments: args });
    const ms = performance.now() - t0;
    const content = msg.result?.content ?? [];
    const text = content.map(c => c.type === 'text' ? c.text : `[${c.type}:${(c.data || '').length}]`).join('\n');
    return { ms, bytes: Buffer.byteLength(JSON.stringify(msg.result ?? msg.error ?? {})), text, isError: !!msg.result?.isError || !!msg.error };
  }
  async close() {
    try { this.proc.stdin.end(); } catch {}
    await new Promise(r => { this.proc.on('exit', r); setTimeout(() => { this.proc.kill('SIGKILL'); r(); }, 3000); });
  }
}
