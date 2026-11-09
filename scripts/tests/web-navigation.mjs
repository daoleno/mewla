// Browser regression against a built export, using an already paired, owned
// Chrome that exposes CDP (a mewla_chrome browser or a throwaway profile).
// Never point this at somebody else's browser.
// Usage: bun scripts/tests/web-navigation.mjs CDP_HOST:PORT ORIGIN [EVIDENCE_DIR]
// It reloads once to start cold, then rejects every additional JS request and
// switches pages twice, including while the browser's socket is disconnected.
import assert from "node:assert/strict";
import { mkdirSync, writeFileSync } from "node:fs";
import { join } from "node:path";

const [cdpHost, origin, evidenceDir] = process.argv.slice(2);
assert(cdpHost && origin, "Provide an owned, paired browser's CDP host:port and its origin");
if (evidenceDir) mkdirSync(evidenceDir, { recursive: true });
const targets = await (await fetch(`http://${cdpHost}/json/list`)).json();
const target = targets.find((page) => page.type === "page" && page.url.startsWith(origin));
assert(target, "The browser must already be open on the tested origin");
const socket = new WebSocket(target.webSocketDebuggerUrl);
await new Promise((resolve, reject) => { socket.onopen = resolve; socket.onerror = reject; });
let nextId = 0;
const pending = new Map();
const blockedScripts = [];
const errors = [];
const results = [];
let connection;
function cdp(method, params = {}) {
  return new Promise((resolve, reject) => {
    const id = ++nextId;
    pending.set(id, { resolve, reject });
    socket.send(JSON.stringify({ id, method, params }));
  });
}
socket.onmessage = (event) => {
  const message = JSON.parse(event.data);
  if (message.id) {
    const waiter = pending.get(message.id);
    pending.delete(message.id);
    if (message.error) waiter?.reject(new Error(JSON.stringify(message.error)));
    else waiter?.resolve(message.result);
  } else if (message.method === "Fetch.requestPaused") {
    blockedScripts.push(new URL(message.params.request.url).pathname);
    void cdp("Fetch.fulfillRequest", {
      requestId: message.params.requestId, responseCode: 404, body: "",
    });
  } else if (message.method === "Runtime.exceptionThrown") {
    errors.push(message.params.exceptionDetails.exception?.description || message.params.exceptionDetails.text);
  }
};
async function evaluate(expression) {
  const result = await cdp("Runtime.evaluate", { expression, returnByValue: true });
  assert(!result.exceptionDetails, JSON.stringify(result.exceptionDetails));
  return result.result.value;
}
async function until(expression, message, timeout = 15_000) {
  const deadline = Date.now() + timeout;
  while (Date.now() < deadline) {
    if (await evaluate(expression)) return;
    await new Promise((resolve) => setTimeout(resolve, 100));
  }
  throw new Error(message);
}
const pages = [
  ["Brain", "", "Tell Brain…"],
  ["Sessions", "list", "Session options"],
  ["Brain", "", "Tell Brain…"],
  ["Sessions", "list", "Session options"],
  ["Calendar", "calendar", "Add calendar item"],
  ["Extensions", "plugins", "Mewla plugins"],
  ["Screen", "screen", "Screen needs a Mac"],
  ["Stats", "stats", "Day range"],
  ["Resources", "resources", "Refresh resources"],
  ["Settings", "settings", "Computers"],
  ["Brain", "", "Tell Brain…"],
  ["Sessions", "list", "New session"],
];
// Both daemon fixtures in this regression run on Linux; Screen explains its
// Mac requirement. Friends is included when enabled on the current server.
let injection;
try {
  await cdp("Runtime.enable");
  await cdp("Page.enable");
  await cdp("Network.enable");
  injection = await cdp("Page.addScriptToEvaluateOnNewDocument", { source: `
    window.__navigationTest = { sockets: [], opened: 0, closed: 0 };
    window.WebSocket = class extends WebSocket {
      constructor(...args) {
        super(...args);
        const test = window.__navigationTest;
        test.sockets.push(this);
        this.addEventListener('open', () => test.opened++);
        this.addEventListener('close', () => test.closed++);
      }
    };
  ` });
  await ab("open", `${origin}/list`);
  await until("document.querySelector('[role=navigation]') && window.__navigationTest.opened > 0", "Paired app did not connect");
  await cdp("Network.setCacheDisabled", { cacheDisabled: true });
  if (await evaluate("Array.from(document.querySelectorAll('[role=navigation] [role=button]')).some(e => e.textContent === 'Friends')")) {
    pages.splice(5, 0, ["Friends", "friends", "Following"]);
  }
  await cdp("Fetch.enable", { patterns: [{ urlPattern: "*", resourceType: "Script", requestStage: "Request" }] });
  if (evidenceDir) mkdirSync(evidenceDir, { recursive: true });
  for (const phase of ["connected", "disconnected"]) {
    if (phase === "disconnected") {
      const closed = await evaluate("window.__navigationTest.closed");
      await evaluate("window.__navigationTest.sockets.forEach(s => s.close(4000, 'navigation regression'))");
      await until(`window.__navigationTest.closed > ${closed}`, "Socket did not close");
      await cdp("Network.emulateNetworkConditions", { offline: true, latency: 0, downloadThroughput: -1, uploadThroughput: -1 });
    }
    for (const [name, path, marker] of pages) {
      const start = Date.now();
      await ab("find", "role", ["Brain", "Sessions"].includes(name) ? "tab" : "button", "click", "--name", name);
      // Check the actual visible page body, not the sidebar or retained,
      // hidden history screens. A title alone does not pass.
      const ready = `location.pathname === ${JSON.stringify(`/${path}`)} && Array.from(document.querySelectorAll('body *')).some(e =>
        !e.closest('[role=navigation]') && e.checkVisibility({visibilityProperty:true}) &&
        (e.getAttribute('aria-label') === ${JSON.stringify(marker)} ||
         e.getAttribute('placeholder') === ${JSON.stringify(marker)} ||
         (e.children.length === 0 && (e.textContent === ${JSON.stringify(marker)} ||
           (${name === "Friends"} && e.textContent === 'Friends is off'))) ||
         (${phase === "disconnected"} && (e.getAttribute('role') === 'progressbar' ||
           (e.children.length === 0 && ['Loading services', 'Disconnected'].includes(e.textContent))))))`;
      await until(ready, `${phase}: ${name} has no visible page content`);
      results.push({ phase, page: name, elapsedMs: Date.now() - start });
      console.log(JSON.stringify(results.at(-1)));
      if (evidenceDir) await ab("screenshot", join(evidenceDir, `${phase}-${path || "brain"}.png`));
    }
  }
  const opened = await evaluate("window.__navigationTest.opened");
  await cdp("Network.emulateNetworkConditions", { offline: false, latency: 0, downloadThroughput: -1, uploadThroughput: -1 });
  await until(`window.__navigationTest.opened > ${opened}`, "Socket did not reconnect", 45_000);
  connection = await evaluate("({ opened: window.__navigationTest.opened, closed: window.__navigationTest.closed })");
  assert.deepEqual(blockedScripts, [], "Navigation tried to load code after the initial bundle");
  // Offline injection can time out an icon font still arriving through the
  // tunnel. Keep those errors in the evidence; they do not unmount a page.
  const fontTimeouts = errors.filter((error) => /Fetching the font family .* timed out/.test(error));
  assert.deepEqual(errors.filter((error) => !fontTimeouts.includes(error)), [], "Browser runtime errors");
  if (fontTimeouts.length) console.warn(`Font availability warnings recorded: ${fontTimeouts.length}`);
  console.log("PASS: cold/repeated page visits, failed-chunk isolation, disconnect and reconnect");
} finally {
  if (evidenceDir) writeFileSync(join(evidenceDir, "navigation.json"), JSON.stringify({ results, blockedScripts, errors, connection }, null, 2));
  await cdp("Fetch.disable");
  await cdp("Network.emulateNetworkConditions", { offline: false, latency: 0, downloadThroughput: -1, uploadThroughput: -1 });
  await cdp("Network.setCacheDisabled", { cacheDisabled: false });
  if (injection) await cdp("Page.removeScriptToEvaluateOnNewDocument", { identifier: injection.identifier });
  socket.close();
}
