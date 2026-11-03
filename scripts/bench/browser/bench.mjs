#!/usr/bin/env node
// Browser-use primitive benchmark. One Chrome, several tools.
//   node bench.mjs --side ab|mewla [--reps 15] [--out file.json]
// Environment: BENCH_CDP (port, agent-browser attaches here), for mewla also
// BENCH_MCP (argv JSON of the MCP server), BENCH_ENV (JSON of extra env).
import { spawn } from 'node:child_process';
import { writeFileSync } from 'node:fs';
import { startFixture } from './fixture.mjs';
import { McpClient } from './mcpclient.mjs';

const args = Object.fromEntries(process.argv.slice(2).reduce((a, v, i, all) => (v.startsWith('--') ? [...a, [v.slice(2), all[i + 1]]] : a), []));
const side = args.side;
const reps = Number(args.reps || 15);
const PORT = 8765;
const base = `http://127.0.0.1:${PORT}`;
const fixture = await startFixture(PORT);

const pct = (xs, p) => { const s = [...xs].sort((a, b) => a - b); return s[Math.min(s.length - 1, Math.floor(p * s.length))]; };
const results = {};
function record(op, ms, bytes) {
  const r = (results[op] ??= { ms: [], bytes: [] });
  r.ms.push(ms);
  if (bytes != null) r.bytes.push(bytes);
}
async function repeat(op, fn, n = reps) {
  for (let i = 0; i < n; i++) {
    try { const { ms, bytes } = await fn(i); record(op, ms, bytes); }
    catch (e) { (results[op] ??= { ms: [], bytes: [], errors: [] }).errors ??= []; results[op].errors.push(String(e).slice(0, 200)); }
  }
}

// ---- agent-browser: every call is a CLI process, as an agent's shell tool runs it.
function ab(...a) {
  const t0 = performance.now();
  return new Promise((res, rej) => {
    const p = spawn('agent-browser', ['--session', 'bench', ...a], { stdio: ['ignore', 'pipe', 'pipe'] });
    let out = '', err = '';
    p.stdout.on('data', d => out += d); p.stderr.on('data', d => err += d);
    p.on('exit', code => {
      const ms = performance.now() - t0;
      if (code) rej(new Error(`ab ${a.join(' ')} -> ${code}: ${err || out}`)); else res({ ms, bytes: Buffer.byteLength(out), text: out });
    });
  });
}

async function runAB() {
  const cdp = process.env.BENCH_CDP || '9333';
  await ab('close').catch(() => {});
  // cold start: daemon spawn + attach + first page.
  const cold = await ab('--cdp', cdp, 'open', `${base}/form`);
  record('cold_start', cold.ms, cold.bytes);
  await repeat('navigate', async i => ab('open', `${base}${i % 2 ? '/table' : '/form'}`));
  await ab('open', `${base}/busy`);
  await repeat('snapshot_busy_full', () => ab('snapshot'));
  await repeat('snapshot_busy_interactive', () => ab('snapshot', '-i'));
  await repeat('find', i => ab('find', 'text', `Channel ${20 + i}`, 'text'));
  await repeat('click', i => ab('click', `#react${10 + i}`));
  await ab('open', `${base}/form`);
  await repeat('fill', i => ab('fill', '#name', `Ada ${i}`));
  await repeat('type_keys', i => ab('type', '#email', `a${i}@x.io`));
  await ab('open', `${base}/delay`);
  await repeat('wait_for_change', async () => {
    await ab('eval', `document.getElementById('status').textContent='Idle'`);
    const t0 = performance.now();
    await ab('click', '#go');
    await ab('wait', '--text', 'Loaded');
    return { ms: performance.now() - t0 };
  });
  await repeat('screenshot', i => ab('screenshot', `/tmp/bench-ab-${i}.png`));
  await ab('open', `${base}/table`);
  await repeat('extract_table', () => ab('get', 'text', '#data'));
  await repeat('multi_tab', async () => {
    const t0 = performance.now();
    await ab('tab', 'new', `${base}/tab2`);
    await ab('tab', 'list');
    await ab('tab', 'close');
    return { ms: performance.now() - t0 };
  }, Math.min(reps, 8));
  await ab('close').catch(() => {});
}

// ---- Mewla: Playwright MCP over stdio, the way an agent's MCP host runs it.
async function runMewla() {
  const argv = JSON.parse(process.env.BENCH_MCP);
  const env = { ...process.env, ...JSON.parse(process.env.BENCH_ENV || '{}') };
  const t0 = performance.now();
  const c = new McpClient(argv, env);
  const init = await c.start();
  record('mcp_initialize', init.ms);
  const first = await c.call('browser_navigate', { url: `${base}/form` });
  record('cold_start', performance.now() - t0, first.bytes);
  const call = async (name, a) => { const r = await c.call(name, a); if (r.isError) throw new Error(`${name}: ${r.text.slice(0, 160)}`); return r; };
  await repeat('navigate', i => call('browser_navigate', { url: `${base}${i % 2 ? '/table' : '/form'}` }));
  await call('browser_navigate', { url: `${base}/busy` });
  await repeat('snapshot_busy_full', () => call('browser_snapshot'));
  results.snapshot_busy_interactive = results.snapshot_busy_full; // MCP has one snapshot form
  await repeat('find', i => call('browser_find', { text: `Channel ${20 + i}` }));
  await repeat('click', i => call('browser_click', { target: `#react${10 + i}` }));
  await call('browser_navigate', { url: `${base}/form` });
  await repeat('fill', i => call('browser_type', { target: '#name', text: `Ada ${i}` }));
  await repeat('type_keys', i => call('browser_type', { target: '#email', text: `a${i}@x.io`, slowly: true }));
  await call('browser_navigate', { url: `${base}/delay` });
  await repeat('wait_for_change', async () => {
    await call('browser_evaluate', { function: `() => { document.getElementById('status').textContent = 'Idle'; }` });
    const t = performance.now();
    await call('browser_click', { target: '#go' });
    await call('browser_wait_for', { text: 'Loaded' });
    return { ms: performance.now() - t };
  });
  await repeat('screenshot', i => call('browser_take_screenshot', { filename: `bench-mewla-${i}.png` }));
  await call('browser_navigate', { url: `${base}/table` });
  await repeat('extract_table', () => call('browser_evaluate', { function: `() => document.querySelector('#data').innerText` }));
  await repeat('multi_tab', async () => {
    const t = performance.now();
    await call('browser_tabs', { action: 'new', url: `${base}/tab2` });
    await call('browser_tabs', { action: 'list' });
    await call('browser_tabs', { action: 'close' });
    return { ms: performance.now() - t };
  }, Math.min(reps, 8));
  await c.close();
}

try {
  if (side === 'ab') await runAB(); else if (side === 'mewla') await runMewla(); else throw new Error('--side ab|mewla');
} finally { fixture.close(); }

const table = {};
for (const [op, r] of Object.entries(results)) {
  table[op] = { n: r.ms.length, p50: Math.round(pct(r.ms, 0.5)), p95: Math.round(pct(r.ms, 0.95)), bytes_p50: r.bytes.length ? pct(r.bytes, 0.5) : null, errors: r.errors?.length || 0 };
}
console.table(table);
if (args.out) writeFileSync(args.out, JSON.stringify({ side, reps, table, errors: Object.fromEntries(Object.entries(results).filter(([, r]) => r.errors).map(([k, r]) => [k, r.errors.slice(0, 3)])) }, null, 1));
process.exit(0);
