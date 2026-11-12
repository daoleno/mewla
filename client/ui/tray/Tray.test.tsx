import { describe, expect, open, test } from "nsh/test";

const HARNESS = "./testing/Harness.tsx";
const NOW = Date.now();

const worker = (id: string, patch: Record<string, unknown> = {}) => ({
  key: `s1:${id}`, id, serverId: "s1", serverName: "atlas", serverUrl: "ws://x", name: id, status: "running",
  cwd: "/home/me/mewla", command: "claude", provider: "claude", summary: "", last_output_lines: [], updated_at: NOW - 60_000, ...patch,
});

const permission = (id: string) => ({ id, kind: "permission", provider: "claude", title: "Run this command?", request: "git push", options: [{ number: 1, label: "Yes" }, { number: 2, label: "No" }] });

describe("the tray", () => {
  test("the state control and the tray's title say the same count", async () => {
    const app = await open(HARNESS, { host: { params: { workers: [worker("alpha"), worker("beta"), worker("gamma", { status: "done" })] } }, platform: "android" });
    await app.waitFor("text:2 running");
    await app.press("id:state-control");
    await app.waitFor("id:tray");
    expect(app.texts().filter((t) => t === "2 running").length).toBe(2);
    expect(app.count("id:tray-running >> id:tray-row")).toBe(2);
    expect(app.shows("id:tray-done")).toBe(true);
  }, 20_000);

  test("a running Worker's reported phase draws its progress bar; a Session that reports none has no bar", async () => {
    const app = await open(HARNESS, { host: { params: { workers: [worker("alpha", { phase: "verifying" }), worker("beta")] } }, platform: "android" });
    await app.waitFor("text:2 running");
    await app.press("id:state-control");
    await app.waitFor("id:tray");
    expect(app.count("id:tray-progress")).toBe(1);
    expect(app.shows("label:Step 4 of 5")).toBe(true);
  }, 20_000);

  test("a request is counted, needs you outranks running, and a plain yes/no gets Approve", async () => {
    const app = await open(HARNESS, { host: { params: { workers: [worker("alpha"), worker("beta", { status: "blocked", prompt: permission("p1") })] } }, platform: "android" });
    await app.waitFor("text:1 needs you");
    await app.press("id:state-control");
    await app.waitFor("id:tray-needs");
    expect(app.shows("id:tray-approve")).toBe(true);
    expect(app.texts()).toContain("git push");
    expect(app.count("id:tray-running >> id:tray-row")).toBe(1);
  }, 20_000);

  test("Back closes the tray on home; on a page pushed from it, Back returns to the open tray", async () => {
    const app = await open(HARNESS, { host: { params: { workers: [worker("alpha")] } }, platform: "android" });
    await app.waitFor("text:1 running");
    await app.press("id:state-control");
    await app.waitFor("id:tray");
    await app.press("id:tray-running >> id:tray-row");
    await app.waitFor("id:session-page");
    await app.back();
    await app.waitUntil(() => !app.shows("id:session-page"));
    // The tray is still open under the Session, as it was left.
    expect(app.shows("id:tray")).toBe(true);
    await app.back();
    await app.waitUntil(() => !app.shows("id:tray"));
    expect(app.shows("id:state-control")).toBe(true);
  }, 20_000);

  test("offline through the relay, the notice dates the computer's own last word on it", async () => {
    const at = new Date(Date.now() - 3_600_000).toISOString();
    const app = await open(HARNESS, { host: { params: {
      server: { transportKind: "link" },
      computers: { computers: [], open_requests: 0, connection: { phase: "offline", cause: "relay unreachable", last_failure: "relay unreachable", last_failure_at: at } },
    } }, platform: "android" });
    await app.wait(300);
    await app.host.send("app.connection", "waiting");
    await app.waitFor("text:Offline");
    await app.press("id:state-control");
    await app.waitFor("id:tray-notice");
    expect(app.texts().some((t) => t.startsWith("Relay unreachable · "))).toBe(true);
  }, 20_000);

  test("the scrim closes it; offline shows the notice and dims the lists", async () => {
    const app = await open(HARNESS, { host: { params: { workers: [worker("alpha")], connection: "waiting" } }, platform: "android" });
    await app.waitFor("text:Offline");
    await app.press("id:state-control");
    await app.waitFor("id:tray-notice");
    expect(app.texts()).toContain("atlas is offline");
    expect(app.shows("id:tray-notice-try_again")).toBe(true);
    await app.press("id:tray-scrim");
    await app.waitUntil(() => !app.shows("id:tray"));
  }, 20_000);
});
