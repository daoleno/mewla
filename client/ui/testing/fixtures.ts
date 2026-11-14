// Shared by the M3 page tests: a paired phone, a host that answers like the Android one, Expo's token service.
import type { open } from "nsh/test";

export const paired = {
  "mewla:v3:servers": JSON.stringify([{ id: "s1", name: "Desk", url: "ws://127.0.0.1:1/ws", daemonId: "a".repeat(64), daemonPublicKey: "b".repeat(64) }]),
  "mewla:v1:current_server_id": "s1",
  "mewla:onboarded": "true",
};

/** Two computers paired, Desk current: a link for Lab switches first. Both answer from the same scripted daemon. */
export const pairedTwo = {
  ...paired,
  "mewla:v3:servers": JSON.stringify([
    { id: "s1", name: "Desk", url: "ws://127.0.0.1:1/ws", daemonId: "a".repeat(64), daemonPublicKey: "b".repeat(64) },
    { id: "s2", name: "Lab", url: "ws://127.0.0.1:2/ws", daemonId: "c".repeat(64), daemonPublicKey: "d".repeat(64) },
  ]),
};

/** Asia/Shanghai has had no DST since 1991: one span is the whole truth. */
export const zoneTable = (zones: string[]) => ({
  device: "Asia/Shanghai",
  hour12: true,
  zones: Object.fromEntries(zones.map((id) => [id, id === "Asia/Shanghai" ? [{ atMs: 0, offsetMinutes: 480, name: "GMT+8" }] : id === "UTC" ? [{ atMs: 0, offsetMinutes: 0, name: "UTC" }] : null])),
});

export function phoneHost(daemon: Record<string, unknown>, extra: Record<string, (data: any) => unknown> = {}) {
  const secure = new Map<string, string>();
  return {
    params: { daemon },
    handlers: {
      "secure.get": ({ key }: { key: string }) => secure.get(key) ?? null,
      "secure.set": ({ key, value }: { key: string; value: string }) => void secure.set(key, value),
      "secure.remove": ({ key }: { key: string }) => void secure.delete(key),
      "app.legacyStorage": () => null,
      "app.legacyStorageCopied": () => undefined,
      "calendar.zones": ({ zones }: { zones: string[] }) => zoneTable(zones),
      "calendar.alertsSchedule": ({ alerts }: { alerts: unknown[] }) => alerts.length,
      "calendar.alertsCancelItem": () => null,
      "calendar.alertsSetStrong": () => null,
      "calendar.alertsTakeActions": () => [],
      "push.permission": () => ({ granted: true }),
      "push.requestPermission": () => true,
      "push.deviceToken": () => ({ type: "fcm", token: "fcm-token", deviceId: "INSTALL-1", appId: "com.daoleno.mewla", development: false }),
      "live.supported": () => true,
      "live.setCurrentServer": () => null,
      "live.apply": () => true,
      "live.widgetState": () => "ok",
      "app.testFrame": () => undefined,
      ...extra,
    },
  };
}

export const expoTokenService = {
  "POST https://exp.host/--/api/v2/push/getExpoPushToken": { status: 200, body: { data: { expoPushToken: "ExponentPushToken[test]" } } },
};

const HOUR = 3_600_000;
/** A calendar item `hours` from now, in Shanghai. */
export function item(id: string, hours: number, patch: Record<string, unknown> = {}) {
  const at = new Date(Date.now() + hours * HOUR).toISOString();
  return { id, title: id, kind: "reminder", status: "scheduled", notify_at: at, next_at: at, timezone: "Asia/Shanghai", recurrence: "none", created_at: at, updated_at: at, revision: 1, ...patch };
}

/** The frames the fake computer received of a type, from the host posts. */
export function framesOf(posts: { name: string; data: unknown }[], type: string): Record<string, unknown>[] {
  return posts.filter((p) => p.name === "app.testFrame" && (p.data as { type?: string })?.type === type).map((p) => p.data as Record<string, unknown>);
}

/**
 * Waits for `condition` a step at a time. A dialog the app shows once a step has resolved (a slow runner's
 * sheet comes after `press`) is answered only while a step runs, which `waitUntil` on the test's own state is not.
 */
export async function stepUntil(app: Awaited<ReturnType<typeof open>>, condition: () => boolean, timeout = 10_000): Promise<void> {
  for (const end = Date.now() + timeout; !condition(); ) {
    if (Date.now() > end) throw new Error(`the condition never held in ${timeout} ms`);
    await app.wait(50);
  }
}

/**
 * Lets the app settle after it opens: headless, the first press or key in the first moments after start
 * (Brain's snapshot still arriving) is lost. The emulator takes a tap 300 ms after the tray opens (11-12),
 * so this is the harness, not the app.
 */
export async function settle(app: Awaited<ReturnType<typeof open>>): Promise<void> {
  await app.wait(500);
}

/**
 * Presses `selector` until `opened` shows. Headless, the first press after the tray opens, while Brain's
 * snapshot is still arriving, is lost (on the emulator a tap 300 ms after the tray opens lands: the harness, not the app).
 */
export async function pressUntil(app: Awaited<ReturnType<typeof open>>, selector: string, opened: string, timeout = 10_000): Promise<void> {
  for (const end = Date.now() + timeout; ; ) {
    if (app.shows(selector)) await app.press(selector);
    try {
      await app.waitFor(opened, { timeout: 400 });
      return;
    } catch (error) {
      if (Date.now() > end) throw error;
    }
  }
}

/**
 * Presses `selector` once it takes the press. A control that waits for a save in flight reads enabled
 * between two frames of a loaded runner and disabled again when the press lands; the press is retried
 * while the harness says "is disabled", never once it went through.
 */
export async function pressWhenEnabled(app: Awaited<ReturnType<typeof open>>, selector: string, timeout = 10_000): Promise<void> {
  for (const end = Date.now() + timeout; ; ) {
    try {
      await app.waitUntil(() => app.enabled(selector), { timeout: Math.max(0, end - Date.now()) });
      await app.press(selector);
      return;
    } catch (error) {
      if (Date.now() > end || !String(error).includes("is disabled")) throw error;
      await app.wait(50);
    }
  }
}

/**
 * Runs `step` (a press, a long press, a sent event), which opens `count` dialogs or action sheets one after
 * another that the test's `onDialog` answers, and resolves once they have been answered: shown (`dialogs()`),
 * then one more step, as the runner answers a dialog only while a step runs. After it, a plain `waitFor` or
 * `waitUntil` on what the answer does is safe.
 */
export async function answering(app: Awaited<ReturnType<typeof open>>, step: () => Promise<unknown>, count = 1, timeout = 10_000): Promise<void> {
  const before = app.dialogs().length;
  await step();
  await stepUntil(app, () => app.dialogs().length >= before + count, timeout);
  await app.wait(50);
}

/**
 * Types `text` into `selector` until the field holds it and `ready` is enabled (its button, say). On a loaded runner the first
 * keys after a page opens can be lost, as the first press can (`pressUntil`).
 */
export async function typeUntil(app: Awaited<ReturnType<typeof open>>, selector: string, text: string, ready: string, timeout = 10_000): Promise<void> {
  for (const end = Date.now() + timeout; ; ) {
    if (app.value(selector) !== text) {
      await app.clear(selector);
      await app.type(selector, text);
    }
    try {
      await app.waitUntil(() => app.value(selector) === text && app.enabled(ready), { timeout: 400 });
      return;
    } catch (error) {
      if (Date.now() > end) throw error;
    }
  }
}
