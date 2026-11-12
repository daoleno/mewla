import { describe, expect, open, test } from "nsh/test";
import { framesOf, paired, phoneHost, pressUntil } from "../testing/fixtures";

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

  test("a cold start with requests open opens on the oldest, centred: the newest stacked rows wait below", async () => {
    const sessions = Array.from({ length: 14 }, (_, i) => session(`s${i}`, { delegated: true, approvals: [approval(`a${i}`, `step ${i} of the release`)] }));
    const app = await open(APP, { platform: "android", storage: paired, host: phoneHost({ brain, worker_sessions: sessions }) });
    await app.waitFor("id:request-card", { timeout: 10_000 });
    await app.wait(800);
    expect(app.count("id:request-stacked")).toBe(13);
    expect(app.shows("id:request-card")).toBe(true);
    // Centred, the thread's end (the newest stacked rows) is below: the jump control shows, with their count.
    await app.waitFor("id:jump-requests");
    expect(app.texts()).toContain("14");
  }, 20_000);

  test("answered on another device: the computer's record seals the card here at once, with who answered", async () => {
    const app = await open(APP, {
      platform: "android", storage: paired,
      host: phoneHost({ brain, worker_sessions: [session("docs", { delegated: true, approvals: [approval("a1", "git push origin main")] })] }),
    });
    await app.waitFor("id:request-card", { timeout: 10_000 });
    await app.host.send("app.daemonPush", { type: "request_answered", request: {
      id: "approval:docs:a1", kind: "approval", session_id: "docs", work_id: "w1", asked: "git push origin main", answer: "Allow", outcome: "approved",
      by: { kind: "device", device_name: "Pixel 8" }, answered_at: new Date().toISOString(), anchor: { approval_id: "a1" },
    } });
    await app.waitFor("id:request-sealed");
    expect(app.shows("id:request-card")).toBe(false);
    expect(app.texts()).toContain("Approved");
    expect(app.texts()).toContain("Allow");
    await app.waitUntil(() => !app.texts().includes("1 needs you"));
  }, 20_000);

  test("after a restart the thread still says what was answered, by whom, where it was asked", async () => {
    const at = "2026-11-12T07:24:49.000Z";
    const row = { id: "answered:prompt:brain-host:p1", seq: 2, timestamp: "2026-11-12T07:24:26.000Z", kind: "request_answered", source: "request_answered", role: "system",
      title: "Waiting for Claude permission", body: "Yes", status: "approved",
      request: { id: "prompt:brain-host:p1", kind: "permission", session_id: "brain-host", thread_id: "t-now", asked: "Bash command rm -rf build", answer: "Yes", outcome: "approved",
        by: { kind: "device", device_name: "Proof phone" }, answered_at: at, anchor: { prompt_id: "p1" } } };
    const app = await open(APP, { platform: "android", storage: paired, host: phoneHost({ brain, conversations: { "brain-host": [row] } }) });
    await app.waitFor("id:request-answered", { timeout: 10_000 });
    expect(app.texts()).toContain("Approved");
    expect(app.texts()).toContain("Yes");
    expect(app.count("id:request-sealed")).toBe(1);
    await app.wait(500);
    await app.press("id:request-sealed");
    await app.waitUntil(() => app.texts().some((t) => t.startsWith("Bash command rm -rf build · Proof phone")));
  }, 20_000);

  test("answered in a Session: the computer's row arrives live, between the rows already there, and says it once", async () => {
    const asked = "2026-11-12T09:23:10.000Z";
    const events = [
      { id: "u1", seq: 1, timestamp: "2026-11-12T09:23:00.000Z", kind: "message", role: "user", body: "Run the bash command touch /tmp/x then say done" },
      { id: "t1", seq: 2, timestamp: "2026-11-12T09:23:20.000Z", kind: "tool_call", role: "assistant", call_id: "c1", tool_name: "Bash", title: "Run touch x", status: "running" },
    ];
    const app = await open(APP, {
      platform: "android", storage: paired,
      host: phoneHost({ brain, worker_sessions: [session("mine", { approvals: [approval("a3", "touch /tmp/x")] })], conversations: { mine: events }, replies: { approval_decision: { type: "approval_decided" } } }),
    });
    await app.waitFor("text:1 needs you", { timeout: 10_000 });
    await app.press("id:state-control");
    await pressUntil(app, "text:touch /tmp/x", "id:request-card");
    await app.wait(500);
    await app.press("id:prompt-option-allow");
    await app.waitFor("id:request-sealed");
    await app.host.send("app.daemonPush", { type: "request_answered", request: {
      id: "approval:mine:a3", kind: "approval", session_id: "mine", asked: "touch /tmp/x", answer: "Allow", outcome: "approved",
      by: { kind: "device", device_name: "Proof phone" }, answered_at: "2026-11-12T09:24:23.000Z", anchor: { approval_id: "a3" },
    } });
    const subscription = framesOf(app.host.posts(), "codex_conversation_subscribe").filter((frame) => frame.worker_id === "mine").at(-1)!;
    await app.host.send("app.daemonPush", { type: "codex_conversation_delta", request_id: subscription.request_id, worker_id: "mine", revision: 2, base_revision: 1, deletes: [], upserts: [
      { id: "answered:approval:mine:a3", seq: 3, timestamp: asked, kind: "request_answered", source: "request_answered", role: "system", title: "Bash", body: "Allow", status: "approved",
        request: { id: "approval:mine:a3", kind: "approval", session_id: "mine", asked: "touch /tmp/x", answer: "Allow", outcome: "approved",
          by: { kind: "device", device_name: "Proof phone" }, answered_at: "2026-11-12T09:24:23.000Z", anchor: { approval_id: "a3" } } },
    ] });
    await app.waitFor("id:request-answered", { timeout: 5_000 });
    expect(app.count("id:request-sealed")).toBe(1);
    expect(app.texts()).toContain("Approved");
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
    unread: true, work_review_state: "resolved", work_session_state: "finalized", work_result_current: true, ...patch,
  });
  const docs = session("docs", { status: "done" });

  test("a result's next steps are ghost buttons; one opens where it names, reads the result and the card becomes its line", async () => {
    const work_actions = [
      { kind: "pull_request", url: "https://github.com/daoleno/mewla/pull/4242" },
      { kind: "git_review", session_id: "docs", cwd: "/home/me/mewla" },
      { kind: "file", session_id: "docs", cwd: "/home/me/mewla", path: "docs/NOTES.md" },
      { kind: "brain_folder", path: "worklog/ship-docs" },
      { kind: "made_up", path: "x" },
    ];
    const app = await open(APP, { platform: "android", storage: paired, host: phoneHost({ brain, worker_sessions: [docs], conversations: { "brain-host": [result({ work_actions })] } }) });
    await app.waitFor("id:work-actions", { timeout: 10_000 });
    expect(app.texts().filter((t) => ["Pull request #4242", "Review changes", "NOTES.md", "Worklog"].includes(t))).toEqual(["Pull request #4242", "Review changes", "NOTES.md", "Worklog"]);
    expect(app.count("id:work-actions >> id:work-action-made_up")).toBe(0);
    await app.wait(500);
    await app.press("id:work-action-git_review");
    await app.waitFor("id:git-page");
    expect(framesOf(app.host.posts(), "brain_work_read")[0]).toMatchObject({ id: "w1" });
  }, 20_000);

  test("came back and new: a result card; opening it reads it", async () => {
    const app = await open(APP, { platform: "android", storage: paired, host: phoneHost({ brain, worker_sessions: [docs], conversations: { "brain-host": [result()] } }) });
    await app.waitFor("id:work-result", { timeout: 10_000 });
    expect(app.texts()).toContain("Ship the docs");
    expect(app.shows("id:work-unread")).toBe(true);
    await app.wait(500);
    await app.press("id:work-result-open");
    await app.waitUntil(() => framesOf(app.host.posts(), "brain_work_read").length > 0);
    expect(framesOf(app.host.posts(), "brain_work_read")[0]).toMatchObject({ id: "w1" });
  }, 20_000);

  test("live with several Workers: three Worker lines and \"N more\"; a line pushes its Session, \"more\" Work detail", async () => {
    const joined = (id: string, live: boolean) => ({ session_id: id, name: `Worker ${id}`, provider: "claude", state: live ? "running" : "done", summary: live ? `working on ${id}` : `${id} done`, live, joined_at: "2026-11-12T11:00:00Z", ended_at: live ? undefined : "2026-11-12T11:10:00Z", updated_at: "2026-11-12T11:10:00Z" });
    const current_work = [{ work_id: "w1", revision: 3, title: "Ship the docs", status: "running", unread_result: false, attempt_session_id: "d",
      worker_sessions: [joined("a", false), joined("b", false), joined("c", true), joined("d", true)] }];
    const app = await open(APP, { platform: "android", storage: paired, host: phoneHost({
      brain: { ...brain, current_work }, worker_sessions: ["a", "b", "c", "d"].map((id) => session(id, { status: id === "a" || id === "b" ? "done" : "running", phase: "verifying" })),
      conversations: { "brain-host": [result({ unread: false, status: "session.done" })] },
    }) });
    await app.waitFor("id:work-card", { timeout: 10_000 });
    expect(app.count("id:work-card-worker")).toBe(3);
    expect(app.texts()).toContain("1 more");
    expect(app.texts()).toContain("Worker a  a done");
    expect(app.texts().join(" ")).not.toContain("Worker d");
    expect(app.count("id:work-card-progress")).toBe(1);
    await app.wait(500);
    await app.press("id:work-card-worker >> nth=2");
    await app.waitFor("id:session-status");
    await app.back();
    await app.waitFor("id:work-card-more");
    await app.wait(500);
    await app.press("id:work-card-more");
    await app.waitFor("id:work-detail");
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
