import { describe, expect, open, test } from "nsh/test";
import { expoTokenService, framesOf, paired, phoneHost } from "../testing/fixtures";

const APP = "../FakeDaemonApp.tsx";
const SLOW = { timeout: 10_000 };
const MIN = 60_000;
const HOUR = 3_600_000;
const ago = (ms: number) => new Date(Date.now() - ms).toISOString();

function trigger(id: string, title: string, patch: Record<string, unknown> = {}) {
  return {
    id, title, check: "~/pr-loop/check.sh", interval_seconds: 600, timeout_seconds: 60, work_id: `w-${id}`,
    instruction: "Review each new PR head.", created_at: ago(10 * HOUR), updated_at: ago(MIN), revision: 1, state: "watching",
    last_checked_at: ago(2 * MIN), ...patch,
  };
}

const watching = () => trigger("t-pr", "PR heads", {
  firings: [{ seq: 1, at: ago(5 * HOUR), outputs: 1, payload: "/p/1.txt", event_id: "e1", handled_at: ago(4 * HOUR) }, { seq: 2, at: ago(5 * MIN), outputs: 2, payload: "/p/2.txt", event_id: "e2" }],
  pending_changes: 1,
});
const failing = () => trigger("t-ci", "CI on main", { created_at: ago(HOUR), failing: true, failures: 4, last_failure: "exit status 1\ngh: HTTP 502", state: "failing" });

async function calendarApp(daemon: Record<string, unknown>) {
  const app = await open(APP, { host: phoneHost(daemon), storage: paired, platform: "android", network: expoTokenService });
  await app.host.send("links.open", { url: "mewla://calendar" });
  await app.waitFor("id:calendar-page");
  return app;
}

describe("Triggers in Calendar", () => {
  test("Calendar lists the computer's Triggers above Today, each with what it did last; none, no section", async () => {
    const app = await calendarApp({ calendar_items: [], triggers: [watching(), failing()] });
    await app.waitFor("id:trigger-section", SLOW);
    expect(app.texts()).toContain("Triggers");
    expect(app.texts()).toContain("Every 10 min · fired 5 min ago · with Brain");
    expect(app.texts()).toContain("Failing: exit status 1: gh: HTTP 502 · checked 2 min ago");
    // Only the failing one carries the seal.
    expect(app.count("id:trigger-row-t-ci >> id:trigger-needs")).toBe(1);
    expect(app.count("id:trigger-row-t-pr >> id:trigger-needs")).toBe(0);

    const empty = await calendarApp({ calendar_items: [] });
    await empty.waitUntil(() => empty.texts().some((text) => text.includes("GMT+8")), SLOW);
    expect(empty.shows("id:trigger-section")).toBe(false);
  }, 30_000);

  test("a Trigger's page: its check, what Brain is told, its firings newest first; Pause, Resume and Check now reach the computer", async () => {
    const app = await calendarApp({ calendar_items: [], triggers: [watching()] });
    await app.press("id:trigger-row-t-pr");
    await app.waitFor("id:trigger-page", SLOW);
    expect(app.texts()).toContain("~/pr-loop/check.sh");
    expect(app.texts()).toContain("Review each new PR head.");
    expect(app.texts()).toContain("Every 10 min · stops after 60 s");
    expect(app.texts()).toContain("1 more change waits for Brain to handle the last one.");
    const firings = app.texts().filter((text) => / changes? · /.test(text));
    expect(firings).toHaveLength(2);
    expect(firings[0]).toMatch(/2 changes · with Brain$/);
    expect(firings[1]).toMatch(/1 change · handled$/);

    await app.press("id:trigger-pause");
    await app.waitFor("id:trigger-resume", SLOW);
    expect(app.texts()).toContain("Paused · fired 5 min ago · with Brain");
    await app.press("id:trigger-resume");
    await app.waitFor("id:trigger-pause", SLOW);
    await app.press("id:trigger-check-now");
    await app.waitUntil(() => framesOf(app.host.posts(), "check_trigger").length === 1, SLOW);
    expect(framesOf(app.host.posts(), "pause_trigger").map((f) => f.id)).toEqual(["t-pr"]);
    expect(framesOf(app.host.posts(), "resume_trigger").map((f) => f.id)).toEqual(["t-pr"]);
  }, 30_000);

  test("a refused check says the computer's reason; Delete asks first, then the Trigger is gone", async () => {
    const app = await calendarApp({ calendar_items: [], triggers: [watching()], replies: { check_trigger: { type: "error", message: "the check is already running" } } });
    await app.press("id:trigger-row-t-pr");
    await app.waitFor("id:trigger-page", SLOW);
    await app.press("id:trigger-check-now");
    await app.waitFor("id:trigger-failure", SLOW);
    expect(app.texts().join(" ")).toContain("the check is already running");

    let asked = "";
    app.onDialog((dialog) => {
      asked = dialog.title ?? "";
      return dialog.title === "Delete Trigger?" ? "Delete" : null;
    });
    await app.press("id:trigger-delete");
    await app.waitUntil(() => framesOf(app.host.posts(), "delete_trigger").length === 1, SLOW);
    await app.waitUntil(() => !app.shows("id:trigger-page"), SLOW);
    expect(asked).toBe("Delete Trigger?");
    await app.waitFor("id:calendar-page");
    expect(app.shows("id:trigger-section")).toBe(false);
  }, 30_000);

  test("a failing Trigger, or a firing not handled for three hours, is under Needs you; its row opens the Trigger", async () => {
    const late = trigger("t-late", "Release notes", { firings: [{ seq: 1, at: ago(3 * HOUR + MIN), outputs: 1, payload: "/p/1.txt", event_id: "e1" }] });
    const app = await open(APP, { host: phoneHost({ triggers: [failing(), late, watching()] }), storage: paired, platform: "android", network: expoTokenService });
    await app.waitFor("id:composer");
    await app.waitUntil(() => app.texts().some((text) => /needs? you/.test(text)), SLOW);
    await app.press("id:state-control");
    await app.waitFor("id:tray-needs");
    expect(app.count("id:tray-needs >> id:tray-request")).toBe(2);
    expect(app.texts()).toContain("Check keeps failing");
    expect(app.texts()).toContain("Not handled");
    expect(app.texts()).toContain("Release notes");
    expect(app.count("id:tray-needs >> id:tray-open")).toBe(2);
    await app.press("label:Release notes, Not handled");
    await app.waitFor("id:trigger-page", SLOW);
    expect(app.texts()).toContain("It fired 3 h ago and Brain has not handled it.");
  }, 30_000);
});
