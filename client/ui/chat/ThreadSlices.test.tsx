import { describe, expect, open, test } from "nsh/test";
import { paired, phoneHost } from "../testing/fixtures";

/**
 * A thread's new events get their rows a slice at a time, each slice a call of its own (kit/derived.ts `preparedRows`).
 * Made in the one call that projects the thread, an update of 4000 tool events took that call past NSH's 2 s on a CI
 * runner (11-15, main CI): NSH cut it off ("the app threw: interrupted") and every run after it started over. The
 * owner's phone, some 8 times slower than a dev box, did the same with a few hundred new events, as on a reconnect.
 * Here the update draws, no call is cut off, and none but the frame's own keeps the engine past the budget below. The
 * frame's own call reads and files the 4000 events (12 MB in one frame; the daemon pages a thread, 40 events a frame
 * after the first): it is bounded apart, about 0.8 s on a dev box.
 */
const BUDGET_MS = 150;
const FRAME_BUDGET_MS = 1300;
const host = { id: "brain-host", name: "Brain", cwd: "/home/me", command: "claude", started_at: "2026-11-11T07:00:00.000Z" };
const brain = { chat_thread_id: "t-now", host_worker: host };
const ts = (i: number) => new Date(Date.UTC(2026, 10, 11, 7, 0, 0) + i * 1000).toISOString();
const say = (i: number, body: string) => ({ id: `m${i}`, seq: i, kind: i % 2 ? "assistant_message" : "user_message", role: i % 2 ? "assistant" : "user", timestamp: ts(i), body });
const output = Array.from({ length: 60 }, (_, i) => `line ${i}: ok some output text here padding....`).join("\n");
const update = Array.from({ length: 4000 }, (_, i) => ({
  id: `h${i}`, seq: 100 + i, kind: "tool", title: "Tool", tool_name: "Read", input: JSON.stringify({ file_path: `/x/${i}` }), output, status: "done", timestamp: ts(100 + i),
}));

describe("a thread update of 4000 tool events", () => {
  test(`draws, and no call but the frame's own keeps the engine past ${BUDGET_MS} ms`, async () => {
    const app = await open("../testing/OpenBudgetApp.tsx", {
      platform: "android", storage: paired,
      host: phoneHost({ brain, conversations: { "brain-host": [say(0, "Message 0."), say(1, "Message 1.")] } }, { "app.calls": () => undefined, "app.gaps": () => undefined }),
    });
    await app.waitFor("text:Message 1.", { timeout: 10_000 });
    await app.host.send("app.gapsReset");
    await app.host.send("app.conversation", { worker: "brain-host", upserts: [...update, say(5001, "The answer, after them.")] });
    await app.waitFor("text:The answer, after them.", { timeout: 30_000 });
    await app.host.send("app.gapsReport");
    await app.host.send("app.callsReport");
    await app.wait(100);
    const gaps = (app.host.posts().filter((post) => post.name === "app.gaps").at(-1)?.data as number[]) ?? [];
    const frame = Math.max(0, ...((app.host.posts().filter((post) => post.name === "app.calls").at(-1)?.data as number[]) ?? []));
    // The frame's own call is one of the gaps; every other is a slice, the thread put together, a render.
    const others = [...gaps];
    const own = others.findIndex((gap) => Math.abs(gap - frame) <= 20);
    if (own >= 0) others.splice(own, 1);
    console.log(`thread slices: the frame's call ${frame} ms, the longest others ${others.slice(0, 5).join(", ")} ms`);
    expect(frame).toBeLessThan(FRAME_BUDGET_MS);
    expect(others.length).toBeGreaterThan(0);
    expect(others[0]!).toBeLessThan(BUDGET_MS);
  }, 60_000);
});
