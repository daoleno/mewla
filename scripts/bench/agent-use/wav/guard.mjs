// What a trial may not do, one rule for every tool layer (used by tasks.mjs while a run goes and by regrade.mjs afterwards):
//  1. look for the answers: the dataset, the package, this repository, the public dataset (any tool call that names them);
//  2. leave the sites: navigate (a url field, `agent-browser open`, fetch()) to a host that is not one of the benchmark's;
//  3. get at the sites around its tool: curl/wget/nc/python http clients (the agent-browser arm's only tool is Bash).
// Scripts run in the page (fetch/XHR through browser_run_code_unsafe, `agent-browser eval`, `browser_evaluate`) are the model's
// own way of using its tool and are not a violation for any arm; the report says how many trials used them.
export function guard(calls, side, allowedHosts) {
  const bad = [];
  const ok = h => allowedHosts.has(h.replace(/[;,)\].'"]+$/, ''));
  for (const c of calls) {
    const s = JSON.stringify(c.input ?? {});
    if (/file:|hard\.json|webarena-verified|webarena_verified|site-packages|\.mewla|huggingface|github\.com\/(ServiceNow|web-arena)|workspace\/mewla|\/etc\/|\/home\/(?![^\"\\ ]*\/\.claude\/projects\/)/i.test(s)) bad.push(`${c.name}: looks for the answers or the machine: ${s.slice(0, 120)}`);
    const urls = [];
    if (typeof c.input?.url === 'string') urls.push(c.input.url);
    const cmd = String(c.input?.command ?? '');
    for (const m of cmd.matchAll(/agent-browser\b[^;&|\n]*?\b(?:open|goto|navigate)\s+["']?(https?:\/\/[^\s"']+)/g)) urls.push(m[1]);
    for (const m of s.matchAll(/(?:fetch|goto)\(\s*\\?["'`](https?:\/\/[^\s"'`\\]+)/g)) urls.push(m[1]);
    for (const u of urls.filter(u => /^https?:\/\//i.test(u))) { const h = u.replace(/^https?:\/\//, '').split(/[\/?#]/)[0]; if (!ok(h)) bad.push(`${c.name}: left the sites for ${h}`); }
    if (side === 'ab' && c.name === 'Bash' && /\b(curl|wget)\b|(^|[\s;&|(])nc\s|(^|[\s;&|(])python3?\s.*\b(urllib|requests|httpx|http\.client)\b/.test(cmd)) bad.push(`Bash: around the tool: ${cmd.slice(0, 120)}`);
  }
  return [...new Set(bad)];
}
