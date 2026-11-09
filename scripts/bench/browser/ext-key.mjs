// Print the Playwright extension's PLAYWRIGHT_MCP_EXTENSION_TOKEN, read from its
// status page over the CDP websocket of a bench Chrome.
// Usage: node ext-key.mjs CDP_PORT EXTENSION_ID
import assert from "node:assert/strict";

const [port, extId] = process.argv.slice(2);
assert(port && extId, "Usage: ext-key.mjs CDP_PORT EXTENSION_ID");
const { webSocketDebuggerUrl } = await (await fetch(`http://127.0.0.1:${port}/json/version`)).json();
const socket = new WebSocket(webSocketDebuggerUrl);
await new Promise((resolve, reject) => { socket.onopen = resolve; socket.onerror = reject; });
let nextId = 0;
const pending = new Map();
socket.onmessage = (event) => {
  const message = JSON.parse(event.data);
  const waiter = pending.get(message.id);
  if (!waiter) return;
  pending.delete(message.id);
  if (message.error) waiter.reject(new Error(JSON.stringify(message.error)));
  else waiter.resolve(message.result);
};
const cdp = (method, params = {}, sessionId) => new Promise((resolve, reject) => {
  const id = ++nextId;
  pending.set(id, { resolve, reject });
  socket.send(JSON.stringify({ id, method, params, sessionId }));
});

const { targetId } = await cdp("Target.createTarget", {
  url: `chrome-extension://${extId}/status.html`,
});
let token = "";
try {
  const { sessionId } = await cdp("Target.attachToTarget", { targetId, flatten: true });
  for (const deadline = Date.now() + 15_000; !token && Date.now() < deadline;) {
    const { result } = await cdp("Runtime.evaluate", {
      expression: "document.body ? document.body.innerText : ''", returnByValue: true,
    }, sessionId);
    token = /PLAYWRIGHT_MCP_EXTENSION_TOKEN=([A-Za-z0-9_=-]+)/.exec(result.value ?? "")?.[1] ?? "";
    if (!token) await new Promise((resolve) => setTimeout(resolve, 250));
  }
} finally {
  await cdp("Target.closeTarget", { targetId }).catch(() => {});
  socket.close();
}
assert(token, "The extension's status page showed no token");
console.log(token);
