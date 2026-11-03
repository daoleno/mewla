#!/usr/bin/env node
// Task suite: a real agent (claude -p, same model) does each task with one
// tool. Wall-clock, turns, tokens and success come from the run itself.
//   node suite.mjs --side mewla|ab [--reps 3] [--only id,id] [--out file.json] [--model claude-sonnet-5-5]
// Env: BENCH_DIR (env.sh), and for the mewla side BENCH_MCP (argv JSON of the Playwright MCP command).
import { spawn, execFileSync } from 'node:child_process';
import { writeFileSync, mkdtempSync, readFileSync } from 'node:fs';
import { tmpdir } from 'node:os';
import path from 'node:path';
import { fileURLToPath } from 'node:url';
import { startFixture, events } from './fixture.mjs';
import { tasks } from './tasks.mjs';

const args = Object.fromEntries(process.argv.slice(2).reduce((a, v, i, all) => (v.startsWith('--') ? [...a, [v.slice(2), all[i + 1]]] : a), []));
const side = args.side, reps = Number(args.reps || 3), model = args.model || 'claude-sonnet-5-5';
const only = args.only ? args.only.split(',') : null;
const here = path.dirname(fileURLToPath(import.meta.url));
const BENCH_DIR = process.env.BENCH_DIR || path.join(process.env.TMPDIR || '/tmp', 'bench');
const CDP = process.env.BENCH_CDP || '9333';
const PORT = 8765, base = `http://127.0.0.1:${PORT}`;
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
  const argv = JSON.parse(process.env.BENCH_MCP);
  const cfg = { mcpServers: { mewla_chrome: { command: 'node', args: [path.join(here, 'mewla-proxy.mjs'), '--', ...argv], env: { DISPLAY: process.env.DISPLAY || ':97', PLAYWRIGHT_MCP_EXTENSION_TOKEN: readFileSync(path.join(BENCH_DIR, 'ext-key'), 'utf8').trim() } } } };
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
      resolve({ code, wall, result, tools, err: err.slice(0, 300) });
    });
  });
}

const rows = [];
for (const task of tasks) {
  if (only && !only.includes(task.id)) continue;
  for (let rep = 0; rep < reps; rep++) {
    events.length = 0; resetBrowser();
    const r = await runClaude(task.prompt.replaceAll('{base}', base));
    const answer = r.result?.result ?? '';
    const u = r.result?.usage ?? {};
    const row = { task: task.id, kind: task.kind, rep, ok: !!task.check(answer, events), wall_ms: Math.round(r.wall), api_ms: r.result?.duration_api_ms ?? null,
      turns: r.result?.num_turns ?? null, tool_calls: r.tools, input: u.input_tokens, cache_read: u.cache_read_input_tokens, cache_create: u.cache_creation_input_tokens, output: u.output_tokens,
      cost_usd: r.result?.total_cost_usd ?? null, answer: answer.slice(0, 160), error: r.result ? undefined : r.err };
    rows.push(row);
    console.log(JSON.stringify(row));
    if (args.out) writeFileSync(args.out, JSON.stringify({ side, model, rows }, null, 1));
  }
}
const med = xs => { const s = xs.filter(x => x != null).sort((a, b) => a - b); return s.length ? s[Math.floor(s.length / 2)] : null; };
console.log('\nSUMMARY', side);
for (const t of [...new Set(rows.map(r => r.task))]) {
  const rs = rows.filter(r => r.task === t);
  console.log(t.padEnd(12), `ok ${rs.filter(r => r.ok).length}/${rs.length}`, `wall ${med(rs.map(r => r.wall_ms))}`, `turns ${med(rs.map(r => r.turns))}`, `tools ${med(rs.map(r => r.tool_calls))}`, `out ${med(rs.map(r => r.output))}`);
}
process.exit(0);
