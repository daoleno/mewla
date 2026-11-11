import { describe, expect, open, test } from "nsh/test";
import { expoTokenService, framesOf, item, paired, phoneHost } from "../testing/fixtures";

const APP = "../FakeDaemonApp.tsx";

async function calendarApp(daemon: Record<string, unknown>, extra: Record<string, (data: any) => unknown> = {}) {
  const app = await open(APP, { host: phoneHost(daemon, extra), storage: paired, platform: "android", network: expoTokenService });
  await app.host.send("links.open", { url: "mewla://calendar" });
  await app.waitFor("id:calendar-page");
  return app;
}

describe("the Calendar tab", () => {
  test("lists what is planned by day, hides cancelled items and opens one", async () => {
    const app = await calendarApp({ calendar_items: [item("Call the bank", 1), item("Dentist", 26, { kind: "event", start_at: new Date(Date.now() + 26 * 3_600_000).toISOString(), end_at: new Date(Date.now() + 27 * 3_600_000).toISOString() }), item("Old plan", 2, { status: "cancelled" })] });
    await app.waitFor("text:Call the bank");
    expect(app.texts()).toContain("Dentist");
    expect(app.texts()).not.toContain("Old plan");
    expect(app.texts().join(" ")).toMatch(/Scheduled · Reminder/);
    await app.press("text:Call the bank");
    await app.waitFor("id:calendar-item-page");
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

  test("the heading says Today, the month picks a day and Hide month goes back to the whole agenda", async () => {
    const app = await calendarApp({ calendar_items: [item("Call the bank", 1), item("Pay rent", 50)] });
    await app.waitFor("text:Call the bank");
    expect(app.texts()).toContain("Today");
    await app.press("id:toggle-month");
    await app.waitFor("id:calendar-month");
    expect(app.texts()).toContain("Hide month");
    await app.press("id:back-to-today").catch(() => undefined);
    await app.press("id:toggle-month");
    await app.waitUntil(() => app.texts().includes("Show month"));
    expect(app.texts()).toContain("Pay rent");
  }, 20_000);

  test("cancels an item after asking, and it leaves the agenda", async () => {
    const app = await calendarApp({ calendar_items: [item("Call the bank", 1)] });
    await app.press("id:calendar-item-Call the bank");
    await app.waitFor("id:cancel-item");
    app.onDialog((dialog) => (dialog.title === "Cancel calendar item?" ? "Cancel item" : null));
    await app.press("id:cancel-item");
    await app.waitUntil(() => framesOf(app.host.posts(), "cancel_calendar_item").length === 1);
    expect(framesOf(app.host.posts(), "cancel_calendar_item")[0]).toMatchObject({ id: "Call the bank", revision: 1 });
    await app.back();
    await app.waitFor("id:calendar-empty");
  }, 20_000);

  test("a new reminder takes its date and time from the system's pickers, in the phone's zone", async () => {
    const app = await calendarApp({ calendar_items: [] }, {
      "calendar.pickDate": () => ({ year: 2031, month: 7, day: 14 }),
      "calendar.pickTime": () => ({ hour: 18, minute: 20 }),
    });
    await app.waitFor("id:calendar-empty");
    await app.press("id:add-item");
    await app.waitFor("id:calendar-editor");
    await app.type("id:editor-title", "Pick up the parcel");
    await app.press("id:editor-start-date");
    await app.press("id:editor-start-time");
    await app.waitFor("text:Monday, July 14, 2031, 6:20 PM GMT+8");
    await app.press("id:editor-save");
    await app.waitUntil(() => framesOf(app.host.posts(), "create_calendar_item").length === 1);
    expect(framesOf(app.host.posts(), "create_calendar_item")[0].calendar_item).toMatchObject({
      title: "Pick up the parcel", kind: "reminder", timezone: "Asia/Shanghai", recurrence: "none", notify_at: "2031-07-14T18:20:00+08:00",
    });
    await app.waitFor("text:Pick up the parcel");
  }, 20_000);

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

  test("an alert that comes due while the app is open asks Done, Snooze or Open, and Snooze tells the computer", async () => {
    // Due in three seconds: the app's own clock opens it.
    const app = await open(APP, { host: phoneHost({ calendar_items: [item("Standup", 3 / 3600)] }), storage: paired, platform: "android", network: expoTokenService });
    const asked: string[] = [];
    app.onDialog((dialog) => {
      asked.push(dialog.title ?? "");
      return dialog.title === "Standup" ? "Snooze 5 min" : null;
    });
    await app.wait(4_000);
    await app.waitUntil(() => framesOf(app.host.posts(), "snooze_calendar_item").length === 1, { timeout: 10_000 });
    expect(asked).toContain("Standup");
    expect(framesOf(app.host.posts(), "snooze_calendar_item")[0]).toMatchObject({ id: "Standup", snooze_minutes: 5 });
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
    expect(app.requests()[0].url).toBe("https://exp.host/--/api/v2/push/getExpoPushToken");
    expect(JSON.parse(app.requests()[0].body ?? "{}")).toMatchObject({ type: "fcm", deviceId: "install-1", appId: "com.daoleno.mewla", deviceToken: "fcm-token" });
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
    // The phone is slow to hand over the old app's storage: the tap comes first.
    const slowStart = { "app.legacyStorage": () => new Promise((resolve) => setTimeout(() => resolve(null), 500)) };
    const app = await open(APP, { host: phoneHost({ calendar_items: [item("Call the bank", 1)] }, slowStart), storage: paired, platform: "android", network: expoTokenService });
    await app.host.send("push.opened", { data: { screen: "calendar", calendar_id: "Call the bank", server_id: "s1" } });
    await app.waitFor("id:calendar-item-page");
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
