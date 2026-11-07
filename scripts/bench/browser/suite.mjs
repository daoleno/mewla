#!/usr/bin/env node
// Task suite: a real agent (claude -p, same model) does each task with one
// tool. Wall-clock, turns, tokens and success come from the run itself.
//   node suite.mjs --side mewla|ab [--reps 3] [--only id,id] [--out file.json] [--model claude-sonnet-5-5]
// Env: BENCH_DIR (env.sh), and for the mewla side BENCH_MCP (argv JSON of the Playwright MCP command).
import { spawn, execFileSync } from 'node:child_process';
import { writeFileSync, mkdtempSync, readFileSync, rmSync } from 'node:fs';
import { tmpdir } from 'node:os';
import path from 'node:path';
import { fileURLToPath } from 'node:url';
import { startFixture, events } from './fixture.mjs';
import { tasks as baseTasks, extraTasks } from './tasks.mjs';
import { longTasks } from './tasks-long.mjs';
// --long runs only the longer tasks; --only picks by id from both sets.
const tasks = [...(process.argv.includes('--long') ? [] : baseTasks), ...(process.argv.includes('--long') || process.argv.includes('--with-long') ? longTasks : []), ...extraTasks.filter(t => (process.argv[process.argv.indexOf('--only') + 1] || '').split(',').includes(t.id))];

const args = Object.fromEntries(process.argv.slice(2).reduce((a, v, i, all) => (v.startsWith('--') ? [...a, [v.slice(2), all[i + 1]]] : a), []));
const side = args.side, reps = Number(args.reps || 3), model = args.model || 'claude-sonnet-5-5';
const only = args.only ? args.only.split(',') : null;
const here = path.dirname(fileURLToPath(import.meta.url));
const BENCH_DIR = process.env.BENCH_DIR || path.join(process.env.TMPDIR || '/tmp', 'bench');
const CDP = process.env.BENCH_CDP || '9333';
const PORT = Number(process.env.BENCH_PORT || 8765), base = `http://127.0.0.1:${PORT}`;
const work = mkdtempSync(path.join(tmpdir(), 'suite-'));
await startFixture(PORT);

// What the agent is told, per tool. Mewla's: the product's own delegation text.
const delegation = readFileSync(path.join(here, '../../../daemon/brain/templates/policies/delegation.md'), 'utf8')
  .split('\n').find(l => l.includes('mewla_chrome')) ?? '';
const SYSTEM = {
  mewla: `You are a Worker. Your browser tool is mewla_chrome (the browser_* tools): the user's own Chrome. ${delegation.replace(/^[^]*?When mewla agent-use status says your Chrome is ready, /, '')} Reply with only the answer when done.`,
  ab: `You are a Worker. Use the agent-browser CLI through Bash for all browser work; attach is preconfigured (no flags needed). Start with \`agent-browser skills get core\` as its own docs say. Close your tabs when done. Reply with only the answer when done.\n\n${readFileSync(path.join(process.env.HOME, '.claude/skills/agent-browser/SKILL.md'), 'utf8').replace(/^---[^]*?---\n/, '')}`,
};

function mcpConfig() {
  // BENCH_BRIDGE: the real chain (daemon/agentuse TestAgentUseBenchBridge): Mewla's extension, native host and bridge,
  // with the tools' argv and env exactly as the daemon builds them. Without it: Playwright's own extension (stand-in).
  const bridge = process.env.BENCH_BRIDGE ? JSON.parse(readFileSync(process.env.BENCH_BRIDGE, 'utf8')) : null;
  const argv = bridge ? bridge.argv : JSON.parse(process.env.BENCH_MCP);
  const env = bridge
    ? { ...Object.fromEntries(bridge.env.map(e => [e.slice(0, e.indexOf('=')), e.slice(e.indexOf('=') + 1)])), MEWLA_WORKER_ID: '%aub' }
    : { DISPLAY: process.env.DISPLAY || ':97', PLAYWRIGHT_MCP_EXTENSION_TOKEN: readFileSync(path.join(BENCH_DIR, 'ext-key'), 'utf8').trim() };
  const cfg = { mcpServers: { mewla_chrome: { command: 'node', args: [path.join(here, 'mewla-proxy.mjs'), '--', ...argv], env } } };
  const file = path.join(work, 'mcp.json'); writeFileSync(file, JSON.stringify(cfg)); return file;
}

function resetBrowser() {
  // Other tabs closed, like a fresh Worker's start; agent-browser's daemon too.
  try { execFileSync('agent-browser', ['close', '--all'], { stdio: 'ignore' }); } catch {}
  const list = JSON.parse(execFileSync('curl', ['-s', `localhost:${CDP}/json/list`]).toString());
  const pages = list.filter(t => t.type === 'page');
  for (const t of pages.slice(1)) execFileSync('curl', ['-s', `localhost:${CDP}/json/close/${t.id}`], { stdio: 'ignore' });
  if (pages[0]) execFileSync('curl', ['-s', `localhost:${CDP}/json/activate/${pages[0].id}`], { stdio: 'ignore' });
}

// --nonint: a person (agent-use/web/xuser.c) types one character a second into a window of its own in front of the
// agent's, while the agent works. The row then says what the agent did to the person: lost keyboard focus, a window
// raised over theirs, typing lost, the real pointer moved, their page hidden.
const NONINT = process.argv.includes('--nonint');
// --control: no agent. The probe alone for 20 s per rep: every criterion must read zero or the probe is measuring itself.
const CONTROL = process.argv.includes('--control');
const XUSER = process.env.BENCH_XUSER || path.join(here, '../agent-use/web/xuser');
async function cdpBrowser(method, params) {
  const url = JSON.parse(execFileSync('curl', ['-s', `localhost:${CDP}/json/version`]).toString()).webSocketDebuggerUrl;
  const ws = new WebSocket(url);
  await new Promise(r => { ws.onopen = r; });
  const reply = new Promise(res => { ws.onmessage = e => { const m = JSON.parse(e.data); if (m.id === 1) res(m); }; });
  ws.send(JSON.stringify({ id: 1, method, params }));
  const m = await reply; ws.close();
  return m.result;
}
// Every task starts from the same browser: one blank window, none left over from the last agent. (Opened first, the old
// ones closed after: Chrome exits when its last window closes.)
async function freshBrowser() {
  const { targetInfos } = await cdpBrowser('Target.getTargets');
  await cdpBrowser('Target.createTarget', { url: 'about:blank', newWindow: true });
  for (const t of targetInfos.filter(t => t.type === 'page')) await cdpBrowser('Target.closeTarget', { targetId: t.targetId }).catch(() => {});
  await new Promise(r => setTimeout(r, 500));
}
async function startUser() {
  const { targetId } = await cdpBrowser('Target.createTarget', { url: `${base}/user`, newWindow: true });
  const xu = spawn(XUSER, ['AUB user page', '1000'], { env: { ...process.env, DISPLAY: process.env.DISPLAY || ':97' }, stdio: ['ignore', 'pipe', 'inherit'] });
  const lines = []; xu.stdout.on('data', d => lines.push(...String(d).split('\n').filter(Boolean)));
  const t0 = Date.now();
  const exited = new Promise(r => xu.on('exit', r));
  await new Promise(r => setTimeout(r, 1500));
  return { targetId, xu, lines, exited, t0 };
}
async function stopUser(u) {
  u.xu.kill('SIGTERM'); await u.exited;
  await new Promise(r => setTimeout(r, 400)); // the last beacons
  // Before the window closes (closing it fires its own visibility event), and only from when the person started typing:
  // Chrome reports a freshly shown window as hidden for a moment, which says nothing about the agent.
  const seen = events.filter(e => e.at == null || e.at >= u.t0);
  await cdpBrowser('Target.closeTarget', { targetId: u.targetId }).catch(() => {});
  let s = {}; for (const l of u.lines) { try { const o = JSON.parse(l); if (o.ev === 'summary') s = o; } catch {} }
  const keys = seen.filter(e => e.type === 'user:key').length;
  const ni = { typed: s.typed ?? null, keys_received: keys, typing_lost: s.typed == null ? null : Math.max(0, s.typed - keys - 1),
    key_focus_lost: s.focus_away ?? null, raised_over_user: s.raised ?? null, raised_covering_user: s.raised_covering ?? null, covered_ms: s.covered_ms ?? null, raised_popup: s.raised_popup ?? null, pointer_moved: s.pointer_moves ?? null,
    user_page_hidden: seen.filter(e => e.type === 'user:vis' && e.state === 'hidden').length,
    measured: ['key_focus_lost', 'raised_over_user', 'typing_lost', 'pointer_moved', 'user_page_hidden'],
    not_measured: ['clipboard', 'space_switch', 'dialog_on_user', 'app_activation'] };
  if (process.env.AUB_DEBUG) ni.debug = { xuser: u.lines.filter(l => !l.includes('"ev":"type"')).slice(0, 60), events: seen.filter(e => e.type !== 'user:key').slice(0, 40).map(e => ({ ...e, dt: e.at - u.t0 })), t0: u.t0 };
  ni.undisturbed = s.typed != null && ni.measured.every(k => ni[k] === 0);
  return ni;
}

function runClaude(prompt) {
  const common = ['-p', prompt, '--model', model, '--output-format', 'stream-json', '--verbose', '--max-turns', '30',
    '--setting-sources', 'user', '--disable-slash-commands', '--no-session-persistence', '--permission-mode', 'bypassPermissions'];
  const sideArgs = side === 'mewla'
    ? ['--strict-mcp-config', '--mcp-config', mcpConfig(), '--tools', '', '--allowedTools', 'mcp__mewla_chrome', '--append-system-prompt', SYSTEM.mewla]
    : ['--strict-mcp-config', '--tools', 'Bash', '--allowedTools', 'Bash', '--append-system-prompt', SYSTEM.ab];
  const env = { ...process.env, DISPLAY: process.env.DISPLAY || ':97', AGENT_BROWSER_CDP: CDP, AGENT_BROWSER_SESSION: `suite-${Date.now()}` };
  const t0 = performance.now();
  return new Promise(resolve => {
    const p = spawn('claude', [...common, ...sideArgs], { cwd: work, env, stdio: ['ignore', 'pipe', 'pipe'] });
    let out = '', err = '';
    const timer = setTimeout(() => p.kill('SIGKILL'), 600000);
    p.stdout.on('data', d => out += d); p.stderr.on('data', d => err += d);
    p.on('exit', code => {
      clearTimeout(timer);
      const wall = performance.now() - t0;
      let result = null, tools = 0, firstToolMs = null;
      for (const line of out.split('\n')) {
        if (!line.trim()) continue;
        try {
          const m = JSON.parse(line);
          if (m.type === 'result') result = m;
          if (m.type === 'assistant') for (const c of m.message?.content ?? []) if (c.type === 'tool_use') tools++;
        } catch {}
      }
      if (args.keep) { try { writeFileSync(path.join(args.keep, `${side}-${Date.now()}.jsonl`), out); } catch {} }
      resolve({ code, wall, result, tools, err: err.slice(0, 300) });
    });
  });
}

const rows = [];
for (const task of tasks) {
  if (only && !only.includes(task.id)) continue;
  for (let rep = 0; rep < reps; rep++) {
   // The probe sometimes fails to start (the person's window was not found in time): that trial says nothing about the
   // agent, so it is run again, once, and the row says so.
   for (let attempt = 0; attempt < 2; attempt++) {
    events.length = 0; resetBrowser();
    if (NONINT) await freshBrowser();
    const user = NONINT ? await startUser() : null;
    const r = CONTROL ? await new Promise(res => setTimeout(() => res({ wall: 20000, result: { result: '' }, tools: 0 }), 20000)) : await runClaude(task.prompt.replaceAll('{base}', base));
    const nonint = user ? await stopUser(user) : undefined;
    if (nonint && nonint.typed == null && attempt === 0) { console.error(`probe did not run for ${task.id} #${rep}; running it again`); continue; }
    const answer = r.result?.result ?? '';
    const u = r.result?.usage ?? {};
    const ok = !!task.check(answer, events);
    const row = { task: task.id, kind: task.kind, rep, ok, nonint, full_pass: nonint ? ok && nonint.undisturbed : undefined, wall_ms: Math.round(r.wall), api_ms: r.result?.duration_api_ms ?? null,
      turns: r.result?.num_turns ?? null, tool_calls: r.tools, input: u.input_tokens, cache_read: u.cache_read_input_tokens, cache_create: u.cache_creation_input_tokens, output: u.output_tokens,
      cost_usd: r.result?.total_cost_usd ?? null, answer: answer.slice(0, 160), error: r.result ? undefined : r.err, probe_retries: attempt || undefined };
    rows.push(row);
    console.log(JSON.stringify(row));
    if (args.out) writeFileSync(args.out, JSON.stringify({ side, model, meta: JSON.parse(process.env.AUB_META || '{}'), rows }, null, 1));
    break;
   }
  }
}
const med = xs => { const s = xs.filter(x => x != null).sort((a, b) => a - b); return s.length ? s[Math.floor(s.length / 2)] : null; };
console.log('\nSUMMARY', side);
for (const t of [...new Set(rows.map(r => r.task))]) {
  const rs = rows.filter(r => r.task === t);
  console.log(t.padEnd(12), `ok ${rs.filter(r => r.ok).length}/${rs.length}`, `wall ${med(rs.map(r => r.wall_ms))}`, `turns ${med(rs.map(r => r.turns))}`, `tools ${med(rs.map(r => r.tool_calls))}`, `out ${med(rs.map(r => r.output))}`);
}
// The MCP config in the work dir can hold the tools' environment: it does not outlive the run.
try { rmSync(work, { recursive: true, force: true }); } catch {}
process.exit(0);
