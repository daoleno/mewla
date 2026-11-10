// Shared by the M3 page tests: a paired phone, a host that answers like the Android one, Expo's token service.
export const paired = {
  "mewla:v3:servers": JSON.stringify([{ id: "s1", name: "Desk", url: "ws://127.0.0.1:1/ws", daemonId: "a".repeat(64), daemonPublicKey: "b".repeat(64) }]),
  "mewla:v1:current_server_id": "s1",
  "mewla:onboarded": "true",
};

/** Asia/Shanghai has had no DST since 1991: one span is the whole truth. */
const zoneTable = (zones: string[]) => ({
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
