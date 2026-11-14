import { describe, expect, open, test } from "nsh/test";
import { paired, phoneHost } from "../testing/fixtures";
import { ownerShapedCurrentWork, ownerShapedSessions, ownerShapedThread, ownerShapedWorkItems } from "../testing/ownerShape";

/**
 * Owner 11-14 (v0.4.2, a OnePlus PYR110): opening Mewla, NSH cut eight calls off at its 2 s budget in two minutes
 * ("Error: interrupted") while the computer's state came in. On this kind of box the longest of those calls was
 * 251 ms with v0.4.2 and 125 ms with v0.4.4, for the owner's sizes (testing/ownerShape.ts): the phone runs some 8x
 * slower. Now no call that hands the app its frames passes the budget below, so a phone keeps well under 2 s.
 */
const BUDGET_MS = 100;

const host = { id: "brain-host", name: "Brain", cwd: "/home/me/.mewla/brain/workspace", command: "claude", started_at: "2026-11-14T06:00:00.000Z" };
const brain = { chat_thread_id: "t-now", host_worker: host, current_work: ownerShapedCurrentWork() };

async function callsOf(app: Awaited<ReturnType<typeof open>>): Promise<number[]> {
  await app.host.send("app.callsReport");
  const posts = app.host.posts().filter((post) => post.name === "app.calls");
  return (posts[posts.length - 1]?.data as number[]) ?? [];
}

describe("opening with the owner's computer (Brain's 240 events, 60 Sessions, 57 Work items)", () => {
  test(`no call that hands the app its frames passes ${BUDGET_MS} ms: the open, the thread's older pages, then a burst of Session updates and a streaming reply`, async () => {
    const thread = ownerShapedThread();
    const sessions = ownerShapedSessions(Date.now());
    const app = await open("../testing/OpenBudgetApp.tsx", {
      platform: "android", storage: paired,
      host: phoneHost({ brain, worker_sessions: sessions, work_items: ownerShapedWorkItems(), conversations: { "brain-host": thread } }, { "app.calls": () => undefined }),
    });
    await app.waitFor("id:brain-chat", { timeout: 20_000 });
    // The older pages come after the first; the thread's first message is drawn once they are in.
    await app.wait(2000);
    for (let i = 0; i < 20; i += 1) {
      await app.host.send("app.daemonPush", { type: "worker_session_updated", worker_session: { ...sessions[i % 60], summary: `Reporting: step ${i}`, updated_at: Date.now() } });
    }
    for (const body of ["Looking", "Looking at the open path", "Looking at the open path: the older events now come a page at a time."]) {
      await app.host.send("app.conversation", { worker: "brain-host", upserts: [{ id: "reply", seq: 400, kind: "assistant_message", role: "assistant", timestamp: new Date().toISOString(), body }] });
    }
    await app.wait(500);
    const calls = await callsOf(app);
    expect(calls.length).toBeGreaterThan(5);
    const longest = Math.max(...calls);
    console.log(`open budget: ${calls.length} calls, the longest ${longest} ms`);
    expect(longest).toBeLessThan(BUDGET_MS);
  }, 120_000);
});
