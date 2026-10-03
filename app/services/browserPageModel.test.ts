import { expect, mock, test } from "bun:test";
mock.module("./auth", () => ({ buildAuthorizationHeader: async () => "signed-fixture" }));
mock.module("./pinnedTransport", () => ({ resolveStoredServerURL: async (server: { url: string }) => server.url }));
const { BrowserRequestError } = await import("./browser");
const { browserIssue, chooseBrowser, controlOffer, launchBrowserId, pickDefaultBrowser, recoveryLabel, supportsBrowserAttachment } = await import("./browserPageModel");

const resource = { id: "r", name: "Personal", state: "running", control: "idle", allow_agents: true };

test("failures read as product states and keep raw text only as diagnostics", () => {
  const missing = browserIssue(new BrowserRequestError("Browser request failed", 404, "404 page not found"), "home");
  expect(missing).toMatchObject({ title: "Browser isn't on this server yet", recovery: "retry" });
  expect(missing!.detail).not.toMatch(/json|daemon|socket|provider|http/i);
  expect(missing!.diagnostic).toBe("HTTP 404 · 404 page not found");
  expect(browserIssue(new BrowserRequestError("x", 401, "unauthorized"), "home")!.recovery).toBe("settings");
  expect(browserIssue(new Error("Browser is controlled by someone else"), "home")!.recovery).toBe("take_control");
  expect(browserIssue(new Error("Browser control changed; request control again"), "home")!.recovery).toBe("take_control");
  expect(browserIssue(new Error("Browser action could not be drained; stop and reopen this browser"), "home")!.recovery).toBe("restart");
  expect(browserIssue(new Error("Browser response timed out. Reconnect before continuing."), "home")!.recovery).toBe("reconnect");
  expect(browserIssue(new TypeError("Network request failed"), "home")!.title).toBe("Can't reach home");
  const parse = browserIssue(new SyntaxError("JSON Parse error: Unexpected character: p"), "home")!;
  expect(parse.title).toBe("That didn't finish");
  expect(`${parse.title} ${parse.detail}`).not.toMatch(/json|parse/i);
  expect(browserIssue(new Error("Browser request cancelled"), "home")).toBeNull();
  for (const recovery of ["retry", "reconnect", "take_control", "settings", "restart"] as const) expect(recoveryLabel(recovery)).toBeTruthy();
  expect(recoveryLabel("none")).toBeNull();
});

test("entering picks the remembered browser and claims input only when nobody holds it", () => {
  const closed = { ...resource, id: "a", state: "stopped" };
  const running = { ...resource, id: "b" };
  expect(pickDefaultBrowser([closed, running], "a")!.id).toBe("a");
  expect(pickDefaultBrowser([closed, running], "gone")!.id).toBe("b");
  expect(pickDefaultBrowser([closed], null)!.id).toBe("a");
  expect(pickDefaultBrowser([], null)).toBeNull();
  expect(controlOffer(resource)).toBe("acquire");
  expect(controlOffer({ ...resource, control: "agent" })).toBe("takeover");
  expect(controlOffer({ ...resource, control: "human" })).toBe("takeover");
  expect(controlOffer({ ...resource, control: "quiescing" })).toBe("wait");
  expect(controlOffer({ ...resource, state: "stopped" })).toBe("wait");
});

test("session launches attach Browser only for commands that support it", () => {
  expect(supportsBrowserAttachment("codex --model x")).toBe(true);
  expect(supportsBrowserAttachment("FOO=1 /usr/bin/claude")).toBe(true);
  expect(supportsBrowserAttachment("opencode")).toBe(false);
  expect(supportsBrowserAttachment("")).toBe(false);
});

test("a preselected browser never breaks unsupported launches or overrides a user's pick", () => {
  const initial = { automatic: false, chosen: false };
  const auto = chooseBrowser(initial, "b1", true);
  expect(launchBrowserId(auto, "codex")).toBe("b1");
  expect(launchBrowserId(auto, "bash")).toBeUndefined();
  const none = chooseBrowser(auto, undefined, false);
  expect(chooseBrowser(none, "b1", true)).toEqual(none);
  const explicit = chooseBrowser(initial, "b2", false);
  expect(launchBrowserId(explicit, "bash")).toBe("b2");
});

test("Browser screen has no per-Agent permission step and gates input on the server lease", async () => {
  const source = await Bun.file(new URL("../components/browser/BrowserScreen.tsx", import.meta.url)).text();
  expect(source).not.toMatch(/setStatus\(|\.message\)|JSON\.stringify\(value|<Switch/);
  expect(source).toContain("browserIssue(error, serverName)");
  // No background copy about what Agents may do later.
  expect(source).not.toMatch(/Agents (can|on this server)/);
  expect(source).toContain("lease(held) { if (active()) setMine(held); }");
  expect(source).toContain('message.type === "input" && message.input && mine');
  // Auto-claim waits for this connection's fresh state, never cached state.
  expect(source).toContain("if (!claimable) return;");
  expect(source).toContain('if (controlOffer(value) === "acquire") void acquire()');
  expect(source).not.toContain("controlOffer(known)");
  expect(source).toContain('Alert.alert(\n    `Delete “${resource.name}”?`');
});
