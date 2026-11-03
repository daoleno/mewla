#!/usr/bin/env node
// Stand-in for agentuse.SuperviseMCP in the benchmark: runs the Playwright MCP
// command (argv after `--`) and adds Mewla's tool advice (read from
// daemon/agentuse/supervise.go) to the tool descriptions, as the product does.
import { spawn } from 'node:child_process';
import { readFileSync } from 'node:fs';
import { fileURLToPath } from 'node:url';
import path from 'node:path';

const src = readFileSync(path.join(path.dirname(fileURLToPath(import.meta.url)), '../../../daemon/agentuse/supervise.go'), 'utf8');
const advice = {};
for (const m of src.matchAll(/^\s*"(browser_\w+)":\s*"((?:[^"\\]|\\.)*)",?\s*$/gm)) advice[m[1]] = JSON.parse(`"${m[2]}"`);
const argv = process.argv.slice(process.argv.indexOf('--') + 1);
const child = spawn(argv[0], argv.slice(1), { stdio: ['pipe', 'pipe', 'inherit'] });
const lists = new Set();
let inBuf = '', outBuf = '';
process.stdin.on('data', d => {
  inBuf += d;
  let i;
  while ((i = inBuf.indexOf('\n')) >= 0) {
    const line = inBuf.slice(0, i + 1); inBuf = inBuf.slice(i + 1);
    try { const m = JSON.parse(line); if (m.method === 'tools/list') lists.add(m.id); } catch {}
    child.stdin.write(line);
  }
});
process.stdin.on('end', () => child.stdin.end());
child.stdout.on('data', d => {
  outBuf += d;
  let i;
  while ((i = outBuf.indexOf('\n')) >= 0) {
    let line = outBuf.slice(0, i + 1); outBuf = outBuf.slice(i + 1);
    if (lists.size && line.includes('"tools"')) {
      try {
        const m = JSON.parse(line);
        if (lists.delete(m.id) && m.result?.tools) {
          for (const t of m.result.tools) if (advice[t.name]) t.description = t.description ? `${t.description}\n\n${advice[t.name]}` : advice[t.name];
          line = JSON.stringify(m) + '\n';
        }
      } catch {}
    }
    process.stdout.write(line);
  }
});
child.on('exit', code => process.exit(code ?? 0));
