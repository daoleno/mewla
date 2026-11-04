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
const compact = new Set();
// Mirror of daemon/agentuse/snapshot_compact.go.
const ROLES = new Set(['button','link','textbox','checkbox','radio','combobox','searchbox','slider','spinbutton','switch','tab','menuitem','menuitemcheckbox','menuitemradio','option','treeitem','listbox']);
const VALUE_ROLES = new Set(['textbox','combobox','searchbox','spinbutton','slider']);
const LINE = /^\s*- ([a-z]+)(?: ("(?:[^"\\]|\\.)*"))?((?: \[[^\]]*\])*)(?::(.*))?$/;
const KEPT = /\[(ref=[^\]]*|level=[^\]]*|checked|disabled|expanded|selected|pressed|required|readonly)\]/g;
function compactSnapshot(text) {
  const start = text.indexOf('```yaml\n');
  if (start < 0) return text;
  const body = text.slice(start + 8);
  const end = body.lastIndexOf('\n```');
  if (end < 0) return text;
  const out = []; let total = 0;
  for (const line of body.slice(0, end).split('\n')) {
    const m = LINE.exec(line); if (!m) continue;
    const [, role, name, attrs, tail] = m;
    if (!attrs.includes('[ref=')) continue;
    total++;
    if (!ROLES.has(role) && role !== 'heading') continue;
    let b = '- ' + role + (name ? ' ' + name : '') + (attrs.match(KEPT) || []).map(a => ' ' + a).join('');
    if (VALUE_ROLES.has(role) && tail && tail.trim()) b += ': ' + tail.trim();
    out.push(b);
  }
  return text.slice(0, start) + '```yaml\n' + out.join('\n') + body.slice(end) + `\n(Controls and headings only: ${out.length} of ${total} elements. Call browser_snapshot without interactive for everything, or with target for one part.)`;
}
let inBuf = '', outBuf = '';
process.stdin.on('data', d => {
  inBuf += d;
  let i;
  while ((i = inBuf.indexOf('\n')) >= 0) {
    const line = inBuf.slice(0, i + 1); inBuf = inBuf.slice(i + 1);
    let forward = line;
    try {
      const m = JSON.parse(line);
      if (m.method === 'tools/list') lists.add(m.id);
      if (m.method === 'tools/call' && m.params?.name === 'browser_snapshot' && m.params.arguments && 'interactive' in m.params.arguments) {
        if (m.params.arguments.interactive === true) compact.add(m.id);
        delete m.params.arguments.interactive;
        forward = JSON.stringify(m) + '\n';
      }
    } catch {}
    child.stdin.write(forward);
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
          for (const t of m.result.tools) if (advice[t.name]) {
            t.description = t.description ? `${t.description}\n\n${advice[t.name]}` : advice[t.name];
            if (t.name === 'browser_snapshot') (t.inputSchema.properties ??= {}).interactive = { type: 'boolean', description: 'interactive' };
          }
          line = JSON.stringify(m) + '\n';
        }
      } catch {}
    }
    if (compact.size && line.includes('"result"')) {
      try {
        const m = JSON.parse(line);
        if (compact.delete(m.id)) {
          for (const part of m.result?.content ?? []) if (part.type === 'text') part.text = compactSnapshot(part.text);
          line = JSON.stringify(m) + '\n';
        }
      } catch {}
    }
    process.stdout.write(line);
  }
});
child.on('exit', code => process.exit(code ?? 0));
