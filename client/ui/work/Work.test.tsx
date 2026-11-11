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
  await app.waitFor("text:Work · 1");
  await app.press("id:open-work");
  await app.waitFor("id:work-page");
  return app;
}

describe("Brain's Work", () => {
  test("Brain's header says how many need you, and the Work page groups it by what it asks of you", async () => {
    const app = await workApp();
    await app.waitFor("text:pick a name");
    const shown = app.texts();
    for (const label of ["Needs you", "Running", "Back", "Queued", "ship the beta", "fix sync", "write the docs", "Claude Code · perpetuo · " + shown.find((t) => t.startsWith("Claude Code · perpetuo · "))?.slice(25)]) expect(shown).toContain(label);
    expect(shown.join(" ")).toContain("1 needs you · 1 running · 1 back · 1 queued");
    expect(shown.join(" ")).toContain("Verifying · Running the release checks");
  }, 20_000);

  test("answers a question with one tap, and Brain has it", async () => {
    const app = await workApp();
    await app.press("id:work-w-q");
    await app.waitFor("id:work-detail");
    expect(app.texts()).toContain("Which name do we ship?");
    await app.press("id:work-action-answer-Mewla");
    await app.waitFor("text:Brain has your answer");
    expect(framesOf(app.host.posts(), "brain_work_action")[0]).toMatchObject({ id: "w-q", action: "reply", text: "Mewla" });
  }, 20_000);

  test("a question is answered right on its slip, and a page that acted closes back to the list", async () => {
    const app = await workApp();
    await app.waitFor("text:pick a name");
    await app.press("id:work-action-answer-Zen");
    await app.waitFor("id:work-notice");
    expect(app.texts()).toContain("Brain has your answer");
    expect(framesOf(app.host.posts(), "brain_work_action")[0]).toMatchObject({ id: "w-q", action: "reply", text: "Zen" });
    expect(app.shows("id:work-detail")).toBe(false);
  }, 20_000);

  test("a Work's page says what it waits for when its summary says something else", async () => {
    const waiting = brain();
    waiting.current_work.push({ work_id: "w-wait", revision: 1, title: "merge-the-push-change", status: "waiting", progress_mode: "ready", attention_state: "queued", unread_result: false, summary: "Pushed the change for review.", wait_for: "Reviewer approval on the pull request", updated_at: now() } as never);
    const app = await workApp({ brain: waiting });
    await app.waitFor("text:merge the push change");
    await app.press("id:work-w-wait");
    await app.waitFor("id:work-wait-for");
    expect(app.texts()).toContain("Waiting for: Reviewer approval on the pull request");
  }, 20_000);

  test("Not needed anymore asks first, and only then tells Brain", async () => {
    const app = await workApp();
    await app.press("id:work-w-q");
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

  test("a result that came back is accepted with one tap and its unread mark is cleared on opening", async () => {
    const app = await workApp();
    await app.press("id:work-w-back");
    await app.waitFor("id:work-detail");
    expect(framesOf(app.host.posts(), "brain_work_read")[0]).toMatchObject({ id: "w-back" });
    expect(app.texts()).toContain("Fixed the sync race and added a test.");
    await app.press("id:work-action-close");
    await app.waitUntil(() => framesOf(app.host.posts(), "brain_work_action").length === 1);
    expect(framesOf(app.host.posts(), "brain_work_action")[0]).toMatchObject({ id: "w-back", action: "close" });
  }, 20_000);

  test("with nothing open it says so", async () => {
    const app = await open(APP, { host: phoneHost({ brain: { current_work: [] } }), storage: paired, platform: "android", network: expoTokenService });
    await app.waitFor("id:open-work");
    await app.press("id:open-work");
    await app.waitFor("text:No open Work");
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
});
