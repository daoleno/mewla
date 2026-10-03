import { expect, mock, test } from "bun:test";
mock.module("./auth", () => ({ buildAuthorizationHeader: async () => "signed-fixture" }));
mock.module("./pinnedTransport", () => ({ resolveStoredServerURL: async (server: { url: string }) => server.url }));
const { BrowserRequestError } = await import("./browser");
const { agentAvailability, browserIssue, browserPresence, recoveryLabel } = await import("./browserPageModel");

const resource = { id: "r", name: "Personal", state: "running", control: "idle", allow_agents: true };

test("failures read as product states and keep raw text only as diagnostics", () => {
  const missing = browserIssue(new BrowserRequestError("Browser request failed", 404, "404 page not found"), "home");
  expect(missing).toMatchObject({ title: "Browser isn’t on this server yet", recovery: "retry" });
  expect(missing!.detail).not.toMatch(/json|daemon|socket|provider|http/i);
  expect(missing!.diagnostic).toBe("HTTP 404 · 404 page not found");
  expect(browserIssue(new BrowserRequestError("x", 401, "unauthorized"), "home")!.recovery).toBe("settings");
  expect(browserIssue(new Error("Browser is controlled by someone else"), "home")!.recovery).toBe("take_control");
  expect(browserIssue(new Error("Browser control changed; request control again"), "home")!.recovery).toBe("take_control");
  expect(browserIssue(new Error("Browser action could not be drained; stop and reopen this browser"), "home")!.recovery).toBe("restart");
  expect(browserIssue(new Error("Browser response timed out. Reconnect before continuing."), "home")!.recovery).toBe("reconnect");
  expect(browserIssue(new TypeError("Network request failed"), "home")!.title).toBe("Can’t reach home");
  const parse = browserIssue(new SyntaxError("JSON Parse error: Unexpected character: p"), "home")!;
  expect(parse.title).toBe("That didn’t finish");
  expect(`${parse.title} ${parse.detail}`).not.toMatch(/json|parse/i);
  expect(browserIssue(new Error("Browser request cancelled"), "home")).toBeNull();
  for (const recovery of ["retry", "reconnect", "take_control", "settings", "restart"] as const) expect(recoveryLabel(recovery)).toBeTruthy();
  expect(recoveryLabel("none")).toBeNull();
});

test("presence distinguishes first use, own control, other control and Agent use", () => {
  expect(browserPresence({ ...resource, state: "stopped" }).label).toBe("Closed");
  expect(browserPresence(resource).label).toBe("Open");
  expect(browserPresence({ ...resource, control: "human" }, true).label).toBe("You’re in control");
  expect(browserPresence({ ...resource, control: "human" }, false).label).toBe("In use on another device");
  expect(browserPresence({ ...resource, control: "agent" }).label).toBe("Agent is using it");
  expect(browserPresence({ ...resource, state: "needs_restart" }).tone).toBe("warning");
  expect(agentAvailability({ ...resource, allow_agents: false }).label).toBe("Agent tasks can’t use it");
  expect(agentAvailability({ ...resource, state: "stopped" }).label).toMatch(/once it’s open/);
});

test("Browser screen renders mapped issues, never raw error text", async () => {
  const source = await Bun.file(new URL("../components/browser/BrowserScreen.tsx", import.meta.url)).text();
  expect(source).not.toMatch(/setStatus\(|\.message\)|JSON\.stringify\(value/);
  expect(source).toContain("browserIssue(error, serverName)");
  expect(source).toContain('accessibilityLabel={open ? "Hide details" : "Show details"}');
  expect(source).toContain('Alert.alert(\n    `Delete “${resource.name}”?`');
});
