import { describe, expect, open, test } from "nsh/test";
import { framesOf, paired, phoneHost } from "../testing/fixtures";

/**
 * Owner 11-13 10:22 (CRASH-1022.md): rows kept across updates read what they were made for after it changed.
 * A Work card's logo read the agent of a Session that had left the store; a Work card's cell was handed
 * another row and read its `event`. Every error the app throws fails these tests.
 */
const APP = "../FakeDaemonApp.tsx";
const host = { id: "brain-host", name: "Brain", cwd: "/home/me", command: "claude", started_at: "2026-11-11T07:00:00.000Z" };
const brain = { chat_thread_id: "t-now", host_worker: host };
const at = (i: number) => new Date(Date.UTC(2026, 10, 13, 2, 0, i)).toISOString();
const say = (i: number, role: "user" | "assistant" = i % 2 ? "assistant" : "user") => ({ id: `m${i}`, seq: i, kind: `${role}_message`, timestamp: at(i), body: `Row ${i}` });
const work = (i: number, session: string) => ({
  id: `wr${i}`, seq: i, timestamp: at(i), kind: "status", title: `Digest ${i}`, body: "Done.", status: "session.done", source: "work_result",
  work_id: `work-${i}`, work_session_id: session, session_name: `digest ${i}`, unread: true, work_review_state: "queued", work_session_state: "open", work_result_current: true,
});
const worker = (id: string) => ({ id, name: id, status: "done", cwd: "/home/me/mewla", command: "claude", provider: "claude", updated_at: Date.now() });

/** Rows that threw and were caught (each reports itself to the computer): none may. Uncaught errors fail a test by themselves. */
const failedRows = (app: { host: { posts(): { name: string; data: unknown }[] } }) => framesOf(app.host.posts(), "client_diagnostic").filter((f) => f.name === "chat_row_error").map((f) => String(f.body).split("\n")[0]);

/** Brain's thread with these events, on a computer with these Sessions. */
async function thread(events: Record<string, unknown>[], sessions: Record<string, unknown>[]) {
  const app = await open(APP, { platform: "android", storage: paired, host: phoneHost({ brain, worker_sessions: sessions, conversations: { "brain-host": events } }) });
  await app.waitFor("id:brain-chat", { timeout: 10_000 });
  await app.waitFor("text:Row 1", { timeout: 10_000 });
  await app.wait(300);
  return app;
}

describe("a row kept across updates holds whatever its inputs become", () => {
  test("a Session leaves the computer while its Work card is on screen", async () => {
    const app = await thread([say(1), work(2, "w9"), say(3)], [worker("w9")]);
    await app.waitFor("text:Digest 2");
    await app.host.send("app.daemonPush", { type: "worker_session_archived", worker_session: { id: "w9" } });
    await app.wait(400);
    expect(app.texts()).toContain("Digest 2");
    expect(app.texts()).toContain("Row 3");
    expect(failedRows(app)).toEqual([]);
  }, 20_000);

  test("a thread delta takes a Work card away and its cell shows the next row", async () => {
    const app = await thread([say(1), work(2, "w9"), say(3), say(4)], [worker("w9")]);
    await app.waitFor("text:Digest 2");
    await app.host.send("app.conversation", { worker: "brain-host", deletes: ["wr2"] });
    await app.waitFor("text:Digest 2", { state: "gone" });
    await app.wait(300);
    expect(app.texts()).toContain("Row 4");
    expect(failedRows(app)).toEqual([]);
  }, 20_000);

  test("a thread delta puts another row where a Work card was: the same cell is handed a message", async () => {
    const app = await thread([say(1), work(2, "w9"), say(3), say(4)], [worker("w9")]);
    await app.waitFor("text:Digest 2");
    await app.host.send("app.conversation", { worker: "brain-host", deletes: ["wr2"], upserts: [{ ...say(2, "assistant"), id: "m2b", body: "Row 2b" }] });
    await app.waitFor("text:Row 2b");
    await app.wait(300);
    expect(app.texts()).not.toContain("Digest 2");
    expect(failedRows(app)).toEqual([]);
  }, 20_000);
});

describe("a long random run of real thread and store events throws nothing", () => {
  for (const seed of [11, 1022, 4242]) {
    test(`seed ${seed}: add, change, remove, reorder, change type, Sessions gone and back, snapshots re-sent`, async () => {
      let state = seed;
      const rand = () => ((state = (state * 1103515245 + 12345) % 2147483648) / 2147483648);
      const pick = <T,>(xs: readonly T[]) => xs[Math.floor(rand() * xs.length)];
      const sessions = ["w1", "w2", "w3"];
      const make = (id: string, seq: number): Record<string, unknown> => {
        const kind = pick(["user", "assistant", "work", "tool", "tool"] as const);
        if (kind === "work") return { ...work(seq, pick(sessions)), id, title: `Work ${id}` };
        if (kind === "tool") return { id, seq, timestamp: at(seq), kind: "command", command: `run ${id}`, status: pick(["running", "completed", "failed"]), exit_code: 1, output: "out" };
        return { ...say(seq, kind), id, body: `Row ${id} ${"words ".repeat(1 + Math.floor(rand() * 20))}` };
      };
      let next = 1;
      const events = new Map<string, Record<string, unknown>>();
      for (let i = 0; i < 12; i += 1) {
        const id = `e${next}`;
        events.set(id, make(id, next++));
      }
      const app = await open(APP, { platform: "android", storage: paired, host: phoneHost({ brain, worker_sessions: sessions.map(worker), conversations: { "brain-host": [{ ...say(0), body: "Row 1" }, ...events.values()] } }) });
      await app.waitFor("id:brain-chat", { timeout: 10_000 });
      await app.waitFor("text:Row 1", { timeout: 10_000 });
      const ids = () => [...events.keys()];
      for (let step = 0; step < 60; step += 1) {
        const op = pick(["add", "add", "change", "remove", "reorder", "type", "replace", "gone", "back", "snapshot", "brain", "scroll"] as const);
        if (op === "add" || (op !== "back" && events.size === 0)) {
          const id = `e${next}`;
          const event = make(id, rand() < 0.3 ? Math.max(1, Math.floor(rand() * next)) : next);
          next += 1;
          events.set(id, event);
          await app.host.send("app.conversation", { worker: "brain-host", upserts: [event] });
        } else if (op === "change" || op === "type") {
          const id = pick(ids());
          const old = events.get(id)!;
          const event = op === "type" ? make(id, Number(old.seq)) : { ...old, body: `${String(old.body ?? "")} more`, status: pick(["running", "completed", "failed", "session.done", "session.needs_input"]) };
          events.set(id, event);
          await app.host.send("app.conversation", { worker: "brain-host", upserts: [event] });
        } else if (op === "remove") {
          const id = pick(ids());
          events.delete(id);
          await app.host.send("app.conversation", { worker: "brain-host", deletes: [id] });
        } else if (op === "replace") {
          const gone = pick(ids());
          const old = events.get(gone)!;
          events.delete(gone);
          const id = `e${next++}`;
          const event = make(id, Number(old.seq));
          events.set(id, event);
          await app.host.send("app.conversation", { worker: "brain-host", deletes: [gone], upserts: [event] });
        } else if (op === "reorder") {
          const id = pick(ids());
          const seq = Math.max(1, Math.floor(rand() * next));
          const event = { ...events.get(id)!, seq, timestamp: at(seq) };
          events.set(id, event);
          await app.host.send("app.conversation", { worker: "brain-host", upserts: [event] });
        } else if (op === "gone") {
          await app.host.send("app.daemonPush", { type: "worker_session_archived", worker_session: { id: pick(sessions) } });
        } else if (op === "back") {
          await app.host.send("app.daemonPush", { type: "worker_session_created", worker_session: { ...worker(pick(sessions)), status: pick(["running", "done", "blocked"]) } });
        } else if (op === "snapshot") {
          await app.host.send("app.conversation", { worker: "brain-host", snapshot: true });
        } else if (op === "brain") {
          await app.host.send("app.daemonPush", { type: "brain_snapshot", brain: { ...brain, current_work: [{ work_id: `work-${Math.floor(rand() * next)}`, title: "Now", status: pick(["running", "needs_input"]), worker_sessions: [{ session_id: pick(sessions) }] }] } });
        } else {
          // The person scrolls (at an end, the list does not move further).
          await app.scroll("id:brain-chat", rand() < 0.5 ? -900 : 900).catch(() => {});
        }
        await app.wait(40 + Math.floor(rand() * 80));
      }
      await app.wait(300);
      expect(app.shows("id:brain-chat")).toBe(true);
      expect(failedRows(app)).toEqual([]);
    }, 60_000);
  }
});
