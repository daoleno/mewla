#!/usr/bin/env node
// Task suite: a real agent (claude -p, same model) does each task with one
// tool. Wall-clock, turns, tokens and success come from the run itself.
//   node suite.mjs --side mewla|ab [--reps 3] [--only id,id] [--out file.json] [--model claude-sonnet-5-5]
// Env: BENCH_DIR (env.sh), and for the mewla side BENCH_MCP (argv JSON of the Playwright MCP command).
import { spawn, execFileSync } from 'node:child_process';
import { writeFileSync, mkdtempSync, readFileSync, rmSync, appendFileSync, existsSync, mkdirSync } from 'node:fs';
import { tmpdir } from 'node:os';
import path from 'node:path';
import { fileURLToPath } from 'node:url';
import { startFixture, events } from './fixture.mjs';
import { tasks as baseTasks, extraTasks } from './tasks.mjs';
import { longTasks } from './tasks-long.mjs';
const args = Object.fromEntries(process.argv.slice(2).reduce((a, v, i, all) => (v.startsWith('--') ? [...a, [v.slice(2), all[i + 1]]] : a), []));
// --long runs only the longer tasks; --only picks by id from both sets.
// --tasks FILE.mjs: another suite (default export: [{id, kind, prompt, before?, check(answer, events, ctx), after?}]) instead of the fixture ones.
const tasks = args.tasks ? (await import(path.resolve(args.tasks))).default : [...(process.argv.includes('--long') ? [] : baseTasks), ...(process.argv.includes('--long') || process.argv.includes('--with-long') ? longTasks : []), ...extraTasks.filter(t => (process.argv[process.argv.indexOf('--only') + 1] || '').split(',').includes(t.id))];

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
  // The Mewla Browser of a Linux computer's virtual screen (daemon/browser): one `browser` tool; the text is the tool's own.
  vscreen: `You are a Worker. Your browser tool is browser (the Mewla Browser): a browser of your own on a virtual screen. Request control first, then work. Reply with only the answer when done.`,
  // browser-use's MCP server, as its CLI/MCP ships; its two tools that call a model of their own are off (the model is the one under test).
  bu: `You are a Worker. Your browser tool is browser-use (the browser_* tools): a browser you drive by element index. Reply with only the answer when done.`,
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

// vscreen: the daemon's own `mewla browser mcp` for a Worker the isolated daemon holds (VS_BIN, VS_STATE, VS_WORKER, VS_BROWSER:
// wav/vscreen.sh), the argv and entry the daemon builds for a real Worker, minus its desktop `display` tool.
// VS_ENV_JSON (vscreen.sh's env.json) is read each time: a restart of the isolated daemon gives a new Worker and browser id.
function vscreenEnv() {
  if (process.env.VS_ENV_JSON) { try { return JSON.parse(readFileSync(process.env.VS_ENV_JSON, 'utf8')); } catch {} }
  return { VS_BIN: process.env.VS_BIN, VS_STATE: process.env.VS_STATE, VS_HOME: process.env.VS_HOME, VS_WORKER: process.env.VS_WORKER, VS_BROWSER: process.env.VS_BROWSER };
}
function vscreenEntry() {
  const v = vscreenEnv();
  return { command: v.VS_BIN, args: ['browser', 'mcp', '--id', v.VS_BROWSER, '--state-dir', v.VS_STATE],
    env: { PATH: process.env.PATH, HOME: v.VS_HOME || process.env.HOME, MEWLA_WORKER_ID: v.VS_WORKER } };
}
// bu: browser-use attached to the same Chrome over CDP as agent-browser.
function buEntry() {
  const dir = path.join(work, 'bu-config'); mkdirSync(dir, { recursive: true });
  writeFileSync(path.join(dir, 'config.json'), JSON.stringify({ browser_profile: { p1: { id: 'p1', default: true, headless: false, cdp_url: `http://127.0.0.1:${CDP}` } }, llm: { l1: { id: 'l1', default: true, model: 'none', api_key: 'none' } }, agent: { a1: { id: 'a1', default: true } } }));
  return { command: process.env.BU_PY, args: ['-m', 'browser_use.mcp'], env: { PATH: process.env.PATH, HOME: process.env.HOME, DISPLAY: process.env.DISPLAY || ':97', BROWSER_USE_CONFIG_DIR: dir, ANONYMIZED_TELEMETRY: 'false', BROWSER_USE_CLOUD_SYNC: 'false', BROWSER_USE_LOGGING_LEVEL: 'warning' } };
}
function sideMcpConfig(name, entry) {
  const file = path.join(work, `mcp-${name}.json`); writeFileSync(file, JSON.stringify({ mcpServers: { [name]: entry } })); return file;
}
// Before each vscreen trial the browser's page is sent somewhere neutral (there is no close-tab action to clean up with).
// It also says whether the browser is alive: the runtime of an isolated daemon that has fallen into "could not be drained" or
// "unavailable" stays that way, as it would for a person, until it is restarted.
async function vscreenReset() {
  const { McpClient } = await import('./mcpclient.mjs');
  const e = vscreenEntry();
  const c = new McpClient([e.command, ...e.args], e.env);
  try {
    await c.start();
    const a = await c.call('browser', { action: 'control' });
    const b = a.isError ? a : await c.call('browser', { action: 'navigate', url: `${base}/` });
    await c.call('browser', { action: 'release' });
    return !a.isError && !b.isError;
  } catch { return false; } finally { await c.close(); }
}
let vscreenRestarts = 0;
function vscreenRestart(why) {
  vscreenRestarts++; console.error(`virtual-screen browser restart #${vscreenRestarts}: ${why}`);
  execFileSync('sh', ['-c', process.env.VS_RESTART], { stdio: ['ignore', 'ignore', 'inherit'], timeout: 180000 });
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
  const common = ['-p', prompt, '--model', model, '--output-format', 'stream-json', '--verbose', '--max-turns', String(args['max-turns'] || 30),
    '--setting-sources', 'user', '--disable-slash-commands', '--no-session-persistence', '--permission-mode', 'bypassPermissions'];
  const sideArgs = side === 'vscreen'
    ? ['--strict-mcp-config', '--mcp-config', sideMcpConfig('mewla_browser', vscreenEntry()), '--tools', '', '--allowedTools', 'mcp__mewla_browser__browser', '--append-system-prompt', SYSTEM.vscreen]
    : side === 'bu'
    ? ['--strict-mcp-config', '--mcp-config', sideMcpConfig('browser_use', buEntry()), '--tools', '', '--allowedTools', 'mcp__browser_use', '--disallowedTools', 'mcp__browser_use__browser_extract_content,mcp__browser_use__retry_with_browser_use_agent', '--append-system-prompt', SYSTEM.bu]
    : side === 'mewla'
    ? ['--strict-mcp-config', '--mcp-config', mcpConfig(), '--tools', '', '--allowedTools', 'mcp__mewla_chrome', '--append-system-prompt', SYSTEM.mewla]
    : ['--strict-mcp-config', '--tools', 'Bash', '--allowedTools', 'Bash', '--append-system-prompt', SYSTEM.ab];
  const env = { ...process.env, DISPLAY: process.env.DISPLAY || ':97', AGENT_BROWSER_CDP: CDP, AGENT_BROWSER_SESSION: `suite-${Date.now()}` };
  const t0 = performance.now();
  return new Promise(resolve => {
    const p = spawn('claude', [...common, ...sideArgs], { cwd: work, env, stdio: ['ignore', 'pipe', 'pipe'] });
    let out = '', err = '';
    const timer = setTimeout(() => p.kill('SIGKILL'), Number(args['trial-timeout'] || 600) * 1000);
    p.stdout.on('data', d => out += d); p.stderr.on('data', d => err += d);
    p.on('exit', code => {
      clearTimeout(timer);
      const wall = performance.now() - t0;
      let result = null, tools = 0, firstToolMs = null; const calls = [];
      for (const line of out.split('\n')) {
        if (!line.trim()) continue;
        try {
          const m = JSON.parse(line);
          if (m.type === 'result') result = m;
          if (m.type === 'assistant') for (const c of m.message?.content ?? []) if (c.type === 'tool_use') { tools++; calls.push({ name: c.name, input: c.input }); }
        } catch {}
      }
      if (args.keep) { try { writeFileSync(path.join(args.keep, `${side}-${Date.now()}.jsonl`), out); } catch {} }
      resolve({ code, wall, result, tools, calls, err: err.slice(0, 300) });
    });
  });
}

// Spend guard shared by every run of a benchmark: AUB_COST_LEDGER is a file the cost of each trial is appended to; once
// the total reaches AUB_COST_STOP (US dollars) no further trial starts.
const LEDGER = process.env.AUB_COST_LEDGER, STOP = Number(process.env.AUB_COST_STOP || 0);
const spent = () => (LEDGER && existsSync(LEDGER) ? readFileSync(LEDGER, 'utf8').split('\n').filter(Boolean).reduce((a, x) => a + Number(x), 0) : 0);
const rows = [];
for (const task of tasks) {
  if (only && !only.includes(task.id)) continue;
  for (let rep = 0; rep < reps; rep++) {
   if (LEDGER && STOP && spent() >= STOP) { console.error(`cost stop: $${spent().toFixed(2)} spent, limit $${STOP}`); process.exit(3); }
   // The probe sometimes fails to start (the person's window was not found in time): that trial says nothing about the
   // agent, so it is run again, once, and the row says so.
   for (let attempt = 0; attempt < 2; attempt++) {
    events.length = 0; resetBrowser();
    if (side === 'vscreen' && !(await vscreenReset())) { vscreenRestart('not answering before the trial'); if (!(await vscreenReset())) console.error('virtual-screen browser still not answering after a restart'); }
    if (task.before) await task.before();
    if (NONINT) await freshBrowser();
    const user = NONINT ? await startUser() : null;
    const r = CONTROL ? await new Promise(res => setTimeout(() => res({ wall: 20000, result: { result: '' }, tools: 0 }), 20000)) : await runClaude(task.prompt.replaceAll('{base}', base));
    const nonint = user ? await stopUser(user) : undefined;
    if (nonint && nonint.typed == null && attempt === 0) { if (LEDGER && r.result?.total_cost_usd) appendFileSync(LEDGER, r.result.total_cost_usd + '\n'); console.error(`probe did not run for ${task.id} #${rep}; running it again`); continue; }
    const answer = r.result?.result ?? '';
    const u = r.result?.usage ?? {};
    // A suite's check may be async and may return { ok, extra }: extra is kept on the row (the judge's score, guard violations).
    const verdict = await task.check(answer, events, { calls: r.calls ?? [], side, wall: r.wall });
    const ok = !!(verdict && typeof verdict === 'object' ? verdict.ok : verdict);
    if (task.after) await task.after();
    // A trial that failed and left the browser dead says nothing about the tool's page handling: run it again once on a
    // restarted browser. The row (and the report) counts the deaths either way.
    let died = false;
    if (side === 'vscreen') {
      died = !(await vscreenReset());
      if (died) {
        if (LEDGER && r.result?.total_cost_usd) appendFileSync(LEDGER, r.result.total_cost_usd + '\n');
        vscreenRestart(`dead after ${task.id} #${rep} (${ok ? 'trial passed' : 'trial failed'})`);
        if (!ok && attempt === 0) continue;
      }
    }
    if (verdict && typeof verdict === 'object' && verdict.extra && side === 'vscreen') Object.assign(verdict.extra, { runtime_died: died || undefined, runtime_restarts_so_far: vscreenRestarts });
    const row = { task: task.id, kind: task.kind, rep, ok, nonint, full_pass: nonint ? ok && nonint.undisturbed : undefined, wall_ms: Math.round(r.wall), api_ms: r.result?.duration_api_ms ?? null,
      turns: r.result?.num_turns ?? null, tool_calls: r.tools, input: u.input_tokens, cache_read: u.cache_read_input_tokens, cache_create: u.cache_creation_input_tokens, output: u.output_tokens,
      cost_usd: r.result?.total_cost_usd ?? null, answer: answer.slice(0, args.tasks ? 600 : 160), extra: verdict && typeof verdict === 'object' ? verdict.extra : undefined, timed_out: r.result ? undefined : true, error: r.result ? undefined : r.err, probe_retries: attempt || undefined };
    rows.push(row);
    if (LEDGER && row.cost_usd) appendFileSync(LEDGER, row.cost_usd + '\n');
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
