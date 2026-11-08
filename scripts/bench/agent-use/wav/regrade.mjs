#!/usr/bin/env node
// Re-apply guard.mjs to the rows of suite.mjs part files (their trial directories hold calls.json and the judge's score is
// in the row): rewrites extra.guard_violations and ok in place. Used by assemble.py so every arm is graded by one rule.
//   node regrade.mjs CONFIG.json PART.json [PART.json ...]
import { readFileSync, writeFileSync, existsSync } from 'node:fs';
import path from 'node:path';
import { guard } from './guard.mjs';
const [cfgFile, ...parts] = process.argv.slice(2);
const cfg = JSON.parse(readFileSync(cfgFile, 'utf8')).environments;
const hosts = new Set(Object.values(cfg).map(v => new URL(v.urls[0]).host));
let changed = 0, rows = 0;
for (const f of parts) {
  const d = JSON.parse(readFileSync(f, 'utf8'));
  for (const r of d.rows) {
    const e = r.extra; if (!e?.dir) continue;
    const cf = path.join(e.dir, 'calls.json'); if (!existsSync(cf)) { console.error('no calls.json for', r.task, e.dir); continue; }
    rows++;
    const v = guard(JSON.parse(readFileSync(cf, 'utf8')), d.side, hosts);
    const ok = e.score === 1 && v.length === 0;
    if (ok !== r.ok || v.length !== (e.guard_violations || []).length) changed++;
    e.guard_violations = v; r.ok = ok; r.full_pass = r.nonint ? ok && r.nonint.undisturbed : undefined;
  }
  writeFileSync(f, JSON.stringify(d, null, 1));
}
console.error(`regraded ${rows} rows, ${changed} changed`);
