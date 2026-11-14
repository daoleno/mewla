import { describe, expect, test } from "bun:test";
import { listTime, sessionRowModel, shortWorkerLabel } from "./listRow";
import { workerReducer, initialWorkerState, serverWorkers } from "./workers";

const worker = (raw: Record<string, unknown>) =>
  serverWorkers(workerReducer(initialWorkerState, { type: "UPSERT_SERVER_WORKERS", serverId: "s", serverName: "Desk", serverUrl: "ws://x", workers: [{ id: "w1", name: "", status: "done", ...raw } as never] }), "s")[0];

describe("Sessions list rows", () => {
  test("times: today, yesterday, this week, older", () => {
    const now = new Date(2026, 10, 11, 15, 0);
    expect(listTime(new Date(2026, 10, 11, 9, 5).getTime(), now)).toBe("9:05 AM");
    expect(listTime(new Date(2026, 10, 10, 9, 5).getTime(), now)).toBe("Yesterday");
    expect(listTime(new Date(2026, 10, 7, 9, 5).getTime(), now)).toBe("Sat");
    expect(listTime(new Date(2026, 9, 27).getTime(), now)).toBe("Oct 27");
    expect(listTime(new Date(2025, 9, 27).getTime(), now)).toBe("Oct 27, 2025");
    expect(listTime(undefined, now)).toBe("");
  });

  test("short labels drop a trailing pid", () => {
    expect(shortWorkerLabel("claude (1234)")).toBe("claude");
    expect(shortWorkerLabel("  ")).toBe("");
  });

  test("title: nickname, then the Session's name, then its Work, then its command", () => {
    const plain = worker({ cwd: "/home/me/mewla", command: "claude --permission-mode bypassPermissions", provider: "claude" });
    expect(sessionRowModel(plain, { alias: "Fixer" }).title).toBe("Fixer");
    expect(sessionRowModel(plain, { workTitle: "Ship 0.3.2" }).title).toBe("Ship 0.3.2");
    expect(sessionRowModel(plain).kind).toBe("claude");
    expect(sessionRowModel(worker({ name: "release notes", provider: "codex" }), { workTitle: "Ship" }).title).toBe("release notes");
  });

  test("a name that is only the Session's id is no name: the folder names it", () => {
    expect(sessionRowModel(worker({ id: "mh-b0455a19", name: "mh-b0455a19", cwd: "/home/me/mewla", provider: "claude" })).title).toBe("mewla");
    const hostId = "mh-b0455a19d53b45ed0dce9dba";
    expect(sessionRowModel(worker({ id: hostId, name: `${hostId} (${hostId})`, cwd: "/home/me/mewla", provider: "claude" })).title).toBe("mewla");
    expect(sessionRowModel(worker({ id: "%3", name: hostId, cwd: "/home/me/mewla", provider: "claude" })).title).toBe("mewla");
  });

  test("one waiting on you says so, in its flag and its line", () => {
    const waiting = sessionRowModel(worker({ needs_attention: true }));
    expect(waiting.needsYou).toBe(true);
    expect(waiting.line).toBe("Waiting for you");
  });

  test("a screen reader hears the row's title, origin, state, line and time (parity #10)", () => {
    const running = worker({ name: "release notes", status: "running", provider: "claude" });
    expect(sessionRowModel(running).accessibilityLabel).toBe("release notes, Running, Claude Code");
    const updated = new Date(2026, 10, 11, 9, 5).getTime();
    const done = worker({ name: "release notes", status: "done", delegated: true, updated_at: updated });
    const label = sessionRowModel(done, { calendarLabel: "Daily" }).accessibilityLabel;
    expect(label.startsWith("release notes, From Calendar, Brain delegated, Done")).toBe(true);
    expect(label.endsWith(listTime(updated))).toBe(true);
  });
});

describe("a Session's row is made once per Session object (owner 11-14: the tray made it three times per Session per change)", () => {
  test("the same Session and names give back the same row; another name, a changed Session or a new day make it again", () => {
    const session = worker({ id: "w1", name: "release", status: "done", cwd: "/home/me/mewla", provider: "claude", summary: "Reporting: green", updated_at: Date.now() - 60_000 });
    const row = sessionRowModel(session);
    expect(sessionRowModel(session)).toBe(row);
    const named = sessionRowModel(session, { alias: "Ship it" });
    expect(named).not.toBe(row);
    expect(named.title).toBe("Ship it");
    expect(sessionRowModel(session, { alias: "Ship it" })).toBe(named);
    const changed = worker({ id: "w1", name: "release", status: "failed", cwd: "/home/me/mewla", provider: "claude", summary: "Reporting: green", updated_at: Date.now() - 60_000 });
    expect(sessionRowModel(changed)).not.toBe(row);
    expect(sessionRowModel(changed).status).toBe("failed");
    // The screen reader's day ("Yesterday") reads the clock: a Session last changed two days ago reads its date.
    const old = worker({ id: "w2", name: "old", status: "done", provider: "claude", summary: "", updated_at: Date.now() - 2 * 86_400_000 });
    expect(sessionRowModel(old).accessibilityLabel).toContain(listTime(old.updated_at));
  });
});
