// The site's visit counter (site/visit.js): what it sends, and that it sends nothing when off.
// Run: node scripts/tests/site_visit_test.mjs
import { readFileSync } from "node:fs";
const src = readFileSync(new URL("../../site/visit.js", import.meta.url), "utf8");
function run({ host = "mewla.ai", path = "/", referrer = "", dnt = null, gpc = false }) {
  const sent = [];
  const listeners = {};
  const env = {
    navigator: { doNotTrack: dnt, globalPrivacyControl: gpc, sendBeacon: (url, body) => { sent.push([url, body]); return true; } },
    window: {},
    location: { hostname: host, pathname: path },
    document: { referrer, addEventListener: (t, f) => { listeners[t] = f; } },
    Element: class {},
    URL,
  };
  new Function(...Object.keys(env), src)(...Object.values(env));
  return { sent, listeners };
}
const cases = [
  [{ path: "/" }, '{"page":"home","ref":"direct"}'],
  [{ path: "/docs/cli/", referrer: "https://news.ycombinator.com/item?id=1" }, '{"page":"docs","ref":"hackernews"}'],
  [{ path: "/privacy/zh/", referrer: "https://www.google.co.jp/" }, '{"page":"privacy","ref":"google"}'],
  [{ path: "/support/", referrer: "https://mewla.ai/" }, '{"page":"support","ref":"mewla"}'],
  [{ path: "/pets/x", referrer: "https://intranet.corp.example/secret" }, '{"page":"other","ref":"other"}'],
  [{ path: "/", referrer: "https://evilgithub.com/" }, '{"page":"home","ref":"other"}'],
];
let fail = 0;
for (const [input, want] of cases) {
  const { sent } = run(input);
  const got = sent[0]?.[1];
  if (got !== want || sent[0][0] !== "https://connect.mewla.ai/v1/visit") { fail++; console.log("FAIL", input, got); }
}
for (const input of [{ dnt: "1" }, { gpc: true }, { host: "localhost" }, { host: "daoleno.github.io" }]) {
  if (run(input).sent.length) { fail++; console.log("FAIL sent while off", input); }
}
if (fail) { console.log(`${fail} failed`); process.exit(1); }
console.log("all visit.js cases pass");
