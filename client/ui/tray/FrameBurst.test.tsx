import { describe, expect, open, test } from "nsh/test";
import { paired, phoneHost } from "../testing/fixtures";

// The host hands JS the frames that came in together in one call, and NSH reruns what reads a signal on every
// write: before frames were handed over a turn at a time, a burst of 36 Session updates reran the tray (and
// everything else that reads the Sessions) 36 times in that one call, 61 ms on a dev box, where a phone with an
// owner's state ran past NSH's 2 s limit ("Error: interrupted"). Now it reruns once, 3 ms.

const NOW = Date.now();
const sessions = Array.from({ length: 36 }, (_, index) => ({
  id: `mh-${index}`, name: `Worker ${index}`, status: "running", cwd: `/home/me/project-${index}`, command: "claude", provider: "claude",
  summary: "Working", last_output_lines: [], updated_at: NOW - index * 60_000, started_at: NOW - 3_600_000, running_since: NOW - 600_000,
}));
const brain = {
  chat_thread_id: "t-now",
  host_worker: { id: "brain-host", name: "Brain", cwd: "/home/me", command: "claude", started_at: "2026-11-11T07:00:00.000Z" },
};

const at = (i: number) => new Date(Date.UTC(2026, 10, 14, 1, 0, i)).toISOString();
const thread = [
  { id: "p0", seq: 1, kind: "user_message", timestamp: at(0), body: "Plan the release." },
  { id: "p1", seq: 2, kind: "assistant_message", timestamp: at(1), body: "On it." },
];
const runsOf = (app: { host: { posts(): { name: string; data: unknown }[] } }) =>
  app.host.posts().filter((post) => post.name === "app.trayRuns").map((post) => post.data);

describe("news that comes in together", () => {
  test("a burst of 36 Session updates in one call reruns the tray once, on the next tick", async () => {
    const host = phoneHost({ worker_sessions: sessions, brain });
    const app = await open("./testing/BurstApp.tsx", { platform: "android", storage: paired, host: { ...host, handlers: { ...host.handlers, "app.trayRuns": null } } });
    await app.waitFor("text:36 running", { timeout: 10_000 });
    // The list came as seq 1; the burst is the next 36.
    await app.host.send("app.burst", { sessions, seq: 2 });
    await app.waitUntil(() => app.host.posts().some((post) => post.name === "app.trayRuns"), { timeout: 10_000 });
    expect(app.host.posts().find((post) => post.name === "app.trayRuns")!.data).toEqual({ during: 0, after: 1 });
  }, 30_000);

  test("a Brain reply streaming in reruns the tray only when its turn starts", async () => {
    const host = phoneHost({ worker_sessions: sessions.slice(0, 2), brain, conversations: { "brain-host": thread } });
    const app = await open("./testing/BurstApp.tsx", { platform: "android", storage: paired, host: { ...host, handlers: { ...host.handlers, "app.trayRuns": null } } });
    await app.waitFor("text:On it.", { timeout: 10_000 });
    const running = { id: "turn-2", status: "running", started_at: at(2) };
    await app.host.send("app.conversation", { worker: "brain-host", upserts: [{ id: "p2", seq: 3, kind: "assistant_message", timestamp: at(2), body: "The" }], activity: running });
    await app.waitFor("text:The", { timeout: 10_000 });
    for (const body of ["The notes", "The notes are", "The notes are drafted."]) {
      await app.host.send("app.conversation", { worker: "brain-host", upserts: [{ id: "p2", seq: 3, kind: "assistant_message", timestamp: at(2), body }], activity: running });
      await app.waitFor(`text:${body}`, { timeout: 10_000 });
      await app.host.send("app.trayRunsSince");
    }
    await app.waitUntil(() => runsOf(app).length === 3, { timeout: 10_000 });
    expect(runsOf(app)).toEqual([{ since: 0 }, { since: 0 }, { since: 0 }]);
  }, 30_000);
});
