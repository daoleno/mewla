// WebArena-Verified tasks for browser/suite.mjs (--tasks this file). Env:
//   WAV_TASKS  tasks.json from `wav.py sample`        WAV_PY   python of the venv that has webarena-verified
//   WAV_CFG    config.json from `wav.py site config`  WAV_CTL  port of proxy.py's control server
//   WAV_RUN    directory for each trial's response and HAR (kept for the audit)
// A task that changes something starts from a fresh site container; one that only reads or navigates does not.
import { readFileSync, writeFileSync, mkdirSync } from 'node:fs';
import { execFileSync } from 'node:child_process';
import path from 'node:path';
import { guard } from './guard.mjs';
import { fileURLToPath } from 'node:url';

const here = path.dirname(fileURLToPath(import.meta.url));
const PY = process.env.WAV_PY, CFG = process.env.WAV_CFG, CTL = process.env.WAV_CTL || '17999', RUN = process.env.WAV_RUN;
const def = JSON.parse(readFileSync(process.env.WAV_TASKS, 'utf8'));
const cfg = JSON.parse(readFileSync(CFG, 'utf8')).environments;
const URL_OF = Object.fromEntries(Object.entries(cfg).map(([k, v]) => [k, v.urls[0]]));
const ALLOWED_HOSTS = new Set(Object.values(URL_OF).map(u => new URL(u).host));
const ACCOUNT = {
  shopping: 'If a task needs an account, sign in on the store with email emma.lopez@gmail.com and password Password.123.',
  shopping_admin: '',
  reddit: 'If a task needs an account, sign in on the forum with username MarvelsGrantMan136 and password test1234.',
  gitlab: 'If a task needs an account, sign in on GitLab with username byteblaze and password hello1234.',
};
const render = u => u.replace(/__[A-Z_]+__/, m => URL_OF[m] ?? m);
const wav = (...a) => execFileSync(PY, [path.join(here, 'wav.py'), ...a], { encoding: 'utf8' });
const ctl = (method, p) => execFileSync('curl', ['-s', '-X', method, `http://127.0.0.1:${CTL}${p}`], { encoding: 'utf8', maxBuffer: 1 << 28 });

function prompt(t) {
  return `${t.intent}

Start here: ${t.start_urls.map(render).join(' , ')}
${ACCOUNT[t.site]} This is a local test copy of the site: use only it, through your browser tool, and do not look for the answer anywhere else.

This is a ${t.type} task: use "${t.type}" as task_type.

When you are done, reply with ONLY one JSON object and nothing else:
{"task_type": "RETRIEVE" | "MUTATE" | "NAVIGATE", "status": "SUCCESS" | "NOT_FOUND_ERROR" | "ACTION_NOT_ALLOWED_ERROR" | "PERMISSION_DENIED_ERROR" | "DATA_VALIDATION_ERROR" | "UNKNOWN_ERROR", "retrieved_data": [ ... ] or null, "error_details": null or a short reason}
RETRIEVE: you read information; retrieved_data is a list (numbers as numbers, also for a single value; a list of objects when the task names keys). MUTATE: you created, changed or deleted something; retrieved_data is null. NAVIGATE: you ended on the page asked for; retrieved_data is null. status is SUCCESS only when the objective was met.`;
}

function parseAnswer(text) {
  const t = String(text).trim().replace(/^```(?:json)?\s*/i, '').replace(/\s*```$/, '');
  try { return JSON.parse(t); } catch {}
  const i = t.lastIndexOf('{"'), j = t.lastIndexOf('}');
  if (i >= 0 && j > i) { try { return JSON.parse(t.slice(i, j + 1)); } catch {} }
  const a = t.indexOf('{'), b = t.lastIndexOf('}');
  if (a >= 0 && b > a) { try { return JSON.parse(t.slice(a, b + 1)); } catch {} }
  return null;
}

// Entry pages worth touching after a site starts (beyond the task's own start URLs).
const WARM = { shopping_admin: ['/admin/dashboard/', '/sales/order/', '/catalog/product/', '/customer/index/'], shopping: ['/', '/customer/account/login/', '/catalogsearch/result/?q=a'], reddit: ['/', '/login'], gitlab: ['/users/sign_in', '/explore'] };
let n = 0;
export default def.tasks.map(t => ({
  id: `wav-${t.id}`, kind: `${t.site}/${t.type}`, site: t.site, wav_id: t.id,
  prompt: prompt(t),
  async before() {
    if (t.type === 'MUTATE') wav('site', 'reset', t.site);
    // WAV_WARM=1: touch the pages a trial starts from before it begins, so the first page load of a freshly started site (Magento
    // compiles on first use: 5+ s for the first /admin) is not what the tool is judged on. A tool layer with a short action
    // deadline (the virtual screen's is 8 s) is otherwise lost on its first navigation. The requests are cleared from the HAR.
    if (process.env.WAV_WARM === '1') {
      const base = URL_OF[`__${t.site.toUpperCase()}__`];
      const pages = [...t.start_urls.map(render), ...(WARM[t.site] ?? []).map(p => base + p)];
      for (const u of new Set(pages)) { try { execFileSync('curl', ['-s', '-o', '/dev/null', '-m', '60', u]); } catch {} }
    }
    ctl('POST', '/clear');
  },
  async check(answer, _events, ctx) {
    const dir = path.join(RUN, `${String(++n).padStart(3, '0')}-${ctx.side}-${t.id}`);
    mkdirSync(dir, { recursive: true });
    const har = ctl('GET', '/har');
    writeFileSync(path.join(dir, 'network.har'), har);
    writeFileSync(path.join(dir, 'calls.json'), JSON.stringify(ctx.calls));
    const parsed = parseAnswer(answer);
    writeFileSync(path.join(dir, 'agent_response.json'), JSON.stringify(parsed ?? { task_type: t.type, status: 'UNKNOWN_ERROR', retrieved_data: null, error_details: 'no JSON in the final message: ' + String(answer).slice(0, 200) }));
    let score = 0, status = 'error', error = null;
    try {
      const out = JSON.parse(wav('judge', '--task', String(t.id), '--response', path.join(dir, 'agent_response.json'), '--har', path.join(dir, 'network.har'), '--config', CFG).trim().split('\n').pop());
      score = out.score; status = out.status; error = out.error;
    } catch (e) { error = String(e).slice(0, 200); }
    const violations = guard(ctx.calls, ctx.side, ALLOWED_HOSTS);
    const events = JSON.parse(har).log.entries.length;
    return { ok: score === 1 && violations.length === 0, extra: { wav_id: t.id, site: t.site, type: t.type, score, judge_status: status, judge_error: error, guard_violations: violations, parsed: parsed != null, har_events: events, dir } };
  },
}));
