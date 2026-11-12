import { describe, expect, open, test } from "nsh/test";
import { answering, expoTokenService, framesOf, item, paired, phoneHost } from "../testing/fixtures";

const APP = "../FakeDaemonApp.tsx";
const now = () => new Date().toISOString();

function brain() {
  return {
    workers: [{ id: "%7", name: "release", status: "running", phase: "verifying", summary: "Running the release checks", command: "claude", cwd: "/home/me/perpetuo" }],
    current_work: [
      { work_id: "w-q", revision: 1, title: "pick-a-name", status: "needs_input", progress_mode: "waiting", unread_result: false, question: "Which name do we ship?", choices: ["Mewla", "Zen"], attention_since: now(), updated_at: now() },
      { work_id: "w-run", revision: 1, title: "ship-the-beta", status: "running", progress_mode: "owned", attempt_session_id: "%7", unread_result: false, updated_at: now() },
      { work_id: "w-back", revision: 1, title: "fix-sync", status: "waiting", progress_mode: "ready", attention_state: "queued", unread_result: true, summary: "Fixed the sync race and added a test.", attention_since: now(), updated_at: now() },
    ],
    open_work: [{ work_id: "w-queued", title: "write-the-docs", status: "open", progress_mode: "owned" }],
  };
}

async function workApp(daemon: Record<string, unknown> = { brain: brain() }) {
  const app = await open(APP, { host: phoneHost(daemon), storage: paired, platform: "android", network: expoTokenService });
  await app.waitFor("id:composer");
  // Brain's snapshot is in: the state control counts what waits on you.
  await app.waitUntil(() => app.texts().some((text) => /needs? you/.test(text)), { timeout: 10_000 });
  return app;
}

/** Work detail, as a Brain notification about it opens it (on home, so Back leads home). */
async function openWork(app: Awaited<ReturnType<typeof workApp>>, workId: string) {
  await app.host.send("links.open", { url: `mewla://brain/work/${workId}` });
  await app.waitFor("id:work-detail");
}

describe("Brain's Work", () => {
  test("the tray is the Work list: a Work that waits on you is under Needs you and counts, and its row opens Work detail", async () => {
    const app = await workApp();
    await app.press("id:state-control");
    await app.waitFor("id:tray-needs");
    await app.press("text:Which name do we ship?");
    await app.waitFor("id:work-detail");
    expect(app.texts()).toContain("Which name do we ship?");
  }, 20_000);

  test("answers a question with one tap, and Brain has it", async () => {
    const app = await workApp();
    await openWork(app, "w-q");
    expect(app.texts()).toContain("Which name do we ship?");
    await app.press("id:work-action-answer-Mewla");
    await app.waitUntil(() => framesOf(app.host.posts(), "brain_work_action").length === 1);
    expect(framesOf(app.host.posts(), "brain_work_action")[0]).toMatchObject({ id: "w-q", action: "reply", text: "Mewla" });
  }, 20_000);

  test("a page that acted closes back to where it was opened", async () => {
    const app = await workApp();
    await openWork(app, "w-q");
    await app.press("id:work-action-answer-Zen");
    await app.waitUntil(() => !app.shows("id:work-detail"));
    expect(framesOf(app.host.posts(), "brain_work_action")[0]).toMatchObject({ id: "w-q", action: "reply", text: "Zen" });
    expect(app.shows("id:home-header")).toBe(true);
  }, 20_000);

  test("a Work's page says what it waits for when its summary says something else", async () => {
    const waiting = brain();
    waiting.current_work.push({ work_id: "w-wait", revision: 1, title: "merge-the-push-change", status: "waiting", progress_mode: "ready", attention_state: "queued", unread_result: false, summary: "Pushed the change for review.", wait_for: "Reviewer approval on the pull request", updated_at: now() } as never);
    const app = await workApp({ brain: waiting });
    await openWork(app, "w-wait");
    await app.waitFor("id:work-wait-for");
    expect(app.texts()).toContain("Waiting for: Reviewer approval on the pull request");
  }, 20_000);

  test("Not needed anymore asks first, and only then tells Brain", async () => {
    const app = await workApp();
    await openWork(app, "w-q");
    await app.waitFor("id:work-action-dismiss");
    let decline = true;
    app.onDialog((dialog) => (dialog.title === "Not needed anymore?" ? (decline ? "Cancel" : "Not needed anymore") : null));
    await answering(app, () => app.press("id:work-action-dismiss"));
    await app.wait(300);
    expect(framesOf(app.host.posts(), "brain_work_action")).toHaveLength(0);
    decline = false;
    await answering(app, () => app.press("id:work-action-dismiss"));
    await app.waitUntil(() => framesOf(app.host.posts(), "brain_work_action").length === 1);
    expect(framesOf(app.host.posts(), "brain_work_action")[0]).toMatchObject({ id: "w-q", action: "dismiss" });
  }, 20_000);

  test("Snooze 1 hour tells the computer the length, which ends it on its own clock", async () => {
    const app = await workApp();
    await openWork(app, "w-q");
    await app.waitFor("text:Snooze 1 hour");
    await app.press("id:work-action-snooze");
    await app.waitUntil(() => framesOf(app.host.posts(), "brain_work_action").length === 1);
    expect(framesOf(app.host.posts(), "brain_work_action")[0]).toMatchObject({ id: "w-q", action: "snooze", snooze_seconds: 3600 });
    expect("snooze_until" in framesOf(app.host.posts(), "brain_work_action")[0]!).toBe(false);
  }, 20_000);

  test("Workers: every Session that joined, oldest first; a live one with its progress, and it opens its Session", async () => {
    const daemon = brain();
    const at = now();
    (daemon.current_work[1] as Record<string, unknown>).worker_sessions = [
      { session_id: "%5", name: "planner", provider: "codex", state: "done", summary: "Plan written", live: false, joined_at: at, ended_at: at, updated_at: at },
      { session_id: "%7", name: "release", provider: "claude", state: "running", summary: "Running the release checks", phase: "verifying", live: true, joined_at: at, updated_at: at },
      { session_id: "%8", name: "builder", provider: "claude", state: "failed", summary: "Build broke on Windows", live: false, joined_at: at, ended_at: at, updated_at: at },
      { session_id: "%9", name: "verifier", provider: "claude", state: "ended", summary: "", live: false, joined_at: at, ended_at: at, updated_at: at },
    ];
    const app = await workApp({ brain: daemon, worker_sessions: [{ id: "%7", name: "release", status: "running", phase: "verifying", summary: "Running the release checks", command: "claude", provider: "claude", cwd: "/home/me/perpetuo", updated_at: Date.now() }] });
    await openWork(app, "w-run");
    await app.waitFor("id:work-workers");
    expect(app.count("id:work-worker")).toBe(4);
    expect(app.texts()).toContain("OpenAI Codex · planner");
    expect(app.texts()).toContain("Failed · Build broke on Windows");
    expect(app.texts()).toContain("Ended");
    expect(app.texts()).toContain("Claude Code · release");
    expect(app.count("id:work-worker-progress")).toBe(1);
    expect(app.enabled("id:work-worker >> nth=0")).toBe(false);
    await app.press("id:work-worker >> nth=1");
    await app.waitFor("id:session-status");
  }, 20_000);

  test("a closed Work's page comes from the computer: its title, how it ended, who worked on it", async () => {
    const at = now();
    const app = await workApp({ brain: brain(), replies: { brain_work_detail: { type: "brain_work_detail", work: {
      work_id: "w-old", revision: 9, title: "rename-the-app", status: "done", created_at: at, updated_at: at,
      worker_sessions: [{ session_id: "%3", name: "renamer", provider: "claude", state: "done", summary: "Renamed everywhere", live: false, joined_at: at, ended_at: at, updated_at: at }],
    } } } });
    await openWork(app, "w-old");
    await app.waitFor("id:work-workers");
    expect(framesOf(app.host.posts(), "brain_work_detail")[0]).toMatchObject({ id: "w-old" });
    expect(app.texts()).toContain("rename-the-app");
    // The state line: how it ended, and since when.
    expect(app.texts().some((text) => /^Done · since /.test(text))).toBe(true);
    expect(app.texts()).toContain("Claude Code · renamer");
    expect(app.texts()).not.toContain("This Work is no longer open");
  }, 20_000);

  test("Work detail's top block: the eyebrow names the project on one line, then the title and the state line", async () => {
    const app = await workApp();
    await openWork(app, "w-run");
    await app.waitFor("id:work-eyebrow");
    expect(app.texts()).toContain("Work · perpetuo");
    expect(app.texts()).toContain("ship the beta");
    expect(app.shows("id:work-state")).toBe(true);
    // Ask Brain about this, then the ways to move it on: Stop with them, not with the links.
    expect(app.count("id:work-links >> id:work-action-ask")).toBe(1);
    expect(app.count("id:work-moves >> id:work-action-stop")).toBe(1);
  }, 20_000);

  test("the Work file its Worker writes into is a row with the file's name, and opens the file", async () => {
    const file = { id: "wf-run", project: "perpetuo", path: "perpetuo/ship-the-beta.md", title: "Ship the beta", mtime: now(), body: "# Ship the beta\n\nRelease checks.\n",
      frontmatter: { id: "wf-run", created: now(), worker_session: "%7" } };
    const app = await workApp({ brain: brain(), work_items: [file] });
    await openWork(app, "w-run");
    await app.waitFor("id:work-file", { timeout: 10_000 });
    expect(app.texts()).toContain("ship-the-beta.md");
    await app.press("id:work-file");
    await app.waitFor("id:work-file-page");
  }, 20_000);

  test("Stop asks with the Work's title, and only then tells Brain", async () => {
    const app = await workApp();
    await openWork(app, "w-run");
    await app.waitFor("id:work-action-stop");
    app.onDialog((dialog) => (dialog.title === "Stop “ship the beta”?" ? "Stop" : null));
    await answering(app, () => app.press("id:work-action-stop"));
    await app.waitUntil(() => framesOf(app.host.posts(), "brain_work_action").length === 1);
    expect(framesOf(app.host.posts(), "brain_work_action")[0]).toMatchObject({ id: "w-run", action: "stop" });
  }, 20_000);

  test("the dock replies to Brain about this Work, and the page closes once Brain has it", async () => {
    const app = await workApp();
    await openWork(app, "w-run");
    await app.waitFor("id:work-detail >> id:composer");
    await app.type("id:work-detail >> id:composer", "check the logs too");
    await app.waitUntil(() => app.value("id:work-detail >> id:composer") === "check the logs too");
    await app.press("id:work-detail >> id:send");
    await app.waitUntil(() => framesOf(app.host.posts(), "brain_work_action").length === 1);
    expect(framesOf(app.host.posts(), "brain_work_action")[0]).toMatchObject({ id: "w-run", action: "reply", text: "check the logs too" });
    await app.waitUntil(() => !app.shows("id:work-detail"));
  }, 20_000);

  test("a result that came back is accepted with one tap and its unread mark is cleared on opening", async () => {
    const app = await workApp();
    await openWork(app, "w-back");
    await app.waitUntil(() => framesOf(app.host.posts(), "brain_work_read").length > 0);
    expect(framesOf(app.host.posts(), "brain_work_read")[0]).toMatchObject({ id: "w-back" });
    expect(app.texts()).toContain("Fixed the sync race and added a test.");
    await app.press("id:work-action-close");
    await app.waitUntil(() => framesOf(app.host.posts(), "brain_work_action").length === 1);
    expect(framesOf(app.host.posts(), "brain_work_action")[0]).toMatchObject({ id: "w-back", action: "close" });
  }, 20_000);
});

describe("Ask Brain about this", () => {
  const quote = "Re: fix sync (work w-back)\n> Fixed the sync race and added a test.\n";
  // Brain's thread is known: its composer holds that thread's draft.
  const daemon = () => ({ brain: { ...brain(), chat_thread_id: "t-now", host_worker: { id: "brain-host", name: "Brain", cwd: "/home/me", command: "claude" } } });
  /** Work detail from the tray's row, pushed over home, which stays as it was under it. */
  async function askFromTray(app: Awaited<ReturnType<typeof workApp>>) {
    await app.press("id:state-control");
    await app.waitFor("text:fix sync");
    await app.press("text:fix sync");
    await app.waitFor("id:work-detail");
    await app.press("id:work-action-ask");
    await app.waitFor("id:home-header");
  }

  test("the quote lands in Brain's open composer", async () => {
    const app = await workApp(daemon());
    await askFromTray(app);
    await app.waitUntil(() => app.value("id:composer") === quote);
  }, 20_000);

  test("what was typed in the composer stays, the quote below it", async () => {
    const app = await workApp(daemon());
    await app.type("id:composer", "also check the logs");
    await app.waitUntil(() => app.value("id:composer") === "also check the logs");
    await askFromTray(app);
    await app.waitUntil(() => app.value("id:composer") === `also check the logs\n\n${quote}`);
  }, 20_000);

  test("from a notification's Work page too: what was typed a moment before is kept", async () => {
    const app = await workApp(daemon());
    await app.type("id:composer", "also check the logs");
    await app.waitUntil(() => app.value("id:composer") === "also check the logs");
    await openWork(app, "w-back");
    await app.press("id:work-action-ask");
    await app.waitFor("id:home-header");
    await app.waitUntil(() => app.value("id:composer") === `also check the logs\n\n${quote}`);
  }, 20_000);
});

describe("a Calendar run's Work file", () => {
  const work = {
    id: "wf-1", project: "calendar", path: "calendar/wf-1.md", title: "Daily digest", mtime: "2026-07-17T03:01:00Z",
    body: "# Daily digest\n\nRead the three papers.\n\n## Calendar metadata\n- item: c1\n",
    frontmatter: { id: "wf-1", kind: "calendar_action", created: "2026-07-17T03:00:00Z", extra: { calendar_item_id: "c1", calendar_run_id: "r1" } },
  };
  const calendarItem = item("Paper digest", 20, { id: "c1", kind: "scheduled_action", due_at: new Date(Date.now() + 20 * 3_600_000).toISOString(), action_instruction: "Read papers.", source_thread_id: "t1",
    runs: [{ id: "r1", status: "completed", scheduled_for: "2026-07-17T03:00:00Z", started_at: "2026-07-17T03:00:00Z", work_id: "wf-1", result_preview: "Three papers read." }] });

  test("opens from its run, reads without the metadata, saves an edit and marks it done", async () => {
    const app = await open(APP, { host: phoneHost({ calendar_items: [calendarItem], work_items: [work] }), storage: paired, platform: "android", network: expoTokenService });
    await app.host.send("links.open", { url: "mewla://calendar?id=c1" });
    // The run's time in the phone's zone: the page has redrawn with it (a slow runner gets the zone late).
    await app.waitFor("text:Fri, Jul 17, 11:00 AM · Done", { timeout: 10_000 });
    await app.press("id:run-r1");
    await app.waitFor("id:open-work-file", { timeout: 10_000 });
    await app.press("id:open-work-file");
    await app.waitFor("id:work-file-page");
    expect(app.texts()).toContain("Read the three papers.");
    expect(app.texts().join(" ")).not.toContain("Calendar metadata");
    await app.press("id:work-file-edit");
    await app.waitFor("id:work-file-body");
    await app.type("id:work-file-body", " Also the fourth.");
    await app.waitUntil(() => framesOf(app.host.posts(), "write_work_item").length === 1);
    expect(String(framesOf(app.host.posts(), "write_work_item")[0].body)).toContain("Also the fourth.");
    expect(framesOf(app.host.posts(), "write_work_item")[0]).toMatchObject({ id: "wf-1", base_mtime: "2026-07-17T03:01:00.000Z" });
    await app.waitFor("text:Saved");
    await app.press("id:work-file-done");
    await app.waitUntil(() => framesOf(app.host.posts(), "write_work_item").length === 2);
    expect((framesOf(app.host.posts(), "write_work_item")[1].frontmatter as Record<string, unknown>).done).toBeTruthy();
  }, 25_000);

  async function openWorkFile(daemon: Record<string, unknown>) {
    const app = await open(APP, { host: phoneHost({ calendar_items: [calendarItem], work_items: [work], ...daemon }), storage: paired, platform: "android", network: expoTokenService });
    await app.host.send("links.open", { url: "mewla://calendar?id=c1&run=r1" });
    await app.waitFor("id:calendar-run-page", { timeout: 10_000 });
    // The run's time in the phone's zone: the page has redrawn with it, so the press is not lost to that redraw (a slow runner gets the zone late).
    await app.waitFor("text:Fri, Jul 17, 11:00 AM · Done", { timeout: 10_000 });
    await app.waitFor("id:open-work-file", { timeout: 10_000 });
    await app.press("id:open-work-file");
    await app.waitFor("id:work-file-page", { timeout: 10_000 });
    return app;
  }

  test("a link with its run opens the run; the Work file's From Calendar goes back to that run", async () => {
    const app = await openWorkFile({});
    await app.waitFor("id:work-file-calendar-link", { timeout: 10_000 });
    await app.press("id:work-file-calendar-link");
    await app.waitFor("id:calendar-run-page");
    await app.wait(300);
    expect(app.shows("id:work-file-page")).toBe(false);
  }, 25_000);

  // FINAL-VERIFY 6387f88f m3 143/141: the computer's echo of the page's own save came before the
  // save's answer and was taken for someone else's newer version; autosave then stopped.
  test("its own autosave is never a newer version on the server, and the next edit saves too", async () => {
    const app = await openWorkFile({});
    await app.press("id:work-file-edit");
    await app.waitFor("id:work-file-body");
    await app.type("id:work-file-body", " XYZ");
    await app.waitUntil(() => framesOf(app.host.posts(), "write_work_item").length === 1, { timeout: 10_000 });
    await app.waitFor("text:Saved", { timeout: 10_000 });
    await app.wait(300);
    expect(app.shows("id:work-file-newer")).toBe(false);
    await app.type("id:work-file-body", "Q");
    await app.waitUntil(() => framesOf(app.host.posts(), "write_work_item").length === 2, { timeout: 10_000 });
    await app.waitFor("text:Saved", { timeout: 10_000 });
    await app.wait(300);
    expect(app.shows("id:work-file-newer")).toBe(false);
    expect(String(framesOf(app.host.posts(), "write_work_item")[1].body)).toContain("XYZQ");
  }, 25_000);

  test("a change made on the computer while an edit is unsaved asks before loading it", async () => {
    const app = await openWorkFile({});
    await app.press("id:work-file-edit");
    await app.waitFor("id:work-file-body");
    await app.type("id:work-file-body", " mine");
    await app.host.send("app.daemonPush", { type: "work_item_changed", id: "wf-1", work_item: { ...work, body: work.body + "theirs\n", mtime: new Date(Date.now() + 60_000).toISOString() } });
    await app.waitFor("id:work-file-newer", { timeout: 10_000 });
    await app.wait(1_000);
    expect(framesOf(app.host.posts(), "write_work_item")).toHaveLength(0);
  }, 25_000);

  // FINAL-VERIFY 6387f88f m3 142: Delete removed the file, then said "Delete failed. cannot read
  // property 'frontmatter' of undefined" and stayed.
  test("Delete removes the file and leaves the page", async () => {
    const app = await openWorkFile({});
    app.onDialog(() => "Delete");
    await app.press("id:work-file-delete");
    await app.waitUntil(() => framesOf(app.host.posts(), "delete_work_item").length === 1, { timeout: 10_000 });
    await app.waitUntil(() => !app.shows("id:work-file-page"), { timeout: 10_000 });
    expect(app.texts().join(" ")).not.toContain("Delete failed");
  }, 25_000);

  // FINAL-VERIFY 6387f88f m3 136/132: removed on the computer while open, the page kept the old body.
  test("a file removed on the computer while its page is open says Work item not found", async () => {
    const app = await openWorkFile({});
    await app.waitFor("text:Read the three papers.");
    await app.host.send("app.daemonPush", { type: "work_item_deleted", id: "wf-1", path: "calendar/wf-1.md" });
    await app.waitFor("text:Work item not found", { timeout: 10_000 });
    expect(app.shows("id:work-file-delete")).toBe(false);
    expect(app.texts()).not.toContain("Read the three papers.");
  }, 25_000);

  test("a save the computer refuses says so, in its words", async () => {
    const app = await openWorkFile({ replies: { write_work_item: { type: "error", message: "disk is full" } } });
    await app.press("id:work-file-edit");
    await app.waitFor("id:work-file-body");
    await app.type("id:work-file-body", " Also the fourth.");
    await app.waitFor("text:Save failed. disk is full", { timeout: 10_000 });
  }, 25_000);

  test("a link in the file opens: a web page in the browser, a path in Brain's file preview, and Back returns to the file", async () => {
    const linked = { ...work, body: "# Daily digest\n\nSee [the paper](https://example.com/paper) and [my notes](/home/me/worklog/notes.md).\n" };
    const host_worker = { id: "brain-host", name: "Brain", cwd: "/home/me", command: "claude", started_at: 1_762_840_800_000, process_id: 4242 };
    const app = await openWorkFile({ work_items: [linked], brain: { chat_thread_id: "t-now", host_worker } });
    await app.waitFor("text:the paper", { timeout: 10_000 });
    await app.press("text:the paper");
    await app.waitUntil(() => app.openedUrls().includes("https://example.com/paper"));
    await app.press("text:my notes");
    await app.waitFor("id:file-preview", { timeout: 10_000 });
    await app.back();
    await app.waitFor("id:work-file-page");
  }, 25_000);
});
