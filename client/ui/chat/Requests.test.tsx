import { describe, expect, open, test } from "nsh/test";
import { framesOf, paired, phoneHost } from "../testing/fixtures";

const APP = "../FakeDaemonApp.tsx";
const brain = { chat_thread_id: "t-now", host_worker: { id: "brain-host", name: "Brain", cwd: "/home/me", command: "claude", started_at: "2026-11-11T07:00:00.000Z" } };
const session = (id: string, patch: Record<string, unknown> = {}) => ({ id, name: id, status: "blocked", cwd: "/home/me/mewla", command: "claude", provider: "claude", updated_at: Date.now(), ...patch });
const approval = (id: string, summary: string) => ({ id, tool: "Bash", summary, options: [{ id: "allow", label: "Allow", scope: "once" }, { id: "deny", label: "Deny", scope: "deny" }] });

describe("requests in Brain's thread", () => {
  test("a delegated Session's approval is a card in Brain's thread; answering it seals it and the count drops", async () => {
    const app = await open(APP, {
      platform: "android", storage: paired,
      host: phoneHost({ brain, worker_sessions: [session("docs", { delegated: true, approvals: [approval("a1", "git push origin main")] })], replies: { approval_decision: { type: "approval_decided" } } }),
    });
    await app.waitFor("id:request-card", { timeout: 10_000 });
    expect(app.texts()).toContain("Approve");
    expect(app.texts()).toContain("git push origin main");
    expect(app.texts()).toContain("1 needs you");
    // A tap while the list still settles at its end only stops that scroll, as on a phone.
    await app.wait(500);
    await app.press("id:prompt-option-allow");
    await app.waitFor("id:request-sealed");
    expect(framesOf(app.host.posts(), "approval_decision").at(-1)).toMatchObject({ worker_id: "docs", id: "a1", option_id: "allow" });
    expect(app.shows("id:request-card")).toBe(false);
    expect(app.texts()).toContain("Approved");
    expect(app.texts()).not.toContain("1 needs you");
  }, 20_000);

  test("several open requests stack: the oldest open, the rest as rows; a row opens instead; a tray row brings its card", async () => {
    const app = await open(APP, {
      platform: "android", storage: paired,
      host: phoneHost({ brain, worker_sessions: [
        session("docs", { delegated: true, approvals: [approval("a1", "git push origin main")] }),
        session("site", { delegated: true, approvals: [approval("a2", "npm publish")] }),
      ], replies: { approval_decision: { type: "approval_decided" } } }),
    });
    await app.waitFor("id:request-card", { timeout: 10_000 });
    expect(app.count("id:request-card")).toBe(1);
    expect(app.count("id:request-stacked")).toBe(1);
    expect(app.texts()).toContain("git push origin main");
    await app.press("id:request-stacked");
    await app.waitFor("text:npm publish");
    expect(app.count("id:request-stacked")).toBe(1);
    // The tray's row of the other goes back to the thread, with that card open.
    await app.press("id:state-control");
    await app.waitFor("id:tray-needs");
    await app.press("id:tray-needs >> id:tray-request >> nth=0");
    await app.waitUntil(() => !app.shows("id:tray"));
    await app.waitFor("text:git push origin main");
    expect(app.count("id:request-card")).toBe(1);
    await app.press("id:prompt-option-allow");
    await app.waitFor("id:request-sealed");
    expect(framesOf(app.host.posts(), "approval_decision").at(-1)).toMatchObject({ worker_id: "docs", id: "a1" });
    // Answering opens the next.
    await app.waitFor("text:npm publish");
    expect(app.count("id:request-card")).toBe(1);
    expect(app.count("id:request-stacked")).toBe(0);
  }, 20_000);

  test("a Session the person started asks only in that Session", async () => {
    const app = await open(APP, {
      platform: "android", storage: paired,
      host: phoneHost({ brain, worker_sessions: [session("mine", { approvals: [approval("a3", "rm -rf build")] })] }),
    });
    await app.waitFor("text:1 needs you", { timeout: 10_000 });
    expect(app.shows("id:request-card")).toBe(false);
  }, 20_000);
});

describe("a Work in Brain's thread", () => {
  const result = (patch: Record<string, unknown> = {}) => ({
    id: "wr1", seq: 1, timestamp: "2026-11-12T11:20:00.000Z", kind: "status", source: "work_result", status: "session.done",
    title: "Ship the docs", body: "The docs are live at the new address.", work_id: "w1", work_session_id: "docs", session_name: "docs",
    unread: true, work_review_state: "resolved", work_session_state: "finalized", work_result_current: true, work_next_action: "Announce it", ...patch,
  });
  const docs = session("docs", { status: "done" });

  test("came back and new: a result card; its next step goes to Brain and it becomes its line, which opens the result sheet", async () => {
    const app = await open(APP, { platform: "android", storage: paired, host: phoneHost({ brain, worker_sessions: [docs], conversations: { "brain-host": [result()] } }) });
    await app.waitFor("id:work-result", { timeout: 10_000 });
    expect(app.texts()).toContain("Ship the docs");
    expect(app.shows("id:work-unread")).toBe(true);
    await app.wait(500);
    await app.press("id:work-next-step");
    await app.waitUntil(() => framesOf(app.host.posts(), "send_input").length > 0);
    expect(JSON.stringify(framesOf(app.host.posts(), "send_input").at(-1))).toContain("Announce it");
    await app.waitUntil(() => framesOf(app.host.posts(), "brain_work_read").length > 0);
    expect(framesOf(app.host.posts(), "brain_work_read")[0]).toMatchObject({ id: "w1" });
  }, 20_000);

  test("read: the quiet line opens the result sheet, whose Open session opens the Session", async () => {
    const app = await open(APP, { platform: "android", storage: paired, host: phoneHost({ brain, worker_sessions: [docs], conversations: { "brain-host": [result({ unread: false })] } }) });
    await app.waitFor("id:work-line", { timeout: 10_000 });
    await app.wait(500);
    await app.press("id:work-line");
    await app.waitFor("id:work-result-sheet");
    expect(app.texts()).toContain("The docs are live at the new address.");
    await app.press("id:work-sheet-open-session");
    await app.waitFor("id:session-status");
  }, 20_000);
});
