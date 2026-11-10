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

  test("a running Session is live; one waiting on you says so", () => {
    expect(sessionRowModel(worker({ status: "running", updated_at: Date.now() })).time).toBe("live");
    expect(sessionRowModel(worker({ needs_attention: true })).needsYou).toBe(true);
  });
});
