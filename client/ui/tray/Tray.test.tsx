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

  test("the header's options glyph opens Sort and Services; + is New session", async () => {
    const app = await open(HARNESS, { host: { params: { workers: [worker("alpha")] } }, platform: "android" });
    await app.waitFor("text:1 running");
    await app.press("id:state-control");
    await app.waitFor("id:tray");
    expect(app.shows("label:Options")).toBe(true);
    expect(app.shows("label:New session")).toBe(true);
    app.onDialog(() => null);
    await app.press("id:tray-menu");
    await app.waitUntil(() => app.dialogs().some((dialog) => dialog.buttons.includes("Sort") && dialog.buttons.includes("Services")));
  }, 20_000);

  test("running is an ink dot, connecting and offline a ring: the dot follows the state it opened in", async () => {
    const app = await open(HARNESS, { host: { params: { workers: [worker("alpha")], connection: "connecting" } }, platform: "android" });
    await app.waitFor("text:Connecting…");
    expect(app.shows("id:state-dot-ring")).toBe(true);
    await app.host.send("app.connection", "connected");
    await app.waitFor("text:1 running");
    expect(app.shows("id:state-dot-ink")).toBe(true);
    expect(app.shows("id:state-dot-ring")).toBe(false);
    await app.host.send("app.connection", "waiting");
    await app.waitFor("text:Offline");
    expect(app.shows("id:state-dot-ring")).toBe(true);
    expect(app.shows("id:state-dot-ink")).toBe(false);
  }, 20_000);

  test("a running row ends in a live dot and how long it has run; its line is words, never the terminal's footer nor its section's word", async () => {
    const footer = "⏸ manual mode on · ? for shortcuts";
    const app = await open(HARNESS, { host: { params: { workers: [
      worker("alpha", { running_since: NOW - 12 * 60_000, summary: footer, last_output_lines: [footer] }),
      worker("beta", { status: "unknown", summary: footer, last_output_lines: [footer] }),
    ] } }, platform: "android" });
    await app.waitFor("text:1 running");
    await app.press("id:state-control");
    await app.waitFor("id:tray");
    expect(app.texts()).toContain("12m");
    // Nothing reported but the footer: each row names its agent and project, not "Working" or "Idle" again.
    expect(app.texts().filter((t) => t === "Claude Code · mewla").length).toBe(2);
    expect(app.texts()).not.toContain("Working");
    expect(app.count("id:tray-running >> id:row-live")).toBe(1);
    expect(app.texts()).not.toContain("live");
    expect(app.texts().some((t) => t.includes("manual mode"))).toBe(false);
  }, 20_000);

  test("a running Work has its judged progress as a ring round its tile, spoken rounded to 5, never written; a Session on its own has none", async () => {
    const brain = { host_worker: { id: "host", name: "brain", status: "running" }, chat_thread_id: "t1", current_work: [
      { work_id: "w1", revision: 1, title: "Ship the docs", status: "running", unread_result: false, attempt_session_id: "alpha", attempt_delegated: true, progress: { stage: "moving", percent: 43 } },
      { work_id: "w2", revision: 1, title: "Fix sync", status: "running", unread_result: false, progress: { stage: "back", percent: 58, back: "More was asked" } },
      { work_id: "w3", revision: 1, title: "Write the brief", status: "waiting", unread_result: false, progress: { stage: "last", percent: 99 } },
    ] };
    const app = await open(HARNESS, { host: { params: { brain, workers: [worker("alpha", { phase: "verifying" }), worker("beta", { phase: "verifying" })] } }, platform: "android" });
    await app.press("id:state-control");
    await app.waitFor("id:tray");
    await app.waitFor("label:45 percent");
    expect(app.count("id:progress-mark")).toBe(3);
    expect(app.shows("label:60 percent, sent back")).toBe(true);
    expect(app.shows("label:Last stretch")).toBe(true);
    // The ring is the only sign: no percentage is written anywhere (the label speaks it).
    expect(app.texts().some((text) => /\d+ ?%/.test(text))).toBe(false);
    // The sent-back reason is Work detail's; the row's line stays what the Worker does.
    expect(app.texts()).not.toContain("More was asked");
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

  test("a row is kept while the tray is made again (each second, each update): a tap spanning one still opens the Session", async () => {
    const app = await open(HARNESS, { host: { params: { workers: [worker("alpha")] } }, platform: "android" });
    await app.waitFor("text:1 running");
    await app.press("id:state-control");
    await app.waitFor("text:Claude Code · mewla");
    await app.dragAndHold("id:tray-running >> id:tray-row", { x: 0, y: 0 });
    // A new list (new objects, the same Session): the row under the finger takes the new line and stays.
    await app.host.send("app.workers", [worker("alpha", { cwd: "/home/me/site" })]);
    await app.waitFor("text:Claude Code · site");
    await app.drop();
    await app.waitFor("id:session-page");
  }, 20_000);

  test("a failed Session in Needs you ends in Open, which opens it; a blocked one still ends in Answer (Brain 11-14)", async () => {
    const app = await open(HARNESS, { host: { params: { workers: [
      worker("alpha", { status: "failed", needs_attention: true, summary: "Tests fail" }),
      worker("beta", { status: "blocked", needs_attention: true, summary: "Which port?" }),
    ] } }, platform: "android" });
    await app.waitFor("text:2 need you");
    await app.press("id:state-control");
    await app.waitFor("id:tray-needs");
    expect(app.count("id:tray-needs >> id:tray-open")).toBe(1);
    expect(app.count("id:tray-needs >> id:tray-answer")).toBe(1);
    expect(app.count("id:tray-needs >> text:Open")).toBe(1);
    expect(app.count("id:tray-needs >> text:Answer")).toBe(1);
    await app.press("id:tray-open");
    await app.waitFor("text:session alpha");
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
