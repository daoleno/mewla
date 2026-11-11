import { describe, expect, test } from "bun:test";
import { memoryStore } from "../testing/memoryPlatform";
import { isCalendarAlert, parseLink, pushLink } from "./links";
import { liveAnswerOf, liveAnswerRequestId, widgetBaseUrl } from "./live";
import { askForReminders, ensurePermission, expoPushToken, PERMISSION_ASKED_KEY, registerPushMessage, reminderPermission, type PushHost } from "./registration";
import { NotificationReporter } from "./report";

describe("a tapped push opens", () => {
  test("the Session a Worker push is about, on the computer that sent it", () => {
    expect(pushLink({ screen: "terminal", worker_id: "agent-1", server_id: "s1" })).toBe("mewla://sessions/agent-1?serverId=s1");
  });
  test("Brain on the thread (and the message) the push names", () => {
    expect(pushLink({ screen: "brain", server_id: "s1", brain_thread_id: "t1", brain_message_id: "calendar_result:item:run" }))
      .toBe("mewla://brain?thread=t1&message=calendar_result%3Aitem%3Arun&serverId=s1");
  });
  test("the calendar item, and the Sessions list for the inbox", () => {
    expect(pushLink({ screen: "calendar", server_id: "s1", calendar_id: "item-1" })).toBe("mewla://calendar?id=item-1&serverId=s1");
    expect(pushLink({ screen: "inbox" })).toBe("mewla://sessions");
  });
  test("nothing for a push it does not know", () => {
    expect(pushLink({ screen: "unknown" })).toBeNull();
    expect(pushLink(null)).toBeNull();
  });
  test("a calendar alert is left to the in-app card while the app is open", () => {
    expect(isCalendarAlert({ screen: "calendar", alert_key: "item@1" })).toBe(true);
    expect(isCalendarAlert({ screen: "calendar" })).toBe(false);
  });
  test("links parse into section, path and decoded params", () => {
    expect(parseLink("mewla://brain/work/w%201?serverId=s1")).toEqual({ section: "brain", path: ["work", "w 1"], params: { serverId: "s1" } });
    expect(parseLink("https://example.com")).toBeNull();
  });
});

describe("live links", () => {
  test("an answer needs a question and a choice 0-3, and its request id is stable", () => {
    expect(liveAnswerOf(parseLink("mewla://live/answer?q=abc&c=2")!)).toEqual({ questionId: "abc", choiceIndex: 2 });
    expect(liveAnswerOf(parseLink("mewla://live/answer?q=abc&c=9")!)).toBeNull();
    expect(liveAnswerOf(parseLink("mewla://live/open?k=running")!)).toBeNull();
    expect(liveAnswerRequestId({ questionId: "q", choiceIndex: 1 })).toBe("live:q:1");
  });
  test("plain computers give the widget an http(s) base, relays give none", () => {
    expect(widgetBaseUrl({ url: "ws://10.0.2.2:5560/ws", transportKind: "manual" })).toBe("http://10.0.2.2:5560");
    expect(widgetBaseUrl({ url: "wss://mewla.example.com/ws" })).toBe("https://mewla.example.com");
    expect(widgetBaseUrl({ url: "wss://relay.example.com/ws", transportKind: "link" })).toBeNull();
  });
});

function fakeHost(granted: boolean, answer = true) {
  const calls: string[] = [];
  let token = "fcm-1";
  const host: PushHost & { calls: string[]; rotate(next: string): void } = {
    calls,
    rotate: (next) => void (token = next),
    permission: async () => (calls.push("permission"), { granted }),
    requestPermission: async () => (calls.push("request"), answer),
    deviceToken: async () => ({ type: "fcm", token, deviceId: "ABC-INSTALL", appId: "com.daoleno.mewla", development: false }),
  };
  return host;
}

describe("notification permission", () => {
  test("is asked once in the app's life, and never when already allowed", async () => {
    const storage = memoryStore();
    const denied = fakeHost(false, false);
    expect(await ensurePermission(denied, storage)).toBe(false);
    expect(await ensurePermission(denied, storage)).toBe(false);
    expect(denied.calls.filter((c) => c === "request")).toHaveLength(1);
    const allowed = fakeHost(true);
    expect(await ensurePermission(allowed, memoryStore())).toBe(true);
    expect(allowed.calls).toEqual(["permission"]);
  });
  test("a phone the Expo app already asked is not asked again", async () => {
    const host = fakeHost(false);
    expect(await ensurePermission(host, memoryStore({ [PERMISSION_ASKED_KEY]: "1" }))).toBe(false);
    expect(host.calls).not.toContain("request");
  });
  test("the Calendar's reminder banner: not enabled until asked, disabled after a no, unavailable when the phone cannot say", async () => {
    expect(await reminderPermission(fakeHost(true), memoryStore())).toBe("granted");
    expect(await reminderPermission(fakeHost(false), memoryStore())).toBe("undetermined");
    expect(await reminderPermission(fakeHost(false), memoryStore({ [PERMISSION_ASKED_KEY]: "1" }))).toBe("denied");
    expect(await reminderPermission({ permission: () => Promise.reject(new Error("no")) }, memoryStore())).toBe("unavailable");
  });
  test("Enable asks once and counts as the app's one ask", async () => {
    const storage = memoryStore();
    expect(await askForReminders(fakeHost(false, false), storage)).toBe("denied");
    expect(await reminderPermission(fakeHost(false), storage)).toBe("denied");
    expect(await askForReminders(fakeHost(false, true), memoryStore())).toBe("granted");
  });
});

describe("the Expo push token", () => {
  test("is the device token exchanged at Expo's service with the Expo installation id, once per device token", async () => {
    const host = fakeHost(true);
    const storage = memoryStore();
    const bodies: Record<string, unknown>[] = [];
    const fetch = async (url: string, init?: RequestInit) => {
      expect(url).toBe("https://exp.host/--/api/v2/push/getExpoPushToken");
      const body = JSON.parse(String(init?.body));
      bodies.push(body);
      return new Response(JSON.stringify({ data: { expoPushToken: `ExponentPushToken[${body.deviceToken}]` } }));
    };
    expect(await expoPushToken(host, storage, fetch)).toBe("ExponentPushToken[fcm-1]");
    expect(await expoPushToken(host, storage, fetch)).toBe("ExponentPushToken[fcm-1]");
    expect(bodies).toEqual([{ type: "fcm", deviceId: "abc-install", development: false, appId: "com.daoleno.mewla", deviceToken: "fcm-1", projectId: "1b76d386-969b-4841-ae68-f32895ad2301" }]);
    host.rotate("fcm-2");
    expect(await expoPushToken(host, storage, fetch)).toBe("ExponentPushToken[fcm-2]");
    expect(bodies).toHaveLength(2);
  });
  test("a failed exchange is an error, not a token", async () => {
    const fetch = async () => new Response("nope", { status: 500 });
    await expect(expoPushToken(fakeHost(true), memoryStore(), fetch)).rejects.toThrow("Expo token exchange failed (500)");
  });
});

describe("register_push", () => {
  test("tells the computer the token, the phone's zone and language, its alert style and that it shows live state", () => {
    expect(registerPushMessage("s1", "T", { timeZone: "Asia/Shanghai", locale: "en-US", platform: "android", strongAlerts: true, liveState: true })).toEqual({
      type: "register_push", push_token: "T", server_ref: "s1", time_zone: "Asia/Shanghai", locale: "en-US", platform: "android", strong_alerts: true, live_state: true,
    });
    expect("live_state" in registerPushMessage("s1", "T", { timeZone: "UTC", locale: "en", platform: "ios", strongAlerts: false, liveState: false })).toBe(false);
  });
});

describe("tap reports", () => {
  test("wait for their computer's connection, keep their time, and retry what a dropped connection refused", () => {
    let now = 1_000;
    const reporter = new NotificationReporter(() => now);
    expect(reporter.tapped({ notification_id: "n1", server_id: "s1" })).toBe("s1");
    expect(reporter.tapped({ notification_id: "n2", server_id: "s1" })).toBe("s1");
    expect(reporter.tapped({ server_id: "s1" })).toBeNull();
    now = 5_000;
    const sent: unknown[] = [];
    let refuse = true;
    reporter.flush("s1", (report) => {
      if (report.notification_id === "n2" && refuse) throw new Error("not connected");
      sent.push(report);
    });
    expect(sent).toEqual([{ type: "notification_response", notification_id: "n1", action: "tapped", occurred_at_ms: 1_000 }]);
    refuse = false;
    reporter.flush("s1", (report) => void sent.push(report));
    expect(sent).toHaveLength(2);
    reporter.flush("s1", () => { throw new Error("nothing left"); });
  });
  test("a report older than a day is dropped", () => {
    let now = 0;
    const reporter = new NotificationReporter(() => now);
    reporter.tapped({ notification_id: "n1", server_id: "s1" });
    now = 25 * 60 * 60 * 1000;
    const sent: unknown[] = [];
    reporter.flush("s1", (r) => void sent.push(r));
    expect(sent).toEqual([]);
  });
});
