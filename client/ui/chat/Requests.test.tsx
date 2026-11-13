import { describe, expect, open, test } from "nsh/test";
import { framesOf, paired, phoneHost, pressUntil } from "../testing/fixtures";

const APP = "../FakeDaemonApp.tsx";
const brain = { chat_thread_id: "t-now", host_worker: { id: "brain-host", name: "Brain", cwd: "/home/me", command: "claude", started_at: "2026-11-11T07:00:00.000Z" } };
const SIGN_IN = { id: "p-1", kind: "permission", provider: "claude", title: "Sign in to GitHub?", request: "gh auth login", options: [{ number: 1, label: "Yes", selected: true }, { number: 2, label: "No" }] };
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

  test("answering waits in place, never optimistic: the ring in the pressed button, \"Didn't reach Desk\" at the 8 s bound, the buttons back", async () => {
    const app = await open(APP, { platform: "android", storage: paired, host: phoneHost({ brain, worker_sessions: [session("docs", { delegated: true, approvals: [approval("a1", "git push origin main")] })] }) });
    await app.waitFor("id:request-card", { timeout: 10_000 });
    await app.wait(500);
    await app.press("id:prompt-option-allow");
    await app.wait(300);
    expect(app.shows("id:request-ring")).toBe(true);
    expect(app.texts()).not.toContain("Approved");
    expect(app.texts().some((text) => /Sending/.test(text))).toBe(false);
    await app.waitFor("id:request-failed", { timeout: 10_000 });
    expect(app.texts()).toContain("Didn't reach Desk");
    expect(app.shows("id:request-ring")).toBe(false);
    expect(app.enabled("id:prompt-option-allow")).toBe(true);
  }, 25_000);

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
    // The stacked row reads the ask as its tool row does; opening it swaps it with the open card.
    expect(app.texts()).toContain("Run npm publish");
    await pressUntil(app, "id:request-stacked", "text:Run git push origin main");
    expect(app.texts()).toContain("npm publish");
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

  test("a stacked row is kept while the requests are made again (lead 11-13, RESULT-web-flicker): a tap spanning an update still opens it", async () => {
    const docs = session("docs", { delegated: true, approvals: [approval("a1", "git push origin main")] });
    const app = await open(APP, { platform: "android", storage: paired, host: phoneHost({ brain, worker_sessions: [docs, session("site", { delegated: true, approvals: [approval("a2", "npm publish")] })] }) });
    await app.waitFor("id:request-stacked", { timeout: 10_000 });
    await app.dragAndHold("id:request-stacked", { x: 0, y: 0 });
    // A new list (a new object for the other Session, the same requests): the row under the finger stays.
    await app.host.send("app.daemonPush", { type: "worker_session_updated", worker_session: { ...docs, updated_at: Date.now() + 1000, summary: "still waiting" } });
    await app.wait(400);
    await app.drop();
    await app.waitFor("text:npm publish");
    expect(app.count("id:request-card")).toBe(1);
    expect(app.shows("id:request-command")).toBe(true);
    expect(app.texts().filter((t) => t === "npm publish")).toHaveLength(1);
  }, 20_000);

  test("a cold start with requests open opens on the oldest, centred: the newest stacked rows wait below", async () => {
    const sessions = Array.from({ length: 20 }, (_, i) => session(`s${i}`, { delegated: true, approvals: [approval(`a${i}`, `step ${i} of the release`)] }));
    const app = await open(APP, { platform: "android", storage: paired, host: phoneHost({ brain, worker_sessions: sessions }) });
    await app.waitFor("id:request-card", { timeout: 10_000 });
    await app.wait(800);
    expect(app.count("id:request-stacked")).toBe(19);
    expect(app.shows("id:request-card")).toBe(true);
    // Centred, the thread's end (the newest stacked rows) is below: the jump control shows, with their count.
    await app.waitFor("id:jump-requests");
    expect(app.texts()).toContain("20");
  }, 20_000);

  test("answered on another device: the computer's record seals the card here at once, with who answered", async () => {
    const app = await open(APP, {
      platform: "android", storage: paired,
      host: phoneHost({ brain, worker_sessions: [session("docs", { delegated: true, approvals: [approval("a1", "git push origin main")] })] }),
    });
    await app.waitFor("id:request-card", { timeout: 10_000 });
    // A hook approval asks nothing but its command: it shows once, in mono, with no question repeating it.
    expect(app.texts().filter((text) => text === "git push origin main")).toHaveLength(1);
    expect(app.shows("id:request-command")).toBe(true);
    expect(app.shows("id:request-question")).toBe(false);
    await app.host.send("app.daemonPush", { type: "request_answered", request: {
      id: "approval:docs:a1", kind: "approval", session_id: "docs", work_id: "w1", asked: "git push origin main", answer: "Allow", outcome: "approved",
      by: { kind: "device", device_name: "Pixel 8" }, answered_at: new Date().toISOString(), anchor: { approval_id: "a1" },
    } });
    await app.waitFor("id:request-sealed");
    expect(app.shows("id:request-card")).toBe(false);
    expect(app.texts()).toContain("Approved");
    expect(app.texts()).toContain("git push origin main");
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
    expect(app.texts()).toContain("You chose");
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
    await pressUntil(app, "id:tray-request", "id:request-card");
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
    // No Worker line draws a progress of its own: the judged one is the Work's (SPEC §11 Progress).
    expect(app.count("id:work-card-worker >> id:progress-mark")).toBe(0);
    await app.wait(500);
    await app.press("id:work-card-worker >> nth=2");
    await app.waitFor("id:session-status");
    await app.back();
    await app.waitFor("id:work-card-more");
    await app.wait(500);
    await app.press("id:work-card-more");
    await app.waitFor("id:work-detail");
  }, 20_000);

  test("a Work's state is its most urgent Worker's: Needs you over a running Worker, never Running", async () => {
    const a = "mh-aaaa", b = "mh-bbbb";
    const current_work = [{ work_id: "w2", revision: 1, title: "Docs refresh", status: "running", unread_result: false, attempt_session_id: a,
      worker_sessions: [
        { session_id: a, name: "Quickstart rewrite", provider: "claude", state: "running", summary: "Rewriting the quickstart", live: true, joined_at: "2026-11-12T11:00:00Z", updated_at: "2026-11-12T11:01:00Z" },
        { session_id: b, name: "Screenshot refresh", provider: "codex", state: "running", summary: "Retaking screenshots", live: true, joined_at: "2026-11-12T11:00:00Z", updated_at: "2026-11-12T11:01:00Z" },
      ] }];
    const app = await open(APP, { platform: "android", storage: paired, host: phoneHost({
      brain: { ...brain, current_work },
      worker_sessions: [session(a, { name: "Quickstart rewrite", status: "running" }), session(b, { name: "Screenshot refresh", status: "blocked", needs_attention: true, delegated: true, prompt: SIGN_IN })],
      conversations: { "brain-host": [result({ unread: false, title: "Docs refresh", work_id: "w2" })] },
    }) });
    await app.waitFor("id:work-card-state", { timeout: 10_000 });
    expect(app.texts()).toContain("Needs you");
    expect(app.count("id:work-card-worker")).toBe(2);
  }, 20_000);

  test("a delegated Worker's own report (--attention user_input) is Brain's: no card, no Needs you, no count (owner 2026-11-13, shot 11)", async () => {
    const a = "mh-cccc";
    const current_work = [{ work_id: "w3", revision: 1, title: "Plaza fix", status: "needs_input", unread_result: false, attempt_session_id: a,
      worker_sessions: [{ session_id: a, name: "ArcAIProdCloudflare", provider: "claude", state: "running", summary: "Plaza fix done, no rollback needed", live: true, joined_at: "2026-11-13T09:00:00Z", updated_at: "2026-11-13T09:01:00Z" }] }];
    const app = await open(APP, { platform: "android", storage: paired, host: phoneHost({
      brain: { ...brain, current_work },
      worker_sessions: [session(a, { name: "ArcAIProdCloudflare", status: "blocked", needs_attention: true, attention: "user_input", delegated: true, summary: "Plaza fix done, no rollback needed. Should I also purge the cache?" })],
      conversations: { "brain-host": [result({ unread: false, title: "Plaza fix", work_id: "w3" })] },
    }) });
    await app.waitFor("text:Plaza fix done, no rollback needed", { timeout: 10_000 });
    await app.wait(300);
    expect(app.texts()).toContain("1 running");
    expect(app.texts()).not.toContain("Needs you");
    expect(app.count("id:request-card")).toBe(0);
    expect(app.count("id:request-open-terminal")).toBe(0);
    expect(app.count("id:home-cat-dot")).toBe(0);
  }, 20_000);

  test("a Work with one Session is that Session's row, named without its Session id and never by its terminal footer", async () => {
    const id = "mh-17b3337d43f38037916f0e5b";
    const footer = "⏵⏵ bypass permissions on (shift+tab to cycle)";
    const current_work = [{ work_id: "w1", revision: 2, title: "Hello file", status: "running", unread_result: false, attempt_session_id: id,
      worker_sessions: [{ session_id: id, name: `Hello writer (${id})`, provider: "claude", state: "running", summary: footer, live: true, joined_at: "2026-11-12T11:00:00Z", updated_at: "2026-11-12T11:01:00Z" }] }];
    const app = await open(APP, { platform: "android", storage: paired, host: phoneHost({
      brain: { ...brain, current_work }, worker_sessions: [session(id, { name: `Hello writer (${id})`, status: "running", summary: footer, last_output_lines: [footer] })],
      conversations: { "brain-host": [result({ unread: false, title: "Hello file" })] },
    }) });
    await app.waitFor("id:work-card-one", { timeout: 10_000 });
    // No title row: the Work's title is not written over its one Session's row.
    expect(app.count("id:work-card-title")).toBe(0);
    const shown = app.texts().join(" ");
    expect(shown).toContain("Hello writer");
    expect(shown).not.toContain("(mh-");
    expect(shown).not.toContain("bypass permissions");
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

describe("a real question, as the owner meets it (owner 2026-11-13, shot 11)", () => {
  const QUESTION = "The plaza fix is live on test. Prod has zero channels, so the plaza stays empty there until a channel is added: do I add the Gemini channel to prod now, with the same group and mappings as test, or wait for your review of the test setup first?";

  test("Brain's question on a Work is that Work's card: the whole question, its choices as rows, Brain's pick; a choice answers at once", async () => {
    const a = "mh-cccc";
    const current_work = [{ work_id: "w4", revision: 1, title: "ArcAIProdCloudflare", status: "needs_input", unread_result: false, attempt_session_id: a,
      question: QUESTION, choices: ["Add it to prod now", "Wait for my review"], recommended: "Wait for my review", attention_since: new Date().toISOString(),
      worker_sessions: [{ session_id: a, name: "ArcAIProdCloudflare", provider: "claude", state: "running", summary: "Plaza fix done", live: true, joined_at: "2026-11-13T09:00:00Z", updated_at: "2026-11-13T09:01:00Z" }] }];
    const app = await open(APP, { platform: "android", storage: paired, host: phoneHost({
      brain: { ...brain, current_work },
      worker_sessions: [session(a, { name: "ArcAIProdCloudflare", status: "running", delegated: true })],
      conversations: { "brain-host": [{ id: "wr4", seq: 1, timestamp: new Date().toISOString(), kind: "status", source: "work_result", status: "session.needs_input",
        title: "ArcAIProdCloudflare", body: "Plaza fix done", work_id: "w4", work_session_id: a, session_name: "ArcAIProdCloudflare", unread: false,
        work_review_state: "queued", work_session_state: "open", work_result_current: true }] },
      replies: { brain_work_action: { type: "brain_work_action", status: "ok", admission: "accepted" } },
    }) });
    await app.waitFor("id:work-asks", { timeout: 10_000 });
    // The whole question, never cut; the choices; Brain's pick on its row; no terminal, no docked card.
    expect(app.texts()).toContain(QUESTION);
    expect(app.texts()).toContain("Brain asks");
    expect(app.shows("id:work-asks-pick")).toBe(true);
    expect(app.texts()).toContain("Something else…");
    expect(app.count("id:request-open-terminal")).toBe(0);
    expect(app.count("id:request-card")).toBe(0);
    expect(app.texts()).toContain("1 needs you");
    await app.press("id:work-asks-choice-Wait for my review");
    await app.waitUntil(() => framesOf(app.host.posts(), "brain_work_action").length > 0);
    expect(framesOf(app.host.posts(), "brain_work_action").at(-1)).toMatchObject({ id: "w4", action: "reply", text: "Wait for my review" });
  }, 20_000);

  test("a card the computer cannot draw says Open, which opens the Session, never only its terminal (owner 11-13 19:4x)", async () => {
    // A question whose options only the agent's transcript holds, read without it: the computer cannot draw it.
    const odd = { id: "p-9", kind: "choice", provider: "claude", title: "Pick a region for the new bucket", call_id: "c-9", questions: [{ question: "Pick a region for the new bucket", options: [] }] };
    const app = await open(APP, { platform: "android", storage: paired, host: phoneHost({ brain, worker_sessions: [session("infra", { delegated: true, prompt: odd })] }) });
    await app.waitFor("id:request-open-terminal", { timeout: 10_000 });
    expect(app.texts()).toContain("Open");
    expect(app.texts()).not.toContain("Open terminal");
    expect(app.texts()).toContain("Pick a region for the new bucket");
    await app.press("id:request-open-terminal");
    await app.waitFor("id:session-status");
  }, 20_000);

  test("Later puts a card aside: it leaves the thread's foot, the tray keeps it, and its tray row brings it back", async () => {
    const app = await open(APP, {
      platform: "android", storage: paired,
      host: phoneHost({ brain, worker_sessions: [session("docs", { delegated: true, approvals: [approval("a1", "git push origin main")] })] }),
    });
    await app.waitFor("id:request-card", { timeout: 10_000 });
    await app.press("id:request-later");
    await app.waitUntil(() => !app.shows("id:request-card"));
    // Still open: the count keeps it, and so does the tray.
    expect(app.texts()).toContain("1 needs you");
    await app.press("id:state-control");
    await app.waitFor("id:tray-needs");
    await app.press("id:tray-needs >> id:tray-request >> nth=0");
    await app.waitUntil(() => !app.shows("id:tray"));
    await app.waitFor("id:request-card");
    expect(app.texts()).toContain("git push origin main");
  }, 20_000);
});
