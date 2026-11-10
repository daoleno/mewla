import { describe, expect, open, test } from "nsh/test";
import { expoTokenService, framesOf, item, paired, phoneHost } from "../testing/fixtures";

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

  test("Not needed anymore asks first, and only then tells Brain", async () => {
    const app = await workApp();
    await app.press("id:work-w-q");
    await app.waitFor("id:work-action-dismiss");
    let decline = true;
    app.onDialog((dialog) => (dialog.title === "Not needed anymore?" ? (decline ? "Cancel" : "Not needed anymore") : null));
    await app.press("id:work-action-dismiss");
    await app.wait(300);
    expect(framesOf(app.host.posts(), "brain_work_action")).toHaveLength(0);
    decline = false;
    await app.press("id:work-action-dismiss");
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
    await app.waitFor("id:run-r1");
    await app.press("id:run-r1");
    await app.waitFor("id:open-work-file");
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
});
