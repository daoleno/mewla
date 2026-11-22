import { describe, expect, open, test } from "nsh/test";
import { answering, expoTokenService, framesOf, item, paired, pairedTwo, phoneHost, zoneTable } from "../testing/fixtures";

const APP = "../FakeDaemonApp.tsx";

/** The phone's date (Shanghai) `days` from today: a day key of the month. */
function dayIn(days: number): string {
  return new Date(Date.now() + 8 * 3_600_000 + days * 86_400_000).toISOString().slice(0, 10);
}
const SLOW = { timeout: 10_000 };

/** "6:20 PM": an instant on the test phone's clock (Shanghai, GMT+8 all year). */
function shanghaiTime(ms: number): string {
  const at = new Date(ms + 8 * 3_600_000);
  const hour = at.getUTCHours();
  return `${hour % 12 || 12}:${String(at.getUTCMinutes()).padStart(2, "0")} ${hour < 12 ? "AM" : "PM"}`;
}

/** Today's now line is on the phone's clock: its zone is in (a slow runner gets it late, and the agenda redraws then). */
async function onPhoneClock(app: Awaited<ReturnType<typeof open>>) {
  await app.waitUntil(() => [Date.now(), Date.now() - 60_000].some((at) => app.shows(`id:calendar-now >> text:${shanghaiTime(at)}`)), SLOW);
}

/** The Calendar tab once the phone's zone is in. */
async function calendarApp(daemon: Record<string, unknown>, extra: Record<string, (data: any) => unknown> = {}) {
  const app = await open(APP, { host: phoneHost(daemon, extra), storage: paired, platform: "android", network: expoTokenService });
  await app.host.send("links.open", { url: "mewla://calendar" });
  await app.waitFor("id:calendar-page");
  await onPhoneClock(app);
  return app;
}

describe("the Calendar tab", () => {
  test("lists what is planned by day, hides cancelled items and opens one", async () => {
    const app = await calendarApp({ calendar_items: [item("Call the bank", 1), item("Dentist", 26, { kind: "event", start_at: new Date(Date.now() + 26 * 3_600_000).toISOString(), end_at: new Date(Date.now() + 27 * 3_600_000).toISOString() }), item("Old plan", 2, { status: "cancelled" })] });
    await app.waitFor("id:calendar-item-Call the bank", SLOW);
    await app.waitFor("text:Dentist", SLOW);
    expect(app.texts()).not.toContain("Old plan");
    // Time first on the phone's clock, then one quiet line: who acts, no status word, no zone, no tile (w-cal).
    expect(app.shows(`id:calendar-item-Call the bank >> text:${shanghaiTime(Date.now() + 3_600_000)}`) || app.shows(`id:calendar-item-Call the bank >> text:${shanghaiTime(Date.now() + 3_540_000)}`)).toBe(true);
    expect(app.shows("id:calendar-item-Call the bank >> text:Reminder")).toBe(true);
    expect(app.texts().join(" ")).not.toMatch(/Scheduled|GMT/);
    expect(app.count("id:calendar-item-lead")).toBe(0);
    await app.press("id:calendar-item-Call the bank");
    await app.waitFor("id:calendar-item-page", SLOW);
    expect(app.shows("id:item-lead")).toBe(true);
    expect(app.texts().join(" ")).toMatch(/at \d+:\d\d (AM|PM)/);
  }, 20_000);

  test("a no to the first connection's prompt: Calendar says reminders are disabled and leads to Settings, without asking again", async () => {
    const calls: string[] = [];
    const app = await calendarApp({ calendar_items: [] }, {
      "push.permission": () => ({ granted: false }),
      "push.requestPermission": () => (calls.push("ask"), false),
      "push.openSettings": () => void calls.push("settings"),
    });
    await app.waitFor("text:Reminders disabled");
    await app.press("id:reminder-settings");
    await app.waitUntil(() => calls.includes("settings"));
    expect(calls.filter((c) => c === "ask")).toHaveLength(1);
  }, 20_000);

  test("back from Settings with notifications allowed there, the banner goes and the phone registers for pushes", async () => {
    let granted = false;
    const app = await calendarApp({ calendar_items: [] }, {
      "push.permission": () => ({ granted }),
      "push.requestPermission": () => false,
      "push.openSettings": () => void (granted = true),
    });
    await app.waitFor("text:Reminders disabled");
    expect(framesOf(app.host.posts(), "register_push")).toHaveLength(0);
    await app.press("id:reminder-settings");
    await app.setLifecycle("background");
    await app.setLifecycle("active");
    await app.waitUntil(() => !app.shows("id:reminder-banner"), SLOW);
    await app.waitUntil(() => framesOf(app.host.posts(), "register_push").length === 1, SLOW);
  }, 20_000);

  test("a new item starts at the next quarter hour an hour from now, in the phone's zone", async () => {
    // The wall clock in Shanghai (GMT+8, no daylight saving) as the editor's time field shows it.
    const expected = (now = Date.now()) => {
      const at = new Date(Math.ceil((now + 3_600_000) / 900_000) * 900_000 + 8 * 3_600_000);
      const hour = at.getUTCHours();
      return `${hour % 12 || 12}:${String(at.getUTCMinutes()).padStart(2, "0")} ${hour < 12 ? "AM" : "PM"}`;
    };
    const app = await calendarApp({ calendar_items: [] });
    // The editor reads the Calendar's clock, which ticks every 30 s: it may be that much behind.
    const before = expected(Date.now() - 31_000);
    await app.press("id:add-item");
    await app.waitFor("id:calendar-editor");
    // Either side of a quarter hour passing while the editor opens.
    await app.waitUntil(() => app.texts().includes(before) || app.texts().includes(expected()), SLOW);
    expect(app.texts()).toContain("Reminder");
    expect(app.texts()).toContain("None");
  }, 20_000);

  test("the headline counts today, Month shows today's card under the month, and Today goes back to the whole agenda", async () => {
    const app = await calendarApp({ calendar_items: [item("Call the bank", 1), item("Pay rent", 50)] });
    await app.waitFor("text:Call the bank");
    await app.waitFor("id:calendar-headline");
    await app.press("id:calendar-view-month");
    await app.waitFor("id:calendar-month");
    await app.waitFor("id:agenda-month >> id:calendar-now");
    await app.press("id:calendar-view-today");
    await app.waitUntil(() => !app.shows("id:calendar-month"));
    expect(app.texts()).toContain("Pay rent");
  }, 20_000);

  test("a day picked on the month is let go when Today is chosen", async () => {
    const app = await calendarApp({ calendar_items: [item("Call the bank", 1), item("Pay rent", 50)] });
    await app.waitFor("text:Call the bank");
    await app.press("id:calendar-view-month");
    await app.waitFor("id:calendar-month");
    // Two days on: in this month's six weeks whatever the date and the zone.
    const picked = dayIn(2);
    await app.press(`id:day-${picked}`);
    await app.waitFor(`id:calendar-day-${picked}`);
    await app.press("id:calendar-view-today");
    await app.waitUntil(() => !app.shows("id:calendar-month"));
    await app.press("id:calendar-view-month");
    await app.waitFor("id:calendar-month");
    await app.waitUntil(() => !app.shows(`id:calendar-day-${picked}`));
    await app.press("id:calendar-view-today");
    await app.waitUntil(() => !app.shows("id:calendar-month"));
    expect(app.texts()).toContain("Pay rent");
  }, 20_000);

  test("Today and Month follow the finger (owner 11-13 20:05): a swipe shows the month, a swipe back the whole agenda again", async () => {
    const app = await calendarApp({ calendar_items: [item("Call the bank", 1), item("Pay rent", 50)] });
    await app.waitFor("text:Call the bank");
    await app.drag("id:calendar-pages-today", { x: -260 });
    await app.waitFor("id:calendar-month");
    // Two days on: in this month's six weeks whatever the date and the zone.
    await app.press(`id:day-${dayIn(2)}`);
    await app.waitFor(`id:calendar-day-${dayIn(2)}`);
    await app.drag("id:calendar-pages-month", { x: 260 });
    await app.waitUntil(() => !app.shows("id:calendar-month"));
    expect(app.texts()).toContain("Pay rent");
  }, 20_000);

  test("a tap that spans the clock's 30 s tick still opens its item: the tick does not rebuild the row under the finger", async () => {
    const app = await open(APP, { host: phoneHost({ calendar_items: [item("Call the bank", 1)] }), storage: paired, platform: "android", network: expoTokenService, clock: {} });
    await app.host.send("links.open", { url: "mewla://calendar" });
    await app.waitFor("id:calendar-item-Call the bank", SLOW);
    await app.dragAndHold("id:calendar-item-Call the bank", { x: 0, y: 0 });
    await app.clock!.runFor(31_000);
    await app.drop();
    await app.waitFor("id:cancel-item", SLOW);
  }, 20_000);

  test("a tap on a run that spans a change to its item still opens the run: the change does not rebuild the rows under the finger", async () => {
    // 11-15 00:xx: Work.test's run tap opened nothing until a 3 s wait. Every change to the item (its full copy arriving after
    // the snapshot's, a change on the computer) made every run row anew, and a tap on a row made anew is lost.
    const runs = [{ id: "r1", status: "completed", scheduled_for: "2026-07-17T03:00:00Z", started_at: "2026-07-17T03:00:00Z", result_preview: "Three papers read." }];
    const app = await open(APP, { host: phoneHost({ calendar_items: [item("Paper digest", 20, { id: "c1", kind: "scheduled_action", action_instruction: "Read papers.", runs })] }), storage: paired, platform: "android", network: expoTokenService });
    await app.host.send("links.open", { url: "mewla://calendar?id=c1" });
    await app.waitFor("text:Fri, Jul 17, 11:00 AM · Done", SLOW);
    await app.dragAndHold("id:run-r1", { x: 0, y: 0 });
    await app.host.send("app.daemonPush", { type: "calendar_item_changed", calendar_item: item("Paper digest", 20, { id: "c1", kind: "scheduled_action", action_instruction: "Read the papers.", runs }) });
    await app.waitFor("text:Read the papers.", SLOW);
    await app.drop();
    await app.waitFor("id:calendar-run-page", SLOW);
  }, 20_000);

  test("cancels an item after asking, goes back to the agenda, and the item leaves it", async () => {
    const app = await calendarApp({ calendar_items: [item("Call the bank", 1)] });
    await app.waitFor("id:calendar-item-Call the bank", SLOW);
    await app.press("id:calendar-item-Call the bank");
    await app.waitFor("id:cancel-item", SLOW);
    app.onDialog((dialog) => (dialog.title === "Cancel calendar item?" ? "Cancel item" : null));
    await answering(app, () => app.press("id:cancel-item"));
    await app.waitUntil(() => framesOf(app.host.posts(), "cancel_calendar_item").length === 1);
    expect(framesOf(app.host.posts(), "cancel_calendar_item")[0]).toMatchObject({ id: "Call the bank", revision: 1 });
    await app.waitFor("text:Nothing today");
  }, 20_000);

  test("a chosen chip keeps its label's weight, so choosing Weekly never widens it into two lines", async () => {
    const app = await calendarApp({ calendar_items: [] });
    await app.waitFor("text:Nothing today");
    await app.press("id:add-item");
    await app.waitFor("id:editor-repeat-weekly");
    await app.press("id:editor-repeat-weekly");
    await app.wait(100);
    const chosen = app.spanStyle("id:editor-repeat-weekly >> text:Weekly");
    const other = app.spanStyle("id:editor-repeat-daily >> text:Daily");
    expect(chosen.fontWeight).toBe(other.fontWeight);
    expect(chosen.color).not.toBe(other.color);
  });

  test("a new reminder takes its date and time from the system's pickers, in the phone's zone", async () => {
    const app = await calendarApp({ calendar_items: [] }, {
      "calendar.pickDate": () => ({ year: 2031, month: 7, day: 14 }),
      "calendar.pickTime": () => ({ hour: 18, minute: 20 }),
    });
    await app.waitFor("text:Nothing today");
    await app.press("id:add-item");
    await app.waitFor("id:calendar-editor");
    await app.type("id:editor-title", "Pick up the parcel");
    await app.waitFor("id:editor-start-date", SLOW);
    await app.press("id:editor-start-date");
    await app.press("id:editor-start-time");
    await app.waitFor("text:Monday, July 14, 2031, 6:20 PM GMT+8", SLOW);
    await app.press("id:editor-save");
    await app.waitUntil(() => framesOf(app.host.posts(), "create_calendar_item").length === 1);
    expect(framesOf(app.host.posts(), "create_calendar_item")[0].calendar_item).toMatchObject({
      title: "Pick up the parcel", kind: "reminder", timezone: "Asia/Shanghai", recurrence: "none", notify_at: "2031-07-14T18:20:00+08:00",
    });
    await app.waitFor("text:Pick up the parcel");
  }, 20_000);

  test("a new item opened before the phone's zone is known takes that zone once it comes, not UTC", async () => {
    const slowZones = { "calendar.zones": ({ zones }: { zones: string[] }) => new Promise((resolve) => setTimeout(() => resolve(zoneTable(zones)), 1_500)) };
    const app = await open(APP, { host: phoneHost({ calendar_items: [] }, slowZones), storage: paired, platform: "android", network: expoTokenService });
    await app.host.send("links.open", { url: "mewla://calendar" });
    await app.waitFor("id:add-item", SLOW);
    // No check that the zone is still unknown here: it comes on a real 1.5 s timer, which a slow start
    // outruns (CI 11-17), and a pending call would hold the app. What matters is the zone it saves in.
    await app.press("id:add-item");
    await app.waitFor("id:calendar-editor");
    await app.type("id:editor-title", "Water the plants");
    await app.waitUntil(() => app.texts().some((text) => text.includes("PM GMT+8") || text.includes("AM GMT+8")), SLOW);
    await app.press("id:editor-save");
    await app.waitUntil(() => framesOf(app.host.posts(), "create_calendar_item").length === 1, SLOW);
    const created = framesOf(app.host.posts(), "create_calendar_item")[0].calendar_item as { timezone: string; notify_at: string };
    expect(created).toMatchObject({ timezone: "Asia/Shanghai" });
    expect(String(created.notify_at)).toMatch(/\+08:00$/);
  }, 20_000);

  // (`texts()` lists a List's cells in their pool's order, not the screen's, so the order is not checked here.)
  test("the agenda redraws in the phone's zone when it comes late, and its rows still open their own item", async () => {
    const slowZones = { "calendar.zones": ({ zones }: { zones: string[] }) => new Promise((resolve) => setTimeout(() => resolve(zoneTable(zones)), 1_500)) };
    const app = await open(APP, { host: phoneHost({ calendar_items: [item("Call the bank", 1), item("Dentist", 26)] }, slowZones), storage: paired, platform: "android", network: expoTokenService });
    await app.host.send("links.open", { url: "mewla://calendar" });
    await app.waitFor("text:Dentist", SLOW);
    await onPhoneClock(app);
    await app.wait(300);
    await app.press("id:calendar-item-Dentist");
    await app.waitFor("id:item-title", SLOW);
    expect(app.texts()).toContain("Dentist");
  }, 20_000);

  test("an image in a run's result is read from the computer, in Brain's host folder", async () => {
    const run = { id: "r1", status: "completed", scheduled_for: "2026-07-17T03:00:00Z", started_at: "2026-07-17T03:00:00Z", result_preview: "Chart", result: "Here is the chart.\n\n![Weekly chart](out/chart.png)\n" };
    const daemon = {
      calendar_items: [item("Digest", 20, { kind: "scheduled_action", action_instruction: "Draw the chart.", runs: [run] })],
      brain: { host_worker: { id: "%1", name: "brain", status: "running", cwd: "/home/me/brain" } },
      replies: { codex_asset: { type: "codex_asset", content_type: "image/png", data_url: "data:image/png;base64,iVBORw0KGgo=" } },
    };
    const app = await open(APP, { host: phoneHost(daemon), storage: paired, platform: "android", network: expoTokenService });
    await app.host.send("links.open", { url: "mewla://calendar?id=Digest&run=r1" });
    await app.waitFor("id:run-result", SLOW);
    // The result draws again once the run's full result comes, so the image may be asked for twice.
    await app.waitUntil(() => framesOf(app.host.posts(), "codex_asset").length >= 1, SLOW);
    expect(framesOf(app.host.posts(), "codex_asset")[0]).toMatchObject({ path: "out/chart.png", cwd: "/home/me/brain" });
    // (What a TextView draws through renderImage is not in `nsh test`'s tree; the picture itself is for the device.)
  }, 20_000);

  test("a run result's picture opens the picture viewer, which saves its original", async () => {
    const run = { id: "r1", status: "completed", scheduled_for: "2026-07-17T03:00:00Z", started_at: "2026-07-17T03:00:00Z", result_preview: "Chart", result: "Here is the chart.\n\n![Weekly chart](out/chart.png)\n" };
    const daemon = {
      calendar_items: [item("Digest", 20, { kind: "scheduled_action", action_instruction: "Draw the chart.", runs: [run] })],
      brain: { host_worker: { id: "%1", name: "brain", status: "running", cwd: "/home/me/brain" } },
      replies: { codex_asset: { type: "codex_asset", content_type: "image/png", data_url: "data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mP8z8BQDwAEhQGAhKmMIQAAAABJRU5ErkJggg==" } },
    };
    const saved: any[] = [];
    const app = await open(APP, {
      host: phoneHost(daemon, { "files.canSaveImage": () => true, "files.saveImage": (data: any) => (saved.push(data), { name: data.name, where: "Pictures/Mewla" }) }),
      storage: paired, platform: "android", network: expoTokenService,
    });
    await app.host.send("links.open", { url: "mewla://calendar?id=Digest&run=r1" });
    await app.waitFor("id:run-result >> id:chat-image-open", SLOW);
    await app.wait(300);
    await app.press("id:run-result >> id:chat-image-open");
    await app.waitFor("text:1 of 1", SLOW);
    await app.waitFor("id:picture-viewer-image", SLOW);
    await app.press("id:picture-viewer-save");
    await app.waitFor("text:Saved to Pictures/Mewla", SLOW);
    expect(saved).toEqual([{ source: daemon.replies.codex_asset.data_url, name: "chart.png", mimeType: "image/png" }]);
    // The viewer read the original (no max_edge), in Brain's host folder.
    expect(framesOf(app.host.posts(), "codex_asset").some((frame) => frame.path === "out/chart.png" && frame.max_edge === undefined && frame.cwd === "/home/me/brain")).toBe(true);
  }, 20_000);

  test("a run whose agent said it failed reads Failed on its row, on its item's page and on its own page, though its status is completed (Brain 2026-11-22)", async () => {
    const at = new Date(Date.now() - 3_600_000).toISOString();
    const check = item("Credits check", -1, {
      kind: "scheduled_action", due_at: at, notify_at: undefined, recurrence: "none", status: "completed",
      runs: [{ id: "r-out", scheduled_for: at, started_at: at, finished_at: at, status: "completed", outcome: "failed", result_preview: "Exit code 1: the sign-in expired" }],
    });
    const app = await calendarApp({ calendar_items: [check] });
    await app.waitFor("id:calendar-item-Credits check", SLOW);
    expect(app.shows("id:calendar-item-Credits check >> text:Failed · Exit code 1: the sign-in expired")).toBe(true);
    await app.press("id:calendar-item-Credits check");
    await app.waitFor("id:calendar-run-page", SLOW);
    expect(app.texts().some((text) => / · Failed$/.test(text))).toBe(true);
    expect(app.texts().some((text) => / · Done$/.test(text))).toBe(false);
    await app.host.send("links.open", { url: "mewla://calendar?id=Credits check" });
    await app.waitFor("id:calendar-item-page", SLOW);
    await app.waitFor("id:run-r-out", SLOW);
    expect(app.texts().some((text) => / · Failed$/.test(text))).toBe(true);
    expect(app.texts().some((text) => / · Done$/.test(text))).toBe(false);
  }, 20_000);

  test("a result that starts with Markdown reads as plain words on its row, in its item's Runs and on its run page (gate 0.4.21)", async () => {
    const at = new Date(Date.now() - 3_600_000).toISOString();
    const md = "**Disk space check failed.** The check could not run because the disk did not answer.";
    const check = item("Disk space check", -1, {
      kind: "scheduled_action", due_at: at, notify_at: undefined, recurrence: "none", status: "completed",
      runs: [{ id: "r-md", scheduled_for: at, started_at: at, finished_at: at, status: "completed", outcome: "failed", result_preview: md.slice(0, 60) }],
    });
    const app = await calendarApp({ calendar_items: [check] });
    await app.waitFor("id:calendar-item-Disk space check", SLOW);
    expect(app.texts().some((text) => text.startsWith("Failed · Disk space check failed. The check could not run"))).toBe(true);
    expect(app.texts().some((text) => text.includes("**"))).toBe(false);
    await app.host.send("links.open", { url: "mewla://calendar?id=Disk space check" });
    await app.waitFor("id:run-r-md", SLOW);
    expect(app.texts().some((text) => text.includes("**"))).toBe(false);
    expect(app.texts().some((text) => text.startsWith("Disk space check failed. The check could not run"))).toBe(true);
    await app.press("id:run-r-md");
    await app.waitFor("id:calendar-run-page", SLOW);
    expect(app.texts().some((text) => text.includes("**"))).toBe(false);
  }, 20_000);

  test("a running item opens its Session while the computer lists it, and its page once the Session has gone (parity #45)", async () => {
    const run = { id: "r1", status: "running", scheduled_for: new Date().toISOString(), started_at: new Date().toISOString(), worker_session: "w1" };
    const gone = { id: "r2", status: "running", scheduled_for: new Date().toISOString(), started_at: new Date().toISOString(), worker_session: "w-gone" };
    const sessions = [{ id: "w1", name: "digest run", status: "running", cwd: "/home/me", command: "claude", provider: "claude", updated_at: Date.now() }];
    const app = await calendarApp({
      calendar_items: [item("Digest", 0.5, { kind: "scheduled_action", status: "running", runs: [run] }), item("Brief", 1, { kind: "scheduled_action", status: "running", runs: [gone] })],
      worker_sessions: sessions, conversations: { w1: [] },
    });
    await app.waitFor("id:calendar-item-Brief", SLOW);
    await app.press("id:calendar-item-Brief");
    await app.waitFor("id:calendar-item-page", SLOW);
    // Its run's Session is not on the computer: the run opens its result, not a dead Session page.
    await app.press("id:run-r2");
    await app.waitFor("text:Still running.", SLOW);
    expect(app.shows("id:open-session")).toBe(false);
    expect(app.shows("id:session-menu")).toBe(false);
    // Back from the run, then from the item, to the agenda.
    await app.back();
    await app.back();
    await app.waitFor("id:calendar-item-Digest", SLOW);
    await app.press("id:calendar-item-Digest");
    await app.waitFor("id:session-menu", SLOW);
  }, 30_000);

  test("a title is required, and the editor says so instead of saving", async () => {
    const app = await calendarApp({ calendar_items: [] });
    await app.press("id:add-item");
    await app.waitFor("id:editor-save");
    await app.press("id:editor-save");
    await app.waitFor("text:Title is required.");
    expect(framesOf(app.host.posts(), "create_calendar_item")).toHaveLength(0);
  }, 20_000);

  test("arms the phone's alerts from the items, under the keys the computer's pushes use", async () => {
    const app = await calendarApp({ calendar_items: [item("Call the bank", 1), item("Brief", 2, { owner: "watch_brief" })] });
    await app.waitUntil(() => app.host.calls().some((c) => c.name === "calendar.alertsSchedule" && (c.data as { alerts: unknown[] }).alerts.length === 1));
    const call = app.host.calls().filter((c) => c.name === "calendar.alertsSchedule").pop()!;
    expect((call.data as { alerts: Record<string, unknown>[] }).alerts[0]).toMatchObject({ itemId: "Call the bank", serverId: "s1", title: "Call the bank", body: "Reminder" });
  }, 20_000);

  // FINAL-VERIFY 6387f88f m3 76: the system's action sheet dropped the kind, time and notes, and Back waved it off.
  test("an alert that comes due while the app is open shows its kind, time and notes until answered; Back leaves it, Snooze tells the computer", async () => {
    // Due in three seconds: the app's own clock opens it.
    const app = await open(APP, { host: phoneHost({ calendar_items: [item("Standup", 3 / 3600, { notes: "Check the oven" })] }), storage: paired, platform: "android", network: expoTokenService });
    await app.waitFor("id:calendar-alert-card", SLOW);
    await app.waitUntil(() => app.texts().includes("Check the oven") && app.texts().includes("Reminder"), SLOW);
    expect(app.texts()).toContain("Standup");
    expect(app.texts().join(" ")).toMatch(/\d+:\d\d (AM|PM)/);
    await app.back();
    await app.wait(500);
    expect(app.shows("id:calendar-alert-card")).toBe(true);
    await app.press("id:alert-snooze5");
    await app.waitUntil(() => framesOf(app.host.posts(), "snooze_calendar_item").length === 1, SLOW);
    expect(framesOf(app.host.posts(), "snooze_calendar_item")[0]).toMatchObject({ id: "Standup", snooze_minutes: 5 });
    await app.waitUntil(() => !app.shows("id:calendar-alert-card"), SLOW);
  }, 20_000);

  // FINAL-VERIFY 6387f88f m3 162–165 / 79: Done on the full-screen alert, then the opening app asked again.
  test("an alert answered on the phone's own alert before the app opened is not asked again; the answer reaches the computer", async () => {
    const fired = new Date(Date.now() - 60_000).toISOString();
    const done = { type: "done", itemId: "Standup", serverId: "s1", minutes: 0, atMs: Date.now() - 30_000 };
    let taken = false;
    const app = await open(APP, {
      host: phoneHost({ calendar_items: [item("Standup", -1 / 60, { alerted_at: fired })] }, {
        // The phone hands its queued answers over a moment after the computer's list arrives.
        "calendar.alertsTakeActions": () => new Promise((resolve) => setTimeout(() => resolve(taken ? [] : ((taken = true), [done])), 800)),
      }),
      storage: paired, platform: "android", network: expoTokenService,
    });
    await app.waitFor("id:composer");
    await app.waitUntil(() => framesOf(app.host.posts(), "done_calendar_item").length === 1, SLOW);
    await app.wait(1_000);
    expect(app.dialogs().map((dialog) => dialog.title)).not.toContain("Standup");
    expect(app.shows("id:calendar-alert-card")).toBe(false);
  }, 20_000);

  test("says the calendar is offline when the computer is not reachable", async () => {
    const app = await open("../App.tsx", { host: phoneHost({}), storage: paired, platform: "android" });
    await app.host.send("links.open", { url: "mewla://calendar" });
    await app.waitFor("text:Calendar offline", { timeout: 15_000 });
  }, 20_000);
});

describe("notifications", () => {
  test("register this phone with the computer on connecting: its Expo token, zone, platform and live state", async () => {
    const app = await calendarApp({ calendar_items: [] });
    await app.waitUntil(() => framesOf(app.host.posts(), "register_push").length > 0, { timeout: 10_000 });
    expect(framesOf(app.host.posts(), "register_push")[0]).toMatchObject({
      push_token: "ExponentPushToken[test]", server_ref: "s1", time_zone: "Asia/Shanghai", platform: "android", strong_alerts: true, live_state: true,
    });
    expect(framesOf(app.host.posts(), "app_state")[0]).toMatchObject({ app_state: "active" });
    // Other requests go out on connecting too (pending device approvals): find the token one.
    const tokenRequest = app.requests().find((r) => r.url === "https://exp.host/--/api/v2/push/getExpoPushToken");
    expect(JSON.parse(tokenRequest?.body ?? "{}")).toMatchObject({ type: "fcm", deviceId: "install-1", appId: "com.daoleno.mewla", deviceToken: "fcm-token" });
  }, 20_000);

  test("a tapped calendar push opens its item, and reports the tap to the computer", async () => {
    const app = await open(APP, { host: phoneHost({ calendar_items: [item("Call the bank", 1)] }), storage: paired, platform: "android", network: expoTokenService });
    await app.waitFor("id:composer");
    await app.host.send("push.opened", { data: { screen: "calendar", calendar_id: "Call the bank", server_id: "s1", notification_id: "n-1" } });
    await app.waitFor("id:calendar-item-page");
    await app.waitFor("id:item-title");
    expect(app.texts()).toContain("Call the bank");
    await app.waitUntil(() => framesOf(app.host.posts(), "notification_response").length === 1);
    expect(framesOf(app.host.posts(), "notification_response")[0]).toMatchObject({ notification_id: "n-1", action: "tapped" });
  }, 20_000);

  test("a tap that starts the app opens its item once the computers are loaded", async () => {
    const app = await open(APP, { host: phoneHost({ calendar_items: [item("Call the bank", 1)] }), storage: paired, platform: "android", network: expoTokenService });
    await app.host.send("push.opened", { data: { screen: "calendar", calendar_id: "Call the bank", server_id: "s1" } });
    await app.waitFor("id:calendar-item-page");
  }, 20_000);

  // Switching computers under the editor's open sheet crashed `nsh test` up to NSH 0.5.0 (SIGSEGV, longbridge/nsh#1643).
  test("a tap for another paired computer switches to it and still lands on its page; the last one's editor closes", async () => {
    const brain = { current_work: [{ work_id: "w-q", revision: 1, title: "pick-a-name", status: "needs_input", progress_mode: "waiting", unread_result: false, question: "Which name?", attention_since: new Date().toISOString() }] };
    const app = await open(APP, { host: phoneHost({ calendar_items: [item("Call the bank", 1)], brain }), storage: pairedTwo, platform: "android", network: expoTokenService });
    await app.host.send("links.open", { url: "mewla://calendar" });
    await app.waitFor("id:add-item");
    await app.press("id:add-item");
    await app.waitFor("id:calendar-editor");
    await app.host.send("push.opened", { data: { screen: "calendar", calendar_id: "Call the bank", server_id: "s2" } });
    await app.waitFor("id:calendar-item-page");
    await app.wait(500);
    expect(app.shows("id:calendar-item-page")).toBe(true);
    expect(app.shows("id:editor-save")).toBe(false);
    // And back to the first computer, onto a Work of Brain's.
    await app.host.send("links.open", { url: "mewla://brain/work/w-q?serverId=s1" });
    await app.waitFor("id:work-detail");
    await app.wait(500);
    expect(app.shows("id:work-detail")).toBe(true);
  }, 20_000);

  test("another computer: the last one's pages close (the stack returns home)", async () => {
    const brain = { current_work: [{ work_id: "w-q", revision: 1, title: "pick-a-name", status: "needs_input", progress_mode: "waiting", unread_result: false, question: "Which name?", attention_since: new Date().toISOString() }] };
    const app = await open(APP, { host: phoneHost({ calendar_items: [item("Call the bank", 1)], brain }), storage: pairedTwo, platform: "android", network: expoTokenService });
    await app.host.send("links.open", { url: "mewla://brain/work/w-q?serverId=s1" });
    await app.waitFor("id:work-detail", SLOW);
    // A notification's link replaces what is above home: the item, over the Calendar.
    await app.host.send("links.open", { url: "mewla://calendar?id=Call%20the%20bank&serverId=s1" });
    await app.waitFor("id:calendar-item-page", SLOW);
    await app.host.send("links.open", { url: "mewla://sessions?serverId=s2" });
    await app.waitUntil(() => !app.shows("id:calendar-item-page") && !app.shows("id:work-detail"), SLOW);
  }, 20_000);

  test("an alert's link opens the item too, and a link for a computer this phone does not know opens nothing", async () => {
    const app = await open(APP, { host: phoneHost({ calendar_items: [item("Call the bank", 1)] }), storage: paired, platform: "android", network: expoTokenService });
    await app.waitFor("id:composer");
    await app.host.send("links.open", { url: "mewla://calendar?id=Call%20the%20bank&serverId=other" });
    await app.wait(300);
    expect(app.shows("id:calendar-item-page")).toBe(false);
    await app.host.send("links.open", { url: "mewla://calendar?id=Call%20the%20bank&serverId=s1" });
    await app.waitFor("id:calendar-item-page");
  }, 20_000);

  test("a live notification's answer chip sends the answer once, with its stable id", async () => {
    const app = await open(APP, { host: phoneHost({}), storage: paired, platform: "android", network: expoTokenService });
    await app.waitFor("id:composer");
    await app.host.send("links.open", { url: "mewla://live/answer?q=q-7&c=1" });
    await app.waitUntil(() => framesOf(app.host.posts(), "live_answer").length === 1);
    expect(framesOf(app.host.posts(), "live_answer")[0]).toMatchObject({ request_id: "live:q-7:1", question_id: "q-7", choice_index: 1 });
  }, 20_000);
});
